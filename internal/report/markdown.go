// Markdown rendering, shaped for pasting into a PR or migration
// tracking issue: a verdict table, then one difference table per
// diverged request.
package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/JaydenCJ/twinget/internal/diff"
)

// Markdown writes the report as GitHub-flavored Markdown.
func Markdown(w io.Writer, meta Meta, results []diff.RequestResult) {
	s := Summarize(results)
	fmt.Fprintf(w, "## twinget parity report\n\n")
	fmt.Fprintf(w, "`a` = %s · `b` = %s\n\n", meta.BaseA, meta.BaseB)
	fmt.Fprintln(w, "| request | result | differences | ignored as noise |")
	fmt.Fprintln(w, "|---|---|---:|---:|")
	for _, r := range results {
		effective, ignored := r.Counts()
		verdict := "✅ parity"
		if !r.Parity() {
			verdict = "❌ diff"
		}
		fmt.Fprintf(w, "| `%s %s` | %s | %d | %d |\n",
			r.Method, r.Path, verdict, effective, ignored)
	}

	for _, r := range results {
		shown := visible(r, meta.ShowIgnored)
		if r.Parity() && !meta.ShowIgnored {
			continue
		}
		if len(shown) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n### %s %s\n\n", r.Method, r.Path)
		fmt.Fprintln(w, "| where | kind | a | b |")
		fmt.Fprintln(w, "|---|---|---|---|")
		for _, d := range shown {
			kind := kindLabel(d.Kind)
			if d.Ignored {
				kind += " (ignored)"
			}
			fmt.Fprintf(w, "| %s | %s | %s | %s |\n",
				mdCode(location(d)), kind, mdCode(d.A), mdCode(d.B))
		}
	}

	fmt.Fprintf(w, "\n**%s: %d parity, %d diff.**\n",
		plural(s.Requests, "request"), s.Parity, s.Diff)
}

// mdCode wraps a value in backticks, escaping pipes so tables survive.
func mdCode(s string) string {
	if s == "" {
		return "—"
	}
	s = strings.ReplaceAll(s, "|", "\\|")
	return "`" + s + "`"
}
