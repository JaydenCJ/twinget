// Package diff performs the structural comparison at the heart of
// twinget: status codes, headers, and JSON bodies from two backends,
// with noise filters applied at difference-recording time so every
// masked difference is still counted and can be shown on demand.
package diff

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/JaydenCJ/twinget/internal/jsonpath"
	"github.com/JaydenCJ/twinget/internal/jsonval"
	"github.com/JaydenCJ/twinget/internal/noise"
)

// Kind classifies a single difference.
type Kind string

const (
	KindValue      Kind = "value"        // same type, different value
	KindType       Kind = "type"         // JSON type changed
	KindMissingA   Kind = "missing_in_a" // present only in backend B
	KindMissingB   Kind = "missing_in_b" // present only in backend A
	KindLength     Kind = "array_length" // arrays of different length
	KindStatus     Kind = "status"       // HTTP status codes differ
	KindHeader     Kind = "header"       // header value differs
	KindBodyFormat Kind = "body_format"  // one side JSON, other not
	KindText       Kind = "text"         // non-JSON bodies differ
)

// Target says which part of the response a difference belongs to.
type Target string

const (
	TargetStatus Target = "status"
	TargetHeader Target = "header"
	TargetBody   Target = "body"
)

// Difference is one observed divergence between backend A and B.
// Ignored differences are recorded, not discarded, so reports can say
// "parity (3 ignored)" and --show-ignored can list them.
type Difference struct {
	Target  Target `json:"target"`
	Path    string `json:"path"` // JSON path, header name, or "status"
	Kind    Kind   `json:"kind"`
	A       string `json:"a"` // rendered snippet; "" when absent
	B       string `json:"b"`
	Ignored bool   `json:"ignored"`
	Reason  string `json:"reason,omitempty"` // why it was ignored
}

// Options selects which noise filters are active.
type Options struct {
	IgnorePaths      []jsonpath.Pattern // --ignore
	UnorderedPaths   []jsonpath.Pattern // --unordered
	IgnoreTimestamps bool               // --ignore-timestamps
	IgnoreIDs        bool               // --ignore-ids
	IgnoreHeaders    []string           // --ignore-header (lowercased by CLI)
	StrictHeaders    bool               // --strict-headers
}

// Status compares HTTP status codes.
func Status(a, b int) []Difference {
	if a == b {
		return nil
	}
	return []Difference{{
		Target: TargetStatus,
		Path:   "status",
		Kind:   KindStatus,
		A:      fmt.Sprintf("%d", a),
		B:      fmt.Sprintf("%d", b),
	}}
}

