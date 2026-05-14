package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

var allExchanges = []string{"binance", "bybit", "okx", "kraken", "gate"}

// ---------------- /api/v1/dashboard/stats ----------------

type ExchangeStat struct {
	OpenOrders      int     `json:"open_orders"`
	OpenPositions   int     `json:"open_positions"`
	ClosedPositions int     `json:"closed_positions"`
	OpenUSD         float64 `json:"open_usd"`
	BalanceUSD      float64 `json:"balance_usd"`
}

type StatsResp struct {
	Exchanges map[string]ExchangeStat `json:"exchanges"`
	Total     ExchangeStat            `json:"total"`
}

func getStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	openCounts, err := countOpenOrdersByExchange(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "open_orders: " + err.Error()})
		return
	}
	posStats, err := aggregatePositionsByExchange(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "positions: " + err.Error()})
		return
	}
	balances := fetchBalancesByExchange(ctx)

	resp := StatsResp{Exchanges: map[string]ExchangeStat{}}
	for _, ex := range allExchanges {
		st := ExchangeStat{
			OpenOrders:      openCounts[ex],
			OpenPositions:   posStats[ex].open,
			ClosedPositions: posStats[ex].closed,
			OpenUSD:         posStats[ex].openUSD,
			BalanceUSD:      balances[ex],
		}
		resp.Exchanges[ex] = st
		resp.Total.OpenOrders += st.OpenOrders
		resp.Total.OpenPositions += st.OpenPositions
		resp.Total.ClosedPositions += st.ClosedPositions
		resp.Total.OpenUSD += st.OpenUSD
		resp.Total.BalanceUSD += st.BalanceUSD
	}
	resp.Total.OpenUSD = round2(resp.Total.OpenUSD)
	resp.Total.BalanceUSD = round2(resp.Total.BalanceUSD)
	writeJSON(w, http.StatusOK, resp)
}

// fetchBalancesByExchange calls TES's existing /api/v1/{ex}/spot/balance and
// /futures/balance routes. Per-exchange failures degrade to 0 silently — the
// dashboard remains operable when a single exchange is misconfigured.
func fetchBalancesByExchange(ctx context.Context) map[string]float64 {
	out := map[string]float64{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ex := range allExchanges {
		wg.Add(1)
		go func(ex string) {
			defer wg.Done()
			var v float64
			for _, m := range []string{"spot", "futures"} {
				cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				var resp struct {
					TotalUSDTValue float64 `json:"total_usdt_value"`
					Data           struct {
						TotalUSDTValue float64 `json:"total_usdt_value"`
					} `json:"data"`
				}
				if err := tesGetJSON(cctx, fmt.Sprintf("/api/v1/%s/%s/balance", ex, m), &resp); err == nil {
					if resp.Data.TotalUSDTValue > 0 {
						v += resp.Data.TotalUSDTValue
					} else {
						v += resp.TotalUSDTValue
					}
				}
				cancel()
			}
			mu.Lock()
			out[ex] = round2(v)
			mu.Unlock()
		}(ex)
	}
	wg.Wait()
	return out
}

// ---------------- /api/v1/dashboard/pnl ----------------

type PnLResp struct {
	Realized   float64 `json:"realized"`
	Unrealized float64 `json:"unrealized"`
	Total      float64 `json:"total"`
	Percent    float64 `json:"percent"`
}

func getPnL(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	clientSince := time.Time{}
	if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
		var sec int64
		fmt.Sscanf(v, "%d", &sec)
		if sec > 0 {
			clientSince = time.Unix(sec, 0).UTC()
		}
	}
	_, sysSince := SystemGetState()
	since := clientSince
	if since.Before(sysSince) {
		since = sysSince
	}

	realized, err := sumRealizedPnL(ctx, since)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "realized: " + err.Error()})
		return
	}
	unrealized := computeUnrealizedPnL(ctx)

	total := realized + unrealized
	balance := totalBalanceUSD(fetchBalancesByExchange(ctx))
	percent := 0.0
	if balance > 0 {
		percent = round2(total / balance * 100)
	}
	writeJSON(w, http.StatusOK, PnLResp{
		Realized:   round2(realized),
		Unrealized: round2(unrealized),
		Total:      round2(total),
		Percent:    percent,
	})
}

// computeUnrealizedPnL marks each open position to the latest live price.
// On any per-position lookup failure (price fetch, missing symbol) the entry
// is skipped — resilient by design.
func computeUnrealizedPnL(ctx context.Context) float64 {
	positions, err := loadOpenPositions(ctx)
	if err != nil || len(positions) == 0 {
		return 0
	}
	var total float64
	for _, p := range positions {
		base := baseCurrencyFromSymbol(p.Symbol)
		if base == "" {
			continue
		}
		px, err := getPriceUSD(base)
		if err != nil || px <= 0 {
			continue
		}
		side := 1.0
		if strings.EqualFold(p.Leg, "sell") {
			side = -1.0
		}
		total += (px - p.AvgFillPrice) * p.FilledQtyTotal * side
	}
	return total
}

// baseCurrencyFromSymbol strips common quote / expiry suffixes:
// "BTCUSDT" → "BTC"; "ETH-USDT-SWAP" → "ETH"; "XRPUSD" → "XRP".
func baseCurrencyFromSymbol(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	if s == "" {
		return ""
	}
	for _, suf := range []string{"-USDT-SWAP", "_USDT", "-USDT", "USDT", "_USD", "-USD", "USD", "_PERP", "-PERP"} {
		if strings.HasSuffix(s, suf) && len(s) > len(suf) {
			return strings.Trim(strings.TrimSuffix(s, suf), "-_")
		}
	}
	return s
}

func totalBalanceUSD(per map[string]float64) float64 {
	var t float64
	for _, v := range per {
		t += v
	}
	return t
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
