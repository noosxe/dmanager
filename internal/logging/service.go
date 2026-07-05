package logging

import (
	"context"
	"log/slog"
	"strings"

	connect "connectrpc.com/connect"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

const (
	levelDebug = "DEBUG"
	levelInfo  = "INFO"
	levelWarn  = "WARN"
	levelWarn2 = "WARNING"
	levelError = "ERROR"
)

// Service implements dmanagerv1connect.LogServiceHandler.
type Service struct {
	logger *slog.Logger
	buffer *RingBuffer
}

// NewService creates a new logging Service.
func NewService(logger *slog.Logger, buffer *RingBuffer) *Service {
	return &Service{
		logger: logger,
		buffer: buffer,
	}
}

// GetSystemLogs retrieves general logs from the backend's structured log ring buffer.
func (s *Service) GetSystemLogs(ctx context.Context, req *connect.Request[v1.GetSystemLogsRequest]) (*connect.Response[v1.GetSystemLogsResponse], error) {
	limit := int(req.Msg.Limit)
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	entries := s.buffer.Get(limit, req.Msg.LevelFilter, req.Msg.SearchQuery)

	return connect.NewResponse(&v1.GetSystemLogsResponse{
		Entries: entries,
	}), nil
}

// SyncLogs ingests a batch of client-side logs and logs them via the structured backend logger.
func (s *Service) SyncLogs(ctx context.Context, req *connect.Request[v1.SyncLogsRequest]) (*connect.Response[v1.SyncLogsResponse], error) {
	var processedCount int32

	for _, entry := range req.Msg.Entries {
		if entry == nil {
			continue
		}

		var level slog.Level
		switch strings.ToUpper(entry.Level) {
		case levelDebug:
			level = slog.LevelDebug
		case levelInfo:
			level = slog.LevelInfo
		case levelWarn, levelWarn2:
			level = slog.LevelWarn
		case levelError:
			level = slog.LevelError
		default:
			level = slog.LevelInfo
		}

		attrs := []any{
			slog.String("source", "frontend"),
			slog.String("client_level", entry.Level),
			slog.String("client_timestamp", entry.Timestamp),
		}

		if entry.Component != "" {
			attrs = append(attrs, slog.String("component", entry.Component))
		}
		if entry.Metadata != "" {
			attrs = append(attrs, slog.String("metadata", entry.Metadata))
		}

		s.logger.Log(ctx, level, entry.Message, attrs...)
		processedCount++
	}

	return connect.NewResponse(&v1.SyncLogsResponse{
		ProcessedCount: processedCount,
	}), nil
}
