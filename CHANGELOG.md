# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-12

### Added

- `diff` subcommand: mirrors one request (method, headers, body, query
  string) to two base URLs concurrently and structurally compares
  status code, headers, and body, with redirects deliberately not
  followed.
- Structural JSON diff on `json.Number` trees: value/type/missing
  kinds, full JSONPath locations (`$.users[2].email`), array length
  plus per-element surplus reporting, and numeric equality across
  spellings (`1.0` == `1`, `1e3` == `1000`).
- Noise filters that record what they suppress instead of discarding
  it: `--ignore PATTERN` path filters (literal, `*`, `[*]`, `**`,
  prefix semantics), `--ignore-timestamps` (RFC 3339 / SQL / RFC 1123
  strings and epoch numbers in s/ms/µs/ns), `--ignore-ids` (UUID,
  ULID, fixed-width hex, same-prefix Stripe-style ids), and
  `--unordered PATTERN` multiset array comparison.
- Header comparison with a documented 21-entry volatile-header ignore
  list, `--ignore-header NAME`, and `--strict-headers`.
- Non-JSON fallbacks: byte comparison with first-divergent-line
  location for text bodies, and an explicit `body_format` difference
  when only one side is JSON.
- `batch` subcommand sweeping a request file (plain `METHOD /path`
  lines or JSON objects with headers/bodies) with per-request verdicts
  and a run summary.
- Output formats: aligned terminal text, versioned JSON
  (`schema_version: 1`) that always includes ignored differences for
  audit, and PR-ready Markdown tables; `--show-ignored` to list
  suppressed noise everywhere.
- Exit-code contract for pipelines: 0 parity, 1 differences, 2 usage
  error, 3 transport failure (naming the failing side).
- Twin demo backends (`examples/demo-backends`) modelling a Node→Go
  rewrite with three planted regressions, a batch file example, and a
  noise-rules reference (`docs/noise-filters.md`).
- 90 deterministic offline tests (unit + in-process CLI integration
  against loopback servers) and `scripts/smoke.sh`.

[0.1.0]: https://github.com/JaydenCJ/twinget/releases/tag/v0.1.0