// Headers compares two header maps case-insensitively. Multi-valued
// headers are joined with ", " in received order. The result is sorted
// by header name for deterministic reports.
func Headers(a, b map[string][]string, opts Options) []Difference {
	extra := make(map[string]bool, len(opts.IgnoreHeaders))
	for _, h := range opts.IgnoreHeaders {
		extra[strings.ToLower(h)] = true
	}
	flatten := func(h map[string][]string) map[string]string {
		out := make(map[string]string, len(h))
		for name, vals := range h {
			out[strings.ToLower(name)] = strings.Join(vals, ", ")
		}
		return out
	}
	fa, fb := flatten(a), flatten(b)

	names := make([]string, 0, len(fa)+len(fb))
	seen := map[string]bool{}
	for n := range fa {
		names = append(names, n)
		seen[n] = true
	}
	for n := range fb {
		if !seen[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)

	var diffs []Difference
	for _, name := range names {
		va, inA := fa[name]
		vb, inB := fb[name]
		if inA && inB && va == vb {
			continue
		}
		d := Difference{Target: TargetHeader, Path: name, Kind: KindHeader, A: va, B: vb}
		switch {
		case !inA:
			d.Kind = KindMissingA
		case !inB:
			d.Kind = KindMissingB
		}
		switch {
		case extra[name]:
			d.Ignored, d.Reason = true, "--ignore-header "+name
		case !opts.StrictHeaders && noise.DefaultIgnoredHeader(name):
			d.Ignored, d.Reason = true, "default header noise"
		}
		diffs = append(diffs, d)
	}
	return diffs
}

// Bodies compares two response bodies. When both parse as JSON the
// comparison is structural; otherwise it falls back to byte equality
// with a first-divergence report.
func Bodies(a, b []byte, opts Options) []Difference {
	aJSON, bJSON := jsonval.Looks(a), jsonval.Looks(b)
	switch {
	case aJSON && bJSON:
		va, _ := jsonval.Parse(a)
		vb, _ := jsonval.Parse(b)
		w := walker{opts: opts}
		w.compare(nil, va, vb)
		return w.diffs
	case aJSON != bJSON:
		d := Difference{
			Target: TargetBody, Path: "$", Kind: KindBodyFormat,
			A: describeBody(a, aJSON), B: describeBody(b, bJSON),
		}
		return applyPathFilters([]Difference{d}, nil, opts)
	default:
		return textBodies(a, b)
	}
}

func describeBody(body []byte, isJSON bool) string {
	if isJSON {
		return "JSON document"
	}
	if len(body) == 0 {
		return "empty body"
	}
	return fmt.Sprintf("non-JSON body (%d bytes)", len(body))
}

// textBodies reports whether two non-JSON bodies differ, locating the
// first divergent line to make the report actionable.
func textBodies(a, b []byte) []Difference {
	if string(a) == string(b) {
		return nil
	}
	line := 1
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			break
		}
		if a[i] == '\n' {
			line++
		}
	}
	return []Difference{{
		Target: TargetBody,
		Path:   fmt.Sprintf("line %d", line),
		Kind:   KindText,
		A:      fmt.Sprintf("%d bytes", len(a)),
		B:      fmt.Sprintf("%d bytes", len(b)),
	}}
}

// walker carries state for one structural body comparison.
type walker struct {
	opts  Options
	diffs []Difference
}

// record appends a difference after running it through the path
// filters and (for same-type value differences) the noise classifiers.
func (w *walker) record(segs []jsonpath.Seg, d Difference) {
	d.Target = TargetBody
	d.Path = jsonpath.Render(segs)
	if !d.Ignored {
		if p, ok := jsonpath.AnyMatches(w.opts.IgnorePaths, segs); ok {
			d.Ignored, d.Reason = true, "--ignore "+p.String()
		}
	}
	w.diffs = append(w.diffs, d)
}

func (w *walker) compare(segs []jsonpath.Seg, a, b any) {
	ta, tb := jsonval.TypeName(a), jsonval.TypeName(b)
	if ta != tb {
		w.record(segs, Difference{
			Kind: KindType,
			A:    fmt.Sprintf("%s %s", ta, jsonval.Snippet(a)),
			B:    fmt.Sprintf("%s %s", tb, jsonval.Snippet(b)),
		})
		return
	}
	switch va := a.(type) {
	case map[string]any:
		w.compareObjects(segs, va, b.(map[string]any))
	case []any:
		w.compareArrays(segs, va, b.([]any))
	case string:
		w.compareStrings(segs, va, b.(string))
	case json.Number:
		w.compareNumbers(segs, va, b.(json.Number))
	case bool:
		if va != b.(bool) {
			w.record(segs, Difference{Kind: KindValue, A: jsonval.Snippet(a), B: jsonval.Snippet(b)})
		}
	case nil:
		// both null: equal
	}
}

func (w *walker) compareObjects(segs []jsonpath.Seg, a, b map[string]any) {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	for _, k := range sorted {
		child := append(append([]jsonpath.Seg(nil), segs...), jsonpath.Key(k))
		va, inA := a[k]
		vb, inB := b[k]
		switch {
		case !inA:
			w.record(child, Difference{Kind: KindMissingA, B: jsonval.Snippet(vb)})
		case !inB:
			w.record(child, Difference{Kind: KindMissingB, A: jsonval.Snippet(va)})
		default:
			w.compare(child, va, vb)
		}
	}
}

