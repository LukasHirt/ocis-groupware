package graphapi

import (
	"strings"
	"time"

	"github.com/LukasHirt/groupkit/ical"
)

// The $filter grammar of event lists:
//
//	filter    = condition *( "and" condition )
//	condition = time-prop time-op time | "subject" "eq" string
//	          | ( "startswith" | "contains" ) "(" "subject" "," string ")"
//	time-prop = "start/dateTime" | "end/dateTime"
//	time-op   = "ge" | "gt" | "le" | "lt"
//
// A time is a string or bare literal that reads as RFC 3339, with or without
// an offset, or as a date. Without an offset it is UTC. Text compares without
// regard to case. Anything that is valid OData but outside this grammar
// answers 501; text that is not valid answers 400.

type filterToken struct {
	text   string
	quoted bool
}

// lexFilter splits an expression into words, quoted strings and the
// punctuation ( ) and ,. A quote inside a string is written twice.
func lexFilter(s string) ([]filterToken, *apiError) {
	var out []filterToken
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case ' ', '\t':
			i++
		case '(', ')', ',':
			out = append(out, filterToken{text: string(c)})
			i++
		case '\'':
			var b strings.Builder
			i++
			closed := false
			for i < len(s) {
				if s[i] == '\'' {
					if i+1 < len(s) && s[i+1] == '\'' {
						b.WriteByte('\'')
						i += 2
						continue
					}
					closed = true
					i++
					break
				}
				b.WriteByte(s[i])
				i++
			}
			if !closed {
				return nil, badRequest("$filter has a string without a closing quote")
			}
			out = append(out, filterToken{text: b.String(), quoted: true})
		default:
			j := i
			for j < len(s) && !strings.ContainsRune(" \t(),'", rune(s[j])) {
				j++
			}
			out = append(out, filterToken{text: s[i:j]})
			i = j
		}
	}
	return out, nil
}

func filterSyntax(msg string) *apiError { return badRequest("$filter: " + msg) }

func filterUnsupported(what string) *apiError {
	return newError(501, codeNotImplemented, "$filter supports start/dateTime and end/dateTime with ge, gt, le and lt, and subject with eq, startswith and contains, joined by and; "+what+" is not supported")
}

// filterParser reads the tokens of one expression.
type filterParser struct {
	toks []filterToken
	pos  int
}

func (p *filterParser) peek() (filterToken, bool) {
	if p.pos >= len(p.toks) {
		return filterToken{}, false
	}
	return p.toks[p.pos], true
}

func (p *filterParser) next() (filterToken, bool) {
	t, ok := p.peek()
	if ok {
		p.pos++
	}
	return t, ok
}

// word takes the next token if it is an unquoted word.
func (p *filterParser) word() (string, bool) {
	t, ok := p.peek()
	if !ok || t.quoted {
		return "", false
	}
	p.pos++
	return t.text, true
}

func (p *filterParser) punct(s string) bool {
	t, ok := p.peek()
	if ok && !t.quoted && t.text == s {
		p.pos++
		return true
	}
	return false
}

// parseEventFilter reads a $filter expression into a test on events.
func parseEventFilter(expr string) (func(eventJSON) bool, *apiError) {
	toks, aerr := lexFilter(expr)
	if aerr != nil {
		return nil, aerr
	}
	if len(toks) == 0 {
		return nil, filterSyntax("the expression is empty")
	}
	p := &filterParser{toks: toks}
	res := ical.NewResolver(nil)
	var tests []func(eventJSON) bool
	for {
		test, aerr := p.condition(res)
		if aerr != nil {
			return nil, aerr
		}
		tests = append(tests, test)
		w, ok := p.word()
		if !ok {
			if _, more := p.peek(); more {
				return nil, filterSyntax("unexpected text after a condition")
			}
			break
		}
		switch strings.ToLower(w) {
		case "and":
		case "or":
			return nil, filterUnsupported("or")
		default:
			return nil, filterSyntax("expected and, found " + clip(w))
		}
	}
	return func(e eventJSON) bool {
		for _, t := range tests {
			if !t(e) {
				return false
			}
		}
		return true
	}, nil
}

