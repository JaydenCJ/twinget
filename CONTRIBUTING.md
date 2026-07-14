# Contributing to twinget

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go ≥1.22; nothing else. Tests and the smoke script only ever
talk to loopback servers they start themselves.

```bash
git clone https://github.com/JaydenCJ/twinget && cd twinget
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the CLI and the twin demo backends, runs
them on ephemeral 127.0.0.1 ports, and asserts on real output and exit
codes across every subcommand; it must finish by printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (90 deterministic tests, no external network).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   modules (only `internal/fetch` performs I/O — the diff engine,
   noise classifiers and renderers never touch the network).

## Ground rules

- Keep dependencies at zero; adding one needs strong justification in
  the PR.
- twinget sends requests only to the two base URLs the user names —
  never anywhere else, never at startup, no telemetry.
- Noise rules are contracts: any change to what `--ignore-timestamps`
  or `--ignore-ids` matches needs a table row in
  `docs/noise-filters.md` and tests for both the new positives and the
  neighbouring negatives.
- The JSON output is versioned: breaking field changes bump
  `schema_version` and get a CHANGELOG entry.
- Code comments and doc comments are written in English.
- Determinism first: the same pair of responses must render
  byte-identically, including all orderings (timings are the sole
  exception).

## Reporting bugs

Include the output of `twinget version`, the full command line, the
report output (redact URLs if needed), and — for wrong-diff reports —
the two response bodies as captured by `--format json`, since that
envelope contains everything the engine saw.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.