func (w *walker) compareArrays(segs []jsonpath.Seg, a, b []any) {
	if _, unordered := anyExact(w.opts.UnorderedPaths, segs); unordered {
		w.compareUnordered(segs, a, b)
		return
	}
	if len(a) != len(b) {
		w.record(segs, Difference{
			Kind: KindLength,
			A:    fmt.Sprintf("%d elements", len(a)),
			B:    fmt.Sprintf("%d elements", len(b)),
		})
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		child := append(append([]jsonpath.Seg(nil), segs...), jsonpath.Index(i))
		w.compare(child, a[i], b[i])
	}
	// Surplus elements on the longer side are reported individually so
	// the report says WHAT is extra, not just that lengths differ.
	for i := n; i < len(a); i++ {
		child := append(append([]jsonpath.Seg(nil), segs...), jsonpath.Index(i))
		w.record(child, Difference{Kind: KindMissingB, A: jsonval.Snippet(a[i])})
	}
	for i := n; i < len(b); i++ {
		child := append(append([]jsonpath.Seg(nil), segs...), jsonpath.Index(i))
		w.record(child, Difference{Kind: KindMissingA, B: jsonval.Snippet(b[i])})
	}
}

// compareUnordered treats the two arrays as multisets keyed by
// canonical rendering: same elements in any order is parity.
func (w *walker) compareUnordered(segs []jsonpath.Seg, a, b []any) {
	counts := map[string]int{}
	byKey := map[string]any{}
	for _, e := range a {
		k := jsonval.Canonical(e)
		counts[k]++
		byKey[k] = e
	}
	for _, e := range b {
		k := jsonval.Canonical(e)
		counts[k]--
		byKey[k] = e
	}
	keys := make([]string, 0, len(counts))
	for k, c := range counts {
		if c != 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		c := counts[k]
		d := Difference{Kind: KindMissingB, A: jsonval.Snippet(byKey[k])}
		if c < 0 {
			d = Difference{Kind: KindMissingA, B: jsonval.Snippet(byKey[k])}
			c = -c
		}
		for i := 0; i < c; i++ {
			w.record(segs, d)
		}
	}
}

func (w *walker) compareStrings(segs []jsonpath.Seg, a, b string) {
	if a == b {
		return
	}
	d := Difference{Kind: KindValue, A: jsonval.Snippet(a), B: jsonval.Snippet(b)}
	switch {
	case w.opts.IgnoreTimestamps && noise.IsTimestamp(a) && noise.IsTimestamp(b):
		d.Ignored, d.Reason = true, "timestamp noise"
	case w.opts.IgnoreIDs && noise.SameIDNoise(a, b):
		d.Ignored, d.Reason = true, "id noise"
	}
	w.record(segs, d)
}

func (w *walker) compareNumbers(segs []jsonpath.Seg, a, b json.Number) {
	if jsonval.NumbersEqual(a, b) {
		return
	}
	d := Difference{Kind: KindValue, A: a.String(), B: b.String()}
	if w.opts.IgnoreTimestamps && noise.IsTimestamp(a) && noise.IsTimestamp(b) {
		d.Ignored, d.Reason = true, "timestamp noise"
	}
	w.record(segs, d)
}

// applyPathFilters marks differences ignored when a pattern matches;
// used for differences created outside the walker.
func applyPathFilters(diffs []Difference, segs []jsonpath.Seg, opts Options) []Difference {
	for i := range diffs {
		if diffs[i].Ignored {
			continue
		}
		if p, ok := jsonpath.AnyMatches(opts.IgnorePaths, segs); ok {
			diffs[i].Ignored, diffs[i].Reason = true, "--ignore "+p.String()
		}
	}
	return diffs
}

func anyExact(patterns []jsonpath.Pattern, segs []jsonpath.Seg) (jsonpath.Pattern, bool) {
	for _, p := range patterns {
		if p.MatchesExact(segs) {
			return p, true
		}
	}
	return jsonpath.Pattern{}, false
}
