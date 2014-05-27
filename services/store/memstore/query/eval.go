package query

import (
	"fmt"
	"math/big"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"

	"veyron/services/store/memstore/state"
	"veyron/services/store/service"

	"veyron2/naming"
	"veyron2/query"
	"veyron2/query/parse"
	"veyron2/security"
	"veyron2/services/store"
	"veyron2/storage"
	"veyron2/vdl"
)

// maxChannelSize is the maximum size of the channels used for concurrent
// query evaluation.
const maxChannelSize = 100

// evalIterator implements service.QueryStream.
type evalIterator struct {
	// mu guards 'result', 'err', and the closing of 'abort'.
	mu sync.Mutex
	// result is what Get will return.  It will be nil if there are no more
	// query results.  Guarded by mu.
	result *store.QueryResult
	// err is the first error encountered during query evaluation.
	// Guarded by mu.
	err error
	// abort is used as the signal to query evaluation to terminate early.
	// evaluator implementations will test for abort closing.  The close()
	// call is guarded by mu.
	abort chan bool

	// results is the output of the top-level evaluator for the query.
	results <-chan *store.QueryResult
	// errc is the path that evaluator implementations use to pass errors
	// to evalIterator.  Any error will abort query evaluation.
	errc chan error
	// cleanup is used for testing to ensure that no goroutines are leaked.
	cleanup sync.WaitGroup
}

// Next implements the QueryStream method.
func (it *evalIterator) Next() bool {
	it.mu.Lock()
	if it.err != nil {
		it.mu.Unlock()
		return false
	}
	it.mu.Unlock()

	select {
	case result, ok := <-it.results:
		if !ok {
			return false
		}
		it.mu.Lock()
		defer it.mu.Unlock()
		// TODO(kash): Need to watch out for fields of type channel and pull them
		// out of line.
		it.result = result
		return true
	case <-it.abort:
		return false
	}
}

// Get implements the QueryStream method.
func (it *evalIterator) Get() *store.QueryResult {
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.result
}

// Abort implements the QueryStream method.
func (it *evalIterator) Abort() {
	it.mu.Lock()
	defer it.mu.Unlock()
	select {
	case <-it.abort:
		// Already closed.
	default:
		close(it.abort)
	}
	it.result = nil
}

// Err implements the QueryStream method.
func (it *evalIterator) Err() error {
	it.mu.Lock()
	defer it.mu.Unlock()
	return it.err
}

// handleErrors watches for errors on it.errc, calling it.Abort when it finds
// one.  It should run in a goroutine.
func (it *evalIterator) handleErrors() {
	select {
	case <-it.abort:
	case err := <-it.errc:
		it.mu.Lock()
		it.err = err
		it.mu.Unlock()
		it.Abort()
	}
}

// wait blocks until all children goroutines are finished.  This is useful in
// tests to ensure that an abort cleans up correctly.
func (it *evalIterator) wait() {
	it.cleanup.Wait()
}

// sendError sends err on errc unless that would block.  In that case, sendError
// does nothing because there was aleady an error reported.
func sendError(errc chan<- error, err error) {
	select {
	case errc <- err:
		// Sent error successfully.
	default:
		// Sending the error would block because there is already an error in the
		// channel.  The first error wins.
	}
}

// Eval evaluates a query and returns a QueryStream for the results. If there is
// an error parsing the query, it will show up as an error in the QueryStream.
// Query evaluation is concurrent, so it is important to call QueryStream.Abort
// if the client does not consume all of the results.
func Eval(sn state.Snapshot, clientID security.PublicID, name storage.PathName, q query.Query) service.QueryStream {
	ast, err := parse.Parse(q)
	if err != nil {
		return &evalIterator{err: err}
	}
	evaluator, err := convert(ast)
	if err != nil {
		return &evalIterator{err: err}
	}

	// Seed the input with the root entity.
	in := make(chan *store.QueryResult, 1)
	in <- &store.QueryResult{Name: ""}
	close(in)

	out := make(chan *store.QueryResult, maxChannelSize)
	it := &evalIterator{
		results: out,
		abort:   make(chan bool),
		errc:    make(chan error),
	}
	go it.handleErrors()
	it.cleanup.Add(1)
	go evaluator.eval(&context{
		sn:           sn,
		suffix:       name.String(),
		clientID:     clientID,
		nestedResult: &monotonicInt{},
		in:           in,
		out:          out,
		abort:        it.abort,
		errc:         it.errc,
		cleanup:      &it.cleanup,
	})
	return it
}

