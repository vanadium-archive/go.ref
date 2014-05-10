package pathregex

import (
	"regexp"
	"sort"
	"unsafe"
)

// state is a state of the NFA.
type state struct {
	// isFinal is true iff the state is a final state.
	isFinal bool

	// isClosed is true iff the epsilon transitions are transitively closed.
	isClosed bool

	// trans is a non-epsilon transitions, or nil.
	trans *transition

	// epsilon are epsilon transitions, meaning the automation can take a
	// state transition without reading any input.
	epsilon hashStateSet
}

// transition represents a non-empty transition.  If the current input symbol matches the
// regular expression, the NFA can take a transition to the next states.
type transition struct {
	re   *regexp.Regexp // nil means accept anything.
	next hashStateSet
}

// hashStateSet represents a set of states using a map.
type hashStateSet map[*state]struct{}

// StateSet represents a set of states of the NFA as a sorted slice of state pointers.
type StateSet []*state

// byPointer is used to sort the pointers in StateSet.
type byPointer StateSet

// tt accepts anything.
func (r tt) compile(final *state) *state {
	t := &transition{next: hashStateSet{final: struct{}{}}}
	return &state{trans: t, epsilon: make(hashStateSet)}
}

// ff has no transititons.
func (r *ff) compile(final *state) *state {
	return &state{epsilon: make(hashStateSet)}
}

// epsilon doesn't require a transition.
func (r *epsilon) compile(final *state) *state {
	return final
}

// single has a single transition from initial to final state.
func (r *single) compile(final *state) *state {
	t := &transition{re: r.r, next: hashStateSet{final: struct{}{}}}
	return &state{trans: t, epsilon: make(hashStateSet)}
}

// sequence composes the automata.
func (r *sequence) compile(final *state) *state {
	return r.r1.compile(r.r2.compile(final))
}

// alt constructs the separate automata, then defines a new start state that
// includes epsilon transitions to the two separate automata.
func (r *alt) compile(final *state) *state {
	s1 := r.r1.compile(final)
	s2 := r.r2.compile(final)
	return &state{epsilon: hashStateSet{s1: struct{}{}, s2: struct{}{}}}
}

// star contains a loop to accepts 0-or-more occurrences of r.re.  There is an
// epsilon transition from the start state s1 to the final state (for 0
// occurrences), and a back epsilon-transition for 1-or-more occurrences.
func (r *star) compile(final *state) *state {
	s2 := &state{epsilon: make(hashStateSet)}
	s1 := r.re.compile(s2)
	s2.epsilon = hashStateSet{s1: struct{}{}, final: struct{}{}}
	s1.epsilon[s2] = struct{}{}
	return s1
}

// close takes the transitive closure of the epsilon transitions.
func (s *state) close() {
	if !s.isClosed {
		s.isClosed = true
		s.epsilon[s] = struct{}{}
		s.epsilon.closeEpsilon()
		if s.trans != nil {
			s.trans.next.closeEpsilon()
		}
	}
}

// isUseless returns true iff the state has only epsilon transitions and it is
// not a final state.
func (s *state) isUseless() bool {
	return s.trans == nil && !s.isFinal
}

// addStates folds the src states into the dst.
func (dst hashStateSet) addStates(src hashStateSet) {
	for s, _ := range src {
		dst[s] = struct{}{}
	}
}

// stateSet converts the hashStateSet to a StateSet.
func (set hashStateSet) stateSet() StateSet {
	states := StateSet{}
	for s, _ := range set {
		if !s.isUseless() {
			states = append(states, s)
		}
	}
	sort.Sort(byPointer(states))
	return states
}

// closeEpsilon closes the state set under epsilon transitions.
func (states hashStateSet) closeEpsilon() {
	for changed := true; changed; {
		size := len(states)
		for s, _ := range states {
			s.close()
			states.addStates(s.epsilon)
		}
		changed = len(states) != size
	}

	// Remove useless states.
	for s, _ := range states {
		if s.isUseless() {
			delete(states, s)
		}
	}
}

// Step takes a transition for input name.
func (ss StateSet) Step(name string) StateSet {
	states := make(hashStateSet)
	for _, s := range ss {
		if s.trans != nil && (s.trans.re == nil || s.trans.re.MatchString(name)) {
			s.close()
			states.addStates(s.trans.next)
		}
	}
	return states.stateSet()
}

// IsFinal returns true iff the StateSet contains a final state.
func (ss StateSet) IsFinal() bool {
	for _, s := range ss {
		if s.isFinal {
			return true
		}
	}
	return false
}

// IsReject returns true iff the StateSet is empty.
func (ss StateSet) IsReject() bool {
	return len(ss) == 0
}

// Union combines the state sets, returning a new StateSet.
func (s1 StateSet) Union(s2 StateSet) StateSet {
	// As a space optimization, detect the case where the two states sets are
	// equal.  If so, return s1 unchanged.
	if s1.Equals(s2) {
		return s1
	}

	i1 := 0
	i2 := 0
	var result StateSet
	for i1 < len(s1) && i2 < len(s2) {
		p1 := uintptr(unsafe.Pointer(s1[i1]))
		p2 := uintptr(unsafe.Pointer(s2[i2]))
		switch {
		case p1 == p2:
			i2++
			fallthrough
		case p1 < p2:
			result = append(result, s1[i1])
			i1++
		case p2 < p1:
			result = append(result, s2[i2])
			i2++
		}
	}
	result = append(result, s1[i1:]...)
	result = append(result, s2[i2:]...)
	return result
}

// Equals returns true iff the state sets are equal.
func (s1 StateSet) Equals(s2 StateSet) bool {
	if len(s1) != len(s2) {
		return false
	}
	for i, s := range s1 {
		if s2[i] != s {
			return false
		}
	}
	return true
}

// sorting methods.
func (a byPointer) Len() int      { return len(a) }
func (a byPointer) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a byPointer) Less(i, j int) bool {
	return uintptr(unsafe.Pointer(a[i])) < uintptr(unsafe.Pointer(a[j]))
}

// Compile compiles the Regex to a NFA.
func compileNFA(r regex) StateSet {
	s := r.compile(&state{isFinal: true, epsilon: make(hashStateSet)})
	s.close()
	return s.epsilon.stateSet()
}
