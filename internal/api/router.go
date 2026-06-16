package api

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/auth"
	"github.com/open-ma/oma-building/internal/console"
	"github.com/open-ma/oma-building/internal/dream"
	"github.com/open-ma/oma-building/internal/integrations/github"
	"github.com/open-ma/oma-building/internal/integrations/linear"
	"github.com/open-ma/oma-building/internal/integrations/slack"
	"github.com/open-ma/oma-building/internal/fileblob"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/mcpproxy"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/oauthflow"
	"github.com/open-ma/oma-building/internal/ratelimit"
	"github.com/open-ma/oma-building/internal/runtime"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/sessionoutputs"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

// Deps holds shared dependencies for HTTP handlers.
type Deps struct {
	Agents       *store.AgentRepo
	Environments *store.EnvironmentRepo
	ModelCards   *store.ModelCardRepo
	Vaults       *store.VaultRepo
	Credentials  *store.CredentialRepo
	Skills       *store.SkillRepo
	SkillFiles   *store.SkillFileStore
	Files        *store.FileRepo
	FileBlobs    *fileblob.Store
	SessionOutputs *sessionoutputs.Store
	ApiKeys      *store.ApiKeyRepo
	Tenants      *store.TenantRepo
	Runtimes       *store.RuntimeRepo
	RuntimeRooms   *runtime.Registry
	Integrations   *store.IntegrationRepo
	MemoryStores   *store.MemoryStoreRepo
	EvalRuns       *store.EvalRunRepo
	Dreams         *store.DreamRepo
	DreamWorker    *dream.Worker
	Events         *store.EventRepo
	Sessions       *sessionHandlers
	APIKey       string
	ConsoleDir   string
	AuthDisabled bool
	AuthUpstream string
	McpProxyBase string
	McpProxyKey  string
	OutboundProxyAddr string
	OutboundProxyKey  string
	InternalSecret    string
	ModelResolver     *modelresolve.Resolver
	LinearGateway     *linear.Handler
	GitHubGateway     *github.Handler
	SlackGateway      *slack.Handler
	RateLimit           *ratelimit.Gates
	OAuthState          *oauthflow.StateStore
	PublicURL           string
	// InstallBridgeHTTP overrides outbound HTTP for install/OAuth (tests).
	InstallBridgeHTTP *http.Client
}

