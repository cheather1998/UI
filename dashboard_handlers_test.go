package main

import "testing"

// baseCurrencyFromSymbol normalizes exchange-specific symbol suffixes.
func TestBaseCurrencyFromSymbol(t *testing.T) {
	cases := map[string]string{
		"BTCUSDT":       "BTC",
		"ETH-USDT-SWAP": "ETH",
		"BTC-USDT":      "BTC",
		"SOL_USDT":      "SOL",
		"XRPUSD":        "XRP",
		"DOGE-PERP":     "DOGE",
		"":              "",
	}
	for in, want := range cases {
		if got := baseCurrencyFromSymbol(in); got != want {
			t.Errorf("baseCurrencyFromSymbol(%q)=%q want %q", in, got, want)
		}
	}
}

func TestRound2(t *testing.T) {
	cases := []struct {
		in, want float64
	}{
		{1.004, 1.00},
		{1.006, 1.01},
		{-1.006, -1.01},
		{0, 0},
		{1234.567, 1234.57},
		{0.125, 0.13},
	}
	for _, c := range cases {
		if got := round2(c.in); got != c.want {
			t.Errorf("round2(%v)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestTotalBalanceUSD(t *testing.T) {
	in := map[string]float64{"binance": 100.0, "bybit": 50.5, "okx": 0.0}
	if got := totalBalanceUSD(in); got != 150.5 {
		t.Errorf("totalBalanceUSD=%v want 150.5", got)
	}
}
