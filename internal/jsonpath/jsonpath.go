// Package jsonpath models the paths twinget reports differences at
// ($.users[2].email) and the patterns users pass to --ignore and
// --unordered ($.users[*].id, $.meta.**, **.request_id).
//
// The dialect is deliberately tiny and fully documented in
// docs/noise-filters.md: dot keys, bracket-quoted keys for names with
// special characters, [N] indexes, and three wildcards — * (one key or
// index), [*] (one index), ** (any run of segments, including none).
package jsonpath

import (
	"fmt"
	"strconv"
	"strings"
)

// Seg is one concrete step from the JSON root to a value: either an
// object key or an array index.
type Seg struct {
	Key     string
	Index   int
	IsIndex bool
}

// Key returns a key segment.
func Key(k string) Seg { return Seg{Key: k} }

// Index returns an array-index segment.
func Index(i int) Seg { return Seg{Index: i, IsIndex: true} }

// plainKey reports whether k can be rendered in dot notation without
// ambiguity. Anything else is rendered bracket-quoted.
func plainKey(k string) bool {
	if k == "" {
		return false
	}
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}

// Render turns a concrete segment list into the display form used in
// every report, e.g. $.users[2]["full name"].
func Render(segs []Seg) string {
	var b strings.Builder
	b.WriteString("$")
	for _, s := range segs {
		switch {
		case s.IsIndex:
			fmt.Fprintf(&b, "[%d]", s.Index)
		case plainKey(s.Key):
			b.WriteString(".")
			b.WriteString(s.Key)
		default:
			fmt.Fprintf(&b, "[%q]", s.Key)
		}
	}
	return b.String()
}

// token is one step of a parsed pattern.
type token struct {
	kind  tokenKind
	key   string // for tKey
	index int    // for tIndex
}

type tokenKind int

const (
	tKey      tokenKind = iota // literal object key
	tIndex                     // literal array index [3]
	tAnyOne                    // * — exactly one segment, key or index
	tAnyIndex                  // [*] — exactly one index segment
	tDeep                      // ** — zero or more segments of any kind
)

// Pattern is a compiled --ignore / --unordered pattern.
type Pattern struct {
	raw    string
	tokens []token
}

// String returns the pattern as the user wrote it.
func (p Pattern) String() string { return p.raw }

// ParsePattern compiles a pattern string. A leading "$" is optional so
// users can write either $.users[*].id or users[*].id.
func ParsePattern(raw string) (Pattern, error) {
	s := strings.TrimSpace(raw)
	rest := strings.TrimPrefix(s, "$")
	toks, err := lex(rest)
	if err != nil {
		return Pattern{}, fmt.Errorf("pattern %q: %w", raw, err)
	}
	if len(toks) == 0 {
		return Pattern{}, fmt.Errorf("pattern %q: empty", raw)
	}
	return Pattern{raw: s, tokens: toks}, nil
}

// lex splits the body of a pattern into tokens. It accepts dot keys,
// bracket forms ([3], [*], ["quoted key"]), * and **.
func lex(s string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(s) {
		switch {
		case s[i] == '.':
			i++
			if i < len(s) && s[i] == '.' {
				// ".." is a common JSONPath habit for deep matching;
				// treat it as sugar for ".**." so it does what users mean.
				toks = append(toks, token{kind: tDeep})
				i++
			}
		case s[i] == '[':
			end := strings.IndexByte(s[i:], ']')
			if end < 0 {
				return nil, fmt.Errorf("unclosed '[' at offset %d", i)
			}
			body := s[i+1 : i+end]
			i += end + 1
			switch {
			case body == "*":
				toks = append(toks, token{kind: tAnyIndex})
			case len(body) >= 2 && body[0] == '"' && body[len(body)-1] == '"':
				key, err := strconv.Unquote(body)
				if err != nil {
					return nil, fmt.Errorf("bad quoted key %s", body)
				}
				toks = append(toks, token{kind: tKey, key: key})
			default:
				n, err := strconv.Atoi(body)
				if err != nil || n < 0 {
					return nil, fmt.Errorf("bad index [%s]", body)
				}
				toks = append(toks, token{kind: tIndex, index: n})
			}
		default:
			j := i
			for j < len(s) && s[j] != '.' && s[j] != '[' {
				j++
			}
			word := s[i:j]
			i = j
			switch word {
			case "**":
				toks = append(toks, token{kind: tDeep})
			case "*":
				toks = append(toks, token{kind: tAnyOne})
			default:
				toks = append(toks, token{kind: tKey, key: word})
			}
		}
	}
	return toks, nil
}

// Matches reports whether the pattern matches the concrete path or any
// of its ancestors. Prefix semantics are deliberate: --ignore $.meta
// silences everything inside $.meta, which is what users reach for when
// a whole subtree is noise.
func (p Pattern) Matches(segs []Seg) bool {
	return match(p.tokens, segs)
}

// match implements wildcard matching where a fully-consumed pattern
// matches any remaining concrete segments (prefix semantics).
func match(toks []token, segs []Seg) bool {
	if len(toks) == 0 {
		return true // pattern consumed: prefix match
	}
	t := toks[0]
	if t.kind == tDeep {
		// ** tries to swallow 0..len(segs) segments.
		for skip := 0; skip <= len(segs); skip++ {
			if match(toks[1:], segs[skip:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	s := segs[0]
	ok := false
	switch t.kind {
	case tKey:
		ok = !s.IsIndex && s.Key == t.key
	case tIndex:
		ok = s.IsIndex && s.Index == t.index
	case tAnyIndex:
		ok = s.IsIndex
	case tAnyOne:
		ok = true
	}
	if !ok {
		return false
	}
	return match(toks[1:], segs[1:])
}

// MatchesExact reports whether the pattern matches the concrete path
// exactly, with no trailing segments. --unordered uses this so that
// $.items relaxes ordering of that one array, not of every array
// nested inside its elements.
func (p Pattern) MatchesExact(segs []Seg) bool {
	return matchExact(p.tokens, segs)
}

func matchExact(toks []token, segs []Seg) bool {
	if len(toks) == 0 {
		return len(segs) == 0
	}
	t := toks[0]
	if t.kind == tDeep {
		for skip := 0; skip <= len(segs); skip++ {
			if matchExact(toks[1:], segs[skip:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	s := segs[0]
	ok := false
	switch t.kind {
	case tKey:
		ok = !s.IsIndex && s.Key == t.key
	case tIndex:
		ok = s.IsIndex && s.Index == t.index
	case tAnyIndex:
		ok = s.IsIndex
	case tAnyOne:
		ok = true
	}
	if !ok {
		return false
	}
	return matchExact(toks[1:], segs[1:])
}

// AnyMatches reports whether any pattern in the set matches the path,
// returning the first pattern that does.
func AnyMatches(patterns []Pattern, segs []Seg) (Pattern, bool) {
	for _, p := range patterns {
		if p.Matches(segs) {
			return p, true
		}
	}
	return Pattern{}, false
}
