package auth

import "context"

type ctxKey int

const (
	tenantKey ctxKey = iota + 1
	userKey
)

// User is the authenticated console user from a cookie session.
type User struct {
	ID    string
	Email string
	Name  string
}

// WithTenant stores the resolved tenant id on the request context.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey, tenantID)
}

// TenantFromContext returns the tenant id set by auth middleware.
func TenantFromContext(ctx context.Context) string {
	v, _ := ctx.Value(tenantKey).(string)
	return v
}

// WithUser stores the authenticated user on the request context.
func WithUser(ctx context.Context, user User) context.Context {
	return context.WithValue(ctx, userKey, user)
}

// UserFromContext returns the user set by auth middleware.
func UserFromContext(ctx context.Context) (User, bool) {
	v, ok := ctx.Value(userKey).(User)
	return v, ok
}
