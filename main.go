// Trading System Control — sidecar service that powers the Desktop UI
// without modifying the TES or AGENT source trees.
//
//   - UI               → this service (port :8081 by default)
//   - this service     → TES :8080 (HTTP, for cancel/close/balance)
//   - this service     → MongoDB  (read tde_positions, tde_trades)
//   - this service     → Redis    (read tde:order:json:v1:* for open count)
//
// Endpoints:
//
//	GET  /health
//	GET  /api/v1/system/status
//	GET  /api/v1/dashboard/stats
//	GET  /api/v1/dashboard/pnl?since=<unix_ts>
//	POST /admin/cancel-all-orders   { "exchange": "binance" }   // optional filter
//	POST /admin/close-all-positions
//	POST /admin/start                 // resets started_at, state=running
//	POST /admin/stop                  // state=stopped
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Globals are intentional — this is a small one-process service. Tests overwrite
// these variables directly to inject fakes (httptest server URL, miniredis, etc.).
var (
	cfg    Config
	rdb    *redis.Client
	mdb    *mongo.Database
	tesURL string
)

type Config struct {
	Port                string
	MongoURI            string
	MongoDB             string
	RedisAddr           string
	TESBaseURL          string
	DecisionBaseURL     string
	ArbiumBaseURL       string
	TradingAgentBaseURL string
}

func loadConfig() Config {
	_ = godotenv.Load()
	return Config{
		Port:                envOr("CONTROL_PORT", "8081"),
		MongoURI:            envOr("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:             envOr("MONGO_DATABASE", "crypto_trading"),
		RedisAddr:           envOr("REDIS_ADDR", "localhost:6379"),
		TESBaseURL:          envOr("TES_BASE_URL", "http://localhost:8080"),
		DecisionBaseURL:     envOr("DECISION_BASE_URL", "http://localhost:3003"),
		ArbiumBaseURL:       envOr("ARBIUM_BASE_URL", ""),
		TradingAgentBaseURL: envOr("TRADING_AGENT_BASE_URL", ""),
	}
}

func envOr(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func main() {
	cfg = loadConfig()
	log.Printf("control: starting (port=%s mongo=%s redis=%s tes=%s)",
		cfg.Port, cfg.MongoURI, cfg.RedisAddr, cfg.TESBaseURL)

	// Mongo (best-effort: dashboard returns zeros if Mongo down)
	mctx, mcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer mcancel()
	mclient, err := mongo.Connect(mctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Printf("⚠️ mongo connect failed: %v (dashboard counts will be 0)", err)
	} else {
		mdb = mclient.Database(cfg.MongoDB)
		log.Printf("✅ mongo connected: %s", cfg.MongoDB)
	}

	// Redis (best-effort: dashboard open-orders count = 0 if down)
	rdb = redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if perr := rdb.Ping(context.Background()).Err(); perr != nil {
		log.Printf("⚠️ redis ping failed: %v (open-order count will be 0)", perr)
	} else {
		log.Printf("✅ redis connected: %s", cfg.RedisAddr)
	}

	tesURL = strings.TrimRight(cfg.TESBaseURL, "/")
	endpointRegistry = loadEndpointRegistry()
	SystemSetState("running")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", health)
	mux.HandleFunc("GET /api/v1/system/status", getStatus)
	mux.HandleFunc("GET /api/v1/dashboard/stats", getStats)
	mux.HandleFunc("GET /api/v1/dashboard/pnl", getPnL)
	mux.HandleFunc("GET /api/v1/endpoints", getEndpoints)
	mux.HandleFunc("GET /api/v1/upstreams/status", getUpstreamStatus)
	mux.HandleFunc("POST /admin/cancel-all-orders", cancelAllOrders)
	mux.HandleFunc("POST /admin/close-all-positions", closeAllPositions)
	mux.HandleFunc("POST /admin/start", startAction)
	mux.HandleFunc("POST /admin/stop", stopAction)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: corsMiddleware(mux)}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		log.Printf("🛰️  control listening on :%s", cfg.Port)
		if lerr := srv.ListenAndServe(); lerr != nil && lerr != http.ErrServerClosed {
			log.Fatalf("server: %v", lerr)
		}
	}()
	<-sigCh
	log.Println("control: shutting down")
	sctx, scancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer scancel()
	_ = srv.Shutdown(sctx)
	if mclient != nil {
		_ = mclient.Disconnect(context.Background())
	}
}

// ---------------- shared helpers ----------------

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
