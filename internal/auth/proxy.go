package auth

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// NewAuthProxy forwards /auth/* to the better-auth sidecar.
func NewAuthProxy(upstream string) (http.Handler, error) {
	target, err := url.Parse(strings.TrimRight(upstream, "/"))
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(w, "auth upstream unavailable: "+err.Error(), http.StatusBadGateway)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.Host = target.Host
		proxy.ServeHTTP(w, r)
	}), nil
}
