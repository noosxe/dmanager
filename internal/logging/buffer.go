package logging

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"time"

	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

type RingBuffer struct {
	mu       sync.RWMutex
	capacity int
	entries  []*v1.LogEntry
	cursor   int
}

func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		capacity: capacity,
		entries:  make([]*v1.LogEntry, 0, capacity),
	}
}

func (rb *RingBuffer) Add(entry *v1.LogEntry) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if len(rb.entries) < rb.capacity {
		rb.entries = append(rb.entries, entry)
	} else {
		rb.entries[rb.cursor] = entry
		rb.cursor = (rb.cursor + 1) % rb.capacity
	}
}

func (rb *RingBuffer) Get(limit int, levelFilter string, searchQuery string) []*v1.LogEntry {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	// Gather all entries in chronological order
	var all []*v1.LogEntry
	n := len(rb.entries)
	if n < rb.capacity {
		all = make([]*v1.LogEntry, n)
		copy(all, rb.entries)
	} else {
		all = make([]*v1.LogEntry, rb.capacity)
		for i := 0; i < rb.capacity; i++ {
			all[i] = rb.entries[(rb.cursor+i)%rb.capacity]
		}
	}

	// Filter and return in reverse chronological order (newest first)
	filtered := make([]*v1.LogEntry, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		entry := all[i]
		if levelFilter != "" && !strings.EqualFold(entry.Level, levelFilter) {
			continue
		}
		if searchQuery != "" {
			hasMatch := false
			if strings.Contains(strings.ToLower(entry.Message), strings.ToLower(searchQuery)) {
				hasMatch = true
			} else if strings.Contains(strings.ToLower(entry.Component), strings.ToLower(searchQuery)) {
				hasMatch = true
			} else if strings.Contains(strings.ToLower(entry.Metadata), strings.ToLower(searchQuery)) {
				hasMatch = true
			}
			if !hasMatch {
				continue
			}
		}
		filtered = append(filtered, entry)
		if limit > 0 && len(filtered) >= limit {
			break
		}
	}

	return filtered
}

type InterceptHandler struct {
	next   slog.Handler
	buffer *RingBuffer
	attrs  []slog.Attr
	group  string
}

func NewInterceptHandler(next slog.Handler, buffer *RingBuffer) *InterceptHandler {
	return &InterceptHandler{
		next:   next,
		buffer: buffer,
	}
}

func (h *InterceptHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *InterceptHandler) Handle(ctx context.Context, record slog.Record) error {
	err := h.next.Handle(ctx, record)

	levelStr := record.Level.String()

	// Format attributes
	metaMap := make(map[string]any)
	for _, attr := range h.attrs {
		metaMap[attr.Key] = attr.Value.Any()
	}

	record.Attrs(func(attr slog.Attr) bool {
		metaMap[attr.Key] = attr.Value.Any()
		return true
	})

	var component string
	if comp, ok := metaMap["component"].(string); ok {
		component = comp
		delete(metaMap, "component")
	} else if mod, ok := metaMap["module"].(string); ok {
		component = mod
		delete(metaMap, "module")
	}

	if cl, ok := metaMap["client_level"].(string); ok {
		levelStr = cl
		delete(metaMap, "client_level")
	}

	timestamp := record.Time.UTC().Format(time.RFC3339)
	if ct, ok := metaMap["client_timestamp"].(string); ok {
		timestamp = ct
		delete(metaMap, "client_timestamp")
	}

	delete(metaMap, "source")

	var metaStr string
	if len(metaMap) > 0 {
		if b, jsonErr := json.Marshal(metaMap); jsonErr == nil {
			metaStr = string(b)
		}
	}

	entry := &v1.LogEntry{
		Level:     levelStr,
		Message:   record.Message,
		Timestamp: timestamp,
		Component: component,
		Metadata:  metaStr,
	}

	h.buffer.Add(entry)

	return err
}

func (h *InterceptHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &InterceptHandler{
		next:   h.next.WithAttrs(attrs),
		buffer: h.buffer,
		attrs:  newAttrs,
		group:  h.group,
	}
}

func (h *InterceptHandler) WithGroup(name string) slog.Handler {
	return &InterceptHandler{
		next:   h.next.WithGroup(name),
		buffer: h.buffer,
		attrs:  h.attrs,
		group:  name,
	}
}
