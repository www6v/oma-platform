package ratelimit

import (
	"os"
	"strconv"
	"time"
)

const defaultWindow = time.Minute

// Limits mirrors CF Workers Rate Limiting bindings (wrangler.jsonc).
type Limits struct {
	APIUserRead    int
	APIUserWrite   int
	AuthIP         int
	AuthSendIP     int
	AuthSendEmail  int
	SessionsTenant int
	UploadTenant   int
}

// DefaultLimits returns production parity limits (per 60s window).
func DefaultLimits() Limits {
	return Limits{
		APIUserRead:    600,
		APIUserWrite:   60,
		AuthIP:         60,
		AuthSendIP:     5,
		AuthSendEmail:  1,
		SessionsTenant: 5,
		UploadTenant:   30,
	}
}

// Gates bundles named rate-limit buckets.
type Gates struct {
	Disabled bool
	Limits   Limits
	limiter  *Limiter
}

// NewGates builds gates with the given limits.
func NewGates(limits Limits) *Gates {
	return &Gates{
		Limits:  limits,
		limiter: NewLimiter(),
	}
}

// FromEnv loads limits from env and returns gates. Disabled when
// OMA_RATE_LIMIT_DISABLED=1.
func FromEnv() *Gates {
	if os.Getenv("OMA_RATE_LIMIT_DISABLED") == "1" {
		return &Gates{Disabled: true, Limits: DefaultLimits()}
	}
	limits := DefaultLimits()
	limits.APIUserRead = envInt("OMA_RL_API_USER_READ", limits.APIUserRead)
	limits.APIUserWrite = envInt("OMA_RL_API_USER_WRITE", limits.APIUserWrite)
	limits.AuthIP = envInt("OMA_RL_AUTH_IP", limits.AuthIP)
	limits.AuthSendIP = envInt("OMA_RL_AUTH_SEND_IP", limits.AuthSendIP)
	limits.AuthSendEmail = envInt("OMA_RL_AUTH_SEND_EMAIL", limits.AuthSendEmail)
	limits.SessionsTenant = envInt("OMA_RL_SESSIONS_TENANT", limits.SessionsTenant)
	limits.UploadTenant = envInt("OMA_RL_UPLOAD_TENANT", limits.UploadTenant)
	return NewGates(limits)
}

func envInt(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func (g *Gates) allow(bucket, key string, limit int) bool {
	if g == nil || g.Disabled || limit <= 0 {
		return true
	}
	return g.limiter.Allow(bucket+":"+key, limit, defaultWindow)
}

// AllowAPIRead checks the /v1/* read bucket for principal.
func (g *Gates) AllowAPIRead(principal string) bool {
	return g.allow("api_read", principal, g.Limits.APIUserRead)
}

// AllowAPIWrite checks the /v1/* write bucket for principal.
func (g *Gates) AllowAPIWrite(principal string) bool {
	return g.allow("api_write", principal, g.Limits.APIUserWrite)
}

// AllowAuthIP checks generic /auth/* per-IP cap.
func (g *Gates) AllowAuthIP(ip string) bool {
	return g.allow("auth_ip", ip, g.Limits.AuthIP)
}

// AllowAuthSendIP checks email-triggering /auth endpoints per IP.
func (g *Gates) AllowAuthSendIP(ip string) bool {
	return g.allow("auth_send_ip", ip, g.Limits.AuthSendIP)
}

// AllowAuthSendEmail checks per-email throttle on auth send paths.
func (g *Gates) AllowAuthSendEmail(email string) bool {
	if email == "" {
		return true
	}
	return g.allow("auth_send_email", email, g.Limits.AuthSendEmail)
}

// AllowSessionCreate checks POST /v1/sessions per-tenant cap.
func (g *Gates) AllowSessionCreate(tenantID string) bool {
	return g.allow("sessions_tenant", tenantID, g.Limits.SessionsTenant)
}

// AllowUpload checks file/skill upload per-tenant cap.
func (g *Gates) AllowUpload(tenantID string) bool {
	return g.allow("upload_tenant", tenantID, g.Limits.UploadTenant)
}
