package main

import (
	"context"
	"encoding/json"
	"strings"
)

// scanOpenOrders walks tde:order:json:v1:* in Redis and returns the docs whose
// Status == "open". Skips alias and pre-registration keys to avoid duplicates.
// Used by /admin/cancel-all-orders.
func scanOpenOrders(ctx context.Context, exchangeFilter string) ([]*TradeData, error) {
	if rdb == nil {
		return nil, nil
	}
	var out []*TradeData
	var cursor uint64
	for iter := 0; iter < 10000; iter++ {
		keys, next, err := rdb.Scan(ctx, cursor, "tde:order:json:v1:*", 250).Result()
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if strings.Contains(k, ":alias:") || strings.Contains(k, ":noid:") {
				continue
			}
			raw, err := rdb.Get(ctx, k).Bytes()
			if err != nil || len(raw) == 0 {
				continue
			}
			var td TradeData
			if err := json.Unmarshal(raw, &td); err != nil {
				continue
			}
			if !strings.EqualFold(td.Status, "open") {
				continue
			}
			if exchangeFilter != "" && !strings.EqualFold(td.Exchange, exchangeFilter) {
				continue
			}
			if strings.TrimSpace(td.OrderID) == "" {
				continue
			}
			out = append(out, &td)
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return out, nil
}

// countOpenOrdersByExchange tallies open orders per exchange. Used by
// /api/v1/dashboard/stats. Returns an empty map (not an error) when Redis is
// unavailable so the dashboard remains responsive.
func countOpenOrdersByExchange(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	if rdb == nil {
		return out, nil
	}
	var cursor uint64
	for iter := 0; iter < 10000; iter++ {
		keys, next, err := rdb.Scan(ctx, cursor, "tde:order:json:v1:*", 250).Result()
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			if strings.Contains(k, ":alias:") || strings.Contains(k, ":noid:") {
				continue
			}
			raw, err := rdb.Get(ctx, k).Bytes()
			if err != nil || len(raw) == 0 {
				continue
			}
			var td TradeData
			if err := json.Unmarshal(raw, &td); err != nil {
				continue
			}
			if !strings.EqualFold(td.Status, "open") {
				continue
			}
			out[strings.ToLower(td.Exchange)]++
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return out, nil
}
