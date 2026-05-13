package main

import (
	"net/http"
	"sync"
	"time"
)

// stateInfo tracks the operator-facing system state. Purely advisory: it does
// not affect any backend behaviour, only what /api/v1/system/status returns
// and the floor used for "PnL since start".
type stateInfo struct {
	state     string // running | stopped
	startedAt time.Time
}

var (
	stateMu sync.RWMutex
	state   = stateInfo{state: "running", startedAt: time.Now().UTC()}
)

func SystemSetState(s string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	if s == "running" && state.state != "running" {
		state.startedAt = time.Now().UTC()
	}
	state.state = s
}

func SystemGetState() (string, time.Time) {
	stateMu.RLock()
	defer stateMu.RUnlock()
	return state.state, state.startedAt
}

func getStatus(w http.ResponseWriter, _ *http.Request) {
	s, t := SystemGetState()
	writeJSON(w, http.StatusOK, map[string]any{
		"state":      s,
		"started_at": t.UTC().Format(time.RFC3339),
		"uptime_sec": int64(time.Since(t).Seconds()),
	})
}

func startAction(w http.ResponseWriter, _ *http.Request) {
	SystemSetState("running")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "running"})
}

func stopAction(w http.ResponseWriter, _ *http.Request) {
	SystemSetState("stopped")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "state": "stopped"})
}
