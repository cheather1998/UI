package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// /api/v1/system/status returns the current state with non-negative uptime.
func TestGetStatus(t *testing.T) {
	SystemSetState("running")
	w := httptest.NewRecorder()
	getStatus(w, httptest.NewRequest(http.MethodGet, "/api/v1/system/status", nil))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["state"] != "running" {
		t.Errorf("state=%v want running", resp["state"])
	}
}

// running → stopped → running must reset startedAt so PnL window restarts.
func TestSystemStateResetOnResume(t *testing.T) {
	SystemSetState("running")
	_, t1 := SystemGetState()
	time.Sleep(10 * time.Millisecond)
	SystemSetState("stopped")
	SystemSetState("running")
	_, t2 := SystemGetState()
	if !t2.After(t1) {
		t.Errorf("startedAt should reset on running transition: t1=%v t2=%v", t1, t2)
	}
}

// /admin/start and /admin/stop must flip state.
func TestStartStopActions(t *testing.T) {
	SystemSetState("stopped")
	w := httptest.NewRecorder()
	startAction(w, httptest.NewRequest(http.MethodPost, "/admin/start", nil))
	if s, _ := SystemGetState(); s != "running" {
		t.Errorf("after start: state=%q want running", s)
	}
	w = httptest.NewRecorder()
	stopAction(w, httptest.NewRequest(http.MethodPost, "/admin/stop", nil))
	if s, _ := SystemGetState(); s != "stopped" {
		t.Errorf("after stop: state=%q want stopped", s)
	}
}

// callCancel forwards to TES /api/v1/{exchange}/{market}/cancel with the
// expected body shape.
func TestCallCancelHitsCorrectURL(t *testing.T) {
	var gotPath string
	var gotBody map[string]string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer fake.Close()

	tesURL = fake.URL
	tesHTTPClient = fake.Client()

	td := &TradeData{
		Exchange: "binance", Market: "spot", OrderID: "X1",
		Symbol: "BTCUSDT", OpportunityID: "opp-1",
	}
	if err := callCancel(td, "admin-cancel-test"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/api/v1/binance/spot/cancel") {
		t.Errorf("path=%s", gotPath)
	}
	if gotBody["order_id"] != "X1" {
		t.Errorf("order_id=%v", gotBody["order_id"])
	}
	if gotBody["request_id"] != "admin-cancel-test" {
		t.Errorf("request_id=%v", gotBody["request_id"])
	}
}

// Non-2xx from TES bubbles up so the admin response can record per-order
// failures.
func TestCallCancelPropagatesNon2xx(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer fake.Close()
	tesURL = fake.URL
	tesHTTPClient = fake.Client()

	td := &TradeData{Exchange: "binance", Market: "spot", OrderID: "X2", Symbol: "BTCUSDT"}
	err := callCancel(td, "rid")
	if err == nil || !strings.Contains(err.Error(), "HTTP 429") {
		t.Fatalf("expected HTTP 429 error, got %v", err)
	}
}

// callClose for futures routes to /futures/close with the right pos_side.
func TestCallCloseFutures(t *testing.T) {
	var gotPath string
	var gotBody map[string]string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer fake.Close()
	tesURL = fake.URL
	tesHTTPClient = fake.Client()

	p := Position{
		Exchange: "okx", Market: "futures", Leg: "buy",
		Symbol: "BTC-USDT-SWAP", FilledQtyTotal: 0.5, OpportunityID: "opp", OrderID: "O1",
	}
	if err := callClose(p, "rid"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/api/v1/okx/futures/close") {
		t.Errorf("path=%s", gotPath)
	}
	if gotBody["pos_side"] != "LONG" {
		t.Errorf("pos_side=%v", gotBody["pos_side"])
	}
	if gotBody["qty"] != "0.5" {
		t.Errorf("qty=%v", gotBody["qty"])
	}
}

// Closing an open spot LONG leg unwinds via /spot/sell.
func TestCallCloseSpotLong(t *testing.T) {
	var gotPath string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gotPath = "captured"
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer fake.Close()
	captured := func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}
	fake2 := httptest.NewServer(http.HandlerFunc(captured))
	defer fake2.Close()
	tesURL = fake2.URL
	tesHTTPClient = fake2.Client()

	p := Position{Exchange: "binance", Market: "spot", Leg: "buy", Symbol: "BTCUSDT", FilledQtyTotal: 0.1}
	if err := callClose(p, "rid"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(gotPath, "/api/v1/binance/spot/sell") {
		t.Errorf("path=%s", gotPath)
	}
}

// Unsupported (market, leg) combos return an explicit error.
func TestCallCloseUnsupportedReturnsError(t *testing.T) {
	tesURL = "http://localhost:1"
	p := Position{Exchange: "binance", Market: "spot", Leg: "sell"}
	if err := callClose(p, "rid"); err == nil {
		t.Fatal("expected error for unsupported close")
	}
}

func TestPosSideForLeg(t *testing.T) {
	cases := map[string]string{"buy": "LONG", "BUY": "LONG", "sell": "SHORT", "": ""}
	for in, want := range cases {
		if got := posSideForLeg(in); got != want {
			t.Errorf("posSideForLeg(%q)=%q want %q", in, got, want)
		}
	}
}
