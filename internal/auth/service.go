package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	"golang.org/x/crypto/bcrypt"

	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

const adminRole = "admin"

type Service struct {
	Queries *db.Queries
	logger  *slog.Logger
}

func NewService(queries *db.Queries, logger *slog.Logger) *Service {
	return &Service{
		Queries: queries,
		logger:  logger,
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

	count, err := s.Queries.CountUsers(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check existing users: %w", err))
	}

	if count > 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("admin account already setup"))
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
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

	return connect.NewResponse(&v1.SetupAdminResponse{
		Username: user.Username,
		Role:     user.Role,
	}), nil
}

func (s *Service) Login(ctx context.Context, req *connect.Request[v1.LoginRequest]) (*connect.Response[v1.LoginResponse], error) {
	username := req.Msg.Username
	password := req.Msg.Password

	if username == "" || password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("username and password are required"))
	}

	user, err := s.Queries.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query user: %w", err))
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
	}

	tokenBytes := make([]byte, 32)
	if _, randErr := rand.Read(tokenBytes); randErr != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to generate session token: %w", randErr))
	}
	sessionID := hex.EncodeToString(tokenBytes)

	expiresAt := time.Now().Add(24 * time.Hour)
	_, err = s.Queries.CreateSession(ctx, db.CreateSessionParams{
		SessionID: sessionID,
		UserID:    user.ID,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save session: %w", err))
	}

	resp := connect.NewResponse(&v1.LoginResponse{
		Username: user.Username,
		Role:     user.Role,
	})

	resp.Header().Set("Set-Cookie", fmt.Sprintf("session_id=%s; Path=/; HttpOnly; SameSite=Lax", sessionID))

	return resp, nil
}

func (s *Service) Logout(ctx context.Context, req *connect.Request[v1.LogoutRequest]) (*connect.Response[v1.LogoutResponse], error) {
	sessionID, ok := SessionIDFromContext(ctx)
	if !ok {
		cookieHeader := req.Header().Get("Cookie")
		sessionID = parseSessionCookie(cookieHeader)
	}

	if sessionID != "" {
		_ = s.Queries.DeleteSession(ctx, sessionID)
	}

	resp := connect.NewResponse(&v1.LogoutResponse{})
	resp.Header().Set("Set-Cookie", "session_id=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0")
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
