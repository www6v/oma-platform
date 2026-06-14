package ratelimit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/open-ma/oma-building/internal/auth"
)

var emailSendPaths = map[string]bool{
	"/auth/sign-up/email":                true,
	"/auth/sign-in/email":                true,
	"/auth/forget-password":              true,
	"/auth/email-otp/send-verification-otp": true,
	"/auth/email-otp/reset-password":     true,
}

// Middleware applies /v1/* and /auth/* rate limits after auth middleware.
// Bypasses when gates are disabled or authDisabled is true (dev escape hatch).
func Middleware(gates *Gates, authDisabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if gates == nil || gates.Disabled || authDisabled {
				next.ServeHTTP(w, r)
				return
			}
			path := r.URL.Path
			if strings.HasPrefix(path, "/v1/") {
				if strings.HasPrefix(path, "/v1/internal/") {
					next.ServeHTTP(w, r)
					return
				}
				principal := apiPrincipal(r)
				isWrite := r.Method == http.MethodPost ||
					r.Method == http.MethodPut ||
					r.Method == http.MethodDelete
				var ok bool
				if isWrite {
					ok = gates.AllowAPIWrite(principal)
				} else {
					ok = gates.AllowAPIRead(principal)
				}
				if !ok {
					writeJSONError(w, http.StatusTooManyRequests, "Rate limit exceeded")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(path, "/auth/") {
				ip := ClientIP(r)
				if !gates.AllowAuthIP(ip) {
					writeJSONError(w, http.StatusTooManyRequests, "Too many requests")
					return
				}
				if !emailSendPaths[path] {
					next.ServeHTTP(w, r)
					return
				}
				if !gates.AllowAuthSendIP(ip) {
					writeJSONError(
						w,
						http.StatusTooManyRequests,
						"Too many email requests from this IP",
					)
					return
				}
				email := peekEmail(r)
				if !gates.AllowAuthSendEmail(email) {
					writeJSONError(
						w,
						http.StatusTooManyRequests,
						"Please wait a minute before requesting another email",
					)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP returns the best-effort client address for rate-limit keys.
func ClientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "anonymous"
}

func apiPrincipal(r *http.Request) string {
	if key := strings.TrimSpace(r.Header.Get("x-api-key")); key != "" {
		prefix := key
		if len(prefix) > 16 {
			prefix = prefix[:16]
		}
		return "apikey:" + prefix
	}
	if user, ok := auth.UserFromContext(r.Context()); ok && user.ID != "" {
		return "user:" + user.ID
	}
	return "ip:" + ClientIP(r)
}

func peekEmail(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var payload struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(payload.Email))
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
