package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

type statsDeps struct {
	Agents       *store.AgentRepo
	Sessions     *store.SessionRepo
	Environments *store.EnvironmentRepo
	ModelCards   *store.ModelCardRepo
	Vaults       *store.VaultRepo
	Skills       *store.SkillRepo
	ApiKeys      *store.ApiKeyRepo
}

type statsResponse struct {
	Agents        int `json:"agents"`
	Sessions      int `json:"sessions"`
	Environments  int `json:"environments"`
	Vaults        int `json:"vaults"`
	Skills        int `json:"skills"`
	ModelCards    int `json:"model_cards"`
	APIKeys       int `json:"api_keys"`
}

func mountStatsRoutes(r chi.Router, deps statsDeps) {
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		tenant := tenantID(req)

		agents, err := deps.Agents.CountActive(ctx, tenant)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		sessions, err := deps.Sessions.CountActive(ctx, tenant)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		environments, err := deps.Environments.CountActive(ctx, tenant)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		modelCards, err := deps.ModelCards.CountActive(ctx, tenant)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		apiKeys := 0
		if deps.ApiKeys != nil {
			apiKeys, err = deps.ApiKeys.Count(ctx, tenant)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		vaults := 0
		if deps.Vaults != nil {
			vaults, err = deps.Vaults.CountActive(ctx, tenant)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		skills := store.BuiltinSkillCount()
		if deps.Skills != nil {
			custom, err := deps.Skills.CountCustom(ctx, tenant)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			skills += custom
		}

		writeJSON(w, http.StatusOK, statsResponse{
			Agents:       agents,
			Sessions:     sessions,
			Environments: environments,
			Vaults:       vaults,
			Skills:       skills,
			ModelCards:   modelCards,
			APIKeys:      apiKeys,
		})
	})
}
