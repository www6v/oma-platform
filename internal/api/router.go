package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

// Deps holds shared dependencies for HTTP handlers.
type Deps struct {
	Agents   *store.AgentRepo
	Sessions *sessionHandlers
	APIKey   string
}

// NewRouter returns the platform HTTP handler.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(AuthMiddleware(deps.APIKey))
	r.Get("/health", handleHealth)

	if deps.Agents != nil {
		r.Route("/v1/agents", func(r chi.Router) {
			mountAgentRoutes(r, deps.Agents)
		})
	}

	if deps.Sessions != nil {
		r.Route("/v1/sessions", func(r chi.Router) {
			mountSessionRoutes(r, deps.Sessions)
		})
	}

	return r
}

// NewSessionHandlers builds session HTTP dependencies.
func NewSessionHandlers(
	sessions *store.SessionRepo,
	events *store.EventRepo,
	hub *stream.Hub,
	registry *session.Registry,
	workdirs *workdir.Manager,
	client harness.Client,
) *sessionHandlers {
	return &sessionHandlers{
		sessions: sessions,
		events:   events,
		hub:      hub,
		registry: registry,
		workdirs: workdirs,
		harness:  client,
	}
}
