# LRO - Lightning Routing Optimizer

**LRO** is an experimental routing optimization tool for the Lightning Network. It connects to an existing LND node, queries candidate payment routes, and applies custom scoring heuristics to select optimal paths based on fees, success probability, and channel reliability.

## Features

- **Smart Route Selection** - Reranks routes using configurable weights for fees, failure history, and mission control probability
- **Invoice-Aware** - Decode BOLT11 invoices to automatically extract destination and amount
- **Dry-Run Mode** - Test routing decisions without executing payments
- **Batch Processing** - Process multiple invoices concurrently with worker pools
- **Analytics** - JSONL logging and reporting for route performance analysis
- **Profile-Driven** - Save credentials and preferences for reproducible experiments

## Quick Start

### Build

```bash
go build ./cmd/lro
```

### Connect to LND

```bash
./lro health \
  --lnd-host localhost:10009 \
  --tls-cert ~/.lnd/tls.cert \
  --macaroon ~/.lnd/data/chain/bitcoin/regtest/admin.macaroon
```

### Find Best Route

```bash
./lro optimize \
  --lnd-host localhost:10009 \
  --tls-cert ~/.lnd/tls.cert \
  --macaroon ~/.lnd/data/chain/bitcoin/regtest/admin.macaroon \
  --invoice <BOLT11_INVOICE> \
  --json
```

## Usage

### Profile Configuration (Recommended)

Create `profile.json` to avoid repeating credentials:

```json
{
  "lnd_host": "127.0.0.1:10009",
  "tls_cert": "/path/to/tls.cert",
  "macaroon": "/path/to/admin.macaroon",
  "num_routes": 10,
  "pick_rank": 1,
  "avoid_failures_hours": 24
}
```

Then use with:

```bash
./lro optimize --profile ./profile.json --invoice <BOLT11> --json
```

### Commands

| Command | Description |
|---------|-------------|
| `health` | Verify LND connectivity |
| `optimize` | Query and rank routes for an invoice |
| `send` | Execute payment using optimized route |
| `batch-send` | Process multiple invoices concurrently |
| `report` | Analyze attempt logs |

### Examples

**Test route selection (safe):**
```bash
./lro send --profile ./profile.json --invoice <BOLT11> --pick-rank 1 --dry-run
```

**Batch process invoices:**
```bash
# Create invoices.txt with one BOLT11 per line
./lro batch-send --profile ./profile.json --invoices-file ./invoices.txt --workers 3 --dry-run
```

**View analytics:**
```bash
./lro report --attempt-log .lro-attempts.jsonl --json
```

## Development Setup

For local testing, use [Polar](https://lightningpolar.com/) to create a regtest Lightning Network with LND nodes.

1. Install and launch Polar
2. Create a network with LND nodes
3. Start the network and open channels
4. Extract credentials from `~/.polar/networks/<id>/volumes/lnd/<node>/`
5. Test LRO against your local node

## Configuration

### Route Scoring Weights

Customize route selection by adjusting weights in your profile:

- `w_fee` (default: 1.0) - Penalty for higher fees
- `w_fail` (default: 4000.0) - Penalty for channels with recent failures  
- `w_prob` (default: 2000.0) - Boost for higher mission control probability
- `avoid_failures_hours` (default: 24) - Only penalize recent failures

### Flags

All profile options can be overridden via CLI flags. Run `./lro <command> --help` for details.

## Security

⚠️ **Never commit credentials to version control**
- Keep TLS certificates and macaroons private
- Use `--insecure-tls` only for development
- Store sensitive data outside the repository

## License

Experimental tool - use at your own risk.
