# Mock backend (development only)

Zero-dependency Python stub that emulates the **TES + sidecar** HTTP surface in a single process so you can iterate on the UI without spinning up MongoDB, Redis, or the real trading stack.

## Run

```bash
python3 server.py            # default :9999
PORT=8888 python3 server.py  # custom port
```

Then open `../../ui/index.html` (served via any static HTTP server) — `API_BASE` defaults to `http://localhost:9999`.

## What it fakes

- `GET /health`, `/api/v1/system/status`
- `GET /api/v1/dashboard/stats`, `/api/v1/dashboard/pnl`
- `POST /admin/start`, `/admin/stop`
- `POST /admin/cancel-all-orders`, `/admin/close-all-positions` (with artificial 1.5–2 s delay + occasional simulated failures, so the UI shutdown progress feels real)

## Not for production

This is a development convenience. **Do not deploy.** The real sidecar lives one directory up at the repo root.
