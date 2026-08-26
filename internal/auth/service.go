package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/timestamppb"

	"dmanager/internal/config"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

const adminRole = "admin"

type Service struct {
	Queries           *db.Queries
	logger            *slog.Logger
	cfg               config.AuthConfig
	trustedProxy      bool
	dummyHash         []byte
	rateLimiter       *RateLimiter
	passwordValidator *PasswordValidator
}

func NewService(queries *db.Queries, logger *slog.Logger, cfg config.AuthConfig, trustedProxy bool) *Service {
	if cfg.SessionIdleTimeout <= 0 {
		cfg.SessionIdleTimeout = 168 * time.Hour
	}
	if cfg.SessionAbsoluteTimeout <= 0 {
		cfg.SessionAbsoluteTimeout = 720 * time.Hour
	}
	if cfg.RememberMeIdleTimeout <= 0 {
		cfg.RememberMeIdleTimeout = 720 * time.Hour
	}
	if cfg.RememberMeAbsoluteTimeout <= 0 {
		cfg.RememberMeAbsoluteTimeout = 2160 * time.Hour
	}
	if cfg.SecureCookies == "" {
		cfg.SecureCookies = config.SecureCookiesAuto
	}
	if cfg.BcryptCost == 0 {
		cfg.BcryptCost = 12
	}

	dummyHash, err := bcrypt.GenerateFromPassword([]byte("dummy-timing-equalization-password-hash"), cfg.BcryptCost)
	if err != nil && logger != nil {
		logger.Warn("Failed to generate dummy bcrypt hash for timing equalization", "error", err)
	}

	return &Service{
		Queries:           queries,
		logger:            logger,
		cfg:               cfg,
		trustedProxy:      trustedProxy,
		dummyHash:         dummyHash,
		rateLimiter:       NewRateLimiter(trustedProxy),
		passwordValidator: NewPasswordValidator(cfg.BreachedPasswordCheck, logger),
	}
}

func (s *Service) recordAuthEvent(ctx context.Context, userID *int64, username, eventType, detail string) {
	var uid sql.NullInt64
	if userID != nil {
		uid = sql.NullInt64{Int64: *userID, Valid: true}
	}
	_, err := s.Queries.CreateAuthEvent(ctx, db.CreateAuthEventParams{
		UserID:   uid,
		Username: username,
		Event:    eventType,
		Detail:   detail,
	})
	if err != nil && s.logger != nil {
		s.logger.Warn("Failed to record auth audit event", "event", eventType, "username", username, "error", err)
	}
}

func (s *Service) GetServerStatus(ctx context.Context, req *connect.Request[v1.GetServerStatusRequest]) (*connect.Response[v1.GetServerStatusResponse], error) {
	count, err := s.Queries.CountUsers(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to count users: %w", err))
	}

	return connect.NewResponse(&v1.GetServerStatusResponse{
		NeedsSetup: count == 0,
	}), nil
}

func (s *Service) SetupAdmin(ctx context.Context, req *connect.Request[v1.SetupAdminRequest]) (*connect.Response[v1.SetupAdminResponse], error) {
	username := req.Msg.Username
	password := req.Msg.Password

	if username == "" || password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username and password are required"))
	}

	if err := s.passwordValidator.Validate(password); err != nil {
		return nil, err
	}

	count, err := s.Queries.CountUsers(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check existing users: %w", err))
	}

	if count > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("admin account already setup"))
	}

	cost := s.cfg.BcryptCost
	if cost == 0 {
		cost = 12
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to hash password: %w", err))
	}

	user, err := s.Queries.CreateUser(ctx, db.CreateUserParams{
		Username:     username,
		PasswordHash: string(hashedPassword),
		Role:         adminRole,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create admin: %w", err))
	}

	clientIP := ExtractClientIP(req.Header(), req.Peer().Addr, s.trustedProxy)
	s.recordAuthEvent(ctx, &user.ID, user.Username, "setup_admin", "ip: "+clientIP)

	return connect.NewResponse(&v1.SetupAdminResponse{
		Username: user.Username,
		Role:     user.Role,
	}), nil
}