// context is a wrapper of all the variables that need to be passed around
// during evaluation.
type context struct {
	// sn is the snapshot of the store's state to use to find query results.
	sn state.Snapshot
	// suffix is the suffix we're evaluating relative to.
	suffix string
	// clientID is the identity of the client that issued the query.
	clientID security.PublicID
	// nestedResult produces a unique nesting identifier to be used as
	// QueryResult.NestedResult.
	nestedResult *monotonicInt
	// in produces the intermediate results from the previous stage of the
	// query.  It will be closed when the evaluator should stop processing
	// results.  It is not necessary to select on 'in' and 'errc'.
	in <-chan *store.QueryResult
	// out is where the evaluator should write the intermediate results.
	// evaluators should use context.emit instead of writing directly
	// to out.
	out chan<- *store.QueryResult
	// abort will be closed if query evaluation should terminate early.
	// evaluator implementations should regularly test if it is still open.
	abort chan bool
	// errc is where evaluators can propagate errors to the client.
	errc chan<- error
	// cleanup is used for testing to ensure that no goroutines are leaked.
	// evaluator.eval implementations should call Done when finished processing.
	cleanup *sync.WaitGroup
}

// emit sends result on c.out.  It is careful to watch for aborts.  result can be
// nil.  Returns true if the caller should continue iterating, returns
// false if it is time to abort.
func (c *context) emit(result *store.QueryResult) bool {
	if result == nil {
		// Check for an abort before continuing iteration.
		select {
		case <-c.abort:
			return false
		default:
			return true
		}
	} else {
		// If c.out is full, we don't want to block on it forever and ignore
		// aborts.
		select {
		case <-c.abort:
			return false
		case c.out <- result:
			return true
		}
	}
}

// evaluator is a node in the query evaluation flow.  It takes intermediate
// results produced by the previous node and produces a new set of results.
type evaluator interface {
	// eval does the work or processing intermediate results to produce a new
	// set of results.  It is expected that the client run eval in its own
	// goroutine (i.e. "go eval(ctxt)").
	eval(c *context)

	// singleResult returns true if this evaluator returns a single result
	// (e.g. an aggregate or a specific field).  This is useful in selection
	// because we want to unbox those results.  For example,
	// "teams/* | { players/* | count as numplayers }" should return
	//   { Name: "teams/cardinals", Fields: {"numplayers": 5}}
	// and not
	//   { Name: "teams/cardinals", Fields: {"numplayers": [{Name: "numplayers", Value: 5}]}}
	singleResult() bool

	// name returns a relative Veyron name that is appropriate for the query
	// results produced by this evaluator.
	name() string
}

// convert transforms the AST produced by parse.Parse into an AST that supports
// evaluation specific to memstore.  This transformation should not produce
// any errors since we know all of the types that parse.Parse can produce.
// Just in case one was overlooked, we use the panic/recover idiom to handle
// unexpected errors.  The conversion functions in the remainder of this file
// do not return errors.  Instead, they are allowed to panic, and this function
// will recover.
func convert(q parse.Pipeline) (ev evaluator, err error) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(runtime.Error); ok {
				panic(r)
			}
			ev = nil
			err = r.(error)
		}
	}()
	return convertPipeline(q), nil
}

// convertPipeline transforms a parse.Pipeline into an evaluator.
func convertPipeline(q parse.Pipeline) evaluator {
	switch q := q.(type) {
	case *parse.PipelineName:
		return &nameEvaluator{q.WildcardName, q.Pos}
	case *parse.PipelineType:
		return &typeEvaluator{convertPipeline(q.Src), q.Type, q.Pos}
	case *parse.PipelineFilter:
		return &filterEvaluator{convertPipeline(q.Src), convertPredicate(q.Pred), q.Pos}
	case *parse.PipelineSelection:
		return convertSelection(q)
	default:
		panic(fmt.Errorf("unexpected type %T", q))
	}
}

