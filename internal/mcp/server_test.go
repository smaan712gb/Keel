package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smaan712gb/keel/internal/locks"
	"github.com/smaan712gb/keel/internal/plans"
	"github.com/smaan712gb/keel/internal/store"
)

// connect spins up a Keel MCP server and a test client wired through an
// in-memory transport pair. The returned session exercises the real
// protocol, not just handler functions directly.
func connect(t *testing.T) *sdk.ClientSession {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	srv := New(locks.New(db), plans.New(db))
	serverTransport, clientTransport := sdk.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() { _ = srv.mcpServer.Run(ctx, serverTransport) }()

	client := sdk.NewClient(&sdk.Implementation{Name: "keel-test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callTool[T any](t *testing.T, sess *sdk.ClientSession, name string, args any) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := sess.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	if res.IsError {
		t.Fatalf("call %s returned IsError: %+v", name, res)
	}
	var out T
	// The SDK returns the structured output as JSON TextContent; decode it.
	if len(res.Content) == 0 {
		t.Fatalf("call %s returned no content", name)
	}
	tc, ok := res.Content[0].(*sdk.TextContent)
	if !ok {
		t.Fatalf("call %s: first content block is %T, want *TextContent", name, res.Content[0])
	}
	if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
		t.Fatalf("decode %s: %v\npayload=%s", name, err, tc.Text)
	}
	return out
}

func TestMCP_ToolsListed(t *testing.T) {
	sess := connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"acquire_lock": true, "release_lock": true, "heartbeat": true,
		"list_locks": true, "declare_plan": true, "get_sibling_context": true,
	}
	for _, tool := range res.Tools {
		delete(want, tool.Name)
	}
	if len(want) > 0 {
		t.Fatalf("missing tools: %v", want)
	}
}

func TestMCP_AcquireContentionFlow(t *testing.T) {
	sess := connect(t)

	first := callTool[AcquireLockResult](t, sess, "acquire_lock", AcquireLockArgs{
		RepoRoot: "/demo", FilePath: "payments.ts", AgentID: "claude-code",
		PlanSummary: "add logging",
	})
	if first.Status != locks.StatusAcquired {
		t.Fatalf("first acquire: status=%q", first.Status)
	}
	if first.LockID == "" {
		t.Fatalf("first acquire: missing lock_id")
	}

	second := callTool[AcquireLockResult](t, sess, "acquire_lock", AcquireLockArgs{
		RepoRoot: "/demo", FilePath: "payments.ts", AgentID: "cursor",
		PlanSummary: "refactor",
	})
	if second.Status != locks.StatusBusy {
		t.Fatalf("second acquire: expected busy, got %q", second.Status)
	}
	if second.Holder == nil || second.Holder.AgentID != "claude-code" || second.Holder.PlanSummary != "add logging" {
		t.Fatalf("second acquire: unexpected holder: %+v", second.Holder)
	}

	released := callTool[ReleaseLockResult](t, sess, "release_lock", ReleaseLockArgs{
		LockID: first.LockID, AgentID: "claude-code",
	})
	if !released.Released {
		t.Fatalf("release: expected true")
	}

	third := callTool[AcquireLockResult](t, sess, "acquire_lock", AcquireLockArgs{
		RepoRoot: "/demo", FilePath: "payments.ts", AgentID: "cursor",
	})
	if third.Status != locks.StatusAcquired {
		t.Fatalf("third acquire after release: status=%q", third.Status)
	}
}

func TestMCP_SiblingContext(t *testing.T) {
	sess := connect(t)

	// Agent A takes a lock and declares a plan.
	alock := callTool[AcquireLockResult](t, sess, "acquire_lock", AcquireLockArgs{
		RepoRoot: "/r", FilePath: "a.ts", AgentID: "A", PlanSummary: "touching a",
	})
	callTool[DeclarePlanResult](t, sess, "declare_plan", DeclarePlanArgs{
		RepoRoot: "/r", AgentID: "A", Summary: "rework auth", Files: []string{"a.ts", "b.ts"},
	})

	// Agent B asks what siblings are up to; should see A's lock and plan.
	ctx := callTool[GetSiblingContextResult](t, sess, "get_sibling_context", GetSiblingContextArgs{
		RepoRoot: "/r", AgentID: "B",
	})
	if len(ctx.Siblings) != 1 || ctx.Siblings[0].AgentID != "A" {
		t.Fatalf("expected one sibling A, got %+v", ctx.Siblings)
	}
	sib := ctx.Siblings[0]
	if len(sib.Locks) != 1 || sib.Locks[0].FilePath != "a.ts" {
		t.Errorf("expected A to hold a.ts, got %+v", sib.Locks)
	}
	if len(sib.Plans) != 1 || sib.Plans[0].Summary != "rework auth" {
		t.Errorf("expected A's plan surfaced, got %+v", sib.Plans)
	}

	// Agent A asking should see no siblings (itself is excluded).
	myCtx := callTool[GetSiblingContextResult](t, sess, "get_sibling_context", GetSiblingContextArgs{
		RepoRoot: "/r", AgentID: "A",
	})
	if len(myCtx.Siblings) != 0 {
		t.Fatalf("expected zero siblings when asking as A, got %+v", myCtx.Siblings)
	}
	_ = alock
}

func TestMCP_Heartbeat(t *testing.T) {
	sess := connect(t)
	lk := callTool[AcquireLockResult](t, sess, "acquire_lock", AcquireLockArgs{
		RepoRoot: "/r", FilePath: "h.ts", AgentID: "A", LeaseSeconds: 60,
	})
	hb := callTool[HeartbeatResult](t, sess, "heartbeat", HeartbeatArgs{
		LockID: lk.LockID, AgentID: "A", LeaseSeconds: 600,
	})
	if !hb.OK {
		t.Fatalf("heartbeat: expected ok=true")
	}
	if !hb.LeaseExpiresAt.After(lk.LeaseExpiresAt) {
		t.Errorf("heartbeat did not extend: before=%v after=%v", lk.LeaseExpiresAt, hb.LeaseExpiresAt)
	}
}

func TestMCP_ListLocks(t *testing.T) {
	sess := connect(t)
	callTool[AcquireLockResult](t, sess, "acquire_lock", AcquireLockArgs{
		RepoRoot: "/r", FilePath: "one.ts", AgentID: "A",
	})
	callTool[AcquireLockResult](t, sess, "acquire_lock", AcquireLockArgs{
		RepoRoot: "/r", FilePath: "two.ts", AgentID: "B",
	})
	callTool[AcquireLockResult](t, sess, "acquire_lock", AcquireLockArgs{
		RepoRoot: "/other", FilePath: "three.ts", AgentID: "C",
	})
	res := callTool[ListLocksResult](t, sess, "list_locks", ListLocksArgs{RepoRoot: "/r"})
	if len(res.Locks) != 2 {
		t.Fatalf("expected 2 locks in /r, got %d: %+v", len(res.Locks), res.Locks)
	}
}
