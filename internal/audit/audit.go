// Package audit records an append-only trail of every mutation action in
// dmanager: RoleAdmin RPC outcomes (success, failure, denied) and
// system-originated actions such as scheduler-triggered container upgrades
// (design.md §12, issue #219).
//
// Recording is best-effort by contract: a failed audit write logs a warning
// and never fails or delays the mutation it observes.
package audit

import (
	"context"
	"log/slog"

	"dmanager/internal/db"
)

// Source values for audit entries.
const (
	SourceUser   = "user"
	SourceSystem = "system"
)

// Outcome values for audit entries.
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
	OutcomeDenied  = "denied"
)

// RetentionRows is the fixed cap on stored entries: the newest 10,000 are
// kept, trimmed opportunistically after each insert. #222 may replace this
// with a configurable policy.
const RetentionRows = 10000

// maxDetailLen bounds the human-readable detail so a pathological daemon
// error message cannot bloat the table.
const maxDetailLen = 2000

// SystemActor is the actor recorded for background actions that run without
// a user context (scheduler-triggered upgrades).
const SystemActor = "system"

// Entry is one audit record awaiting persistence.
type Entry struct {
	Actor        string
	ActorRole    string
	Source       string
	Action       string
	ResourceType string
	ResourceID   string
	Outcome      string
	Detail       string
}

// Auditor is the consumption interface — the auth interceptor and the
// container upgrade path record through it. Nil auditors disable recording.
type Auditor interface {
	Record(ctx context.Context, e Entry)
}

// Recorder persists entries into the audit_logs table and enforces the
// retention cap.
type Recorder struct {
	queries   *db.Queries
	logger    *slog.Logger
	retention int
}

// NewRecorder wires a Recorder over the shared database handle. A retention
// value of zero or less falls back to the default cap.
func NewRecorder(queries *db.Queries, logger *slog.Logger, retention int) *Recorder {
	if retention <= 0 {
		retention = RetentionRows
	}
	return &Recorder{
		queries:   queries,
		logger:    logger,
		retention: retention,
	}
}

// Record persists one entry, detached from the request context so a client
// disconnect cannot cancel the write. Errors are logged, never returned:
// recording must not fail the mutation it observes.
func (r *Recorder) Record(ctx context.Context, e Entry) {
	detail := e.Detail
	if len(detail) > maxDetailLen {
		detail = detail[:maxDetailLen]
	}

	ctx = context.WithoutCancel(ctx)
	if _, err := r.queries.CreateAuditLog(ctx, db.CreateAuditLogParams{
		Actor:        e.Actor,
		ActorRole:    e.ActorRole,
		Source:       e.Source,
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		Outcome:      e.Outcome,
		Detail:       detail,
	}); err != nil {
		r.logger.Warn("audit write failed", "action", e.Action, "error", err)
		return
	}

	// Trim below the retention watermark after every successful insert —
	// cheap at this size and immune to a missed background scheduler.
	if err := r.queries.TrimAuditLogs(ctx, int64(r.retention)); err != nil {
		r.logger.Warn("audit trim failed", "error", err)
	}
}
