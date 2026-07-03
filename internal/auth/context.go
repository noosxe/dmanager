package auth

import (
	"context"
	"dmanager/internal/db"
)

type contextKey string

const (
	userContextKey    contextKey = "user"
	sessionContextKey contextKey = "session"
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
