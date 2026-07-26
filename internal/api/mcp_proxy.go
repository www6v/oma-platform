package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/auth"
	"github.com/open-ma/oma-building/internal/mcpproxy"
	"github.com/open-ma/oma-building/internal/store"
)

const defaultTenantID = "default"

// sessionTenantLookup resolves a session's tenant without a prior tenant id.
// Used when the harness authenticates with the global OMA_API_KEY (not a
// per-tenant key) — same idea as open-managed-agents MCP proxy RPC path,
// where tenant comes from session context rather than a single default.
type sessionTenantLookup interface {
	GetByID(ctx context.Context, id string) (*store.Session, error)
}

type mcpProxyDeps struct {
	Resolver *mcpproxy.Resolver
	Sessions sessionTenantLookup
	ApiKeys  *store.ApiKeyRepo
	APIKey   string
}

func mountMcpProxyRoutes(r chi.Router, deps mcpProxyDeps) {
	if deps.Resolver == nil {
		return
	}
	h := &mcpProxyHandler{deps: deps}
	r.Route("/v1/mcp-proxy", func(r chi.Router) {
		r.HandleFunc("/{sid}/{serverName}", h.serve)
		r.HandleFunc("/{sid}/{serverName}/*", h.serve)
	})
}

type mcpProxyHandler struct {
	deps mcpProxyDeps
}

func (h *mcpProxyHandler) serve(w http.ResponseWriter, r *http.Request) {
	sid := chi.URLParam(r, "sid")
	serverName := chi.URLParam(r, "serverName")
	if sid == "" || serverName == "" {
		writeError(w, http.StatusBadRequest, "sid and server name required")
		return
	}

	tenantID, ok := h.resolveTenant(r, sid)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	target, err := h.deps.Resolver.Resolve(
		r.Context(), tenantID, sid, serverName,
	)
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}

	var bodyBytes []byte
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
	}

	var body io.Reader
	if len(bodyBytes) > 0 {
		body = bytes.NewReader(bodyBytes)
	}

	upReq, err := http.NewRequestWithContext(
		r.Context(), r.Method, target.UpstreamURL, body,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	copyHeaders(upReq.Header, r.Header)
	upReq.Header.Set("Authorization", "Bearer "+target.UpstreamToken)
	upReq.Header.Del("Host")
	upReq.Header.Del("x-api-key")
	if len(bodyBytes) > 0 {
		upReq.ContentLength = int64(len(bodyBytes))
		upReq.Header.Set("Content-Length", strconv.Itoa(len(bodyBytes)))
	}

	resp, err := http.DefaultClient.Do(upReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream unreachable")
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (h *mcpProxyHandler) resolveTenant(
	r *http.Request,
	sid string,
) (string, bool) {
	if tid := auth.TenantFromContext(r.Context()); tid != "" {
		return tid, true
	}
	key := strings.TrimSpace(r.Header.Get("x-api-key"))
	if key == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			key = strings.TrimSpace(authHeader[7:])
		}
	}
	if key == "" {
		return "", false
	}
	if h.deps.APIKey != "" && key == h.deps.APIKey {
		// Harness turns always carry the process-wide OMA_API_KEY. Resolve
		// tenant from the session id so multi-tenant console users work
		// (previously hard-coded to "default" → 403 for real tenants).
		if sid != "" && h.deps.Sessions != nil {
			sess, err := h.deps.Sessions.GetByID(r.Context(), sid)
			if err == nil && sess != nil && sess.TenantID != "" {
				return sess.TenantID, true
			}
		}
		return defaultTenantID, true
	}
	if h.deps.ApiKeys == nil {
		return "", false
	}
	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])
	rec, err := h.deps.ApiKeys.FindByHash(r.Context(), hash)
	if err != nil || rec == nil {
		return "", false
	}
	tid := rec.TenantID
	if tid == "" {
		tid = defaultTenantID
	}
	return tid, true
}

func copyHeaders(dst, src http.Header) {
	for k, vals := range src {
		if hopByHopHeader(k) {
			continue
		}
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

func hopByHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "proxy-authenticate",
		"proxy-authorization", "te", "trailers", "transfer-encoding",
		"upgrade", "host":
		return true
	default:
		return false
	}
}
