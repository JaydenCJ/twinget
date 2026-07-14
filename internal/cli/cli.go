// Package cli parses arguments and drives a twinget run. It is fully
// in-process testable: Run takes an argv slice plus writers and
// returns the process exit code.
//
// Exit codes: 0 parity, 1 differences found, 2 usage error,
// 3 transport/runtime failure.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/JaydenCJ/twinget/internal/jsonpath"
	"github.com/JaydenCJ/twinget/internal/version"
)

// Exit codes.
const (
	ExitParity  = 0
	ExitDiff    = 1
	ExitUsage   = 2
	ExitRuntime = 3
)

const usage = `twinget — send one request to two backends and diff the answers

Usage:
  twinget diff  [flags] /path        compare one request
  twinget batch [flags] FILE         compare every request in a batch file
  twinget version                    print the version

Required flags:
  --a URL             base URL of backend A (the reference)
  --b URL             base URL of backend B (the candidate)

Request flags (diff):
  -X, --method M      HTTP method (default GET)
  -H, --header 'K: V' extra request header (repeatable)
  -d, --body STRING   request body
  --body-file FILE    request body from a file

Noise filters:
  --ignore PATTERN    ignore a JSON path, e.g. '$.meta.**' (repeatable)
  --unordered PATTERN compare an array as a multiset (repeatable)
  --ignore-timestamps treat two timestamp-shaped values as equal
  --ignore-ids        treat two same-shaped ids (uuid/ulid/hex/prefixed) as equal
  --ignore-header N   ignore a response header by name (repeatable)
  --strict-headers    disable the built-in volatile-header ignore list

Output and transport:
  --format F          text | json | markdown (default text)
  --show-ignored      list noise-suppressed differences too
  --timeout D         per-request timeout (default 10s)
  --max-body-size N   per-side response body cap in bytes (default 10485760)

Exit codes: 0 parity · 1 differences · 2 usage error · 3 transport failure
`

// multiFlag collects repeatable string flags.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// options holds every parsed flag for one run.
type options struct {
	baseA, baseB string
	method       string
	headers      multiFlag
	body         string
	bodyFile     string

	ignore           multiFlag
	unordered        multiFlag
	ignoreTimestamps bool
	ignoreIDs        bool
	ignoreHeaders    multiFlag
	strictHeaders    bool

	format      string
	showIgnored bool
	timeout     time.Duration
	maxBodySize int64

	ignorePatterns    []jsonpath.Pattern
	unorderedPatterns []jsonpath.Pattern
}

// newFlagSet wires the shared flag surface for a subcommand.
func newFlagSet(name string, opts *options) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&opts.baseA, "a", "", "")
	fs.StringVar(&opts.baseB, "b", "", "")
	fs.StringVar(&opts.method, "method", "GET", "")
	fs.StringVar(&opts.method, "X", "GET", "")
	fs.Var(&opts.headers, "header", "")
	fs.Var(&opts.headers, "H", "")
	fs.StringVar(&opts.body, "body", "", "")
	fs.StringVar(&opts.body, "d", "", "")
	fs.StringVar(&opts.bodyFile, "body-file", "", "")

	fs.Var(&opts.ignore, "ignore", "")
	fs.Var(&opts.unordered, "unordered", "")
	fs.BoolVar(&opts.ignoreTimestamps, "ignore-timestamps", false, "")
	fs.BoolVar(&opts.ignoreIDs, "ignore-ids", false, "")
	fs.Var(&opts.ignoreHeaders, "ignore-header", "")
	fs.BoolVar(&opts.strictHeaders, "strict-headers", false, "")

	fs.StringVar(&opts.format, "format", "text", "")
	fs.BoolVar(&opts.showIgnored, "show-ignored", false, "")
	fs.DurationVar(&opts.timeout, "timeout", 10*time.Second, "")
	fs.Int64Var(&opts.maxBodySize, "max-body-size", 10*1024*1024, "")
	return fs
}

// validate checks cross-flag invariants and compiles patterns.
func (o *options) validate() error {
	if o.baseA == "" || o.baseB == "" {
		return fmt.Errorf("both --a and --b base URLs are required")
	}
	for _, base := range []string{o.baseA, o.baseB} {
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			return fmt.Errorf("base URL %q must start with http:// or https://", base)
		}
	}
	switch o.format {
	case "text", "json", "markdown":
	default:
		return fmt.Errorf("unknown --format %q (want text, json or markdown)", o.format)
	}
	if o.body != "" && o.bodyFile != "" {
		return fmt.Errorf("--body and --body-file are mutually exclusive")
	}
	if o.timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	if o.maxBodySize <= 0 {
		return fmt.Errorf("--max-body-size must be positive")
	}
	for _, raw := range o.ignore {
		p, err := jsonpath.ParsePattern(raw)
		if err != nil {
			return fmt.Errorf("--ignore: %v", err)
		}
		o.ignorePatterns = append(o.ignorePatterns, p)
	}
	for _, raw := range o.unordered {
		p, err := jsonpath.ParsePattern(raw)
		if err != nil {
			return fmt.Errorf("--unordered: %v", err)
		}
		o.unorderedPatterns = append(o.unorderedPatterns, p)
	}
	return nil
}

// Run executes twinget with argv (excluding the program name) and
// returns the exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}
	switch args[0] {
	case "version", "--version", "-version":
		fmt.Fprintf(stdout, "twinget %s\n", version.Version)
		return ExitParity
	case "help", "--help", "-h", "-help":
		fmt.Fprint(stdout, usage)
		return ExitParity
	case "diff":
		return runSubcommand("diff", args[1:], stdout, stderr)
	case "batch":
		return runSubcommand("batch", args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "twinget: unknown command %q\n\n%s", args[0], usage)
		return ExitUsage
	}
}

// runSubcommand parses flags for diff/batch and hands off to the
// executor in run.go.
func runSubcommand(name string, args []string, stdout, stderr io.Writer) int {
	var opts options
	fs := newFlagSet(name, &opts)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			return ExitParity
		}
		fmt.Fprintf(stderr, "twinget %s: %v\n", name, err)
		return ExitUsage
	}
	if err := opts.validate(); err != nil {
		fmt.Fprintf(stderr, "twinget %s: %v\n", name, err)
		return ExitUsage
	}
	if fs.NArg() != 1 {
		hint := ""
		for _, a := range fs.Args() {
			if strings.HasPrefix(a, "-") {
				hint = " (flags must come before the positional argument)"
				break
			}
		}
		fmt.Fprintf(stderr, "twinget %s: want exactly one argument, got %d%s\n",
			name, fs.NArg(), hint)
		return ExitUsage
	}
	arg := fs.Arg(0)

	var (
		code int
		err  error
	)
	if name == "diff" {
		code, err = runDiff(&opts, arg, stdout)
	} else {
		code, err = runBatch(&opts, arg, stdout)
	}
	if err != nil {
		fmt.Fprintf(stderr, "twinget %s: %v\n", name, err)
		return code
	}
	return code
}