func (s *Service) issueSession(ctx context.Context, userID int64, rememberMe bool, reqHeader http.Header) (*db.Session, string, error) {
	idleTimeout := s.cfg.SessionIdleTimeout
	absTimeout := s.cfg.SessionAbsoluteTimeout
	if rememberMe {
		idleTimeout = s.cfg.RememberMeIdleTimeout
		absTimeout = s.cfg.RememberMeAbsoluteTimeout
	}

	tokenBytes := make([]byte, 32)
	if _, randErr := rand.Read(tokenBytes); randErr != nil {
		return nil, "", fmt.Errorf("failed to generate session token: %w", randErr)
	}
	sessionID := hex.EncodeToString(tokenBytes)

	now := time.Now()
	expiresAt := now.Add(idleTimeout)
	absoluteExpiresAt := now.Add(absTimeout)

	var userAgent string
	if reqHeader != nil {
		userAgent = reqHeader.Get("User-Agent")
	}

	session, err := s.Queries.CreateSession(ctx, db.CreateSessionParams{
		SessionID:         sessionID,
		UserID:            userID,
		UserAgent:         userAgent,
		ExpiresAt:         expiresAt,
		LastSeenAt:        now,
		AbsoluteExpiresAt: absoluteExpiresAt,
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to save session: %w", err)
	}

	cookie := formatSessionCookie(sessionID, int(idleTimeout.Seconds()), s.cfg.SecureCookies, reqHeader)
	return &session, cookie, nil
}

func formatSessionCookie(sessionID string, maxAge int, secureCookies string, reqHeader http.Header) string {
	secureSuffix := ""
	switch secureCookies {
	case config.SecureCookiesAlways:
		secureSuffix = "; Secure"
	case config.SecureCookiesNever:
		secureSuffix = ""
	case config.SecureCookiesAuto:
		if reqHeader != nil && strings.EqualFold(reqHeader.Get("X-Forwarded-Proto"), "https") {
			secureSuffix = "; Secure"
		}
	}

	return fmt.Sprintf("session_id=%s; Path=/; HttpOnly; SameSite=Lax; Max-Age=%d%s", sessionID, maxAge, secureSuffix)
}

func (s *Service) Login(ctx context.Context, req *connect.Request[v1.LoginRequest]) (*connect.Response[v1.LoginResponse], error) {
	username := req.Msg.Username
	password := req.Msg.Password

	if username == "" || password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username and password are required"))
	}

	clientIP := ExtractClientIP(req.Header(), req.Peer().Addr, s.trustedProxy)

	locked, retryAfter := s.rateLimiter.Check(username, clientIP, time.Now())
	if locked {
		retrySec := int(math.Ceil(retryAfter.Seconds()))
		if retrySec < 1 {
			retrySec = 1
		}
		s.recordAuthEvent(ctx, nil, username, "rate_limited", "ip: "+clientIP)
		return nil, connect.NewError(
			connect.CodeResourceExhausted,
			fmt.Errorf("too many failed login attempts, please try again in %d seconds", retrySec),
		)
	}

	user, err := s.Queries.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if len(s.dummyHash) > 0 {
				_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
			}
			s.rateLimiter.RecordFailure(username, clientIP, time.Now())
			s.recordAuthEvent(ctx, nil, username, "login_failed", "method: password, reason: invalid credentials, ip: "+clientIP)
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query user: %w", err))
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		s.rateLimiter.RecordFailure(username, clientIP, time.Now())
		s.recordAuthEvent(ctx, &user.ID, user.Username, "login_failed", "method: password, reason: invalid credentials, ip: "+clientIP)
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
	}

	s.rateLimiter.RecordSuccess(username)

	_, cookieHeader, err := s.issueSession(ctx, user.ID, req.Msg.RememberMe, req.Header())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.recordAuthEvent(ctx, &user.ID, user.Username, "login_success", "method: password, ip: "+clientIP)

	resp := connect.NewResponse(&v1.LoginResponse{
		Username: user.Username,
		Role:     user.Role,
	})

	resp.Header().Set("Set-Cookie", cookieHeader)

	return resp, nil
}