// NewRouter returns the platform HTTP handler.
func NewRouter(deps Deps) http.Handler {
	r := chi.NewRouter()

	authCfg := auth.Config{
		Disabled:       deps.AuthDisabled,
		APIKey:         deps.APIKey,
		ApiKeys:        deps.ApiKeys,
		Tenants:        deps.Tenants,
		ConsoleMounted: deps.ConsoleDir != "",
	}
	if !deps.AuthDisabled && deps.AuthUpstream != "" {
		authCfg.Session = &auth.SessionResolver{Upstream: deps.AuthUpstream}
	}
	r.Use(auth.Middleware(authCfg))
	if deps.RateLimit != nil {
		r.Use(ratelimit.Middleware(deps.RateLimit, deps.AuthDisabled))
	}

	r.Get("/health", handleHealth)

	routeDeps := auth.RouteDepsFromEnv(deps.AuthDisabled)
	routeDeps.AuthUpstream = deps.AuthUpstream
	if err := auth.Mount(r, routeDeps); err != nil {
		log.Printf("warning: auth routes: %v", err)
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
			mountSessionRoutes(r, deps.Sessions, deps.RateLimit)
		})
	}

	if deps.Agents != nil && deps.Sessions != nil && deps.Environments != nil {
		r.Route("/v1/stats", func(r chi.Router) {
			mountStatsRoutes(r, statsDeps{
				Agents:       deps.Agents,
				Sessions:     deps.Sessions.sessions,
				Environments: deps.Environments,
				ModelCards:   deps.ModelCards,
				Vaults:       deps.Vaults,
				Skills:       deps.Skills,
				ApiKeys:      deps.ApiKeys,
			})
		})
	}

	if deps.Vaults != nil && deps.Credentials != nil {
		r.Route("/v1/vaults", func(r chi.Router) {
			mountVaultRoutes(r, vaultDeps{
				Vaults:      deps.Vaults,
				Credentials: deps.Credentials,
			})
		})
	}

	oauthDeps := oauthV1Deps{
		Vaults:      deps.Vaults,
		Credentials: deps.Credentials,
		State:       deps.OAuthState,
		PublicURL:   deps.PublicURL,
	}
	r.Route("/v1/oauth", func(r chi.Router) {
		mountOAuthV1Routes(r, oauthDeps)
	})

	if deps.Skills != nil && deps.SkillFiles != nil {
		r.Route("/v1/clawhub", func(r chi.Router) {
			mountClawhubRoutes(r, clawhubDeps{
				Skills:     deps.Skills,
				SkillFiles: deps.SkillFiles,
			})
		})
	}

	if deps.Sessions != nil && deps.Credentials != nil {
		mountMcpProxyRoutes(r, mcpProxyDeps{
			Resolver: &mcpproxy.Resolver{
				Sessions:    deps.Sessions.sessions,
				Credentials: deps.Credentials,
			},
			ApiKeys: deps.ApiKeys,
			APIKey:  deps.APIKey,
		})
	}

	if deps.Skills != nil && deps.SkillFiles != nil {
		r.Route("/v1/skills", func(r chi.Router) {
			mountSkillRoutes(r, skillsDeps{
				Skills:    deps.Skills,
				Files:     deps.SkillFiles,
				RateLimit: deps.RateLimit,
			})
		})
	}

	r.Route("/v1/me", func(r chi.Router) {
		mountMeRoutes(r, meDeps{
			AuthDisabled: deps.AuthDisabled,
			ApiKeys:      deps.ApiKeys,
			Tenants:      deps.Tenants,
		})
	})

	if deps.Tenants != nil {
		r.Route("/v1/tenants", func(r chi.Router) {
			mountTenantRoutes(r, tenantDeps{Tenants: deps.Tenants})
		})
	}

	if deps.ApiKeys != nil {
		r.Route("/v1/api_keys", func(r chi.Router) {
			mountApiKeyRoutes(r, deps.ApiKeys)
		})
	}

	if deps.Runtimes != nil {
		rtDeps := runtimesDeps{
			Runtimes:       deps.Runtimes,
			ApiKeys:        deps.ApiKeys,
			Tenants:        deps.Tenants,
			Rooms:          deps.RuntimeRooms,
			InternalSecret: deps.InternalSecret,
		}
		r.Route("/v1/runtimes", func(r chi.Router) {
			mountRuntimeRoutes(r, rtDeps)
		})
		r.Route("/agents/runtime", func(r chi.Router) {
			mountRuntimeDaemonRoutes(r, rtDeps)
		})
	}

	gatewayOrigin := integrationsGatewayOrigin()
	installBridge, installProxy := newInstallBridgeDeps(
		deps.Integrations, gatewayOrigin, deps.InternalSecret,
	)
	if installBridge != nil && deps.InstallBridgeHTTP != nil {
		installBridge.HTTP = deps.InstallBridgeHTTP
	}
	integrationDeps := integrationsDeps{
		Integrations:  deps.Integrations,
		GatewayOrigin: gatewayOrigin,
		Linear:        deps.LinearGateway,
		InstallBridge: installBridge,
		InstallProxy:  installProxy,
	}
	r.Route("/v1/integrations", func(r chi.Router) {
		mountIntegrationRoutes(r, integrationDeps)
	})

	if deps.LinearGateway != nil {
		mountLinearGatewayRoutes(r, linearGatewayDeps{Handler: deps.LinearGateway})
	}
	if deps.GitHubGateway != nil {
		mountGitHubGatewayRoutes(r, githubGatewayDeps{Handler: deps.GitHubGateway})
	}
	if deps.SlackGateway != nil {
		mountSlackGatewayRoutes(r, slackGatewayDeps{Handler: deps.SlackGateway})
	}
	if installBridge != nil {
		mountInstallGatewayRoutes(r, installGatewayDeps{Bridge: installBridge})
	}

	mountMemoryStoreRoutes(r, memoryStoresDeps{
		MemoryStores: deps.MemoryStores,
		Dreams:       deps.Dreams,
	})
	evalDeps := evalRunsDeps{
		EvalRuns:     deps.EvalRuns,
		Agents:       deps.Agents,
		Environments: deps.Environments,
	}
	r.Route("/v1/evals", func(r chi.Router) {
		mountEvalRunRoutes(r, evalDeps)
	})
	mountDreamRoutes(r, dreamsDeps{
		Dreams:       deps.Dreams,
		MemoryStores: deps.MemoryStores,
		Sessions:     sessionRepoFromHandlers(deps.Sessions),
		Worker:       deps.DreamWorker,
	})
	costDeps := costReportDeps{
		Events:   deps.Events,
		Sessions: sessionRepoFromHandlers(deps.Sessions),
	}
	r.Route("/v1/cost_report", func(r chi.Router) {
		mountCostReportRoutes(r, costDeps)
	})

	mountModelsListRoutes(r, modelsListDeps{})

	mountInternalRoutes(r, internalDeps{
		Secret:        deps.InternalSecret,
		Cards:         deps.ModelCards,
		Resolver:      deps.ModelResolver,
		Tenants:       deps.Tenants,
		Sessions:      deps.Sessions,
		Vaults:        deps.Vaults,
		Credentials:   deps.Credentials,
		LinearGateway: deps.LinearGateway,
		GitHubGateway: deps.GitHubGateway,
		SlackGateway:  deps.SlackGateway,
		RuntimeRooms:  deps.RuntimeRooms,
	})

	mountOmaAliasRoutes(r, deps)

	mountConsoleStubRoutes(r, consoleStubDeps{
		SessionOutputs: deps.SessionOutputs,
		Files:          deps.Files,
		FileBlobs:      deps.FileBlobs,
		RateLimit:      deps.RateLimit,
	})

	if deps.ConsoleDir != "" {
		static := console.NewStaticHandler(deps.ConsoleDir)
		r.NotFound(static.ServeHTTP)
	}

	return r
}

