# twinget examples

Everything here is offline and self-contained: the "backends" are two
tiny loopback servers shipped in this directory.

## demo-backends

Twin fake APIs modelling a real migration: backend A plays a legacy
Node service, backend B plays its Go rewrite. B carries four planted
regressions (a renamed enum value, a dropped field, a stringified
count, a lost route) plus realistic noise — fresh request ids and
timestamps on every response, different `Server` headers, a `charset`
difference in `Content-Type`.

```bash
go run ./examples/demo-backends --port-a 8801 --port-b 8802
# prints A=http://127.0.0.1:8801 and B=http://127.0.0.1:8802, then serves
```

With the backends running, in another terminal:

```bash
twinget diff --a http://127.0.0.1:8801 --b http://127.0.0.1:8802 /api/users
twinget diff --a http://127.0.0.1:8801 --b http://127.0.0.1:8802 \
  --ignore-timestamps --ignore-ids /api/users
```

The first command drowns you in noise; the second shows only the real
regressions. That contrast is the whole point of the tool.

## requests.txt

A batch file for `twinget batch`, demonstrating both line formats:
plain `METHOD /path` lines and JSON objects with query strings and
headers.

```bash
twinget batch --a http://127.0.0.1:8801 --b http://127.0.0.1:8802 \
  --ignore-timestamps --ignore-ids --ignore-header content-type \
  --ignore '$.uptime_s' examples/requests.txt
```

Exit code 1 and a `FAIL` summary as long as any regression remains —
wire it into a pre-deploy check and flip backends when it prints `OK`.

Ports are only fixed here for readability; both programs accept
ephemeral ports (`--port-a 0`), which is what `scripts/smoke.sh` uses.
