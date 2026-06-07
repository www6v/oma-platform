package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/console"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

// Deps holds shared dependencies for HTTP handlers.
type Deps struct {
	Agents       *store.AgentRepo
	Environments *store.EnvironmentRepo
	ModelCards   *store.ModelCardRepo
	Sessions     *sessionHandlers
	APIKey       string
	ConsoleDir   string
	ConsoleDev   bool
}

// NewRouter returns the platform HTTP handler.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(AuthMiddleware(AuthConfig{
		APIKey:         deps.APIKey,
		ConsoleMounted: deps.ConsoleDir != "",
		ConsoleDev:     deps.ConsoleDev,
	}))
	r.Get("/health", handleHealth)

	if deps.ConsoleDev {
		mountConsoleDevRoutes(r)
	}

	if deps.Agents != nil {
		r.Route("/v1/agents", func(r chi.Router) {
			mountAgentRoutes(r, deps.Agents)
		})
	}

	if deps.Environments != nil {
		r.Route("/v1/environments", func(r chi.Router) {
			mountEnvironmentRoutes(r, deps.Environments)
		})
	}

	if deps.ModelCards != nil {
		r.Route("/v1/model_cards", func(r chi.Router) {
			mountModelCardRoutes(r, deps.ModelCards)
		})
	}

	if deps.Sessions != nil {
		r.Route("/v1/sessions", func(r chi.Router) {
			mountSessionRoutes(r, deps.Sessions)
		})
	}

	if deps.ConsoleDir != "" {
		static := console.NewStaticHandler(deps.ConsoleDir)
		r.NotFound(static.ServeHTTP)
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
	models *modelresolve.Resolver,
) *sessionHandlers {
	return &sessionHandlers{
		sessions: sessions,
		events:   events,
		hub:      hub,
		registry: registry,
		workdirs: workdirs,
		harness:  client,
		models:   models,
	}
}
