package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/open-ma/oma-building/internal/eval"
	"github.com/open-ma/oma-building/internal/store"
)

type evalSessionRunner struct {
	h *sessionHandlers
}

// NewEvalSessionRunner adapts session handlers for the eval worker.
func NewEvalSessionRunner(h *sessionHandlers) eval.SessionRunner {
	if h == nil {
		return nil
	}
	return &evalSessionRunner{h: h}
}

func (r *evalSessionRunner) CreateTaskSession(
	ctx context.Context,
	tenantID, agentID, environmentID, title string,
) (string, error) {
	sess, err := r.h.sessions.Create(ctx, store.CreateSessionInput{
		TenantID:      tenantID,
		AgentID:       agentID,
		Title:         title,
		EnvironmentID: environmentID,
	})
	if err != nil {
		return "", err
	}
	r.h.registerMachine(sess)
	return sess.ID, nil
}

func (r *evalSessionRunner) SendUserMessage(
	ctx context.Context,
	sessionID, text string,
) error {
	sess, err := r.h.sessions.Get(ctx, "", sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return store.ErrNotFound
	}
	r.h.registerMachine(sess)
	payload, err := json.Marshal(map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	})
	if err != nil {
		return err
	}
	return r.h.registry.EnqueueEvents(
		ctx, sessionID, []json.RawMessage{payload}, true, false, nil,
	)
}

func (r *evalSessionRunner) SessionStatus(
	ctx context.Context,
	tenantID, sessionID string,
) (string, error) {
	sess, err := r.h.sessions.Get(ctx, tenantID, sessionID)
	if err != nil {
		return "", err
	}
	if sess == nil {
		return "", fmt.Errorf("session %s not found", sessionID)
	}
	return string(sess.Status), nil
}
