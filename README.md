# Trading System Control Panel

Desktop control panel for the **Aylab Trading System** (TES + AGENT). Runs as a small Go service alongside the trading stack, exposes a clean dashboard / shutdown UI, and is fully decoupled from the upstream trading code — it only calls **public HTTP routes** of `trade-execution-system` and reads from the shared MongoDB / Redis.

```
┌────────────── This repo ──────────────┐
│  ┌──────────────────────────────────┐  │
│  │  Browser / Desktop window        │  │
│  │  ui/index.html                   │  │
│  └────────────────┬─────────────────┘  │
│                   │ HTTP                │
│  ┌────────────────▼─────────────────┐  │
│  │  Sidecar (Go)         :8081      │  │
│  │  • cancel / close fan-out        │  │
│  │  • dashboard aggregation         │  │
│  └─┬──────┬─────────────────────────┘  │
└────┼──────┼────────────────────────────┘
     │      │
     │ HTTP │ direct read
     ▼      ▼
  TES :8080   Mongo :27017
  (Aylab)     Redis :6379
              (shared)
```

## What this is — and what it isn't

- ✅ **Read-mostly UI** for ops: shows per-exchange open orders / positions / balances / PnL since last start; lets the operator cancel all open orders or close all open positions in one click.
- ✅ **Fully independent** from `trade-execution-system-main` and `trading-agent-main`. This repo never imports their Go modules and never copies their source. They are external dependencies reached over HTTP/Mongo/Redis at runtime.
- ❌ **Not** a trading engine. It does not place new orders, run strategies, or call exchange REST APIs directly.
- ❌ **Not** a TES/AGENT replacement. Those must be running for the panel to show useful data.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET`  | `/health` | Liveness |
| `GET`  | `/api/v1/system/status` | UI state pill + uptime |
| `GET`  | `/api/v1/dashboard/stats` | Per-exchange open orders / positions / balance |
| `GET`  | `/api/v1/dashboard/pnl?since=<unix>` | Realized + unrealized PnL since `max(since, started_at)` |
| `POST` | `/admin/start` | UI Start → `state=running`, reset `started_at` |
| `POST` | `/admin/stop`  | UI Shut Down (final step) → `state=stopped` |
| `POST` | `/admin/cancel-all-orders` | Scan Redis open orders → fan-out to TES `/api/v1/{ex}/{market}/cancel` |
| `POST` | `/admin/close-all-positions` | Scan Mongo open positions → fan-out to TES `/api/v1/{ex}/futures/close` (or `/spot/sell` for long unwinds) |

## Prerequisites

To run the panel against real data you need the following already running on your network (locally or remote):

- **trade-execution-system** at `http://localhost:8080` — Aylab's TES
- **MongoDB** at `mongodb://localhost:27017` (database `crypto_trading`)
- **Redis** at `localhost:6379`
- (Optional) **trading-agent** — only needed if you actually want orders/positions to appear; the panel works fine against an empty DB

For installing TES + AGENT, see those repos directly.

## Quick start — development mode (no TES required)

The repo ships a tiny mock backend that emulates the TES surface, so you can iterate on the UI with **zero infrastructure**.

```bash
# Terminal 1 — mock backend
cd dev/mock-backend
python3 server.py             # listens on :9999

# Terminal 2 — UI static server
python3 -m http.server 5180 --directory ui

# Browser
open http://localhost:5180/   # defaults to http://localhost:9999 (mock)
```

Click around: shutdown sequence, manual recovery buttons, dashboard refresh — all wired up against the mock.

## Production mode — connect to real TES

```bash
# 1. Ensure TES + Mongo + Redis are running. Example (macOS / Homebrew):
brew services start mongodb-community
brew services start redis
# (start TES per its own README)

# 2. Build & run the sidecar
go build -o control-panel .
./control-panel               # listens on :8081

# 3. Serve the UI (any static server)
python3 -m http.server 5180 --directory ui

# 4. Browser
open "http://localhost:5180/?api=http://localhost:8081"
```

### Configuration (env vars)

| Variable | Default | Purpose |
|---|---|---|
| `CONTROL_PORT`  | `8081`                       | Sidecar HTTP port |
| `MONGO_URI`     | `mongodb://localhost:27017`  | MongoDB connection string |
| `MONGO_DATABASE`| `crypto_trading`             | Database name |
| `REDIS_ADDR`    | `localhost:6379`             | Redis address |
| `TES_BASE_URL`  | `http://localhost:8080`      | Trade-execution-system base URL |

Mongo / Redis are **best-effort**: dashboard returns zeros instead of 500 if either is unreachable, so the panel remains usable while you debug infra.

## Test

```bash
go test ./...
```

All tests are unit-level, use `httptest` to fake TES, and don't require Mongo / Redis. CI-friendly.

## Roadmap

- [ ] Wails-based native desktop window (`.app` / `.exe`)
- [ ] GitHub Actions: cross-platform build + Releases on tag push
- [ ] Optional auth on admin endpoints
- [ ] System tray icon (close to tray vs quit)

## Source isolation rule (hard)

This repo **must not modify** `trade-execution-system-main` or `trading-agent-main`. Every interaction with them is via HTTP at runtime. Schemas the sidecar reads (`TradeData`, `Position`) are redeclared locally in [`models.go`](./models.go) — only the fields actually consumed.

If a future feature seems to require editing TES or AGENT source: stop, rethink. The HTTP API is the contract.

## License

MIT — see [LICENSE](./LICENSE).
