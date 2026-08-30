package container

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	connect "connectrpc.com/connect"

	"dmanager/internal/audit"
	v1 "dmanager/internal/gen/proto/dmanager/v1"
)

type upgradeAuditStub struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (a *upgradeAuditStub) Record(_ context.Context, e audit.Entry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, e)
}

func (a *upgradeAuditStub) snapshot() []audit.Entry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]audit.Entry(nil), a.entries...)
}

func (a *upgradeAuditStub) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = nil
}

// TestAuditUpgradeRecordsDigestTransition covers the single recording point
// for container upgrades (design.md §12.3): user and system origins share the
// helper, success carries the old → new digest detail, failure the error.
func TestAuditUpgradeRecordsDigestTransition(t *testing.T) {
	dbConn, queries := newTestDB(t)
	_ = queries
	stub := &upgradeAuditStub{}
	svc := NewService(dbConn, NewBroker(), nil, slog.Default(), nil, stub)

	resp := &v1.UpgradeContainerResponse{
		Id:              "c1",
		PreviousImageId: "sha256:aaaaaaaaaaaaaaaaaaaa",
		CurrentImageId:  "sha256:bbbbbbbbbbbbbbbbbbbb",
	}

	// System origin (scheduler auto-upgrade): actor "system", no role.
	svc.auditUpgrade(context.Background(), audit.SourceSystem, audit.SystemActor, "", "c1", resp, nil)
	entries := stub.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Source != audit.SourceSystem || e.Actor != audit.SystemActor || e.ActorRole != "" {
		t.Errorf("unexpected system entry: %+v", e)
	}
	if e.Action != "container.upgrade" || e.ResourceType != "container" || e.ResourceID != "c1" {
		t.Errorf("unexpected action/resource: %+v", e)
	}
	if e.Outcome != audit.OutcomeSuccess {
		t.Errorf("expected success, got %q", e.Outcome)
	}
	wantDetail := "upgraded sha256:aaaaaaaaaaaa… → sha256:bbbbbbbbbbbb…"
	if e.Detail != wantDetail {
		t.Errorf("expected detail %q, got %q", wantDetail, e.Detail)
	}

	// User origin failure: outcome failure with the connect error message.
	stub.reset()
	svc.auditUpgrade(context.Background(), audit.SourceUser, "admin", "admin", "c2", nil,
		connect.NewError(connect.CodeUnavailable, errors.New("engine unreachable")))
	entries = stub.snapshot()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e = entries[0]
	if e.Outcome != audit.OutcomeFailure || e.Detail != "engine unreachable" {
		t.Errorf("unexpected failure entry: %+v", e)
	}
	if e.Source != audit.SourceUser || e.Actor != "admin" || e.ActorRole != "admin" {
		t.Errorf("unexpected user entry: %+v", e)
	}
}

// TestAuditUpgradeNilAuditorNoop: a nil auditor (recording disabled) must
// never panic — recording is best-effort by contract.
func TestAuditUpgradeNilAuditorNoop(t *testing.T) {
	dbConn, _ := newTestDB(t)
	svc := NewService(dbConn, NewBroker(), nil, slog.Default(), nil, nil)

	svc.auditUpgrade(context.Background(), audit.SourceSystem, audit.SystemActor, "", "c1",
		&v1.UpgradeContainerResponse{}, nil) // must not panic
}