// nameEvaluator is the evaluator version of parse.PipelineName.
type nameEvaluator struct {
	wildcardName *parse.WildcardName

	// pos specifies where in the query string this component started.
	pos parse.Pos
}

// eval implements the evaluator method.
func (e *nameEvaluator) eval(c *context) {
	defer c.cleanup.Done()
	defer close(c.out)

	for result := range c.in {
		nestedResult := c.nestedResult.Next()
		basePath := naming.Join(c.suffix, result.Name)
		path := storage.ParsePath(naming.Join(basePath, e.wildcardName.VName))
		for it := c.sn.NewIterator(c.clientID, path, state.ImmediateFilter); it.IsValid(); it.Next() {
			entry := it.Get()
			result := &store.QueryResult{
				NestedResult: store.NestedResult(nestedResult),
				Name:         naming.Join(e.wildcardName.VName, it.Name()),
				Value:        entry.Value,
			}
			if !c.emit(result) {
				return
			}
			if e.singleResult() {
				return
			}
		}
	}
}

// singleResult implements the evaluator method.
func (e *nameEvaluator) singleResult() bool {
	return e.wildcardName.Exp == parse.Self
}

// name implements the evaluator method.
func (e *nameEvaluator) name() string {
	return e.wildcardName.VName
}

// startSource creates a goroutine for src.eval().  It returns the
// output channel for src.
func startSource(c *context, src evaluator) chan *store.QueryResult {
	srcOut := make(chan *store.QueryResult, maxChannelSize)
	srcContext := context{
		sn:           c.sn,
		suffix:       c.suffix,
		clientID:     c.clientID,
		nestedResult: c.nestedResult,
		in:           c.in,
		out:          srcOut,
		abort:        c.abort,
		errc:         c.errc,
		cleanup:      c.cleanup,
	}
	c.cleanup.Add(1)
	go src.eval(&srcContext)
	return srcOut
}

// typeEvaluator is the evaluator version of parse.PipelineType.
type typeEvaluator struct {
	// src produces the results to be filtered by type.
	src evaluator
	// ty restricts the results to a specific type of object.
	ty string
	// Pos specifies where in the query string this component started.
	pos parse.Pos
}

// eval implements the evaluator method.
func (e *typeEvaluator) eval(c *context) {
	defer c.cleanup.Done()
	defer close(c.out)

	for result := range startSource(c, e.src) {
		if val := reflect.ValueOf(result.Value); e.ty != val.Type().Name() {
			continue
		}
		if !c.emit(result) {
			return
		}
	}
}

// singleResult implements the evaluator method.
func (e *typeEvaluator) singleResult() bool {
	return false
}

// name implements the evaluator method.
func (e *typeEvaluator) name() string {
	return e.src.name()
}

// filterEvaluator is the evaluator version of parse.PipelineFilter.
type filterEvaluator struct {
	// src produces intermediate results that will be filtered by pred.
	src evaluator
	// pred determines whether an intermediate result produced by src should be
	// filtered out.
	pred predicate
	// pos specifies where in the query string this component started.
	pos parse.Pos
}

// eval implements the evaluator method.
func (e *filterEvaluator) eval(c *context) {
	defer c.cleanup.Done()
	defer close(c.out)

	for result := range startSource(c, e.src) {
		if e.pred.match(c, result) {
			if !c.emit(result) {
				return
			}
		}
	}
}

// singleResult implements the evaluator method.
func (e *filterEvaluator) singleResult() bool {
	return false
}

// name implements the evaluator method.
func (e *filterEvaluator) name() string {
	return e.src.name()
}

