package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/sessionoutputs"
)

// mountConsoleStubRoutes registers empty-list stubs for Console pages that
// main-node implements fully but oma-platform defers. Responses match the
// wire shapes the Console SPA expects so pages render empty states without
// console.warn noise or HTML 404 bodies.
func mountConsoleStubRoutes(
	r chi.Router,
	outputs *sessionoutputs.Store,
) {
	r.Get("/v1/runtimes", handleRuntimesListStub)
	r.Post("/v1/runtimes/connect-runtime", handleStubNotImplemented)
	r.Delete("/v1/runtimes/{id}", handleStubNotImplemented)

	r.Get("/v1/models/list", handleModelsListStub)

	r.Route("/v1/files", func(r chi.Router) {
		mountFileRoutes(r, filesDeps{Outputs: outputs})
	})

	r.Get("/v1/memory_stores", writeEmptyDataList)
	r.Post("/v1/memory_stores", handleStubNotImplemented)
	r.Route("/v1/memory_stores/{id}", func(r chi.Router) {
		r.Get("/", handleStubNotFound)
		r.Delete("/", handleStubNotImplemented)
		r.Post("/archive", handleStubNotImplemented)
		r.Get("/memories", writeEmptyDataList)
		r.Post("/memories", handleStubNotImplemented)
		r.Route("/memories/{memoryId}", func(r chi.Router) {
			r.Get("/", handleStubNotFound)
			r.Post("/", handleStubNotImplemented)
			r.Delete("/", handleStubNotImplemented)
		})
		r.Get("/memory_versions", writeEmptyDataList)
		r.Route("/memory_versions/{versionId}", func(r chi.Router) {
			r.Post("/redact", handleStubNotImplemented)
		})
	})

	r.Get("/v1/evals/runs", writeEmptyDataList)
	r.Route("/v1/evals/runs/{id}", func(r chi.Router) {
		r.Get("/", handleStubNotFound)
		r.Delete("/", handleStubNotImplemented)
	})

	mountIntegrationStubRoutes(r)
}

func mountIntegrationStubRoutes(r chi.Router) {
	r.Route("/v1/integrations", func(r chi.Router) {
		for _, provider := range []string{"linear", "github", "slack"} {
			r.Route("/"+provider, func(r chi.Router) {
				r.Get("/installations", writeEmptyDataList)
				r.Get("/publications", writeEmptyDataList)
				r.Get("/agents/{agentId}/publications", writeEmptyDataList)
				r.Get(
					"/installations/{installationId}/publications",
					writeEmptyDataList,
				)
				r.Post("/start-a1", handleStubNotImplemented)
				r.Post("/credentials", handleStubNotImplemented)
				r.Post("/handoff-link", handleStubNotImplemented)
				r.Post("/personal-token", handleStubNotImplemented)
				r.Post("/publications", handleStubNotImplemented)
				r.Route("/publications/{publicationId}", func(r chi.Router) {
					r.Get("/", handleStubNotFound)
					r.Patch("/", handleStubNotImplemented)
					r.Delete("/", handleStubNotImplemented)
					r.Get("/form-token", handleStubNotFound)
					r.Patch("/credentials", handleStubNotImplemented)
					r.Get("/dispatch-rules", writeEmptyDataList)
					r.Post("/dispatch-rules", handleStubNotImplemented)
					r.Route("/dispatch-rules/{ruleId}", func(r chi.Router) {
						r.Patch("/", handleStubNotImplemented)
						r.Delete("/", handleStubNotImplemented)
					})
				})
			})
		}
	})
}

func handleRuntimesListStub(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"runtimes": []any{},
	})
}

func handleModelsListStub(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data": []map[string]any{
			{
				"id":           "claude-haiku-4-5-20251001",
				"display_name": "Claude Haiku 4.5",
				"speeds":       []string{"standard", "fast"},
			},
			{
				"id":           "claude-sonnet-4-6",
				"display_name": "Claude Sonnet 4.6",
				"speeds":       []string{"standard"},
			},
			{
				"id":           "claude-opus-4-7",
				"display_name": "Claude Opus 4.7",
				"speeds":       []string{"standard"},
			},
		},
	})
}

func writeEmptyDataList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
}

func handleStubNotFound(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotFound, "not found")
}

func handleStubNotImplemented(w http.ResponseWriter, _ *http.Request) {
	writeError(
		w,
		http.StatusNotImplemented,
		"not implemented in oma-platform MVP",
	)
}
