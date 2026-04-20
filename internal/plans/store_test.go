package plans

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/smaan712gb/keel/internal/store"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db)
}

func TestDeclare_AndListExcludesCaller(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_, err := s.Declare(ctx, DeclareParams{RepoRoot: "/r", AgentID: "A", Summary: "add logging", Files: []string{"src/a.ts"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Declare(ctx, DeclareParams{RepoRoot: "/r", AgentID: "B", Summary: "refactor payments"})
	if err != nil {
		t.Fatal(err)
	}
	// Caller is A — should see only B.
	got, err := s.ListActive(ctx, "/r", "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AgentID != "B" {
		t.Fatalf("expected only B's plan, got %+v", got)
	}
	// Empty exclude returns everything.
	all, _ := s.ListActive(ctx, "/r", "")
	if len(all) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(all))
	}
}

func TestComplete_RemovesFromActive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	id, _ := s.Declare(ctx, DeclareParams{RepoRoot: "/r", AgentID: "A", Summary: "x"})
	ok, err := s.Complete(ctx, id, "A")
	if err != nil || !ok {
		t.Fatalf("complete: ok=%v err=%v", ok, err)
	}
	got, _ := s.ListActive(ctx, "/r", "")
	if len(got) != 0 {
		t.Fatalf("completed plan should not be active, got %+v", got)
	}
}

func TestActiveWindow_ExcludesStalePlans(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().UTC()
	s.now = func() time.Time { return now.Add(-2 * ActivePlanWindow) }
	_, _ = s.Declare(context.Background(), DeclareParams{RepoRoot: "/r", AgentID: "A", Summary: "old"})
	s.now = func() time.Time { return now }
	_, _ = s.Declare(context.Background(), DeclareParams{RepoRoot: "/r", AgentID: "B", Summary: "fresh"})

	got, err := s.ListActive(context.Background(), "/r", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].AgentID != "B" {
		t.Fatalf("expected only the fresh plan, got %+v", got)
	}
}
