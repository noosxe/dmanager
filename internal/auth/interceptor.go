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

		var authCtx context.Context
		var err error

		if isUnauthenticatedProcedure(procedure) {
			authCtx = ctx
			if sessionID := parseSessionCookie(cookieHeader); sessionID != "" {
				if actx, aerr := i.authenticate(ctx, cookieHeader); aerr == nil {
					authCtx = actx
				}
			}
		} else {
			authCtx, err = i.authenticate(ctx, cookieHeader)
			if err != nil {
				i.logger.Info("Unauthorized request blocked", "procedure", procedure, "error", err)
				return nil, err
			}
		}

		userStr := "unauthenticated"
		if u, ok := UserFromContext(authCtx); ok {
			userStr = u.Username
		}

		startTime := time.Now()
		resp, err := next(authCtx, req)
		duration := time.Since(startTime)

		if err != nil {
			i.logger.Info("Request failed",
				"procedure", procedure,
				"user", userStr,
				"duration", duration,
				"error", err.Error(),
			)
		} else {
			i.logger.Info("Request completed",
				"procedure", procedure,
				"user", userStr,
				"duration", duration,
			)
		}
		return resp, err
	})
}

func (i *Interceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *Interceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return connect.StreamingHandlerFunc(func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		procedure := conn.Spec().Procedure
		cookieHeader := conn.RequestHeader().Get("Cookie")

		var authCtx context.Context
		var err error

		if isUnauthenticatedProcedure(procedure) {
			authCtx = ctx
			if sessionID := parseSessionCookie(cookieHeader); sessionID != "" {
				if actx, aerr := i.authenticate(ctx, cookieHeader); aerr == nil {
					authCtx = actx
				}
			}
		} else {
			authCtx, err = i.authenticate(ctx, cookieHeader)
			if err != nil {
				i.logger.Info("Unauthorized streaming request blocked", "procedure", procedure, "error", err)
				return err
			}
		}

		userStr := "unauthenticated"
		if u, ok := UserFromContext(authCtx); ok {
			userStr = u.Username
		}

		startTime := time.Now()
		i.logger.Info("Streaming request started", "procedure", procedure, "user", userStr)
		streamErr := next(authCtx, conn)
		duration := time.Since(startTime)

		if streamErr != nil {
			i.logger.Info("Streaming request failed",
				"procedure", procedure,
				"user", userStr,
				"duration", duration,
				"error", streamErr.Error(),
			)
		} else {
			i.logger.Info("Streaming request completed",
				"procedure", procedure,
				"user", userStr,
				"duration", duration,
			)
		}
		return streamErr
	})
}

func isUnauthenticatedProcedure(procedure string) bool {
	return procedure == "/dmanager.v1.AuthService/GetServerStatus" ||
		procedure == "/dmanager.v1.AuthService/SetupAdmin" ||
		procedure == "/dmanager.v1.AuthService/Login" ||
		procedure == "/dmanager.v1.LogService/SyncLogs"
}
