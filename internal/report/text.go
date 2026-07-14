// Terminal text rendering: aligned difference tables, per-request
// verdicts, and a run summary. No ANSI colors — output is meant to be
// grep-able and byte-stable for a given set of responses.
package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/JaydenCJ/twinget/internal/diff"
)

// Text renders results for humans. In batch mode (more than one
// result, or forced by the caller) it prints one verdict line per
// request followed by detail blocks for requests that diverged.
func Text(w io.Writer, meta Meta, results []diff.RequestResult) {
	if len(results) == 1 {
		writeDetail(w, meta, results[0])
		return
	}
	for _, r := range results {
		effective, ignored := r.Counts()
		verdict := "ok  "
		note := "parity"
		if !r.Parity() {
			verdict = "DIFF"
			note = plural(effective, "difference")
		}
		if ignored > 0 {
			note += fmt.Sprintf(" (%d ignored)", ignored)
		}
		fmt.Fprintf(w, "%s  %-7s %-32s %s\n", verdict, r.Method, r.Path, note)
	}
	for _, r := range results {
		if !r.Parity() || (meta.ShowIgnored && countIgnored(r) > 0) {
			fmt.Fprintln(w)
			writeDetail(w, meta, r)
		}
	}
	s := Summarize(results)
	overall := "OK"
	if s.Diff > 0 {
		overall = "FAIL"
	}
	fmt.Fprintf(w, "\n%s: %d parity, %d diff — %s\n",
		plural(s.Requests, "request"), s.Parity, s.Diff, overall)
}

// writeDetail prints the full block for one request.
func writeDetail(w io.Writer, meta Meta, r diff.RequestResult) {
	fmt.Fprintf(w, "twinget diff %s %s\n", r.Method, r.Path)
	fmt.Fprintf(w, "  a: %s  %d (%s, %.1f ms)\n",
		r.A.URL, r.A.Status, byteCount(r.A.BodyBytes), r.A.DurationMS)
	fmt.Fprintf(w, "  b: %s  %d (%s, %.1f ms)\n",
		r.B.URL, r.B.Status, byteCount(r.B.BodyBytes), r.B.DurationMS)

	shown := visible(r, meta.ShowIgnored)
	if len(shown) > 0 {
		fmt.Fprintln(w)
		writeDiffTable(w, shown)
	}

	effective, ignored := r.Counts()
	fmt.Fprintln(w)
	if r.Parity() {
		fmt.Fprintf(w, "result: PARITY%s\n", ignoredNote(ignored))
	} else {
		fmt.Fprintf(w, "result: DIFF — %s%s\n",
			plural(effective, "difference"), ignoredNote(ignored))
	}
}

// visible selects the differences to print: effective ones always,
// ignored ones only with --show-ignored.
func visible(r diff.RequestResult, showIgnored bool) []diff.Difference {
	var out []diff.Difference
	for _, d := range r.Differences {
		if !d.Ignored || showIgnored {
			out = append(out, d)
		}
	}
	return out
}

// writeDiffTable prints differences with aligned columns.
func writeDiffTable(w io.Writer, diffs []diff.Difference) {
	pathWidth, kindWidth := 4, 4
	for _, d := range diffs {
		if n := len(location(d)); n > pathWidth {
			pathWidth = n
		}
		if n := len(kindLabel(d.Kind)); n > kindWidth {
			kindWidth = n
		}
	}
	if pathWidth > 44 {
		pathWidth = 44
	}
	for _, d := range diffs {
		fmt.Fprintf(w, "  %-*s  %-*s  %s\n",
			pathWidth, location(d), kindWidth, kindLabel(d.Kind), sides(d))
	}
}

// location renders where a difference lives, prefixing non-body
// targets so status/header rows read naturally next to JSON paths.
func location(d diff.Difference) string {
	switch d.Target {
	case diff.TargetHeader:
		return "header " + d.Path
	case diff.TargetStatus:
		return "status"
	default:
		return d.Path
	}
}

// sides renders the a/b value columns, omitting absent sides.
func sides(d diff.Difference) string {
	var parts []string
	switch d.Kind {
	case diff.KindMissingA:
		parts = append(parts, "b: "+d.B)
	case diff.KindMissingB:
		parts = append(parts, "a: "+d.A)
	default:
		parts = append(parts, "a: "+d.A, "b: "+d.B)
	}
	if d.Ignored {
		parts = append(parts, "(ignored: "+d.Reason+")")
	}
	return strings.Join(parts, "  ")
}

func countIgnored(r diff.RequestResult) int {
	_, ignored := r.Counts()
	return ignored
}

func ignoredNote(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d ignored as noise)", n)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func byteCount(n int) string {
	return fmt.Sprintf("%d B", n)
}
