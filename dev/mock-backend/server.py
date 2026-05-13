#!/usr/bin/env python3
"""
Trading System — Mock Backend

Zero-dependency stub server that emulates BOTH the TES (port 8080-style)
and AGENT (port 8090-style) admin/dashboard endpoints on a single port.
Use this to demo the UI locally without Mongo / Redis / exchange keys.

Run:
    python3 server.py            # default :9999
    PORT=8888 python3 server.py  # custom port

Then open ../ui/index.html and ensure API_BASE matches the chosen port.
"""

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse, parse_qs
import json, time, random, os, threading

# ---------------- Mutable demo state ----------------
def _seed_exchanges():
    return {
        # open_orders, open_positions, closed_positions, open_usd, balance_usd
        "binance": [12, 3, 47, 24500.0, 84200.0],
        "bybit":   [ 5, 2, 31,  8900.0, 42100.0],
        "okx":     [ 8, 1, 22,  5200.0, 31800.0],
        "kraken":  [ 3, 0, 18,     0.0, 19500.0],
        "gate":    [ 6, 2, 29, 11400.0, 28900.0],
    }

STATE = {
    "system_state": "running",          # running | halted | stopped
    "started_at": time.time(),
    "pnl_base_realized": 1892.40,
    "pnl_base_unrealized": 1355.42,
    "frozen_pnl": None,                 # last realized at moment of stop; freezes total
    "exchanges": _seed_exchanges(),
}
LOCK = threading.Lock()


# ---------------- Helpers ----------------
def jitter(stats):
    """Tiny random walk so the dashboard feels alive."""
    new = {}
    for ex, (oo, op, cp, ousd, busd) in stats.items():
        # only nudge dollar values; counts change less often
        new[ex] = [
            max(0, oo + random.choice([-1, 0, 0, 0, 1])),
            max(0, op + random.choice([-1, 0, 0, 0, 0, 1])),
            cp + random.choice([0, 0, 0, 1]),
            max(0.0, round(ousd * random.uniform(0.995, 1.005), 2)),
            round(busd * random.uniform(0.999, 1.001), 2),
        ]
    return new


def aggregate(exchanges):
    total = [0, 0, 0, 0.0, 0.0]
    for v in exchanges.values():
        for i in range(5):
            total[i] += v[i]
    return total


def shape_stats():
    with LOCK:
        # Freeze numbers when system is stopped — no trading, no jitter.
        if STATE["system_state"] == "running":
            STATE["exchanges"] = jitter(STATE["exchanges"])
        ex_obj = {
            ex: {
                "open_orders":      v[0],
                "open_positions":   v[1],
                "closed_positions": v[2],
                "open_usd":         v[3],
                "balance_usd":      v[4],
            } for ex, v in STATE["exchanges"].items()
        }
        t = aggregate(STATE["exchanges"])
        total = {
            "open_orders": t[0], "open_positions": t[1], "closed_positions": t[2],
            "open_usd": round(t[3], 2), "balance_usd": round(t[4], 2),
        }
    return {"exchanges": ex_obj, "total": total}


def shape_pnl(since_ts: float):
    with LOCK:
        state    = STATE["system_state"]
        frozen   = STATE["frozen_pnl"]
        elapsed  = max(0.0, time.time() - max(since_ts, STATE["started_at"]))
    base = 200_000.0
    # When stopped, lock realized to its frozen value, unrealized = 0
    # (no open positions means nothing left to mark-to-market).
    if state == "stopped" and frozen is not None:
        total = round(frozen, 2)
        return {
            "realized":   total,
            "unrealized": 0.0,
            "total":      total,
            "percent":    round(total / base * 100, 4),
        }
    drift_r = elapsed * 0.05 + random.uniform(-2, 2)
    drift_u = elapsed * 0.03 + random.uniform(-3, 3)
    realized   = round(STATE["pnl_base_realized"]   + drift_r, 2)
    unrealized = round(STATE["pnl_base_unrealized"] + drift_u, 2)
    total      = round(realized + unrealized, 2)
    return {
        "realized":   realized,
        "unrealized": unrealized,
        "total":      total,
        "percent":    round(total / base * 100, 4),
    }


def shape_status():
    with LOCK:
        s = STATE["system_state"]
        started = STATE["started_at"]
    return {
        "state":      s,
        "started_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(started)),
        "uptime_sec": int(time.time() - started),
    }