// convertSelection transforms a parse.PipelineSelection into a
// selectionEvaluator.
func convertSelection(p *parse.PipelineSelection) evaluator {
	e := &selectionEvaluator{
		src:          convertPipeline(p.Src),
		subpipelines: make([]alias, len(p.SubPipelines), len(p.SubPipelines)),
		pos:          p.Pos,
	}
	for i, a := range p.SubPipelines {
		// TODO(kash): Protect against aliases that have slashes in them?
		e.subpipelines[i] = alias{convertPipeline(a.Pipeline), a.Alias, a.Hidden}
	}
	return e
}

// alias is the evaluator version of parse.Alias.  It represents a pipeline
// that has an alternate name inside of a selection using the 'as' keyword.
type alias struct {
	// evaluator is the evaluator to be aliased.
	evaluator evaluator
	// alias is the new name for the output of evaluator.
	alias string
	// hidden is true if this field in the selection should not be included
	// in the results sent to the client.
	// TODO(kash): hidden is currently ignored during evaluation.
	hidden bool
}

// selectionEvaluator is the evaluator version of parse.PipelineSelection.
type selectionEvaluator struct {
	// src produces intermediate results on which to select.
	src evaluator
	// subpipelines is the list of pipelines to run for each result produced
	// by src.
	subpipelines []alias
	// pos specifies where in the query string this component started.
	pos parse.Pos
}

// eval implements the evaluator method.
func (e *selectionEvaluator) eval(c *context) {
	defer c.cleanup.Done()
	defer close(c.out)

	for result := range startSource(c, e.src) {
		if !e.processSubpipelines(c, result) {
			return
		}
	}
}

func (e *selectionEvaluator) processSubpipelines(c *context, result *store.QueryResult) bool {
	sel := &store.QueryResult{
		Name:   result.Name,
		Fields: make(map[string]vdl.Any),
	}
	for _, a := range e.subpipelines {
		// We create a new channel for each intermediate result, so there's no need to
		// create a large buffer.
		in := make(chan *store.QueryResult, 1)
		in <- result
		close(in)
		out := make(chan *store.QueryResult, maxChannelSize)
		ctxt := &context{
			sn:           c.sn,
			suffix:       c.suffix,
			clientID:     c.clientID,
			nestedResult: c.nestedResult,
			in:           in,
			out:          out,
			abort:        c.abort,
			errc:         c.errc,
			cleanup:      c.cleanup,
		}
		c.cleanup.Add(1)
		go a.evaluator.eval(ctxt)

		// If the subpipeline would produce a single result, use that single result
		// as the field value.  Otherwise, put the channel as the field value and let
		// evalIterator do the right thing with the sub-results.
		var value interface{}
		if a.evaluator.singleResult() {
			select {
			case <-c.abort:
				return false
			case sub, ok := <-out:
				if !ok {
					return false
				}
				value = sub.Value
			}
		} else {
			value = out
		}

		if a.alias != "" {
			sel.Fields[a.alias] = value
		} else {
			sel.Fields[a.evaluator.name()] = value
		}
	}
	return c.emit(sel)
}

// singleResult implements the evaluator method.
func (e *selectionEvaluator) singleResult() bool {
	return false
}

// name implements the evaluator method.
func (e *selectionEvaluator) name() string {
	return e.src.name()
}

// predicate determines whether an intermediate query result should be
// filtered out.
type predicate interface {
	match(c *context, e *store.QueryResult) bool
}

// convertPredicate transforms a parse.Predicate into a predicate.
func convertPredicate(p parse.Predicate) predicate {
	switch p := p.(type) {
	case *parse.PredicateBool:
		return &predicateBool{p.Bool, p.Pos}
	case *parse.PredicateCompare:
		return &predicateCompare{convertExpr(p.LHS), convertExpr(p.RHS), p.Comp, p.Pos}
	case *parse.PredicateAnd:
		return &predicateAnd{convertPredicate(p.LHS), convertPredicate(p.RHS), p.Pos}
	case *parse.PredicateOr:
		return &predicateOr{convertPredicate(p.LHS), convertPredicate(p.RHS), p.Pos}
	case *parse.PredicateNot:
		return &predicateNot{convertPredicate(p.Pred), p.Pos}
	// TODO(kash): Support parse.PredicateFunc.
	default:
		panic(fmt.Errorf("unexpected type %T", p))
	}
}

