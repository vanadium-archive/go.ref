package pathregex

import (
	"bytes"
	"io"
	"regexp"
	"strings"

	"veyron2/verror"
)

// parser is a recursive-descent parser for path regular expressions.
type parser struct {
	reader *strings.Reader

	// commas are treated specially within braces, but are otherwise normal
	// characters.
	commaIsSpecial bool

	// isErrored is true iff an error has been encountered.
	isErrored bool
}

// Compile compiles a string to a finite automaton.  Returns a StateSet of the
// initial states of the automaton.
func Compile(s string) (StateSet, error) {
	re, err := compileRegex(s)
	if err != nil {
		return nil, err
	}
	return compileNFA(re), nil
}

// CompileReverse compiles a path regular expression to a finite automaton,
// reversing the regular expression.  Returns a StateSet of the initial states
// of the automaton.
func CompileReverse(s string) (StateSet, error) {
	re, err := compileRegex(s)
	if err != nil {
		return nil, err
	}
	re.reverse()
	return compileNFA(re), nil
}

func compileRegex(s string) (regex, error) {
	p := &parser{reader: strings.NewReader(s)}
	re := p.parsePath()
	if p.isErrored || p.reader.Len() != 0 {
		pos, _ := p.reader.Seek(0, 1)
		err := verror.BadArgf("Syntax error at char %d: %q", pos, s)
		return nil, err
	}
	return re, nil
}

// parsePath reads a path regular expression.  Reads as much of the input as
// possible, stopping at special characters, like '}' or ',' (if
// commaIsSpecial).
func (p *parser) parsePath() regex {
	var path []regex
	for !p.isErrored {
		// If the next rune is '{', parse as an alternation; otherwise, parse
		// the next component.
		c, ok := p.readRune()
		if !ok {
			break
		}
		var re regex
		if c == '{' {
			re = p.parsePathAlt()
		} else {
			p.unreadRune()
			re = p.parseComponent()
		}
		if re != nil {
			path = append(path, re)
		}

		c, ok = p.readRune()
		if !ok {
			break
		}
		if c != '/' {
			p.unreadRune()
			break
		}
	}
	return newSequence(path)
}

// parsePathAlt reads an alternation {p1,p2,...,pn}.  Assumes the opening brace
// has already been read; consumes the closing brace.
func (p *parser) parsePathAlt() regex {
	s := p.commaIsSpecial
	defer func() { p.commaIsSpecial = s }()
	p.commaIsSpecial = true

	var choices []regex
parseLoop:
	for !p.isErrored {
		if re := p.parsePath(); re != nil {
			choices = append(choices, re)
		}
		c, ok := p.readRune()
		if !ok {
			break parseLoop
		}
		switch c {
		case ',':
			// skip
		case '}':
			return newAlt(choices)
		default:
			break parseLoop
		}
	}
	p.setError()
	return nil
}

// parseComponent parses a single component of a path.  This is a glob
// expression.
func (p *parser) parseComponent() regex {
	// p.reader.Seek(0, 1) just returns the current position.
	startPos, _ := p.reader.Seek(0, 1)
	var buf bytes.Buffer
	p.parseComponentPiece(&buf)
	if buf.Len() == 0 {
		return nil
	}
	endPos, _ := p.reader.Seek(0, 1)

	// The ... component name is special.
	if endPos-startPos == 3 {
		var literal [3]byte
		p.reader.ReadAt(literal[:], startPos)
		if string(literal[:]) == "..." {
			return dotDotDotRegex
		}
	}

	// Everything else is a regular expression.
	re, err := regexp.Compile("^" + buf.String() + "$")
	if err != nil {
		p.setError()
		return nil
	}
	return newSingle(re)
}

// parseComponentPiece reads a glob expression and converts it to a regular
// expression.
func (p *parser) parseComponentPiece(buf *bytes.Buffer) {
	for !p.isErrored {
		c, ok := p.readRune()
		if !ok {
			return
		}
		switch c {
		case '*':
			buf.WriteString(".*")
		case '?':
			buf.WriteString(".")
		case '.', '(', ')':
			buf.WriteRune('\\')
			buf.WriteRune(c)
		case '\\':
			p.parseEscapedRune(buf)
		case '[':
			buf.WriteRune('[')
			p.parseCharRange(buf)
			buf.WriteRune(']')
		case '{':
			buf.WriteRune('(')
			p.parseComponentAlt(buf)
			buf.WriteRune(')')
		case '}', ']', '/':
			p.unreadRune()
			return
		case ',':
			if p.commaIsSpecial {
				p.unreadRune()
				return
			} else {
				buf.WriteRune(c)
			}
		default:
			buf.WriteRune(c)
		}
	}
}

// parseCharRange copies the input range literally.
//
// TODO(jyh): Translate the glob range.
func (p *parser) parseCharRange(buf *bytes.Buffer) {
	// Initial ] does not close the range.
	c, ok := p.readRune()
	if !ok {
		p.setError()
		return
	}
	buf.WriteRune(c)
	for {
		c, ok := p.readRune()
		if !ok {
			p.setError()
			return
		}
		if c == ']' {
			break
		}
		buf.WriteRune(c)
	}
}

// parseComponentAlt parses an alternation.
func (p *parser) parseComponentAlt(buf *bytes.Buffer) {
	s := p.commaIsSpecial
	p.commaIsSpecial = true
	defer func() { p.commaIsSpecial = s }()
	for {
		p.parseComponentPiece(buf)
		c, ok := p.readRune()
		if !ok {
			p.setError()
			return
		}
		switch c {
		case ',':
			buf.WriteRune('|')
		case '}':
			return
		default:
			p.setError()
			return
		}
	}
}

// parseEscapedRune parses a rune immediately after a backslash.
func (p *parser) parseEscapedRune(buf *bytes.Buffer) {
	c, ok := p.readRune()
	if !ok {
		return
	}
	// TODO(jyh): Are there any special escape sequences?
	buf.WriteRune('\\')
	buf.WriteRune(c)
}

// readRune reads the next rune from the input reader.  If there is an error,
// returns false and sets the isErrored flag if the error is not io.EOF.
func (p *parser) readRune() (rune, bool) {
	if p.isErrored {
		return 0, false
	}
	c, _, err := p.reader.ReadRune()
	switch {
	case err == io.EOF:
		return c, false
	case err != nil:
		p.setError()
		return c, false
	}
	return c, true
}

// unreadRune pushes back the last rune read.
func (p *parser) unreadRune() {
	p.reader.UnreadRune()
}

// setError sets the error flag.
func (p *parser) setError() {
	p.isErrored = true
}
