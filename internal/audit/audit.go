// Package audit records an append-only trail of every mutation action in
// dmanager: RoleAdmin RPC outcomes (success, failure, denied) and
// system-originated actions such as scheduler-triggered container upgrades
// (design.md §12, issue #219).
//
// Retention is days-based and admin-configurable (§12.7, issue #222): the
// window is read from the settings table at trim time, so a policy change
// takes effect on the next recorded action without a restart.
//
// Recording is best-effort by contract: a failed audit write logs a warning
// and never fails or delays the mutation it observes.
package audit

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strconv"
	"time"

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

// RetentionSettingKey is the settings-table key holding the audit retention
// window in days.
const RetentionSettingKey = "audit_retention_days"

// DefaultRetentionDays applies when the setting row is missing or invalid.
const DefaultRetentionDays = 90

// validRetentionDays is the closed preset set the admin can pick from in
// Settings → General: 7d, 1M (30d), 3M (90d, default), 6M (180d), 1Y (365d).
var validRetentionDays = map[int]bool{7: true, 30: true, 90: true, 180: true, 365: true}

// ValidRetentionDayList is the preset set as an ordered slice — the server
// error message and the docs both list windows in this order.
func ValidRetentionDayList() []int {
	return []int{7, 30, 90, 180, 365}
}

// IsValidRetentionDays reports whether the value is one of the fixed
// retention presets.
func IsValidRetentionDays(days int) bool {
	return validRetentionDays[days]
}

// cutoffFormat mirrors the UTC text CURRENT_TIMESTAMP writes into
// created_at — string comparison over this shape is chronologically
// correct, and the trim query normalizes through datetime() so driver
// time-binding is moot.
const cutoffFormat = "2006-01-02 15:04:05"

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
// retention policy.
type Recorder struct {
	queries *db.Queries
	logger  *slog.Logger
}

// NewRecorder wires a Recorder over the shared database handle.
func NewRecorder(queries *db.Queries, logger *slog.Logger) *Recorder {
	return &Recorder{
		queries: queries,
		logger:  logger,
	}
}

// retentionDays resolves the configured window at trim time: missing row →
// default, unparsable or off-preset value → default with a warning (a
// corrupt value must never disable trimming).
func (r *Recorder) retentionDays(ctx context.Context) int {
	setting, err := r.queries.GetSetting(ctx, RetentionSettingKey)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			r.logger.Warn("audit retention read failed", "error", err)
		}
		return DefaultRetentionDays
	}
	days, parseErr := strconv.Atoi(setting.Value)
	if parseErr != nil || !validRetentionDays[days] {
		r.logger.Warn("invalid audit retention setting", "value", setting.Value)
		return DefaultRetentionDays
	}
	return days
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

	// Trim everything older than the configured window after every
	// successful insert — cheap at this size, immune to a missed
	// background scheduler, and it applies a freshly lowered policy
	// on the very next recorded action.
	cutoff := time.Now().UTC().AddDate(0, 0, -r.retentionDays(ctx)).Format(cutoffFormat)
	if err := r.queries.TrimAuditLogsBefore(ctx, cutoff); err != nil {
		r.logger.Warn("audit trim failed", "error", err)
	}
}