// predicateBool represents a boolean literal.
type predicateBool struct {
	b   bool
	pos parse.Pos
}

// match implements the predicate method.
func (p *predicateBool) match(c *context, e *store.QueryResult) bool {
	return p.b
}

// predicateCompare handles the comparison on two expressions.
type predicateCompare struct {
	// lhs is the left-hand-side of the comparison.
	lhs expr
	// rhs is the right-hand-side of the comparison.
	rhs expr
	// comp specifies the operator to use in the comparison.
	comp parse.Comparator
	// pos specifies where in the query string this component started.
	pos parse.Pos
}

// match implements the predicate method.
func (p *predicateCompare) match(c *context, result *store.QueryResult) bool {
	lval := p.lhs.value(c, result)
	ltype := reflect.TypeOf(lval)
	rval := p.rhs.value(c, result)
	rtype := reflect.TypeOf(rval)

	if ltype != rtype {
		sendError(c.errc,
			fmt.Errorf("type mismatch while evaluating predicate at %v; name: %s, left: %v, right: %v",
				p.pos, result.Name, ltype, rtype))
	}
	switch lval := lval.(type) {
	case string:
		rval := rval.(string)
		return p.compareStrings(c, lval, rval)
	case *big.Rat:
		rval := rval.(*big.Rat)
		return p.compareRats(c, lval, rval)
	case *big.Int:
		rval := rval.(*big.Int)
		return p.compareInts(c, lval, rval)
	default:
		sendError(c.errc, fmt.Errorf("unexpected type %T from predicate %v for %s", lval, p.pos, result.Name))
		return false
	}
}

func (p *predicateCompare) compareStrings(c *context, lval, rval string) bool {
	switch p.comp {
	case parse.CompEQ:
		return lval == rval
	case parse.CompNE:
		return lval != rval
	case parse.CompLT:
		return lval < rval
	case parse.CompGT:
		return lval > rval
	case parse.CompLE:
		return lval <= rval
	case parse.CompGE:
		return lval >= rval
	default:
		sendError(c.errc, fmt.Errorf("unknown comparator %d at %v", p.comp, p.pos))
		return false
	}
}

func (p *predicateCompare) compareRats(c *context, lval, rval *big.Rat) bool {
	switch p.comp {
	case parse.CompEQ:
		return lval.Cmp(rval) == 0
	case parse.CompNE:
		return lval.Cmp(rval) != 0
	case parse.CompLT:
		return lval.Cmp(rval) < 0
	case parse.CompGT:
		return lval.Cmp(rval) > 0
	case parse.CompLE:
		return lval.Cmp(rval) <= 0
	case parse.CompGE:
		return lval.Cmp(rval) >= 0
	default:
		sendError(c.errc, fmt.Errorf("unknown comparator %d at %v", p.comp, p.pos))
		return false
	}
}

func (p *predicateCompare) compareInts(c *context, lval, rval *big.Int) bool {
	switch p.comp {
	case parse.CompEQ:
		return lval.Cmp(rval) == 0
	case parse.CompNE:
		return lval.Cmp(rval) != 0
	case parse.CompLT:
		return lval.Cmp(rval) < 0
	case parse.CompGT:
		return lval.Cmp(rval) > 0
	case parse.CompLE:
		return lval.Cmp(rval) <= 0
	case parse.CompGE:
		return lval.Cmp(rval) >= 0
	default:
		sendError(c.errc, fmt.Errorf("unknown comparator %d at %v", p.comp, p.pos))
		return false
	}
}

// predicateAnd is a predicate that is the logical conjunction of two
// predicates.
type predicateAnd struct {
	// lhs is the left-hand-side of the conjunction.
	lhs predicate
	// rhs is the right-hand-side of the conjuction.
	rhs predicate
	// pos specifies where in the query string this component started.
	pos parse.Pos
}