# ---------------- HTTP handlers ----------------
class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        # cleaner one-line log
        print(f"[mock] {self.command} {self.path} → {args[1] if len(args) > 1 else ''}")

    def _cors(self):
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")

    def _json(self, code, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(code)
        self._cors()
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _read_json(self):
        n = int(self.headers.get("Content-Length", "0") or "0")
        if n <= 0:
            return {}
        try:
            return json.loads(self.rfile.read(n).decode("utf-8") or "{}")
        except Exception:
            return {}

    # --- routes ---
    def do_OPTIONS(self):
        self.send_response(204)
        self._cors()
        self.end_headers()

    def do_GET(self):
        u = urlparse(self.path)
        if u.path == "/health":
            return self._json(200, {"ok": True})
        if u.path == "/api/v1/system/status":
            return self._json(200, shape_status())
        if u.path == "/api/v1/dashboard/stats":
            return self._json(200, shape_stats())
        if u.path == "/api/v1/dashboard/pnl":
            qs = parse_qs(u.query)
            since = float(qs.get("since", ["0"])[0] or 0)
            return self._json(200, shape_pnl(since))
        return self._json(404, {"error": "not found", "path": u.path})

    def do_POST(self):
        u = urlparse(self.path)
        body = self._read_json()

        if u.path == "/admin/start":
            with LOCK:
                STATE["system_state"] = "running"
                STATE["started_at"] = time.time()
                STATE["frozen_pnl"] = None
                # re-seed exchanges so a fresh "session" has activity
                STATE["exchanges"] = _seed_exchanges()
            return self._json(200, {"ok": True, "state": "running"})

        if u.path == "/admin/stop":
            with LOCK:
                STATE["system_state"] = "stopped"
                # Freeze PnL at its final realized value (unrealized → 0).
                final_elapsed = max(0.0, time.time() - STATE["started_at"])
                STATE["frozen_pnl"] = round(
                    STATE["pnl_base_realized"] + final_elapsed * 0.05, 2
                )
            return self._json(200, {"ok": True, "state": "stopped"})

        if u.path == "/admin/cancel-all-orders":
            time.sleep(1.5)  # pretend to do work
            with LOCK:
                total = sum(v[0] for v in STATE["exchanges"].values())
                # random partial-failure for realism
                failed_count = random.choice([0, 0, 0, 1, 2])
                cancelled = max(0, total - failed_count)
                failed = []
                if failed_count > 0:
                    pool = list(STATE["exchanges"].keys())
                    for _ in range(failed_count):
                        ex = random.choice(pool)
                        failed.append({
                            "exchange": ex, "market": "spot",
                            "order_id": f"mock-{random.randint(10000,99999)}",
                            "error": "exchange returned 429 (rate limit)",
                        })
                # zero out open orders after cancel
                for k in STATE["exchanges"]:
                    STATE["exchanges"][k][0] = 0
            return self._json(200, {
                "cancelled": cancelled, "total": total, "failed": failed,
            })

        if u.path == "/admin/close-all-positions":
            time.sleep(2.0)
            with LOCK:
                total = sum(v[1] for v in STATE["exchanges"].values())
                failed_count = random.choice([0, 0, 1])
                closed = max(0, total - failed_count)
                failed = []
                if failed_count > 0:
                    failed.append({
                        "exchange": random.choice(list(STATE["exchanges"].keys())),
                        "market":   "futures",
                        "order_id": f"mock-{random.randint(10000,99999)}",
                        "error":    "insufficient margin to close (mock)",
                    })
                for k in STATE["exchanges"]:
                    STATE["exchanges"][k][2] += STATE["exchanges"][k][1]  # closed += open
                    STATE["exchanges"][k][1] = 0                          # open = 0
                    STATE["exchanges"][k][3] = 0.0                        # open_usd = 0
                # Note: state transition to "stopped" + PnL freeze happen in
                # /admin/stop, which the UI calls explicitly after this step.
            return self._json(200, {
                "closed": closed, "total": total, "failed": failed,
            })

        return self._json(404, {"error": "not found", "path": u.path})


def main():
    port = int(os.environ.get("PORT", "9999"))
    addr = ("0.0.0.0", port)
    httpd = ThreadingHTTPServer(addr, Handler)
    print(f"[mock] Trading System mock backend listening on http://localhost:{port}")
    print(f"[mock] endpoints: /health  /api/v1/system/status  /api/v1/dashboard/stats")
    print(f"[mock]            /api/v1/dashboard/pnl?since=<unix_ts>")
    print(f"[mock]            POST /admin/start  /admin/stop")
    print(f"[mock]            POST /admin/cancel-all-orders  /admin/close-all-positions")
    try:
        httpd.serve_forever()
    except KeyboardInterrupt:
        print("\n[mock] shutting down")


if __name__ == "__main__":
    main()
