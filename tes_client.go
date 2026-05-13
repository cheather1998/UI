package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// tesHTTPClient is shared. Tests may override by setting it to httptest's client.
var tesHTTPClient = &http.Client{Timeout: 30 * time.Second}

// tesPostJSON does POST {tesURL}{path} with a JSON body. Returns nil on 2xx,
// or "HTTP <code>: <body>" on non-2xx so callers can record per-order failures.
func tesPostJSON(path string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, tesURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := tesHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	b, _ := io.ReadAll(resp.Body)
	msg := strings.TrimSpace(string(b))
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
}

// tesGetJSON does GET {tesURL}{path} and decodes the body into out.
func tesGetJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tesURL+path, nil)
	if err != nil {
		return err
	}
	resp, err := tesHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
