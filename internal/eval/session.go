package eval

import "context"

// SessionRunner creates eval task sessions and drives user messages.
type SessionRunner interface {
	CreateTaskSession(
		ctx context.Context,
		tenantID, agentID, environmentID, title string,
	) (sessionID string, err error)
	SendUserMessage(ctx context.Context, sessionID, text string) error
	SessionStatus(ctx context.Context, tenantID, sessionID string) (string, error)
}
