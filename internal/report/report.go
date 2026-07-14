// Package report renders comparison results as terminal text, stable
// machine-readable JSON (schema_version 1), and PR-ready Markdown.
package report

import "github.com/JaydenCJ/twinget/internal/diff"

// Meta carries run-level context every renderer needs.
type Meta struct {
	BaseA       string
	BaseB       string
	ShowIgnored bool
}

// Summary aggregates results across a run.
type Summary struct {
	Requests int `json:"requests"`
	Parity   int `json:"parity"`
	Diff     int `json:"diff"`
}

// Summarize counts parity vs diff outcomes.
func Summarize(results []diff.RequestResult) Summary {
	s := Summary{Requests: len(results)}
	for _, r := range results {
		if r.Parity() {
			s.Parity++
		} else {
			s.Diff++
		}
	}
	return s
}

// kindLabel maps difference kinds to the short labels used in text and
// Markdown output.
func kindLabel(k diff.Kind) string {
	switch k {
	case diff.KindValue:
		return "value"
	case diff.KindType:
		return "type"
	case diff.KindMissingA:
		return "only in b"
	case diff.KindMissingB:
		return "only in a"
	case diff.KindLength:
		return "length"
	case diff.KindStatus:
		return "mismatch"
	case diff.KindHeader:
		return "value"
	case diff.KindBodyFormat:
		return "format"
	case diff.KindText:
		return "text"
	}
	return string(k)
}
