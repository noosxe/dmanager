package auth

import (
	"context"
	"time"

	"dmanager/internal/db"
)

type contextKey string

const (
	userContextKey    contextKey = "user"
	sessionContextKey contextKey = "session"
	refreshContextKey contextKey = "session-refresh"
)

func WithUser(ctx context.Context, user db.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func UserFromContext(ctx context.Context) (db.User, bool) {
	u, ok := ctx.Value(userContextKey).(db.User)
	return u, ok
}

func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionContextKey, sessionID)
}

func SessionIDFromContext(ctx context.Context) (string, bool) {
	s, ok := ctx.Value(sessionContextKey).(string)
	return s, ok
}

// sessionRefresh records that authentication slid the session's idle expiry,
// so the interceptor can renew the session cookie's lifetime to match.
type sessionRefresh struct {
	sessionID string
	expiresAt time.Time
}

func withSessionRefresh(ctx context.Context, r sessionRefresh) context.Context {
	return context.WithValue(ctx, refreshContextKey, r)
}

func sessionRefreshFromContext(ctx context.Context) (sessionRefresh, bool) {
	r, ok := ctx.Value(refreshContextKey).(sessionRefresh)
	return r, ok
}
