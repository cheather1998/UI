package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"
)

type EndpointRegistry struct {
	Version                   string                   `json:"version"`
	GeneratedFromPrivateRepos map[string]string        `json:"generated_from_private_repos"`
	Notes                     []string                 `json:"notes"`
	Services                  map[string]ServiceConfig `json:"services"`
}

type ServiceConfig struct {
	Repo            string                    `json:"repo,omitempty"`
	DefaultBaseURL  any                       `json:"default_base_url,omitempty"`
	OwnedByThisRepo bool                      `json:"owned_by_this_repo,omitempty"`
	SourceFile      string                    `json:"source_file,omitempty"`
	SourceFiles     []string                  `json:"source_files,omitempty"`
	Notes           []string                  `json:"notes,omitempty"`
	Endpoints       map[string]EndpointConfig `json:"endpoints,omitempty"`
	OutboundDeps    map[string]EndpointConfig `json:"outbound_dependencies,omitempty"`
}

type EndpointConfig struct {
	Method    string   `json:"method"`
	Path      string   `json:"path"`
	BaseEnv   string   `json:"base_env,omitempty"`
	Exchanges []string `json:"exchanges,omitempty"`
	Markets   []string `json:"markets,omitempty"`
	Sides     []string `json:"sides,omitempty"`
}

type UpstreamStatus struct {
	Name      string `json:"name"`
	Repo      string `json:"repo,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	HealthURL string `json:"health_url,omitempty"`
	Reachable bool   `json:"reachable"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

var endpointRegistry *EndpointRegistry

func loadEndpointRegistry() *EndpointRegistry {
	path := envOr("ENDPOINTS_CONFIG", "config/endpoints.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return &EndpointRegistry{Version: "unknown", Notes: []string{"config/endpoints.json not found"}, Services: map[string]ServiceConfig{}}
	}
	var reg EndpointRegistry
	if err := json.Unmarshal(b, &reg); err != nil {
		return &EndpointRegistry{Version: "invalid", Notes: []string{"config/endpoints.json parse failed: " + err.Error()}, Services: map[string]ServiceConfig{}}
	}
	return &reg
}

func getEndpoints(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, endpointRegistry)
}

func getUpstreamStatus(w http.ResponseWriter, _ *http.Request) {
	items := []UpstreamStatus{
		checkHTTPHealth("ui_control_panel", "", "http://localhost:"+cfg.Port, "/health"),
		checkHTTPHealth("trade_execution_system", "Aylab-company/trade-execution-system", cfg.TESBaseURL, "/health"),
		checkHTTPHealth("decision_making_service", "Aylab-company/decision-making-service", cfg.DecisionBaseURL, "/health"),
		checkHTTPHealth("arbium", "Aylab-company/Arbium", cfg.ArbiumBaseURL, "/health"),
	}
	// trading-agent currently has no discovered health/admin HTTP endpoint.
	items = append(items, UpstreamStatus{
		Name:      "trading_agent",
		Repo:      "Aylab-company/trading-agent",
		BaseURL:   cfg.TradingAgentBaseURL,
		Reachable: false,
		Status:    "not_configured_no_discovered_http_admin_api",
	})
	writeJSON(w, http.StatusOK, items)
}

func checkHTTPHealth(name, repo, baseURL, path string) UpstreamStatus {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	st := UpstreamStatus{Name: name, Repo: repo, BaseURL: base}
	if base == "" {
		st.Status = "not_configured"
		return st
	}
	st.HealthURL = base + path
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, st.HealthURL, nil)
	if err != nil {
		st.Status = "invalid_url"
		st.Error = err.Error()
		return st
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		st.Status = "unreachable"
		st.Error = err.Error()
		return st
	}
	defer resp.Body.Close()
	st.Reachable = resp.StatusCode >= 200 && resp.StatusCode < 500
	st.Status = resp.Status
	return st
}
