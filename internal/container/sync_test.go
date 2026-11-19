package container

import (
	"context"
	"testing"

	"dmanager/internal/db"
)

func TestPruneContainers(t *testing.T) {
	ctx := context.Background()

	seed := func(t *testing.T, q *db.Queries) {
		t.Helper()
		for _, id := range []string{"a", "b"} {
			if err := q.UpsertContainerFromEvent(ctx, db.UpsertContainerFromEventParams{
				ID: id, Name: id, Image: "img", ImageID: "sha", State: "running",
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("empty active list deletes all rows", func(t *testing.T) {
		_, q := newTestDB(t)
		seed(t, q)
		if err := pruneContainers(ctx, q, nil); err != nil {
			t.Fatal(err)
		}
		rows, err := q.ListContainers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 0 {
			t.Fatalf("expected no rows, got %d", len(rows))
		}
	})

	t.Run("non-empty active list keeps only active rows", func(t *testing.T) {
		_, q := newTestDB(t)
		seed(t, q)
		if err := pruneContainers(ctx, q, []string{"a"}); err != nil {
			t.Fatal(err)
		}
		rows, err := q.ListContainers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].ID != "a" {
			t.Fatalf("expected only row a, got %+v", rows)
		}
	})
}
