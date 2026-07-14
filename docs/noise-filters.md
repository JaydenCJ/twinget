# Noise filters — exact rules

twinget's value is proving *parity*, and parity is unprovable if every
response carries a fresh request id and timestamp. This document pins
down exactly what each filter neutralizes, because a noise filter that
is vague about its rules is a regression-hiding machine.

Two invariants hold for every filter below:

1. **Suppressed ≠ discarded.** An ignored difference is still recorded
   and counted; text output says `(N ignored as noise)`,
   `--show-ignored` lists each one with its reason, and JSON output
   always includes them with `"ignored": true`.
2. **Type changes are never masked.** `--ignore-timestamps` and
   `--ignore-ids` only apply when both sides have the same JSON type.
   A string timestamp that became an epoch number is a client-breaking
   change and always surfaces.

## `--ignore PATTERN` — path filters

Patterns use a deliberately tiny JSONPath dialect:

| Syntax | Meaning |
|---|---|
| `$.users[2].name` | literal keys and indexes (leading `$` optional) |
| `$["a.b"]` | bracket-quoted key for names with special characters |
| `*` | exactly one segment, key or index |
| `[*]` | exactly one array index |
| `**` | any run of segments, including none (`$..x` is accepted sugar) |

Matching is **prefix-based**: `--ignore $.meta` silences everything
under `$.meta`, which is what you want when a whole subtree is
diagnostics. `--unordered` uses **exact** matching instead, so
`--unordered $.items` relaxes ordering of that one array without
relaxing arrays nested inside its elements.

## `--ignore-timestamps`

A value difference is neutralized when **both** sides are
timestamp-shaped:

- strings parsing as RFC 3339 (with/without fractions or zone),
  `2006-01-02 15:04:05[.fff][±zz]` SQL style, bare dates
  (`2026-07-12`), or RFC 1123 HTTP dates;
- numbers (or pure-digit strings) in the Unix epoch ranges for
  seconds, milliseconds, microseconds or nanoseconds, bounded to the
  years 2001–2096. `42`, version-like strings (`2.4.1`) and bare years
  (`2026`) never match.

Format changes between two valid timestamp spellings (Node's
`.000Z` milliseconds vs Go's plain RFC 3339) are treated as noise —
that is precisely the churn a Node→Go rewrite produces.

## `--ignore-ids`

A value difference is neutralized when both sides match the **same**
identifier shape:

| Shape | Rule |
|---|---|
| `uuid` | canonical 8-4-4-4-12 hex, any case, any version |
| `ulid` | 26 Crockford-base32 chars, uppercase, at least one digit |
| `hex` | hex string of width 16/20/24/32/40/64 with at least one digit |
| `prefixed` | Stripe-style `pre_Xxxxxxxx…`: lowercase prefix (2–8), `_`, ≥8 alphanumerics incl. a digit — prefixes must be equal on both sides |

`req_…` vs `cus_…` is a real difference (the field changed meaning),
as is a UUID on one side and a Mongo ObjectId on the other. Purely
numeric ids (snowflakes) are indistinguishable from counters, so they
are deliberately out of scope — use a path filter for those.

## Header noise

Unless `--strict-headers` is set, a built-in list of 21 volatile
headers is skipped: per-response entropy (`x-request-id`,
`x-correlation-id`, `x-trace-id`, `traceparent`, `x-amzn-requestid`,
`x-amzn-trace-id`, `cf-ray`, `etag`, `set-cookie`), clocks (`date`,
`age`, `last-modified`, `x-runtime`, `x-response-time`), transport
plumbing (`content-length`, `transfer-encoding`, `connection`,
`keep-alive`), and deployment identity (`server`, `via`,
`x-powered-by`).

Contract-bearing headers — `Content-Type`, `Cache-Control`,
`Location`, `Allow`, `Vary`, CORS headers — are always compared.
`--ignore-header NAME` adds to the ignore set (and wins even under
`--strict-headers`); `--strict-headers` compares everything else.

## What filters cannot do

Filters classify *values*, not *fields*. If your API stores a
timestamp in a field that sometimes holds `"pending"`, the string case
will surface as a difference — correctly, since a client parsing it as
a date would crash. When in doubt, prefer an explicit `--ignore` on
the exact path: it is visible in the report's reasons, greppable in
JSON output, and reviewable in your pipeline definition.
