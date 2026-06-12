package mcpproxy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/open-ma/oma-building/internal/store"
)

// Target is an upstream MCP server URL plus bearer token for injection.
type Target struct {
	UpstreamURL   string
	UpstreamToken string
}

// MCPServer is one entry from agent_snapshot.mcp_servers.
type MCPServer struct {
	Name               string `json:"name"`
	Type               string `json:"type,omitempty"`
	URL                string `json:"url"`
	AuthorizationToken string `json:"authorization_token,omitempty"`
}

// SessionStore loads persisted sessions for proxy resolution.
type SessionStore interface {
	Get(ctx context.Context, tenantID, sessionID string) (*store.Session, error)
}

// Resolver resolves MCP proxy targets from session state and vault creds.
type Resolver struct {
	Sessions    SessionStore
	Credentials *store.CredentialRepo
}

// Resolve validates tenant/session/server and returns upstream + token.
func (r *Resolver) Resolve(
	ctx context.Context,
	tenantID, sessionID, serverName string,
) (*Target, error) {
	if r == nil || r.Sessions == nil {
		return nil, fmt.Errorf("mcp proxy resolver not configured")
	}
	sess, err := r.Sessions.Get(ctx, tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, store.ErrNotFound
	}
	if sess.ArchivedAt != nil {
		return nil, store.ErrNotFound
	}

	server, err := findMCPServer(sess.AgentSnapshot, serverName)
	if err != nil {
		return nil, err
	}
	if server.URL == "" {
		return nil, fmt.Errorf("mcp server %q has no url", serverName)
	}

	if server.AuthorizationToken != "" {
		return &Target{
			UpstreamURL:   server.URL,
			UpstreamToken: server.AuthorizationToken,
		}, nil
	}

	if r.Credentials == nil {
		return nil, fmt.Errorf("no credential resolver for mcp server %q", serverName)
	}
	cred, err := r.Credentials.FindActiveByMcpURL(
		ctx, tenantID, server.URL,
	)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, fmt.Errorf("no credential for mcp server %q", serverName)
	}
	token, err := tokenFromAuth(cred.Auth)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, fmt.Errorf("credential for mcp server %q has no token", serverName)
	}
	return &Target{UpstreamURL: server.URL, UpstreamToken: token}, nil
}

func findMCPServer(
	snapshot json.RawMessage,
	serverName string,
) (*MCPServer, error) {
	var agent struct {
		MCPServers []MCPServer `json:"mcp_servers"`
	}
	if len(snapshot) > 0 {
		if err := json.Unmarshal(snapshot, &agent); err != nil {
			return nil, fmt.Errorf("parse agent snapshot: %w", err)
		}
	}
	for i := range agent.MCPServers {
		if agent.MCPServers[i].Name == serverName {
			return &agent.MCPServers[i], nil
		}
	}
	return nil, fmt.Errorf("mcp server %q not declared on session agent", serverName)
}

func tokenFromAuth(auth json.RawMessage) (string, error) {
	var m map[string]any
	if err := json.Unmarshal(auth, &m); err != nil {
		return "", fmt.Errorf("parse credential auth: %w", err)
	}
	for _, key := range []string{
		"bearer_token", "token", "access_token",
	} {
		if v, ok := m[key].(string); ok && v != "" {
			return v, nil
		}
	}
	return "", nil
}
