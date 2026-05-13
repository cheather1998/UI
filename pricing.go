package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Cache live USD prices for 5 min. Mark-to-market doesn't need ticks, and the
// dashboard refreshes every 5 s — caching avoids hammering Binance public on
// every PnL request.
var (
	priceCache   = map[string]float64{}
	priceCacheTs = map[string]time.Time{}
	priceCacheMu sync.RWMutex
)

const priceTTL = 5 * time.Minute

// getPriceUSD returns the latest USD price for a base currency by hitting
// Binance's public ticker. Stablecoins return 1.0 immediately. Kraken's
// odd-shaped tickers (XBT, XXBT, XETH) are normalized.
func getPriceUSD(currency string) (float64, error) {
	upper := strings.ToUpper(strings.TrimSpace(currency))
	if upper == "" {
		return 0, fmt.Errorf("empty currency")
	}

	switch upper {
	case "USDT", "USDC", "TUSD", "BUSD", "USDD", "DAI", "USD", "ZUSD":
		return 1.0, nil
	case "XBT", "XXBT":
		upper = "BTC"
	case "XETH":
		upper = "ETH"
	}

	priceCacheMu.RLock()
	if ts, ok := priceCacheTs[upper]; ok && time.Since(ts) < priceTTL {
		v := priceCache[upper]
		priceCacheMu.RUnlock()
		return v, nil
	}
	priceCacheMu.RUnlock()

	url := fmt.Sprintf("https://api.binance.com/api/v3/ticker/price?symbol=%sUSDT", upper)
	cli := &http.Client{Timeout: 5 * time.Second}
	resp, err := cli.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("binance HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var t struct {
		Price string `json:"price"`
	}
	if err := json.Unmarshal(body, &t); err != nil {
		return 0, err
	}
	p, err := strconv.ParseFloat(t.Price, 64)
	if err != nil || p <= 0 {
		return 0, fmt.Errorf("invalid price: %q", t.Price)
	}

	priceCacheMu.Lock()
	priceCache[upper] = p
	priceCacheTs[upper] = time.Now()
	priceCacheMu.Unlock()
	return p, nil
}
