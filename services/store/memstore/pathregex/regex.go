package pathregex

import (
	"regexp"
)

// regex is the type of regular expressions.
type regex interface {
	// compile constructs an NFA.  The argument is the final state.  Returns the
	// initial state.
	compile(final *state) *state

	// reverse reverses the regular expression in place.
	reverse()

	// String returns a string representation of the regular expression.
	String() string
}

// regexCommon is a common base type of regular expressions.
type regexCommon struct{}

// tt accepts everything.
type tt struct {
	regexCommon
}

// ff rejects everything.
type ff struct {
	regexCommon
}

// epsilon matches the empty path.
type epsilon struct {
	regexCommon
}

// single matches a single component of a path, using a regexp.Regexp for the
// match.
type single struct {
	regexCommon
	r *regexp.Regexp
}

// sequence matches a sequence of components.
type sequence struct {
	regexCommon
	r1, r2 regex
}

// alt matches one of two regular expressions.
type alt struct {
	regexCommon
	r1, r2 regex
}

// star is the Kleene closure.
type star struct {
	regexCommon
	re regex
}

var (
	trueRegex      = &tt{}
	falseRegex     = &ff{}
	epsilonRegex   = &epsilon{}
	dotDotDotRegex = &star{re: trueRegex}
)

// newSingle returns a Regex that matches one component of a path.
func newSingle(r *regexp.Regexp) regex {
	return &single{r: r}
}

// newSequence returns a Regex that matches a sequence of path components.
func newSequence(rl []regex) regex {
	if len(rl) == 0 {
		return epsilonRegex
	}
	r := rl[len(rl)-1]
	for i := len(rl) - 2; i >= 0; i-- {
		r = &sequence{r1: rl[i], r2: r}
	}
	return r
}

// newAlt returns a Regex that matches one of a set of regular expressions.
func newAlt(rl []regex) regex {
	if len(rl) == 0 {
		return falseRegex
	}
	r := rl[len(rl)-1]
	for i := len(rl) - 2; i >= 0; i-- {
		r = &alt{r1: rl[i], r2: r}
	}
	return r
}

// newStar returns the Kleene closure of a regular expression.
func newStar(re regex) regex {
	return &star{re: re}
}

func (re *tt) String() string {
	return "<true>"
}

func (re *ff) String() string {
	return "<false>"
}

func (re *epsilon) String() string {
	return "<epsilon>"
}

func (re *single) String() string {
	return re.r.String()
}

func (re *sequence) String() string {
	return re.r1.String() + "/" + re.r2.String()
}

func (re *alt) String() string {
	return "(" + re.r1.String() + "|" + re.r2.String() + ")"
}

func (re *star) String() string {
	return "(" + re.re.String() + ")*"
}

func (re *regexCommon) reverse() {
}

func (re *sequence) reverse() {
	re.r1, re.r2 = re.r2, re.r1
	re.r1.reverse()
	re.r2.reverse()
}

func (re *alt) reverse() {
	re.r1.reverse()
	re.r2.reverse()
}

func (re *star) reverse() {
	re.re.reverse()
}
