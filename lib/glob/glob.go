// glob implements a glob language.
//
// Globs match a slash separated series of glob expressions.
//
// pattern:
// term ['/' term]*
// term:
// '*'         matches any sequence of non-Separator characters
// '?'         matches any single non-Separator character
// '[' [ '^' ] { character-range } ']'
// character class (must be non-empty)
// c           matches character c (c != '*', '?', '\\', '[', '/')
// '\\' c      matches character c
// character-range:
// c           matches character c (c != '\\', '-', ']')
// '\\' c      matches character c
// lo '-' hi   matches character c for lo <= c <= hi

package glob

import (
	"path/filepath"
	"strings"
)

// Glob represents a slash separated path glob expression.
type Glob struct {
	elems     []string
	recursive bool
}

// Parse returns a new Glob.
func Parse(pattern string) (*Glob, error) {
	if len(pattern) > 0 && pattern[0] == '/' {
		return nil, filepath.ErrBadPattern
	}

	g := &Glob{}
	if pattern != "" {
		g.elems = strings.Split(pattern, "/")
	}
	if last := len(g.elems) - 1; last >= 0 && g.elems[last] == "..." {
		g.elems = g.elems[:last]
		g.recursive = true
	}

	// The only error we can get from the filepath library is badpattern.
	// A future implementation would most likely recognize that here, so for now
	// I'll just check every part to make sure it's error free.
	for _, elem := range g.elems {
		if _, err := filepath.Match(elem, ""); err != nil {
			return nil, err
		}
	}

	return g, nil
}

// Len returns the number of path elements represented by the glob expression.
func (g *Glob) Len() int {
	return len(g.elems)
}

// Finished returns true if the pattern cannot match anything.
func (g *Glob) Finished() bool {
	return !g.recursive && len(g.elems) == 0
}

// Split returns the suffix of g starting at the path element corresponding to start.
func (g *Glob) Split(start int) *Glob {
	if start >= len(g.elems) {
		return &Glob{elems: nil, recursive: g.recursive}
	}
	return &Glob{elems: g.elems[start:], recursive: g.recursive}
}

// MatchInitialSegment tries to match segment against the initial element of g.
// Returns:
// matched, a boolean indicating whether the match was successful;
// exact, a boolean indicating whether segment matched a fixed string pattern;
// remainder, a Glob representing the unmatched remainder of g.
func (g *Glob) MatchInitialSegment(segment string) (matched bool, exact bool, remainder *Glob) {
	if len(g.elems) == 0 {
		if !g.recursive {
			return false, false, nil
		}
		// The segment matches "...". This is not an exact match.
		return true, false, g
	}

	if matches, err := filepath.Match(g.elems[0], segment); err != nil {
		panic("Error in glob pattern found.")
	} else if matches {
		_, fixed := isFixed(g.elems[0])
		return true, fixed, g.Split(1)
	}
	return false, false, nil
}

// PartialMatch tries matching elems against part of a glob pattern.
// Returns:
// matched, a boolean indicating whether each element e_i of elems matches the
// (start + i)th element of the glob pattern;
// exact, a boolean indicating whether elems matched a fixed string pattern.
// <path> is considered an exact match for pattern <path>/...;
// remainder, a Glob representing the unmatched remainder of g. remainder will
// be empty if the pattern is completely matched.
//
// Note that if the glob is recursive elems can have more elements then
// the glob pattern and still get a true result.
func (g *Glob) PartialMatch(start int, elems []string) (matched bool, exact bool, remainder *Glob) {
	g = g.Split(start)
	allExact := true
	for i := 0; i < len(elems); i++ {
		var matched, exact bool
		if matched, exact, g = g.MatchInitialSegment(elems[i]); !matched {
			return false, false, nil
		} else if !exact {
			allExact = false
		}
	}
	return true, allExact, g
}

// isFixed returns the unescaped string and true if 's' is a pattern specifying
// a fixed string.  Otherwise it returns the original string and false.
func isFixed(s string) (string, bool) {
	// No special characters.
	if !strings.ContainsAny(s, "*?[") {
		return s, true
	}
	// Special characters and no backslash.
	if !strings.ContainsAny(s, "\\") {
		return "", false
	}
	unescaped := ""
	escape := false
	for _, c := range s {
		if escape {
			escape = false
			unescaped += string(c)
		} else if strings.ContainsRune("*?[", c) {
			// S contains an unescaped special character.
			return s, false
		} else if c == '\\' {
			escape = true
		} else {
			unescaped += string(c)
		}
	}
	return unescaped, true
}

func (g *Glob) SplitFixedPrefix() ([]string, *Glob) {
	var prefix []string
	start := 0
	for _, elem := range g.elems {
		if u, q := isFixed(elem); q {
			prefix = append(prefix, u)
			start++
		} else {
			break
		}
	}
	return prefix, g.Split(start)
}

func (g *Glob) String() string {
	e := g.elems
	if g.recursive {
		e = append(e, "...")
	}
	return filepath.Join(e...)
}
