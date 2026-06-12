package outbound

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/open-ma/oma-building/internal/store"
)

const (
	sessionHeader = "X-OMA-Session-Id"
	defaultTenant = "default"
)

// ProxyDeps configures the outbound HTTP forward proxy.
type ProxyDeps struct {
	Resolver *Resolver
	ApiKeys  *store.ApiKeyRepo
	APIKey   string
}

// Proxy forwards sandbox HTTP with optional vault bearer injection.
type Proxy struct {
	deps ProxyDeps
}

// NewProxy returns an outbound forward proxy handler.
func NewProxy(deps ProxyDeps) *Proxy {
	return &Proxy{deps: deps}
}

// ServeHTTP implements http.Handler for standard HTTP proxy requests.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p == nil || p.deps.Resolver == nil {
		http.Error(w, "outbound proxy not configured", http.StatusServiceUnavailable)
		return
	}

	tenantID, sessionID, ok := p.resolveAuth(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if sessionID == "" {
		http.Error(w, "session id required", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodConnect {
		http.Error(
			w,
			"HTTPS CONNECT requires oma-vault MITM (not in MVP)",
			http.StatusNotImplemented,
		)
		return
	}

	targetURL, err := requestTargetURL(r)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var bodyBytes []byte
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
	}

	var body io.Reader
	if len(bodyBytes) > 0 {
		body = bytes.NewReader(bodyBytes)
	}

	upReq, err := http.NewRequestWithContext(
		r.Context(), r.Method, targetURL, body,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	copyForwardHeaders(upReq.Header, r.Header)
	upReq.Header.Del(sessionHeader)
	upReq.Header.Del("x-api-key")
	upReq.Header.Del("Proxy-Authorization")
	upReq.Header.Del("Proxy-Connection")

	host := hostnameFromURL(targetURL)
	target, err := p.deps.Resolver.Resolve(
		r.Context(), tenantID, sessionID, host,
	)
	if err != nil {
		log.Printf(
			"outbound resolve tenant=%s session=%s host=%s: %v",
			tenantID, sessionID, host, err,
		)
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if target != nil && target.Token != "" {
		upReq.Header.Set("Authorization", "Bearer "+target.Token)
	}
	if len(bodyBytes) > 0 {
		upReq.ContentLength = int64(len(bodyBytes))
		upReq.Header.Set("Content-Length", strconv.Itoa(len(bodyBytes)))
	}

	resp, err := http.DefaultClient.Do(upReq)
	if err != nil {
		http.Error(w, "upstream unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyForwardHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (p *Proxy) resolveAuth(r *http.Request) (tenantID, sessionID string, ok bool) {
	sessionID = strings.TrimSpace(r.Header.Get(sessionHeader))
	key := strings.TrimSpace(r.Header.Get("x-api-key"))
	if key == "" {
		authHeader := r.Header.Get("Proxy-Authorization")
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			key = strings.TrimSpace(authHeader[7:])
		}
	}
	if key == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			key = strings.TrimSpace(authHeader[7:])
		}
	}
	if key == "" {
		return "", sessionID, false
	}
	if p.deps.APIKey != "" && key == p.deps.APIKey {
		return defaultTenant, sessionID, true
	}
	if p.deps.ApiKeys == nil {
		return "", sessionID, false
	}
	sum := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(sum[:])
	rec, err := p.deps.ApiKeys.FindByHash(r.Context(), hash)
	if err != nil || rec == nil {
		return "", sessionID, false
	}
	tid := rec.TenantID
	if tid == "" {
		tid = defaultTenant
	}
	return tid, sessionID, true
}

func requestTargetURL(r *http.Request) (string, error) {
	if r.URL != nil && r.URL.IsAbs() {
		return r.URL.String(), nil
	}
	raw := strings.TrimSpace(r.RequestURI)
	if raw == "" || strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("absolute request uri required")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() {
		return "", err
	}
	return u.String(), nil
}

func copyForwardHeaders(dst, src http.Header) {
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
		"upgrade", "host", "proxy-connection":
		return true
	default:
		return false
	}
}

// ListenAndServe starts the outbound proxy on addr until ctx is done.
func ListenAndServe(ctx context.Context, addr string, handler http.Handler) error {
	if addr == "" {
		return nil
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: handler}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	log.Printf("outbound proxy listening on %s", addr)
	err = srv.Serve(ln)
	if err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
