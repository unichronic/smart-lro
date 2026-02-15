# LRO (Lightning Routing Optimizer)

A standalone **experimental developer utility** for routing experiments on top of an existing LND node.

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

## Phase-by-phase MVP

1. **Phase 1 - LND connectivity**
   - `lro health`
2. **Phase 2 - Route collection and scoring**
   - `lro routes`
3. **Phase 3 - Optional route execution**
   - `lro send-route`

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

### 2) Query + rerank routes

```bash
./lro routes \
  --dest <33-byte-node-pubkey-hex> \
  --amt-sat 5000 \
  --num-routes 10 \
  --failure-log .lro-failures.json
```

Optional scoring weights:
- `--w-fee`
- `--w-fail`
- `--w-prob`

JSON output mode:

```bash
./lro routes --dest <pubkeyhex> --amt-sat 5000 --json
```

### 3) Send via top ranked route

```bash
./lro send-route \
  --dest <pubkeyhex> \
  --amt-sat 5000 \
  --payment-hash <32-byte-hash-hex> \
  --failure-log .lro-failures.json
```

## Local dev recommendation

Use Polar for regtest experimentation and connect this CLI to one LND node.


## Current status and what's left

Implemented now:
- ✅ LND connectivity health command.
- ✅ Route querying + reranking with custom weights.
- ✅ Optional route execution with rank selection (`--pick-rank`) and safe dry-run mode (`--dry-run`).
- ✅ Local failure-history tracking to penalize unreliable channels over time.

Still left (next phases):
- ⏳ Optional lightweight proxy mode (interceptor/wrapper behavior).
- ⏳ Better persistence/analytics (attempt logs, route outcomes, aggregate stats).
- ⏳ Config profiles and richer output formats for experiment reproducibility.
- ⏳ Integration tests against a live Polar network.

## Security notes

- Never commit TLS certs or macaroon files.
- `--insecure-tls` is for development only.
