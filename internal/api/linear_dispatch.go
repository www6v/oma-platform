package api

import (
	"context"
	"encoding/json"

	"github.com/open-ma/oma-building/internal/integrations/linear"
	"github.com/open-ma/oma-building/internal/store"
)

type linearSessionDispatch struct {
	sessions *sessionHandlers
}

func (d *linearSessionDispatch) DispatchUserMessage(
	ctx context.Context,
	input linear.DispatchInput,
) (string, error) {
	sess, err := d.sessions.sessions.Create(ctx, store.CreateSessionInput{
		TenantID:      input.TenantID,
		AgentID:       input.AgentID,
		Title:         input.Title,
		EnvironmentID: input.EnvironmentID,
	})
	if err != nil {
		return "", err
	}
	d.sessions.registerMachine(sess)
	if err := d.sessions.registry.EnqueueEvents(
		ctx, sess.ID, []json.RawMessage{input.UserEvent}, true, false, nil,
	); err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (d *linearSessionDispatch) ResumeUserMessage(
	ctx context.Context,
	sessionID string,
	userEvent []byte,
) error {
	sess, err := d.sessions.sessions.Get(ctx, "", sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return store.ErrNotFound
	}
	d.sessions.registerMachine(sess)
	return d.sessions.registry.EnqueueEvents(
		ctx, sessionID, []json.RawMessage{userEvent}, true, false, nil,
	)
}
