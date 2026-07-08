package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"dmanager/internal/db"
)

// Dispatcher manages notification routing to a configured Gotify server.
type Dispatcher struct {
	db     db.DBTX
	logger *slog.Logger
}

// NewDispatcher returns an initialized Dispatcher instance.
func NewDispatcher(dbConn db.DBTX, logger *slog.Logger) *Dispatcher {
	return &Dispatcher{
		db:     dbConn,
		logger: logger,
	}
}

// SendGotify sends a notification message payload to the configured Gotify destination.
func (d *Dispatcher) SendGotify(ctx context.Context, title, message string, priority int) {
	queries := db.New(d.db)
	urlSetting, err := queries.GetSetting(ctx, "gotify_url")
	if err != nil || urlSetting.Value == "" {
		return
	}
	tokenSetting, err := queries.GetSetting(ctx, "gotify_token")
	if err != nil || tokenSetting.Value == "" {
		return
	}

	url := urlSetting.Value
	token := tokenSetting.Value

	// Ensure URL has http/https prefix
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}
	url = strings.TrimSuffix(url, "/")
	targetURL := fmt.Sprintf("%s/message", url)

	payload := map[string]interface{}{
		"title":    title,
		"message":  message,
		"priority": priority,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		d.logger.Error("Notification: failed to marshal Gotify payload", "error", err)
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewBuffer(jsonData))
	if err != nil {
		d.logger.Error("Notification: failed to create http request", "error", err)
		return
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Gotify-Key", token)

	client := &http.Client{Timeout: 10 * time.Second}
	d.logger.Info("Dispatching Gotify notification", "title", title)
	resp, err := client.Do(httpReq)
	if err != nil {
		d.logger.Error("Notification: failed to dispatch Gotify message", "url", targetURL, "error", err)
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		d.logger.Error("Notification: Gotify server returned non-OK status", "status", resp.Status)
	}
}
