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
	"strconv"
	"net/http"
	"strings"
	"time"

	connect "connectrpc.com/connect"
	"github.com/moby/moby/client"

	"dmanager/internal/audit"
	"dmanager/internal/auth"
	"dmanager/internal/config"
	"dmanager/internal/db"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
	"dmanager/internal/gen/proto/dmanager/v1/dmanagerv1connect"
)

const roleAdmin = "admin"

// Service implements the dmanagerv1connect.SettingsServiceHandler interface.
type Service struct {
	dmanagerv1connect.UnimplementedSettingsServiceHandler
	db           db.DBTX
	logger       *slog.Logger
	registries   []config.Registry
	dockerClient *client.Client
}

// NewService creates a new settings service.
func NewService(dbConn db.DBTX, logger *slog.Logger, registries []config.Registry, dockerClient *client.Client) *Service {
	return &Service{
		db:           dbConn,
		logger:       logger,
		registries:   registries,
		dockerClient: dockerClient,
	}
}

func (s *Service) checkAdmin(ctx context.Context) error {
	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}
	if user.Role != roleAdmin {
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

	res := &v1.GetSettingsResponse{
		// Effective value: the default window until an admin picks one,
		// so the UI always shows what is actually enforced.
		AuditRetentionDays: int32(audit.DefaultRetentionDays),
	}
	for _, setting := range settingsList {
		switch setting.Key {
		case "gotify_url":
			res.GotifyUrl = setting.Value
		case "gotify_token":
			res.GotifyToken = setting.Value
		case audit.RetentionSettingKey:
			if days, parseErr := strconv.Atoi(setting.Value); parseErr == nil && audit.IsValidRetentionDays(days) {
				res.AuditRetentionDays = int32(days) //nolint:gosec // G115: the preset check bounds days to ≤ 365
			}
		}
	}

	return connect.NewResponse(res), nil
}

// UpdateSettings updates configuration options in the SQLite database.
func (s *Service) UpdateSettings(ctx context.Context, req *connect.Request[v1.UpdateSettingsRequest]) (*connect.Response[v1.UpdateSettingsResponse], error) {
	if err := s.checkAdmin(ctx); err != nil {
		return nil, err
	}

	if !audit.IsValidRetentionDays(int(req.Msg.AuditRetentionDays)) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("audit_retention_days must be one of %v", audit.ValidRetentionDayList()))
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

	err = queries.UpdateSetting(ctx, db.UpdateSettingParams{
		Key:   audit.RetentionSettingKey,
		Value: strconv.Itoa(int(req.Msg.AuditRetentionDays)),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to save audit_retention_days: %w", err))
	}

	s.logger.Info("System settings updated successfully", "gotify_url", req.Msg.GotifyUrl, "audit_retention_days", req.Msg.AuditRetentionDays)

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
	defer func() {
		_ = resp.Body.Close()
	}()

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

// GetRegistryStatus performs dynamic connectivity and credentials checks against configured private registries.
func (s *Service) GetRegistryStatus(ctx context.Context, req *connect.Request[v1.GetRegistryStatusRequest]) (*connect.Response[v1.GetRegistryStatusResponse], error) {
	if err := s.checkAdmin(ctx); err != nil {
		return nil, err
	}

	var results []*v1.RegistryStatus

	for _, reg := range s.registries {
		status := &v1.RegistryStatus{
			Host:         reg.Host,
			Username:     reg.Username,
			IsConfigured: reg.Host != "" && reg.Username != "" && reg.Password != "",
			IsHealthy:    false,
		}

		if !status.IsConfigured {
			status.ErrorMessage = "Missing host, username, or password in configuration"
			results = append(results, status)
			continue
		}

		if s.dockerClient == nil {
			status.ErrorMessage = "Docker client is not initialized on host"
			results = append(results, status)
			continue
		}

		// Execute RegistryLogin on docker daemon to verify registry connection/auth
		_, err := s.dockerClient.RegistryLogin(ctx, client.RegistryLoginOptions{
			Username:      reg.Username,
			Password:      reg.Password,
			ServerAddress: reg.Host,
		})

		if err != nil {
			status.IsHealthy = false
			status.ErrorMessage = err.Error()
		} else {
			status.IsHealthy = true
		}

		results = append(results, status)
	}

	return connect.NewResponse(&v1.GetRegistryStatusResponse{
		Registries: results,
	}), nil
}
