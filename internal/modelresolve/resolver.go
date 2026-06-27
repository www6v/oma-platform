package modelresolve

import (
	"context"
	"os"
	"strings"

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
	cfg, err := r.resolve(ctx, tenantID, agentModel)
	if err != nil {
		return harness.ModelConfig{}, err
	}
	return remapLegacyClaude(cfg), nil
}

func (r *Resolver) resolve(
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

	if isQwenModel(agentModel) {
		cfg := harness.ModelConfig{
			Model:    bareModelID(agentModel),
			Provider: "dashscope",
		}
		if key := os.Getenv("DASHSCOPE_API_KEY"); key != "" {
			cfg.APIKey = key
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

func remapLegacyClaude(cfg harness.ModelConfig) harness.ModelConfig {
	if !isClaudeModel(cfg.Model) {
		return cfg
	}
	target := os.Getenv("OMA_DEFAULT_MODEL")
	if target == "" {
		target = "qwen3.7-plus"
	}
	if isClaudeModel(target) {
		return cfg
	}
	out := harness.ModelConfig{
		Model:    target,
		Provider: "dashscope",
	}
	if key := os.Getenv("DASHSCOPE_API_KEY"); key != "" {
		out.APIKey = key
	}
	return out
}

func isClaudeModel(model string) bool {
	m := bareModelID(model)
	return strings.HasPrefix(strings.ToLower(m), "claude-")
}

func bareModelID(model string) string {
	m := strings.TrimSpace(model)
	if idx := strings.LastIndex(m, "/"); idx >= 0 {
		return m[idx+1:]
	}
	return m
}

func looksLikeProviderModel(model string) bool {
	if len(model) < 3 {
		return false
	}
	return model != "faux/test"
}

func isQwenModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(bareModelID(model)), "qwen")
}
