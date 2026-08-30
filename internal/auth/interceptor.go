package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"log/slog"

	connect "connectrpc.com/connect"

	"dmanager/internal/audit"
	"dmanager/internal/db"
	dmanagerv1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
)

type Interceptor struct {
	queries     *db.Queries
	logger      *slog.Logger
	idleTimeout time.Duration
	auditor     audit.Auditor // nil disables audit recording
}

func NewInterceptor(queries *db.Queries, logger *slog.Logger, idleTimeout time.Duration, auditor audit.Auditor) *Interceptor {
	if idleTimeout <= 0 {
		idleTimeout = 168 * time.Hour
	}
	return &Interceptor{
		queries:     queries,
		logger:      logger,
		idleTimeout: idleTimeout,
		auditor:     auditor,
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
	dmanagerv1connect.AdminServiceDeleteNetworkProcedure:              RoleAdmin,
	dmanagerv1connect.AdminServicePruneNetworksProcedure:              RoleAdmin,
	dmanagerv1connect.ContainerServiceStopContainerProcedure:          RoleAdmin,
	dmanagerv1connect.ContainerServiceUpgradeContainerProcedure:       RoleAdmin,
	dmanagerv1connect.ContainerServiceSetContainerAutoUpdateProcedure: RoleAdmin,
	dmanagerv1connect.ContainerServiceCheckContainerUpdatesProcedure:  RoleAdmin,
	dmanagerv1connect.SettingsServiceUpdateSettingsProcedure:          RoleAdmin,
	dmanagerv1connect.SettingsServiceTestGotifyNotificationProcedure:  RoleAdmin,
	dmanagerv1connect.AdminServiceListAuditLogsProcedure:              RoleAdmin,
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
					i.recordDenied(authCtx, procedure, u)
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

		if u, ok := UserFromContext(authCtx); ok {
			i.recordOutcome(authCtx, procedure, req, resp, err, u)
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

// auditSpec is the static audit classification of one procedure. Procedures
// absent from auditSpecs are never audited: reads, unauthenticated calls and
// ContainerServiceUpgradeContainer — the upgrade path records richer entries
// itself (old → new image digest, user or system origin) and must not be
// double-recorded here (design.md §12.3).
type auditSpec struct {
	action       string
	resourceType string
}

// auditResource names the audited resource types.
const (
	auditResContainer = "container"
)

var auditSpecs = map[string]auditSpec{
	dmanagerv1connect.AdminServiceDeleteImageProcedure:                {action: "image.delete", resourceType: "image"},
	dmanagerv1connect.AdminServicePruneImagesProcedure:                {action: "image.prune", resourceType: "image"},
	dmanagerv1connect.AdminServicePruneBuildCacheProcedure:            {action: "builder.prune", resourceType: "builder"},
	dmanagerv1connect.AdminServicePruneBuildCacheRecordProcedure:      {action: "builder.record_prune", resourceType: "builder"},
	dmanagerv1connect.AdminServicePruneVolumesProcedure:               {action: "volume.prune", resourceType: "volume"},
	dmanagerv1connect.AdminServiceDeleteNetworkProcedure:              {action: "network.delete", resourceType: "network"},
	dmanagerv1connect.AdminServicePruneNetworksProcedure:              {action: "network.prune", resourceType: "network"},
	dmanagerv1connect.ContainerServiceStartContainerProcedure:         {action: "container.start", resourceType: auditResContainer},
	dmanagerv1connect.ContainerServiceStopContainerProcedure:          {action: "container.stop", resourceType: auditResContainer},
	dmanagerv1connect.ContainerServiceSetContainerAutoUpdateProcedure: {action: "container.auto_update_set", resourceType: auditResContainer},
	dmanagerv1connect.ContainerServiceCheckContainerUpdatesProcedure:  {action: "container.update_check", resourceType: auditResContainer},
	dmanagerv1connect.SettingsServiceUpdateSettingsProcedure:          {action: "settings.update", resourceType: "settings"},
	dmanagerv1connect.SettingsServiceTestGotifyNotificationProcedure:  {action: "settings.gotify_test", resourceType: "settings"},
}

// recordDenied writes an entry for an authenticated non-admin attempting an
// admin mutation — a security signal, recorded before the denial is returned.
func (i *Interceptor) recordDenied(ctx context.Context, procedure string, u db.User) {
	spec, ok := auditSpecs[procedure]
	if !ok || i.auditor == nil {
		return
	}
	i.auditor.Record(ctx, audit.Entry{
		Actor:        u.Username,
		ActorRole:    u.Role,
		Source:       audit.SourceUser,
		Action:       spec.action,
		ResourceType: spec.resourceType,
		Outcome:      audit.OutcomeDenied,
		Detail:       "admin role required",
	})
}

// recordOutcome writes exactly one entry per audited mutation: success with
// response-derived detail, or failure with the daemon/service error message.
func (i *Interceptor) recordOutcome(ctx context.Context, procedure string, req connect.AnyRequest, resp connect.AnyResponse, handlerErr error, u db.User) {
	spec, ok := auditSpecs[procedure]
	if !ok || i.auditor == nil {
		return
	}

	e := audit.Entry{
		Actor:        u.Username,
		ActorRole:    u.Role,
		Source:       audit.SourceUser,
		Action:       spec.action,
		ResourceType: spec.resourceType,
	}

	if handlerErr != nil {
		e.Outcome = audit.OutcomeFailure
		e.Detail = handlerErr.Error()
		var cerr *connect.Error
		if errors.As(handlerErr, &cerr) {
			e.Detail = cerr.Message()
		}
		i.auditor.Record(ctx, e)
		return
	}

	e.Outcome = audit.OutcomeSuccess
	e.ResourceID, e.Detail = auditSuccessDetail(procedure, req, resp)
	i.auditor.Record(ctx, e)
}

// auditSuccessDetail extracts the resource id and human summary from the
// request/response pair. Unreachable procedure cases and failed type
// assertions degrade to empty strings — the entry still records the outcome.
func auditSuccessDetail(procedure string, req connect.AnyRequest, resp connect.AnyResponse) (string, string) {
	switch procedure {
	case dmanagerv1connect.AdminServiceDeleteImageProcedure:
		m, _ := req.Any().(*dmanagerv1.DeleteImageRequest)
		detail := "image deleted"
		if m != nil && m.Force {
			detail = "image deleted (force)"
		}
		if m == nil {
			return "", detail
		}
		return m.Id, detail

	case dmanagerv1connect.AdminServicePruneImagesProcedure:
		if m, ok := resp.Any().(*dmanagerv1.PruneImagesResponse); ok {
			return "", fmt.Sprintf("pruned %d image(s), reclaimed %d bytes", len(m.ImagesDeleted), m.SpaceReclaimed)
		}

	case dmanagerv1connect.AdminServicePruneBuildCacheProcedure:
		if m, ok := resp.Any().(*dmanagerv1.PruneBuildCacheResponse); ok {
			return "", fmt.Sprintf("pruned %d cache record(s), reclaimed %d bytes", m.CachesDeleted, m.SpaceReclaimed)
		}

	case dmanagerv1connect.AdminServicePruneBuildCacheRecordProcedure:
		m, _ := req.Any().(*dmanagerv1.PruneBuildCacheRecordRequest)
		detail := ""
		if r, ok := resp.Any().(*dmanagerv1.PruneBuildCacheRecordResponse); ok {
			detail = fmt.Sprintf("pruned %d record(s)", r.CachesDeleted)
		}
		if m == nil {
			return "", detail
		}
		return m.Id, detail

	case dmanagerv1connect.AdminServicePruneVolumesProcedure:
		if m, ok := resp.Any().(*dmanagerv1.PruneVolumesResponse); ok {
			return "", fmt.Sprintf("pruned %d volume(s) (%s), reclaimed %d bytes", m.VolumesDeleted, truncateNames(m.Names), m.SpaceReclaimed)
		}

	case dmanagerv1connect.AdminServiceDeleteNetworkProcedure:
		m, _ := req.Any().(*dmanagerv1.DeleteNetworkRequest)
		if m == nil {
			return "", "network deleted"
		}
		return m.Id, "network deleted"

	case dmanagerv1connect.AdminServicePruneNetworksProcedure:
		if m, ok := resp.Any().(*dmanagerv1.PruneNetworksResponse); ok {
			return "", fmt.Sprintf("pruned %d network(s) (%s)", m.NetworksDeleted, truncateNames(m.Names))
		}

	case dmanagerv1connect.ContainerServiceStartContainerProcedure, dmanagerv1connect.ContainerServiceStopContainerProcedure:
		var id string
		if m, ok := req.Any().(*dmanagerv1.StartContainerRequest); ok {
			id = m.Id
		} else if m, ok := req.Any().(*dmanagerv1.StopContainerRequest); ok {
			id = m.Id
		}
		detail := ""
		if r, ok := resp.Any().(*dmanagerv1.StartContainerResponse); ok {
			detail = "previous state: " + r.PreviousState
		} else if r, ok := resp.Any().(*dmanagerv1.StopContainerResponse); ok {
			detail = "previous state: " + r.PreviousState
		}
		return id, detail

	case dmanagerv1connect.ContainerServiceSetContainerAutoUpdateProcedure:
		m, _ := req.Any().(*dmanagerv1.SetContainerAutoUpdateRequest)
		if m == nil {
			return "", "auto-update toggled"
		}
		detail := "auto-update disabled"
		if m.AutoUpdate {
			detail = "auto-update enabled"
		}
		return m.Id, detail

	case dmanagerv1connect.ContainerServiceCheckContainerUpdatesProcedure:
		m, _ := req.Any().(*dmanagerv1.CheckContainerUpdatesRequest)
		detail := ""
		if r, ok := resp.Any().(*dmanagerv1.CheckContainerUpdatesResponse); ok {
			if r.UpdateAvailable {
				detail = "update available: " + r.LatestImageDigest
			} else {
				detail = "container up to date"
			}
		}
		if m == nil {
			return "", detail
		}
		return m.Id, detail

	case dmanagerv1connect.SettingsServiceUpdateSettingsProcedure:
		return "", "settings updated"

	case dmanagerv1connect.SettingsServiceTestGotifyNotificationProcedure:
		return "", "notification test succeeded"
	}
	return "", ""
}

// truncateNames keeps at most three names in a detail summary.
func truncateNames(names []string) string {
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, +%d more", strings.Join(names[:3], ", "), len(names)-3)
}
