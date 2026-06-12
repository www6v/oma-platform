package modelresolve

import (
	"context"
	"os"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

// Resolver maps agent.model handles to provider credentials.
type Resolver struct {
	Cards *store.ModelCardRepo
}

// Resolve returns harness model config for an agent model field.
func (r *Resolver) Resolve(
	ctx context.Context,
	tenantID, agentModel string,
) (harness.ModelConfig, error) {
	if r == nil || r.Cards == nil || agentModel == "" {
		return harness.ModelConfig{Model: agentModel}, nil
	}

	card, err := r.Cards.GetByModelID(ctx, tenantID, agentModel)
	if err != nil {
		return harness.ModelConfig{}, err
	}
	if card != nil {
		key, err := r.Cards.GetAPIKey(ctx, tenantID, card.ID)
		if err != nil {
			return harness.ModelConfig{}, err
		}
		cfg := harness.ModelConfig{
			Model:    card.Model,
			Provider: card.Provider,
			APIKey:   key,
			BaseURL:  card.BaseURL,
		}
		if len(card.CustomHeaders) > 0 {
			cfg.CustomHeaders = card.CustomHeaders
		}
		return cfg, nil
	}

	defaultCard, err := r.Cards.GetDefault(ctx, tenantID)
	if err != nil {
		return harness.ModelConfig{}, err
	}
	if defaultCard != nil && looksLikeProviderModel(agentModel) {
		key, err := r.Cards.GetAPIKey(ctx, tenantID, defaultCard.ID)
		if err != nil {
			return harness.ModelConfig{}, err
		}
		cfg := harness.ModelConfig{
			Model:    agentModel,
			Provider: defaultCard.Provider,
			APIKey:   key,
			BaseURL:  defaultCard.BaseURL,
		}
		if len(defaultCard.CustomHeaders) > 0 {
			cfg.CustomHeaders = defaultCard.CustomHeaders
		}
		return cfg, nil
	}

	cfg := harness.ModelConfig{Model: agentModel}
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		cfg.APIKey = key
		cfg.Provider = "ant"
	}
	return cfg, nil
}

func looksLikeProviderModel(model string) bool {
	if len(model) < 3 {
		return false
	}
	return model != "faux/test"
}