func sessionRepoFromHandlers(h *sessionHandlers) *store.SessionRepo {
	if h == nil {
		return nil
	}
	return h.sessions
}

// NewSessionHandlers builds session HTTP dependencies.
func NewSessionHandlers(
	sessions *store.SessionRepo,
	agents *store.AgentRepo,
	events *store.EventRepo,
	pending *store.PendingRepo,
	hub *stream.Hub,
	registry *session.Registry,
	workdirs *workdir.Manager,
	outputs *sessionoutputs.Store,
	client harness.Client,
	models *modelresolve.Resolver,
	resources *harness.ResourceResolver,
	wakeups *store.WakeupRepo,
	mcpProxyBase string,
	mcpProxyKey string,
	platformBase string,
	internalSecret string,
	outboundProxyAddr string,
	outboundProxyKey string,
) *sessionHandlers {
	return &sessionHandlers{
		sessions:          sessions,
		agents:            agents,
		events:            events,
		pending:           pending,
		hub:               hub,
		registry:          registry,
		workdirs:          workdirs,
		outputs:           outputs,
		harness:           client,
		models:            models,
		resources:         resources,
		wakeups:           wakeups,
		mcpProxyBase:      mcpProxyBase,
		mcpProxyKey:       mcpProxyKey,
		platformBase:      platformBase,
		internalSecret:    internalSecret,
		outboundProxyAddr: outboundProxyAddr,
		outboundProxyKey:  outboundProxyKey,
	}
}
