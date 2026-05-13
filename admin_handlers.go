package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ---------------- /admin/cancel-all-orders ----------------

type cancelAllReq struct {
	Exchange string `json:"exchange,omitempty"`
}

type cancelAllResp struct {
	Cancelled int                  `json:"cancelled"`
	Total     int                  `json:"total"`
	Failed    []OrderActionFailure `json:"failed"`
}

func cancelAllOrders(w http.ResponseWriter, r *http.Request) {
	var req cancelAllReq
	_ = json.NewDecoder(r.Body).Decode(&req)

	orders, err := scanOpenOrders(r.Context(), strings.ToLower(req.Exchange))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "scan_open_orders: " + err.Error()})
		return
	}
	resp := cancelAllResp{Total: len(orders), Failed: []OrderActionFailure{}}
	if len(orders) == 0 {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	reqID := fmt.Sprintf("admin-cancel-%d", time.Now().UnixNano())

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, o := range orders {
		wg.Add(1)
		sem <- struct{}{}
		go func(td *TradeData) {
			defer wg.Done()
			defer func() { <-sem }()
			err := callCancel(td, reqID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				resp.Failed = append(resp.Failed, OrderActionFailure{
					Exchange: td.Exchange, Market: td.Market, OrderID: td.OrderID, Error: err.Error(),
				})
			} else {
				resp.Cancelled++
			}
		}(o)
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, resp)
}

func callCancel(td *TradeData, reqID string) error {
	body := map[string]string{
		"order_id":       td.OrderID,
		"symbol":         td.Symbol,
		"opportunity_id": coalesce(td.OpportunityID, "admin-noopp"),
		"request_id":     reqID,
	}
	path := fmt.Sprintf("/api/v1/%s/%s/cancel",
		strings.ToLower(td.Exchange), strings.ToLower(td.Market))
	return tesPostJSON(path, body)
}

// ---------------- /admin/close-all-positions ----------------

type closeAllResp struct {
	Closed int                  `json:"closed"`
	Total  int                  `json:"total"`
	Failed []OrderActionFailure `json:"failed"`
}

func closeAllPositions(w http.ResponseWriter, r *http.Request) {
	positions, err := loadOpenPositions(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "load_open_positions: " + err.Error()})
		return
	}
	resp := closeAllResp{Total: len(positions), Failed: []OrderActionFailure{}}
	if len(positions) == 0 {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	reqID := fmt.Sprintf("admin-close-%d", time.Now().UnixNano())

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	closed := 0
	failed := []OrderActionFailure{}
	for i := range positions {
		wg.Add(1)
		sem <- struct{}{}
		go func(p Position) {
			defer wg.Done()
			defer func() { <-sem }()
			err := callClose(p, reqID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, OrderActionFailure{
					Exchange: p.Exchange, Market: p.Market, OrderID: p.OrderID, Error: err.Error(),
				})
			} else {
				closed++
			}
		}(positions[i])
	}
	wg.Wait()
	resp.Closed = closed
	resp.Failed = failed
	writeJSON(w, http.StatusOK, resp)
}

func callClose(p Position, reqID string) error {
	qtyStr := fmt.Sprintf("%g", p.FilledQtyTotal)
	market := strings.ToLower(p.Market)
	leg := strings.ToLower(p.Leg)
	exchange := strings.ToLower(p.Exchange)

	switch {
	case market == "futures":
		body := map[string]string{
			"symbol":         p.Symbol,
			"qty":            qtyStr,
			"opportunity_id": p.OpportunityID,
			"request_id":     reqID,
			"pos_side":       posSideForLeg(leg),
		}
		return tesPostJSON(fmt.Sprintf("/api/v1/%s/futures/close", exchange), body)

	case market == "spot" && leg == "buy":
		// Closing an open long spot leg = selling back the bought asset.
		body := map[string]string{
			"symbol":         p.Symbol,
			"qty":            qtyStr,
			"opportunity_id": p.OpportunityID,
			"request_id":     reqID,
		}
		return tesPostJSON(fmt.Sprintf("/api/v1/%s/spot/sell", exchange), body)

	default:
		return fmt.Errorf("unsupported close: market=%s leg=%s", market, leg)
	}
}

func posSideForLeg(leg string) string {
	switch strings.ToLower(leg) {
	case "buy":
		return "LONG"
	case "sell":
		return "SHORT"
	}
	return ""
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
