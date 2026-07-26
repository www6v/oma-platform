package api

import "testing"

func TestIntegrationsGatewayOriginFallback(t *testing.T) {
	// Mirrors open-managed-agents main-node:
	// GATEWAY_ORIGIN ?? PUBLIC_BASE_URL ?? localhost default.
	t.Setenv("GATEWAY_ORIGIN", "")
	t.Setenv("INTEGRATIONS_GATEWAY_ORIGIN", "")
	t.Setenv("OMA_GATEWAY_ORIGIN", "")
	t.Setenv("OMA_PUBLIC_URL", "")
	t.Setenv("PUBLIC_BASE_URL", "")

	if got := integrationsGatewayOrigin(); got != "http://127.0.0.1:8787" {
		t.Fatalf("default origin = %q, want localhost", got)
	}

	t.Setenv("PUBLIC_BASE_URL", "http://124.221.28.203:8787/")
	if got := integrationsGatewayOrigin(); got != "http://124.221.28.203:8787" {
		t.Fatalf("PUBLIC_BASE_URL fallback = %q", got)
	}

	t.Setenv("OMA_PUBLIC_URL", "http://public.example:8787")
	if got := integrationsGatewayOrigin(); got != "http://public.example:8787" {
		t.Fatalf("OMA_PUBLIC_URL should win over PUBLIC_BASE_URL: %q", got)
	}

	t.Setenv("OMA_GATEWAY_ORIGIN", "http://oma-gateway.example:8787")
	if got := integrationsGatewayOrigin(); got != "http://oma-gateway.example:8787" {
		t.Fatalf("OMA_GATEWAY_ORIGIN should win: %q", got)
	}

	t.Setenv("INTEGRATIONS_GATEWAY_ORIGIN", "http://integrations.example:8787")
	if got := integrationsGatewayOrigin(); got != "http://integrations.example:8787" {
		t.Fatalf("INTEGRATIONS_GATEWAY_ORIGIN should win: %q", got)
	}

	t.Setenv("GATEWAY_ORIGIN", "http://gateway.example:8787")
	if got := integrationsGatewayOrigin(); got != "http://gateway.example:8787" {
		t.Fatalf("GATEWAY_ORIGIN should win (upstream name): %q", got)
	}
}