func (s *Service) Logout(ctx context.Context, req *connect.Request[v1.LogoutRequest]) (*connect.Response[v1.LogoutResponse], error) {
	sessionID, ok := SessionIDFromContext(ctx)
	if !ok {
		cookieHeader := req.Header().Get("Cookie")
		sessionID = parseSessionCookie(cookieHeader)
	}

	if user, userOk := UserFromContext(ctx); userOk {
		s.recordAuthEvent(ctx, &user.ID, user.Username, "logout", "")
	}

	if sessionID != "" {
		_ = s.Queries.DeleteSession(ctx, sessionID)
	}

	resp := connect.NewResponse(&v1.LogoutResponse{})
	resp.Header().Set("Set-Cookie", formatSessionCookie("", 0, s.cfg.SecureCookies, req.Header()))
	return resp, nil
}

func (s *Service) GetMe(ctx context.Context, req *connect.Request[v1.GetMeRequest]) (*connect.Response[v1.GetMeResponse], error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}

	return connect.NewResponse(&v1.GetMeResponse{
		UserId:   user.ID,
		Username: user.Username,
		Role:     user.Role,
	}), nil
}

func (s *Service) ListAuthEvents(ctx context.Context, req *connect.Request[v1.ListAuthEventsRequest]) (*connect.Response[v1.ListAuthEventsResponse], error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}

	limit := req.Msg.Limit
	if limit <= 0 {
		limit = 50
	} else if limit > 100 {
		limit = 100
	}

	offset := req.Msg.Offset
	if offset < 0 {
		offset = 0
	}

	var events []db.AuthEvent
	var totalCount int64
	var err error

	if user.Role == adminRole {
		events, err = s.Queries.ListAuthEvents(ctx, db.ListAuthEventsParams{
			Limit:  int64(limit),
			Offset: int64(offset),
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list auth events: %w", err))
		}
		totalCount, err = s.Queries.CountAuthEvents(ctx)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to count auth events: %w", err))
		}
	} else {
		events, err = s.Queries.ListAuthEventsByUser(ctx, db.ListAuthEventsByUserParams{
			UserID: sql.NullInt64{Int64: user.ID, Valid: true},
			Limit:  int64(limit),
			Offset: int64(offset),
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list user auth events: %w", err))
		}
		totalCount, err = s.Queries.CountAuthEventsByUser(ctx, sql.NullInt64{Int64: user.ID, Valid: true})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to count user auth events: %w", err))
		}
	}

	pbEvents := make([]*v1.AuthEvent, 0, len(events))
	for _, e := range events {
		var uid *int64
		if e.UserID.Valid {
			v := e.UserID.Int64
			uid = &v
		}
		pbEvents = append(pbEvents, &v1.AuthEvent{
			Id:        e.ID,
			UserId:    uid,
			Username:  e.Username,
			EventType: e.Event,
			Detail:    e.Detail,
			CreatedAt: timestamppb.New(e.CreatedAt),
		})
	}

	return connect.NewResponse(&v1.ListAuthEventsResponse{
		Events:     pbEvents,
		TotalCount: totalCount,
	}), nil
}

func (s *Service) ListSessions(ctx context.Context, req *connect.Request[v1.ListSessionsRequest]) (*connect.Response[v1.ListSessionsResponse], error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}

	currentSessionID, _ := SessionIDFromContext(ctx)

	rows, err := s.Queries.ListSessionsByUser(ctx, user.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list sessions: %w", err))
	}

	pbSessions := make([]*v1.Session, 0, len(rows))
	for _, r := range rows {
		pbSessions = append(pbSessions, &v1.Session{
			SessionId:         r.SessionID,
			UserAgent:         r.UserAgent,
			DeviceLabel:       formatDeviceLabel(r.UserAgent),
			CreatedAt:         timestamppb.New(r.CreatedAt),
			LastSeenAt:        timestamppb.New(r.LastSeenAt),
			ExpiresAt:         timestamppb.New(r.ExpiresAt),
			AbsoluteExpiresAt: timestamppb.New(r.AbsoluteExpiresAt),
			IsCurrent:         r.SessionID == currentSessionID,
		})
	}

	return connect.NewResponse(&v1.ListSessionsResponse{
		Sessions: pbSessions,
	}), nil
}

