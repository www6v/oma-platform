package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/open-ma/oma-building/internal/store"
)

// Target is a bearer token to inject for outbound HTTP to a hostname.
type Target struct {
	Token string
}

// SessionStore loads persisted sessions for outbound resolution.
type SessionStore interface {
	Get(ctx context.Context, tenantID, sessionID string) (*store.Session, error)
}

// Resolver picks vault credentials by upstream hostname.
type Resolver struct {
	Sessions    SessionStore
	Credentials *store.CredentialRepo
}

// Resolve returns a bearer token for hostname, or nil when no match.
func (r *Resolver) Resolve(
	ctx context.Context,
	tenantID, sessionID, hostname string,
) (*Target, error) {
	if r == nil || r.Sessions == nil || r.Credentials == nil {
		return nil, fmt.Errorf("outbound resolver not configured")
	}
	host := normalizeHostname(hostname)
	if host == "" {
		return nil, fmt.Errorf("hostname required")
	}

	sess, err := r.Sessions.Get(ctx, tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	if sess == nil || sess.ArchivedAt != nil {
		return nil, store.ErrNotFound
	}

	creds, err := r.Credentials.ListActiveForTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	var best *store.Credential
	var bestTS int64
	for _, cred := range creds {
		credHost := hostnameFromCredential(cred)
		if credHost == "" || credHost != host {
			continue
		}
		token, err := tokenFromAuth(cred.Auth)
		if err != nil {
			return nil, err
		}
		if token == "" {
			continue
		}
		ts := cred.CreatedAt
		if cred.UpdatedAt != nil && *cred.UpdatedAt > ts {
			ts = *cred.UpdatedAt
		}
		if best == nil || ts > bestTS {
			copy := *cred
			best = &copy
			bestTS = ts
		}
	}
	if best == nil {
		return nil, nil
	}
	token, err := tokenFromAuth(best.Auth)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, nil
	}
	return &Target{Token: token}, nil
}

func hostnameFromCredential(cred *store.Credential) string {
	if cred == nil {
		return ""
	}
	var auth map[string]any
	if err := json.Unmarshal(cred.Auth, &auth); err == nil && auth != nil {
		if raw, ok := auth["mcp_server_url"].(string); ok && raw != "" {
			return hostnameFromURL(raw)
		}
	}
	return ""
}

func hostnameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return normalizeHostname(u.Hostname())
}

func normalizeHostname(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return ""
	}
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return host
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
