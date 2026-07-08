package settings

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	connect "connectrpc.com/connect"

	"dmanager/internal/auth"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
)

// Service implements the dmanagerv1connect.SettingsServiceHandler interface.
type Service struct {
	dmanagerv1connect.UnimplementedSettingsServiceHandler
	db     db.DBTX
	logger *slog.Logger
}

// NewService creates a new settings service.
func NewService(dbConn db.DBTX, logger *slog.Logger) *Service {
	return &Service{
		db:     dbConn,
		logger: logger,
	}
}

func (s *Service) checkAdmin(ctx context.Context) error {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}
	if user.Role != "admin" {
		return connect.NewError(connect.CodePermissionDenied, errors.New("admin privilege required"))
	}
	return nil
}

// GetSettings retrieves configured setting items from the database.
func (s *Service) GetSettings(ctx context.Context, req *connect.Request[v1.GetSettingsRequest]) (*connect.Response[v1.GetSettingsResponse], error) {
	if err := s.checkAdmin(ctx); err != nil {
		return nil, err
	}

	queries := db.New(s.db)
	settingsList, err := queries.ListSettings(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to retrieve settings: %w", err))
	}

	res := &v1.GetSettingsResponse{}
	for _, setting := range settingsList {
		switch setting.Key {
		case "gotify_url":
			res.GotifyUrl = setting.Value
		case "gotify_token":
			res.GotifyToken = setting.Value
		}
	}

	return connect.NewResponse(res), nil
}

// UpdateSettings updates configuration options in the SQLite database.
func (s *Service) UpdateSettings(ctx context.Context, req *connect.Request[v1.UpdateSettingsRequest]) (*connect.Response[v1.UpdateSettingsResponse], error) {
	if err := s.checkAdmin(ctx); err != nil {
		return nil, err
	}

	queries := db.New(s.db)

	err := queries.UpdateSetting(ctx, db.UpdateSettingParams{
		Key:   "gotify_url",
		Value: req.Msg.GotifyUrl,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save gotify_url: %w", err))
	}

	err = queries.UpdateSetting(ctx, db.UpdateSettingParams{
		Key:   "gotify_token",
		Value: req.Msg.GotifyToken,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save gotify_token: %w", err))
	}

	s.logger.Info("System settings updated successfully", "gotify_url", req.Msg.GotifyUrl)

	return connect.NewResponse(&v1.UpdateSettingsResponse{}), nil
}

// TestGotifyNotification dispatches a connection test message to the designated Gotify destination.
func (s *Service) TestGotifyNotification(ctx context.Context, req *connect.Request[v1.TestGotifyNotificationRequest]) (*connect.Response[v1.TestGotifyNotificationResponse], error) {
	if err := s.checkAdmin(ctx); err != nil {
		return nil, err
	}

	url := req.Msg.GotifyUrl
	token := req.Msg.GotifyToken

	queries := db.New(s.db)
	if url == "" {
		setting, err := queries.GetSetting(ctx, "gotify_url")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return connect.NewResponse(&v1.TestGotifyNotificationResponse{
					Success:      false,
					ErrorMessage: "Gotify URL is not configured",
				}), nil
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query gotify_url: %w", err))
		}
		url = setting.Value
	}

	if token == "" {
		setting, err := queries.GetSetting(ctx, "gotify_token")
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return connect.NewResponse(&v1.TestGotifyNotificationResponse{
					Success:      false,
					ErrorMessage: "Gotify token is not configured",
				}), nil
			}
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query gotify_token: %w", err))
		}
		token = setting.Value
	}

	if url == "" || token == "" {
		return connect.NewResponse(&v1.TestGotifyNotificationResponse{
			Success:      false,
			ErrorMessage: "Both Gotify URL and Application Token must be configured",
		}), nil
	}

	// Ensure URL has http/https prefix
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	url = strings.TrimSuffix(url, "/")
	targetURL := fmt.Sprintf("%s/message", url)

	payload := map[string]interface{}{
		"title":    "DManager Connection Test",
		"message":  "This is a test notification from DManager to verify your Gotify integration settings.",
		"priority": 5,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to marshal Gotify payload: %w", err))
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create http request: %w", err))
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Gotify-Key", token)

	client := &http.Client{Timeout: 10 * time.Second}
	s.logger.Info("Sending test Gotify notification", "url", targetURL)
	resp, err := client.Do(httpReq)
	if err != nil {
		return connect.NewResponse(&v1.TestGotifyNotificationResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Failed to connect to Gotify: %v", err),
		}), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return connect.NewResponse(&v1.TestGotifyNotificationResponse{
			Success:      false,
			ErrorMessage: fmt.Sprintf("Gotify returned status %d: %s", resp.StatusCode, string(bodyBytes)),
		}), nil
	}

	return connect.NewResponse(&v1.TestGotifyNotificationResponse{
		Success: true,
	}), nil
}
