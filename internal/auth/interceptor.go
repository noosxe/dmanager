package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"log/slog"

	connect "connectrpc.com/connect"

	"dmanager/internal/db"
)

type Interceptor struct {
	queries *db.Queries
	logger  *slog.Logger
}

func NewInterceptor(queries *db.Queries, logger *slog.Logger) *Interceptor {
	return &Interceptor{
		queries: queries,
		logger:  logger,
	}
}

func (i *Interceptor) authenticate(ctx context.Context, cookieHeader string) (context.Context, error) {
	sessionID := parseSessionCookie(cookieHeader)
	if sessionID == "" {
		return ctx, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}

	session, err := i.queries.GetSession(ctx, sessionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ctx, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
		}
		return ctx, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query session: %w", err))
	}

	if session.ExpiresAt.Before(time.Now()) {
		_ = i.queries.DeleteSession(ctx, sessionID)
		return ctx, connect.NewError(connect.CodeUnauthenticated, errors.New("session expired"))
	}

	user, err := i.queries.GetUser(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ctx, connect.NewError(connect.CodeUnauthenticated, errors.New("user not found"))
		}
		return ctx, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query user: %w", err))
	}

	ctx = WithUser(ctx, user)
	ctx = WithSessionID(ctx, sessionID)
	return ctx, nil
}

func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return connect.UnaryFunc(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		procedure := req.Spec().Procedure
		cookieHeader := req.Header().Get("Cookie")

		if isUnauthenticatedProcedure(procedure) {
			if sessionID := parseSessionCookie(cookieHeader); sessionID != "" {
				if authCtx, err := i.authenticate(ctx, cookieHeader); err == nil {
					ctx = authCtx
				}
			}
			return next(ctx, req)
		}

		authCtx, err := i.authenticate(ctx, cookieHeader)
		if err != nil {
			return nil, err
		}
		return next(authCtx, req)
	})
}

func (i *Interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *Interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return connect.StreamingHandlerFunc(func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		procedure := conn.Spec().Procedure
		cookieHeader := conn.RequestHeader().Get("Cookie")

		if isUnauthenticatedProcedure(procedure) {
			if sessionID := parseSessionCookie(cookieHeader); sessionID != "" {
				if authCtx, err := i.authenticate(ctx, cookieHeader); err == nil {
					ctx = authCtx
				}
			}
			return next(ctx, conn)
		}

		authCtx, err := i.authenticate(ctx, cookieHeader)
		if err != nil {
			return err
		}
		return next(authCtx, conn)
	})
}

func isUnauthenticatedProcedure(procedure string) bool {
	return procedure == "/dmanager.v1.AuthService/GetServerStatus" ||
		procedure == "/dmanager.v1.AuthService/SetupAdmin" ||
		procedure == "/dmanager.v1.AuthService/Login" ||
		procedure == "/dmanager.v1.LogService/SyncLogs"
}
