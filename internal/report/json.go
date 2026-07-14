// Stable JSON output for machines. The envelope is versioned: any
// breaking change to field names or semantics bumps schema_version.
package report

import (
	"encoding/json"
	"io"

	"github.com/JaydenCJ/twinget/internal/diff"
)

// envelope is the top-level JSON document.
type envelope struct {
	Tool          string      `json:"tool"`
	SchemaVersion int         `json:"schema_version"`
	A             string      `json:"a"`
	B             string      `json:"b"`
	Results       []resultDoc `json:"results"`
	Summary       summaryDoc  `json:"summary"`
}

type resultDoc struct {
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	A           diff.Side         `json:"a"`
	B           diff.Side         `json:"b"`
	Parity      bool              `json:"parity"`
	Differences []diff.Difference `json:"differences"`
	Counts      countsDoc         `json:"counts"`
}

type countsDoc struct {
	Differences int `json:"differences"`
	Ignored     int `json:"ignored"`
}

type summaryDoc struct {
	Requests int `json:"requests"`
	Parity   int `json:"parity"`
	Diff     int `json:"diff"`
}

// JSON writes the versioned envelope. Ignored differences are always
// included (with "ignored": true) so downstream tooling can audit what
// the noise filters suppressed.
func JSON(w io.Writer, meta Meta, results []diff.RequestResult) error {
	env := envelope{
		Tool:          "twinget",
		SchemaVersion: 1,
		A:             meta.BaseA,
		B:             meta.BaseB,
	}
	for _, r := range results {
		effective, ignored := r.Counts()
		doc := resultDoc{
			Method:      r.Method,
			Path:        r.Path,
			A:           r.A,
			B:           r.B,
			Parity:      r.Parity(),
			Differences: r.Differences,
			Counts:      countsDoc{Differences: effective, Ignored: ignored},
		}
		if doc.Differences == nil {
			doc.Differences = []diff.Difference{}
		}
		env.Results = append(env.Results, doc)
	}
	s := Summarize(results)
	env.Summary = summaryDoc{Requests: s.Requests, Parity: s.Parity, Diff: s.Diff}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(env)
}