// match implements the predicate method.
func (p *predicateAnd) match(c *context, result *store.QueryResult) bool {
	// Short circuit to avoid extra processing.
	if !p.lhs.match(c, result) {
		return false
	}
	return p.rhs.match(c, result)
}

// predicateAnd is a predicate that is the logical disjunction of two
// predicates.
type predicateOr struct {
	// lhs is the left-hand-side of the disjunction.
	lhs predicate
	// rhs is the right-hand-side of the disjunction.
	rhs predicate
	// pos specifies where in the query string this component started.
	pos parse.Pos
}

// match implements the predicate method.
func (p *predicateOr) match(c *context, result *store.QueryResult) bool {
	// Short circuit to avoid extra processing.
	if p.lhs.match(c, result) {
		return true
	}
	return p.rhs.match(c, result)
}

// predicateAnd is a predicate that is the logical negation of another
// predicate.
type predicateNot struct {
	// pred is the predicate to be negated.
	pred predicate
	// pos specifies where in the query string this component started.
	pos parse.Pos
}

// match implements the predicate method.
func (p *predicateNot) match(c *context, result *store.QueryResult) bool {
	return !p.pred.match(c, result)
}

// expr produces a value in the context of a store.QueryResult.
type expr interface {
	// value returns a value in the context of result.
	value(c *context, result *store.QueryResult) interface{}
}

// convertExpr transforms a parse.Expr into an expr.
func convertExpr(e parse.Expr) expr {
	switch e := e.(type) {
	case *parse.ExprString:
		return &exprString{e.Str, e.Pos}
	case *parse.ExprRat:
		return &exprRat{e.Rat, e.Pos}
	case *parse.ExprInt:
		return &exprInt{e.Int, e.Pos}
	case *parse.ExprName:
		return &exprName{e.Name, e.Pos}
	// TODO(kash): Support the other types of expressions.
	default:
		panic(fmt.Errorf("unexpected type %T", e))
	}
}

// exprString is an expr that represents a string constant.
type exprString struct {
	// str is the string constant specified in the query.
	str string
	// pos specifies where in the query string this component started.
	pos parse.Pos
}

// value implements the expr method.
func (e *exprString) value(c *context, result *store.QueryResult) interface{} {
	return e.str
}

// exprRat is an expr that represents a rational number constant.
type exprRat struct {
	rat *big.Rat
	// pos specifies where in the query string this component started.
	pos parse.Pos
}

// value implements the expr method.
func (e *exprRat) value(c *context, result *store.QueryResult) interface{} {
	return e.rat
}

// exprInt is an expr that represents an integer constant.
type exprInt struct {
	i *big.Int
	// pos specifies where in the query string this component started.
	pos parse.Pos
}

// value implements the expr method.
func (e *exprInt) value(c *context, result *store.QueryResult) interface{} {
	return e.i
}

// exprName is an expr for a Veyron name literal.
type exprName struct {
	// name is the Veyron name used in the query.
	name string
	// pos specifies where in the query string this component started.
	pos parse.Pos
}

// value implements the expr method.
func (e *exprName) value(c *context, result *store.QueryResult) interface{} {
	if result.Fields != nil {
		// TODO(kash): Handle multipart names.  This currently only works if
		// e.name has no slashes.
		val, ok := result.Fields[e.name]
		if !ok {
			sendError(c.errc, fmt.Errorf("name '%s' was not selected from '%s', found: [%s]",
				e.name, result.Name, mapKeys(result.Fields)))
			return nil
		}
		return val
	}
	fullpath := naming.Join(result.Name, e.name)
	entry, err := c.sn.Get(c.clientID, storage.ParsePath(fullpath))
	if err != nil {
		sendError(c.errc, fmt.Errorf("could not look up name '%s' relative to '%s': %v", e.name, result.Name, err))
		return nil
	}
	return entry.Value
}

func mapKeys(m map[string]vdl.Any) string {
	s := make([]string, 0, len(m))
	for key, _ := range m {
		s = append(s, key)
	}
	sort.Strings(s)
	return strings.Join(s, ", ")
}
