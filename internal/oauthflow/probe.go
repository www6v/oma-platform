package oauthflow

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProbeMcpServer POSTs tools/list with the bearer token (5s timeout).
func ProbeMcpServer(
	ctx context.Context,
	client *http.Client,
	mcpURL string,
	bearerToken string,
) (ok bool, message string) {
	if client == nil {
		client = http.DefaultClient
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, mcpURL, strings.NewReader(body),
	)
	if err != nil {
		return false, fmt.Sprintf("MCP probe failed: %s", truncate(err.Error(), 120))
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json, text/event-stream")
	req.Header.Set("authorization", "Bearer "+bearerToken)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		msg := err.Error()
		return false, fmt.Sprintf("MCP probe failed: %s", truncate(msg, 120))
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, ""
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 240))
	slice := strings.TrimSpace(string(raw))
	if slice != "" {
		return false, fmt.Sprintf("MCP probe HTTP %d: %s", resp.StatusCode, slice)
	}
	return false, fmt.Sprintf("MCP probe HTTP %d", resp.StatusCode)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
