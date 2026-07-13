package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/harness"
)

// mountHarnessConfigRoutes wires the GET /v1/config/harnesses endpoint.
// The console UI calls this on load to know which managed harnesses
// (OpenClaw / Hermes) are currently enabled so it can grey out the
// disabled options in the Harness dropdown.
func mountHarnessConfigRoutes(r chi.Router, state harness.ManagedHarnessState) {
	r.Get("/harnesses", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	})
}