func (p *filterParser) condition(res *ical.Resolver) (func(eventJSON) bool, *apiError) {
	if p.punct("(") {
		return nil, filterUnsupported("parentheses")
	}
	w, ok := p.word()
	if !ok {
		return nil, filterSyntax("expected a property or function name")
	}
	lw := strings.ToLower(w)
	if lw == "and" || lw == "or" {
		return nil, filterSyntax("expected a property or function name")
	}
	if lw == "not" {
		return nil, filterUnsupported("not")
	}
	if p.punct("(") {
		return p.function(lw)
	}
	switch w {
	case "start/dateTime", "end/dateTime":
		return p.timeCondition(res, w == "start/dateTime")
	case "subject":
		return p.subjectEquals()
	}
	return nil, filterUnsupported("the property " + clip(w))
}

func (p *filterParser) function(name string) (func(eventJSON) bool, *apiError) {
	if name != "startswith" && name != "contains" {
		return nil, filterUnsupported("the function " + clip(name))
	}
	prop, ok := p.word()
	if !ok || !p.punct(",") {
		return nil, filterSyntax(name + " takes a property and a string")
	}
	if prop != "subject" {
		return nil, filterUnsupported(name + " on " + clip(prop))
	}
	arg, ok := p.next()
	if !ok || !arg.quoted {
		return nil, filterSyntax(name + " takes a quoted string")
	}
	if !p.punct(")") {
		return nil, filterSyntax(name + " is missing its closing parenthesis")
	}
	needle := strings.ToLower(arg.text)
	if name == "startswith" {
		return func(e eventJSON) bool { return strings.HasPrefix(strings.ToLower(e.Subject), needle) }, nil
	}
	return func(e eventJSON) bool { return strings.Contains(strings.ToLower(e.Subject), needle) }, nil
}

func (p *filterParser) subjectEquals() (func(eventJSON) bool, *apiError) {
	op, ok := p.word()
	if !ok {
		return nil, filterSyntax("subject needs an operator")
	}
	if strings.ToLower(op) != "eq" {
		return nil, filterUnsupported("subject " + clip(op))
	}
	lit, ok := p.next()
	if !ok || !lit.quoted {
		return nil, filterSyntax("subject eq takes a quoted string")
	}
	want := strings.ToLower(lit.text)
	return func(e eventJSON) bool { return strings.ToLower(e.Subject) == want }, nil
}

func (p *filterParser) timeCondition(res *ical.Resolver, start bool) (func(eventJSON) bool, *apiError) {
	op, ok := p.word()
	if !ok {
		return nil, filterSyntax("a time property needs an operator")
	}
	op = strings.ToLower(op)
	switch op {
	case "ge", "gt", "le", "lt":
	case "eq", "ne":
		return nil, filterUnsupported(op + " on a time")
	default:
		return nil, filterSyntax("unknown operator " + clip(op))
	}
	lit, ok := p.next()
	if !ok || lit.text == "" || strings.ContainsAny(lit.text, "(),") && !lit.quoted {
		return nil, filterSyntax("a time property needs a time to compare with")
	}
	bound, ok := parseWindowTime(lit.text)
	if !ok {
		return nil, filterSyntax("the time must look like 2026-11-01T00:00:00Z")
	}
	return func(e eventJSON) bool {
		d := e.End
		if start {
			d = e.Start
		}
		z, aerr := parseDateTimeZone(d)
		if aerr != nil {
			return false
		}
		return compareTime(instant(res, z), bound, op)
	}, nil
}

func compareTime(got, bound time.Time, op string) bool {
	switch op {
	case "ge":
		return !got.Before(bound)
	case "gt":
		return got.After(bound)
	case "le":
		return !got.After(bound)
	default:
		return got.Before(bound)
	}
}
