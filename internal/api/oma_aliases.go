package api

import (
	"github.com/go-chi/chi/v5"
)

// mountOmaAliasRoutes mirrors OMA-specific namespaces under /v1/oma/* for
// parity with open-managed-agents main index.
func mountOmaAliasRoutes(r chi.Router, deps Deps) {
	me := meDeps{
		AuthDisabled: deps.AuthDisabled,
		ApiKeys:      deps.ApiKeys,
		Tenants:      deps.Tenants,
	}
	tenant := tenantDeps{Tenants: deps.Tenants}
	gatewayOrigin := integrationsGatewayOrigin()
	integrationDeps := integrationsDeps{
		Integrations:  deps.Integrations,
		GatewayOrigin: gatewayOrigin,
		Linear:        deps.LinearGateway,
	}
	evalDeps := evalRunsDeps{
		EvalRuns:     deps.EvalRuns,
		Agents:       deps.Agents,
		Environments: deps.Environments,
	}
	costDeps := costReportDeps{
		Events:   deps.Events,
		Sessions: sessionRepoFromHandlers(deps.Sessions),
	}

	if deps.ApiKeys != nil {
		r.Route("/v1/oma/api_keys", func(r chi.Router) {
			mountApiKeyRoutes(r, deps.ApiKeys)
		})
	}

	r.Route("/v1/oma/me", func(r chi.Router) {
		mountMeRoutes(r, me)
	})

	if deps.Tenants != nil {
		r.Route("/v1/oma/tenants", func(r chi.Router) {
			mountTenantRoutes(r, tenant)
		})
	}

	if deps.EvalRuns != nil {
		r.Route("/v1/oma/evals", func(r chi.Router) {
			mountEvalRunRoutes(r, evalDeps)
		})
	}

	if deps.Events != nil {
		r.Route("/v1/oma/cost_report", func(r chi.Router) {
			mountCostReportRoutes(r, costDeps)
		})
	}

	r.Route("/v1/oma/integrations", func(r chi.Router) {
		mountIntegrationRoutes(r, integrationDeps)
	})

	if deps.Runtimes != nil {
		rtDeps := runtimesDeps{
			Runtimes:       deps.Runtimes,
			ApiKeys:        deps.ApiKeys,
			Tenants:        deps.Tenants,
			Rooms:          deps.RuntimeRooms,
			InternalSecret: deps.InternalSecret,
		}
		r.Route("/v1/oma/runtimes", func(r chi.Router) {
			mountRuntimeRoutes(r, rtDeps)
		})
	}

	if deps.ModelCards != nil {
		r.Route("/v1/oma/model_cards", func(r chi.Router) {
			mountModelCardRoutes(r, deps.ModelCards)
		})
	}

	oauthDeps := oauthV1Deps{
		Vaults:      deps.Vaults,
		Credentials: deps.Credentials,
		State:       deps.OAuthState,
		PublicURL:   deps.PublicURL,
	}
	r.Route("/v1/oma/oauth", func(r chi.Router) {
		mountOAuthV1Routes(r, oauthDeps)
	})

	if deps.Skills != nil && deps.SkillFiles != nil {
		r.Route("/v1/oma/clawhub", func(r chi.Router) {
			mountClawhubRoutes(r, clawhubDeps{
				Skills:     deps.Skills,
				SkillFiles: deps.SkillFiles,
			})
		})
	}
}
