package locks

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/smaan712gb/keel/internal/store"
)

func newTestManager(t *testing.T) (*Manager, *fakeClock) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	clock := &fakeClock{t: time.Date(2026, 4, 19, 12, 0, 0, 0, time.UTC)}
	m := New(db)
	m.now = clock.Now
	return m, clock
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestAcquire_Basic(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	res, err := m.Acquire(ctx, AcquireParams{
		RepoRoot: "/r", FilePath: "payments.ts", AgentID: "claude-code", PlanSummary: "add logging",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusAcquired {
		t.Fatalf("expected acquired, got %q", res.Status)
	}
	if res.LockID == "" {
		t.Fatalf("expected lock_id")
	}
}

func TestAcquire_Contention_ReturnsBusyWithHolder(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	eta := 120
	_, err := m.Acquire(ctx, AcquireParams{
		RepoRoot: "/r", FilePath: "payments.ts", AgentID: "claude-code",
		PlanSummary: "add logging", ETASeconds: &eta,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.Acquire(ctx, AcquireParams{
		RepoRoot: "/r", FilePath: "payments.ts", AgentID: "cursor",
		PlanSummary: "refactor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusBusy {
		t.Fatalf("expected busy, got %q", res.Status)
	}
	if res.Holder == nil {
		t.Fatalf("expected holder payload on busy")
	}
	if res.Holder.AgentID != "claude-code" {
		t.Errorf("holder agent = %q", res.Holder.AgentID)
	}
	if res.Holder.PlanSummary != "add logging" {
		t.Errorf("holder plan = %q", res.Holder.PlanSummary)
	}
	if res.Holder.ETASeconds == nil || *res.Holder.ETASeconds != 120 {
		t.Errorf("holder eta not surfaced")
	}
	if res.Holder.LeaseExpiresAt.IsZero() {
		t.Errorf("holder lease_expires_at missing")
	}
}

func TestRelease_AllowsReacquire(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	first, _ := m.Acquire(ctx, AcquireParams{RepoRoot: "/r", FilePath: "a.ts", AgentID: "A"})
	ok, err := m.Release(ctx, first.LockID, "A")
	if err != nil || !ok {
		t.Fatalf("release: ok=%v err=%v", ok, err)
	}
	second, err := m.Acquire(ctx, AcquireParams{RepoRoot: "/r", FilePath: "a.ts", AgentID: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Status != StatusAcquired {
		t.Fatalf("after release, expected acquired, got %q", second.Status)
	}
}

func TestRelease_WrongAgentFails(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	res, _ := m.Acquire(ctx, AcquireParams{RepoRoot: "/r", FilePath: "a.ts", AgentID: "A"})
	ok, err := m.Release(ctx, res.LockID, "impostor")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("expected release to refuse wrong agent")
	}
}

func TestExpiredLease_AllowsReacquire(t *testing.T) {
	m, clock := newTestManager(t)
	ctx := context.Background()
	_, err := m.Acquire(ctx, AcquireParams{
		RepoRoot: "/r", FilePath: "a.ts", AgentID: "A", LeaseSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(61 * time.Second)
	res, err := m.Acquire(ctx, AcquireParams{RepoRoot: "/r", FilePath: "a.ts", AgentID: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusAcquired {
		t.Fatalf("expected acquired after lease expiry, got %q", res.Status)
	}
}

func TestHeartbeat_ExtendsLease(t *testing.T) {
	m, clock := newTestManager(t)
	ctx := context.Background()
	res, _ := m.Acquire(ctx, AcquireParams{
		RepoRoot: "/r", FilePath: "a.ts", AgentID: "A", LeaseSeconds: 60,
	})
	clock.Advance(30 * time.Second)
	newExpiry, ok, err := m.Heartbeat(ctx, res.LockID, "A", 120)
	if err != nil || !ok {
		t.Fatalf("heartbeat: ok=%v err=%v", ok, err)
	}
	// New expiry should be 120s after the clock's current time, i.e. ~90s after the original.
	want := clock.Now().Add(120 * time.Second)
	if !newExpiry.Equal(want) {
		t.Errorf("expected lease_expires_at=%v, got %v", want, newExpiry)
	}
}

func TestHeartbeat_RefusesExpired(t *testing.T) {
	m, clock := newTestManager(t)
	ctx := context.Background()
	res, _ := m.Acquire(ctx, AcquireParams{
		RepoRoot: "/r", FilePath: "a.ts", AgentID: "A", LeaseSeconds: 60,
	})
	clock.Advance(120 * time.Second)
	_, ok, err := m.Heartbeat(ctx, res.LockID, "A", 60)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("expected heartbeat to refuse an already-expired lease")
	}
}

func TestReap_ReleasesExpiredOnly(t *testing.T) {
	m, clock := newTestManager(t)
	ctx := context.Background()
	active, _ := m.Acquire(ctx, AcquireParams{RepoRoot: "/r", FilePath: "a.ts", AgentID: "A", LeaseSeconds: 3600})
	_, _ = m.Acquire(ctx, AcquireParams{RepoRoot: "/r", FilePath: "b.ts", AgentID: "B", LeaseSeconds: 10})
	clock.Advance(30 * time.Second)

	n, err := m.Reap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected reap to release 1 expired lock, got %d", n)
	}
	// The still-active lock should survive reap.
	active2, err := m.Acquire(ctx, AcquireParams{RepoRoot: "/r", FilePath: "a.ts", AgentID: "C"})
	if err != nil {
		t.Fatal(err)
	}
	if active2.Status != StatusBusy {
		t.Fatalf("long-lease lock should still be held after reap, got %q", active2.Status)
	}
	if active2.Holder.AgentID != "A" {
		t.Errorf("holder should still be A, got %s", active2.Holder.AgentID)
	}
	_ = active
}

func TestListActive_ExcludesReleasedAndExpired(t *testing.T) {
	m, clock := newTestManager(t)
	ctx := context.Background()
	r1, _ := m.Acquire(ctx, AcquireParams{RepoRoot: "/r", FilePath: "a.ts", AgentID: "A"})
	_, _ = m.Acquire(ctx, AcquireParams{RepoRoot: "/r", FilePath: "b.ts", AgentID: "B", LeaseSeconds: 10})
	_, _ = m.Acquire(ctx, AcquireParams{RepoRoot: "/r", FilePath: "c.ts", AgentID: "C"})

	_, _ = m.Release(ctx, r1.LockID, "A")
	clock.Advance(30 * time.Second)

	ls, err := m.ListActive(ctx, "/r")
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 1 {
		t.Fatalf("expected 1 active lock, got %d (%+v)", len(ls), ls)
	}
	if ls[0].AgentID != "C" {
		t.Errorf("expected remaining lock to be C, got %s", ls[0].AgentID)
	}
}

func TestNormalizeFilePath(t *testing.T) {
	cases := map[string]string{
		"src/a.ts":       "src/a.ts",
		"./src/a.ts":     "src/a.ts",
		"src\\win\\a.ts": "src/win/a.ts",
		"src/a.ts/":      "src/a.ts",
	}
	for in, want := range cases {
		if got := NormalizeFilePath(in); got != want {
			t.Errorf("NormalizeFilePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAcquire_ConcurrentSerialized(t *testing.T) {
	// Two goroutines race on the same file; exactly one wins.
	m, _ := newTestManager(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	var results [2]*AcquireResult
	var errs [2]error
	for i := 0; i < 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := m.Acquire(ctx, AcquireParams{
				RepoRoot: "/r", FilePath: "hot.ts", AgentID: fmt.Sprintf("agent-%d", i),
			})
			results[i] = res
			errs[i] = err
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	var acquired, busy int
	for _, r := range results {
		switch r.Status {
		case StatusAcquired:
			acquired++
		case StatusBusy:
			busy++
		}
	}
	if acquired != 1 || busy != 1 {
		t.Fatalf("expected exactly one acquired and one busy, got acquired=%d busy=%d", acquired, busy)
	}
}
