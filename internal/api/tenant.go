package api

import (
	"net/http"

	"github.com/open-ma/oma-building/internal/auth"
)

func tenantID(r *http.Request) string {
	if id := auth.TenantFromContext(r.Context()); id != "" {
		return id
	}
	return defaultTenant
}

func userID(r *http.Request) string {
	if user, ok := auth.UserFromContext(r.Context()); ok {
		return user.ID
	}
	return ""
}
