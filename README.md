# LRO (Lightning Routing Optimizer)

A standalone **experimental developer utility** for routing experiments on top of an existing LND node, now with invoice-aware optimize/send aliases, profile-driven runs, and JSONL attempt logging.

## Scope

This tool is intentionally narrow:
- ✅ Connects to an existing LND node over gRPC.
- ✅ Fetches candidate routes (`QueryRoutes`).
- ✅ Applies custom reranking heuristics (fees, mission control probability, local failure history).
- ✅ Optionally attempts payment with selected route (`SendToRouteV2`).

Not included:
- ❌ Wallet features
- ❌ Channel management
- ❌ Full payment processor behavior
- ❌ Running a Lightning node

## Commands

- `health` - check LND connectivity
- `routes` - query/rerank candidate routes
- `optimize` - alias of `routes` (invoice-friendly)
- `send-route` - rerank and attempt payment over selected route
- `send` - alias of `send-route` (invoice-friendly)
- `batch-send` - process multiple invoices concurrently
- `report` - summarize JSONL attempt logs

## Build

```bash
go mod tidy
go build ./cmd/lro
```

## Usage

### 1) Health check

```bash
./lro health \
  --lnd-host localhost:10009 \
  --tls-cert ~/.lnd/tls.cert \
  --macaroon ~/.lnd/data/chain/bitcoin/regtest/admin.macaroon
```

### 2) Optimize routes from invoice (safe)

```bash
./lro optimize \
  --profile ./profile.json \
  --invoice <bolt11> \
  --num-routes 10 \
  --avoid-failures-hours 24 \
  --attempt-log .lro-attempts.jsonl \
  --json
```

### 3) Send using optimized path (or dry-run)

```bash
./lro send \
  --profile ./profile.json \
  --invoice <bolt11> \
  --pick-rank 1 \
  --dry-run \
  --failure-log .lro-failures.json \
  --attempt-log .lro-attempts.jsonl
```


### 4) Batch send / batch dry-run (concurrent)

Create `invoices.txt` with one BOLT11 invoice per line, then:

```bash
./lro batch-send \
  --profile ./profile.json \
  --invoices-file ./invoices.txt \
  --workers 3 \
  --dry-run \
  --pick-rank 1 \
  --attempt-log .lro-attempts.jsonl
```

### 5) Attempt report

```bash
./lro report --attempt-log .lro-attempts.jsonl --json
```

## Profile format (optional)

```json
{
  "lnd_host": "localhost:10009",
  "tls_cert": "/home/user/.lnd/tls.cert",
  "macaroon": "/home/user/.lnd/data/chain/bitcoin/regtest/admin.macaroon",
  "num_routes": 10,
  "pick_rank": 1,
  "w_fee": 1.0,
  "w_fail": 4000.0,
  "w_prob": 2000.0,
  "avoid_failures_hours": 24,
  "failure_log": ".lro-failures.json",
  "attempt_log": ".lro-attempts.jsonl"
}
```

CLI flags override profile values.

## Local dev recommendation

Use Polar for regtest experimentation and connect this CLI to one LND node.

## Current status and what's left

Implemented now:
- ✅ LND connectivity health command.
- ✅ Invoice-aware optimize/send flow (`--invoice`).
- ✅ Route querying + reranking with custom weights and recent-failure window.
- ✅ Optional route execution with rank selection (`--pick-rank`) and safe dry-run mode (`--dry-run`).
- ✅ Local failure-history tracking to penalize unreliable channels over time.
- ✅ Structured JSONL attempt logging for route/sending outcomes.
- ✅ Reproducible profile-driven runs (`--profile`).
- ✅ Reporting over attempt logs (`report`).
- ✅ Concurrency mode for batch/multi-payment experiments (`batch-send`, worker pool).

Still left (next phases):
- ⏳ Optional lightweight proxy mode (interceptor/wrapper behavior).
- ⏳ Streaming payment lifecycle tracking/fallback routing.

## Security notes

- Never commit TLS certs or macaroon files.
- `--insecure-tls` is for development only.
