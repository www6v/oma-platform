package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestHarnessConfigEndpoint_AllEnabled(t *testing.T) {
	r := chi.NewRouter()
	mountHarnessConfigRoutes(r, harness.ManagedHarnessState{
		OpenClaw: true,
		Hermes:   true,
	})

	req := httptest.NewRequest(http.MethodGet, "/harnesses", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got harness.ManagedHarnessState
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if !got.OpenClaw || !got.Hermes {
		t.Errorf("expected both enabled, got %+v", got)
	}
}

func TestHarnessConfigEndpoint_OpenClawDisabled(t *testing.T) {
	r := chi.NewRouter()
	mountHarnessConfigRoutes(r, harness.ManagedHarnessState{
		OpenClaw: false,
		Hermes:   true,
	})

	req := httptest.NewRequest(http.MethodGet, "/harnesses", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var got harness.ManagedHarnessState
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.OpenClaw {
		t.Errorf("OpenClaw should be disabled, got %+v", got)
	}
	if !got.Hermes {
		t.Errorf("Hermes should remain enabled, got %+v", got)
	}
}

func TestHarnessConfigEndpoint_BothDisabled(t *testing.T) {
	r := chi.NewRouter()
	mountHarnessConfigRoutes(r, harness.ManagedHarnessState{
		OpenClaw: false,
		Hermes:   false,
	})

	req := httptest.NewRequest(http.MethodGet, "/harnesses", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var got harness.ManagedHarnessState
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.OpenClaw || got.Hermes {
		t.Errorf("both should be disabled, got %+v", got)
	}
}

func TestManagedState_ReflectsDisabledAndURL(t *testing.T) {
	cases := []struct {
		name             string
		oc               harness.OpenClawConfig
		hc               harness.HermesConfig
		wantOC, wantHC   bool
	}{
		{
			name:   "disabled overrides URL",
			oc:     harness.OpenClawConfig{GatewayURL: "http://x", Disabled: true},
			hc:     harness.HermesConfig{GatewayURL: "http://y"},
			wantOC: false, wantHC: true,
		},
		{
			name:   "empty URL counts as disabled",
			oc:     harness.OpenClawConfig{},
			hc:     harness.HermesConfig{},
			wantOC: false, wantHC: false,
		},
		{
			name:   "URL without disabled flag is enabled",
			oc:     harness.OpenClawConfig{GatewayURL: "http://x"},
			hc:     harness.HermesConfig{GatewayURL: "http://y"},
			wantOC: true, wantHC: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := harness.ManagedState(c.oc, c.hc)
			if got.OpenClaw != c.wantOC || got.Hermes != c.wantHC {
				t.Errorf("ManagedState = %+v, want OC=%v HC=%v", got, c.wantOC, c.wantHC)
			}
		})
	}
}