func (s *Service) RevokeSession(ctx context.Context, req *connect.Request[v1.RevokeSessionRequest]) (*connect.Response[v1.RevokeSessionResponse], error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}

	sessionID := req.Msg.SessionId
	if sessionID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("session_id is required"))
	}

	rowsAffected, err := s.Queries.DeleteSessionByIDAndUser(ctx, db.DeleteSessionByIDAndUserParams{
		SessionID: sessionID,
		UserID:    user.ID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to revoke session: %w", err))
	}

	if rowsAffected == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("session not found"))
	}

	s.recordAuthEvent(ctx, &user.ID, user.Username, "session_revoked", "action: revoke_single")

	return connect.NewResponse(&v1.RevokeSessionResponse{}), nil
}

func (s *Service) RevokeAllOtherSessions(ctx context.Context, req *connect.Request[v1.RevokeAllOtherSessionsRequest]) (*connect.Response[v1.RevokeAllOtherSessionsResponse], error) {
	user, ok := UserFromContext(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}

	currentSessionID, _ := SessionIDFromContext(ctx)

	rowsAffected, err := s.Queries.DeleteSessionsByUser(ctx, db.DeleteSessionsByUserParams{
		UserID:    user.ID,
		SessionID: currentSessionID,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to revoke other sessions: %w", err))
	}

	s.recordAuthEvent(ctx, &user.ID, user.Username, "session_revoked", "action: revoke_all_other")

	return connect.NewResponse(&v1.RevokeAllOtherSessionsResponse{
		RevokedCount: rowsAffected,
	}), nil
}

func formatDeviceLabel(ua string) string {
	if ua == "" {
		return "Unknown Device"
	}
	uaLower := strings.ToLower(ua)

	// OS detection
	os := "Unknown OS"
	if strings.Contains(uaLower, "windows") {
		os = "Windows"
	} else if strings.Contains(uaLower, "android") {
		os = "Android"
	} else if strings.Contains(uaLower, "iphone") || strings.Contains(uaLower, "ipad") || strings.Contains(uaLower, "ipod") || strings.Contains(uaLower, "ios") {
		os = "iOS"
	} else if strings.Contains(uaLower, "macintosh") || strings.Contains(uaLower, "mac os") {
		os = "macOS"
	} else if strings.Contains(uaLower, "linux") {
		os = "Linux"
	}

	// Browser / Client detection
	browser := "Browser"
	if strings.Contains(uaLower, "edg/") || strings.Contains(uaLower, "edge/") {
		browser = "Edge"
	} else if strings.Contains(uaLower, "opr/") || strings.Contains(uaLower, "opera/") {
		browser = "Opera"
	} else if strings.Contains(uaLower, "chrome/") || strings.Contains(uaLower, "crios/") {
		browser = "Chrome"
	} else if strings.Contains(uaLower, "firefox/") || strings.Contains(uaLower, "fxios/") {
		browser = "Firefox"
	} else if strings.Contains(uaLower, "safari/") && !strings.Contains(uaLower, "chrome") {
		browser = "Safari"
	} else if strings.Contains(uaLower, "curl/") {
		return "curl"
	} else if strings.Contains(uaLower, "postman") {
		return "Postman"
	}

	if os == "Unknown OS" && browser == "Browser" {
		if len(ua) > 30 {
			return ua[:30] + "..."
		}
		return ua
	}

	return fmt.Sprintf("%s · %s", browser, os)
}

func parseSessionCookie(cookieHeader string) string {
	parts := strings.Split(cookieHeader, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "session_id=") {
			return strings.TrimPrefix(part, "session_id=")
		}
	}
	return ""
}
