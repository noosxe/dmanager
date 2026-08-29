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
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
)

type Interceptor struct {
	queries     *db.Queries
	logger      *slog.Logger
	idleTimeout time.Duration
}

func NewInterceptor(queries *db.Queries, logger *slog.Logger, idleTimeout time.Duration) *Interceptor {
	if idleTimeout <= 0 {
		idleTimeout = 168 * time.Hour
	}
	return &Interceptor{
		queries:     queries,
		logger:      logger,
		idleTimeout: idleTimeout,
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

	now := time.Now()
	if now.After(session.AbsoluteExpiresAt) {
		_ = i.queries.DeleteSession(ctx, sessionID)
		return ctx, connect.NewError(connect.CodeUnauthenticated, errors.New("session expired"))
	}
	if now.After(session.ExpiresAt) {
		_ = i.queries.DeleteSession(ctx, sessionID)
		return ctx, connect.NewError(connect.CodeUnauthenticated, errors.New("session expired"))
	}

	// Slide only when now > expires_at - idle_timeout/2 (avoid a DB write on every request), clamped to absolute_expires_at
	if now.After(session.ExpiresAt.Add(-i.idleTimeout / 2)) {
		newIdle := now.Add(i.idleTimeout)
		if newIdle.After(session.AbsoluteExpiresAt) {
			newIdle = session.AbsoluteExpiresAt
		}
		_ = i.queries.TouchSession(ctx, db.TouchSessionParams{
			SessionID:  session.SessionID,
			ExpiresAt:  newIdle,
			LastSeenAt: now,
		})
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

type ProcedureRole int

const (
	RoleUnauthenticated ProcedureRole = iota
	RoleViewer
	RoleAdmin
)

var procedureRoles = map[string]ProcedureRole{
	// Unauthenticated allowlist
	dmanagerv1connect.AuthServiceGetServerStatusProcedure:    RoleUnauthenticated,
	dmanagerv1connect.AuthServiceSetupAdminProcedure:         RoleUnauthenticated,
	dmanagerv1connect.AuthServiceLoginProcedure:              RoleUnauthenticated,
	dmanagerv1connect.AuthServiceBeginPasskeyLoginProcedure:  RoleUnauthenticated,
	dmanagerv1connect.AuthServiceFinishPasskeyLoginProcedure: RoleUnauthenticated,

	// Viewer procedures (any authenticated user)
	dmanagerv1connect.AuthServiceLogoutProcedure:                    RoleViewer,
	dmanagerv1connect.AuthServiceGetMeProcedure:                     RoleViewer,
	dmanagerv1connect.AuthServiceListAuthEventsProcedure:            RoleViewer,
	dmanagerv1connect.AuthServiceListSessionsProcedure:              RoleViewer,
	dmanagerv1connect.AuthServiceRevokeSessionProcedure:             RoleViewer,
	dmanagerv1connect.AuthServiceRevokeAllOtherSessionsProcedure:    RoleViewer,
	dmanagerv1connect.AuthServiceBeginPasskeyRegistrationProcedure:  RoleViewer,
	dmanagerv1connect.AuthServiceFinishPasskeyRegistrationProcedure: RoleViewer,
	dmanagerv1connect.AuthServiceListPasskeysProcedure:              RoleViewer,
	dmanagerv1connect.AuthServiceRenamePasskeyProcedure:             RoleViewer,
	dmanagerv1connect.AuthServiceDeletePasskeyProcedure:             RoleViewer,
	dmanagerv1connect.ContainerServiceListContainersProcedure:       RoleViewer,
	dmanagerv1connect.ContainerServiceGetContainerLogsProcedure:     RoleViewer,
	dmanagerv1connect.ContainerServiceStreamContainersProcedure:     RoleViewer,
	dmanagerv1connect.SettingsServiceGetSettingsProcedure:           RoleViewer,
	dmanagerv1connect.SettingsServiceGetRegistryStatusProcedure:     RoleViewer,
	dmanagerv1connect.LogServiceGetSystemLogsProcedure:              RoleViewer,
	dmanagerv1connect.LogServiceSyncLogsProcedure:                   RoleViewer,
	dmanagerv1connect.AdminServiceListImagesProcedure:               RoleViewer,
	dmanagerv1connect.AdminServiceListVolumesProcedure:              RoleViewer,
	dmanagerv1connect.AdminServiceGetVolumeUsageProcedure:           RoleViewer,
	dmanagerv1connect.AdminServiceListNetworksProcedure:             RoleViewer,
	dmanagerv1connect.AdminServiceGetBuildCacheStatsProcedure:       RoleViewer,
	dmanagerv1connect.AdminServiceListBuildCacheRecordsProcedure:    RoleViewer,
	dmanagerv1connect.AdminServiceCheckEngineProcedure:              RoleViewer,

	// Admin procedures (requires User.Role == "admin")
	dmanagerv1connect.AdminServiceDeleteImageProcedure:                RoleAdmin,
	dmanagerv1connect.ContainerServiceStartContainerProcedure:         RoleAdmin,
	dmanagerv1connect.AdminServicePruneImagesProcedure:                RoleAdmin,
	dmanagerv1connect.AdminServicePruneBuildCacheProcedure:            RoleAdmin,
	dmanagerv1connect.AdminServicePruneBuildCacheRecordProcedure:      RoleAdmin,
	dmanagerv1connect.AdminServicePruneVolumesProcedure:               RoleAdmin,
	dmanagerv1connect.ContainerServiceStopContainerProcedure:          RoleAdmin,
	dmanagerv1connect.ContainerServiceUpgradeContainerProcedure:       RoleAdmin,
	dmanagerv1connect.ContainerServiceSetContainerAutoUpdateProcedure: RoleAdmin,
	dmanagerv1connect.ContainerServiceCheckContainerUpdatesProcedure:  RoleAdmin,
	dmanagerv1connect.SettingsServiceUpdateSettingsProcedure:          RoleAdmin,
	dmanagerv1connect.SettingsServiceTestGotifyNotificationProcedure:  RoleAdmin,
}

func getProcedureRole(procedure string) (ProcedureRole, bool) {
	role, ok := procedureRoles[procedure]
	return role, ok
}

func (i *Interceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return connect.UnaryFunc(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		procedure := req.Spec().Procedure
		cookieHeader := req.Header().Get("Cookie")

		requiredRole, classified := getProcedureRole(procedure)
		if !classified {
			i.logger.Error("Unclassified procedure blocked", "procedure", procedure)
			return nil, connect.NewError(connect.CodeInternal, errors.New("procedure is not registered in access control matrix"))
		}

		var authCtx context.Context
		var err error

		if requiredRole == RoleUnauthenticated {
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

			if requiredRole == RoleAdmin {
				u, _ := UserFromContext(authCtx)
				if u.Role != adminRole {
					i.logger.Info("Forbidden request blocked", "procedure", procedure, "user", u.Username, "role", u.Role)
					return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin role required"))
				}
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

		requiredRole, classified := getProcedureRole(procedure)
		if !classified {
			i.logger.Error("Unclassified streaming procedure blocked", "procedure", procedure)
			return connect.NewError(connect.CodeInternal, errors.New("procedure is not registered in access control matrix"))
		}

		var authCtx context.Context
		var err error

		if requiredRole == RoleUnauthenticated {
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

			if requiredRole == RoleAdmin {
				u, _ := UserFromContext(authCtx)
				if u.Role != adminRole {
					i.logger.Info("Forbidden streaming request blocked", "procedure", procedure, "user", u.Username, "role", u.Role)
					return connect.NewError(connect.CodePermissionDenied, errors.New("admin role required"))
				}
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
