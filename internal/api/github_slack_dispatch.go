package api

import (
	"context"
	"encoding/json"

	"github.com/open-ma/oma-building/internal/integrations/github"
	"github.com/open-ma/oma-building/internal/integrations/slack"
	"github.com/open-ma/oma-building/internal/store"
)

type githubSessionDispatch struct {
	sessions *sessionHandlers
}

func (d *githubSessionDispatch) DispatchUserMessage(
	ctx context.Context,
	input github.DispatchInput,
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

func (d *githubSessionDispatch) ResumeUserMessage(
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

type slackSessionDispatch struct {
	sessions *sessionHandlers
}

func (d *slackSessionDispatch) DispatchUserMessage(
	ctx context.Context,
	input slack.DispatchInput,
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

func (d *slackSessionDispatch) ResumeUserMessage(
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

// NewGitHubGatewayHandler wires GitHub webhook dispatch.
func NewGitHubGatewayHandler(
	integrations *store.IntegrationRepo,
	sessions *sessionHandlers,
	origin string,
) *github.Handler {
	if integrations == nil || sessions == nil {
		return nil
	}
	if origin == "" {
		origin = integrationsGatewayOrigin()
	}
	return &github.Handler{
		Integrations: integrations,
		Origin:       origin,
		Dispatch:     &githubSessionDispatch{sessions: sessions},
	}
}

// NewSlackGatewayHandler wires Slack webhook dispatch.
func NewSlackGatewayHandler(
	integrations *store.IntegrationRepo,
	sessions *sessionHandlers,
	origin string,
) *slack.Handler {
	if integrations == nil || sessions == nil {
		return nil
	}
	if origin == "" {
		origin = integrationsGatewayOrigin()
	}
	return &slack.Handler{
		Integrations: integrations,
		Origin:       origin,
		Dispatch:     &slackSessionDispatch{sessions: sessions},
	}
}
