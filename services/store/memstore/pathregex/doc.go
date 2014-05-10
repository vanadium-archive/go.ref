// Package pathregex implements path regular expressions.  These are like glob
// patterns, with the following kinds of expressions.
//
// Each component of a path is specified using a glob expression, where:
//    * matches any sequence of characters.
//    ? matches a single character.
//    [...] matches any single character in a range, where the ranges include:
//       c: matches the single character c
//       c1-c2: matches all characters in the range { c1,...,c2 }
//       ]: can be specified as the first character in the range
//       ^: a leading ^ inverts the range
//    {r1,r2,...,rN} matches r1 OR r1 OR ... OR rN.
//
// A path regular expression composes a path.
//
//    R ::= r                    // a single component glob expression
//       | ...                   // matches any path
//       | R1 / R2               // sequential composition
//       | { R1, R2, ..., RN }   // alternation, R1 OR R2 OR ... OR RN
//
// Examples:
//
//    x.jpg              - matches a path with one component x.jpg.
//    a/b                - matches a two component path.
//    .../a/b/...        - matches a path containing an a/b somewhere within it.
//    .../[abc]/...      - matches a path containing an "a" or "b" or "c" component.
//    .../{a,b,c}/...    - same as above.
//    {.../a/...,.../b/...,.../c/...} - same as above.
//
// The usage is to compile a path expression to a finite automaton, then execute
// the automaton.
//
//    ss, err := pathregex.Compile(".../{a,b/c,d/e/f}/...")
//    ss = ss.Step("x")  // Match against x/d/e/f/y.
//    ss = ss.Step("d")
//    ss = ss.Step("e")
//    ss = ss.Step("f")
//    ss = ss.Step("y")
//    if ss.IsFinal() { log.Printf("Matched") }
package pathregex
