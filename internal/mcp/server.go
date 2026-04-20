// Package mcp wires Keel's managers into a Model Context Protocol server
// over stdio. Tool schemas here are the frozen M1 contract — additive changes
// only; never rename or remove a field.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smaan712gb/keel/internal/locks"
	"github.com/smaan712gb/keel/internal/plans"
)

// Version advertised to MCP clients. Bump on any user-visible behaviour change.
const Version = "0.1.0"

// Server bundles the managers and the underlying MCP server instance.
type Server struct {
	Locks *locks.Manager
	Plans *plans.Store

	mcpServer *sdk.Server
}

// New builds a Keel MCP server with all six M1 tools registered.
func New(lm *locks.Manager, ps *plans.Store) *Server {
	impl := &sdk.Implementation{Name: "keel", Version: Version}
	srv := sdk.NewServer(impl, nil)
	s := &Server{Locks: lm, Plans: ps, mcpServer: srv}
	s.registerTools()
	return s
}

// Run blocks serving MCP over stdio until ctx is cancelled or stdin closes.
func (s *Server) Run(ctx context.Context) error {
	return s.mcpServer.Run(ctx, &sdk.StdioTransport{})
}

// --- Tool input/output types (frozen M1 contract) ---

type AcquireLockArgs struct {
	RepoRoot     string `json:"repo_root"     jsonschema:"absolute path to the repo root"`
	FilePath     string `json:"file_path"     jsonschema:"file path relative to repo_root, whole-file granularity in M1"`
	AgentID      string `json:"agent_id"      jsonschema:"stable agent id, e.g. claude-code-<pid> or cursor-<session>"`
	PlanSummary  string `json:"plan_summary,omitempty" jsonschema:"one-sentence description of what this agent is about to do"`
	ETASeconds   *int   `json:"eta_seconds,omitempty"  jsonschema:"self-estimated seconds until release; surfaced to competing agents"`
	LeaseSeconds int    `json:"lease_seconds,omitempty" jsonschema:"lease in seconds (default 300, max 3600); call heartbeat to extend"`
}

type AcquireLockResult struct {
	Status         string        `json:"status" jsonschema:"acquired or busy"`
	LockID         string        `json:"lock_id,omitempty"`
	LeaseExpiresAt time.Time     `json:"lease_expires_at,omitempty"`
	Holder         *locks.Holder `json:"holder,omitempty" jsonschema:"present when status=busy; the agent currently holding the file"`
}

type ReleaseLockArgs struct {
	LockID  string `json:"lock_id"  jsonschema:"lock_id returned by acquire_lock"`
	AgentID string `json:"agent_id" jsonschema:"must match the acquirer's agent_id"`
}

type ReleaseLockResult struct {
	Released bool `json:"released"`
}

type HeartbeatArgs struct {
	LockID       string `json:"lock_id"`
	AgentID      string `json:"agent_id"`
	LeaseSeconds int    `json:"lease_seconds,omitempty" jsonschema:"new lease in seconds from now (default 300)"`
}

type HeartbeatResult struct {
	OK             bool      `json:"ok" jsonschema:"false if the lock was already expired or released"`
	LeaseExpiresAt time.Time `json:"lease_expires_at,omitempty"`
}

type ListLocksArgs struct {
	RepoRoot string `json:"repo_root"`
}

type ListLocksResult struct {
	Locks []locks.ActiveLock `json:"locks"`
}

type DeclarePlanArgs struct {
	RepoRoot string   `json:"repo_root"`
	AgentID  string   `json:"agent_id"`
	Summary  string   `json:"summary" jsonschema:"one-sentence plan; surfaced to sibling agents to prevent duplicate work"`
	Files    []string `json:"files,omitempty" jsonschema:"files this plan intends to touch (advisory; does not auto-lock)"`
}

type DeclarePlanResult struct {
	PlanID string `json:"plan_id"`
}

type GetSiblingContextArgs struct {
	RepoRoot string `json:"repo_root"`
	AgentID  string `json:"agent_id" jsonschema:"calling agent's id; siblings are all OTHER agents active in the repo"`
}

type Sibling struct {
	AgentID string             `json:"agent_id"`
	Locks   []locks.ActiveLock `json:"locks"`
	Plans   []plans.Plan       `json:"plans"`
}

type GetSiblingContextResult struct {
	Siblings []Sibling `json:"siblings"`
}

// --- Tool registration ---

func (s *Server) registerTools() {
	sdk.AddTool(s.mcpServer, &sdk.Tool{
		Name:        "acquire_lock",
		Description: "Atomically acquire a whole-file lock. On contention returns status=busy with the current holder's declared plan and ETA — the caller decides whether to wait or pivot.",
	}, s.handleAcquireLock)

	sdk.AddTool(s.mcpServer, &sdk.Tool{
		Name:        "release_lock",
		Description: "Release a lock held by this agent. Idempotent — returns released=false if the lock doesn't exist or belongs to another agent.",
	}, s.handleReleaseLock)

	sdk.AddTool(s.mcpServer, &sdk.Tool{
		Name:        "heartbeat",
		Description: "Extend the lease on an active lock. Refuses to revive already-expired leases — that is the reaper's job.",
	}, s.handleHeartbeat)

	sdk.AddTool(s.mcpServer, &sdk.Tool{
		Name:        "list_locks",
		Description: "List every currently-held (unexpired, unreleased) lock in the given repo.",
	}, s.handleListLocks)

	sdk.AddTool(s.mcpServer, &sdk.Tool{
		Name:        "declare_plan",
		Description: "Record this agent's intent. Plans are surfaced via get_sibling_context; active dedupe is deferred to M2.",
	}, s.handleDeclarePlan)

	sdk.AddTool(s.mcpServer, &sdk.Tool{
		Name:        "get_sibling_context",
		Description: "Return what every OTHER agent in the repo is currently doing (their locks + recent plans). Call this before planning work to avoid duplication.",
	}, s.handleGetSiblingContext)
}

// --- Handlers ---

func (s *Server) handleAcquireLock(ctx context.Context, _ *sdk.CallToolRequest, args AcquireLockArgs) (*sdk.CallToolResult, AcquireLockResult, error) {
	res, err := s.Locks.Acquire(ctx, locks.AcquireParams{
		RepoRoot:     args.RepoRoot,
		FilePath:     args.FilePath,
		AgentID:      args.AgentID,
		PlanSummary:  args.PlanSummary,
		ETASeconds:   args.ETASeconds,
		LeaseSeconds: args.LeaseSeconds,
	})
	if err != nil {
		return nil, AcquireLockResult{}, err
	}
	out := AcquireLockResult{
		Status:         res.Status,
		LockID:         res.LockID,
		LeaseExpiresAt: res.LeaseExpiresAt,
		Holder:         res.Holder,
	}
	return textResult(out), out, nil
}

func (s *Server) handleReleaseLock(ctx context.Context, _ *sdk.CallToolRequest, args ReleaseLockArgs) (*sdk.CallToolResult, ReleaseLockResult, error) {
	ok, err := s.Locks.Release(ctx, args.LockID, args.AgentID)
	if err != nil {
		return nil, ReleaseLockResult{}, err
	}
	out := ReleaseLockResult{Released: ok}
	return textResult(out), out, nil
}

func (s *Server) handleHeartbeat(ctx context.Context, _ *sdk.CallToolRequest, args HeartbeatArgs) (*sdk.CallToolResult, HeartbeatResult, error) {
	expires, ok, err := s.Locks.Heartbeat(ctx, args.LockID, args.AgentID, args.LeaseSeconds)
	if err != nil {
		return nil, HeartbeatResult{}, err
	}
	out := HeartbeatResult{OK: ok, LeaseExpiresAt: expires}
	return textResult(out), out, nil
}

func (s *Server) handleListLocks(ctx context.Context, _ *sdk.CallToolRequest, args ListLocksArgs) (*sdk.CallToolResult, ListLocksResult, error) {
	if args.RepoRoot == "" {
		return nil, ListLocksResult{}, errors.New("repo_root is required")
	}
	ls, err := s.Locks.ListActive(ctx, args.RepoRoot)
	if err != nil {
		return nil, ListLocksResult{}, err
	}
	if ls == nil {
		ls = []locks.ActiveLock{}
	}
	out := ListLocksResult{Locks: ls}
	return textResult(out), out, nil
}

func (s *Server) handleDeclarePlan(ctx context.Context, _ *sdk.CallToolRequest, args DeclarePlanArgs) (*sdk.CallToolResult, DeclarePlanResult, error) {
	id, err := s.Plans.Declare(ctx, plans.DeclareParams{
		RepoRoot: args.RepoRoot,
		AgentID:  args.AgentID,
		Summary:  args.Summary,
		Files:    args.Files,
	})
	if err != nil {
		return nil, DeclarePlanResult{}, err
	}
	out := DeclarePlanResult{PlanID: id}
	return textResult(out), out, nil
}

func (s *Server) handleGetSiblingContext(ctx context.Context, _ *sdk.CallToolRequest, args GetSiblingContextArgs) (*sdk.CallToolResult, GetSiblingContextResult, error) {
	if args.RepoRoot == "" || args.AgentID == "" {
		return nil, GetSiblingContextResult{}, errors.New("repo_root and agent_id are required")
	}
	allLocks, err := s.Locks.ListActive(ctx, args.RepoRoot)
	if err != nil {
		return nil, GetSiblingContextResult{}, err
	}
	activePlans, err := s.Plans.ListActive(ctx, args.RepoRoot, args.AgentID)
	if err != nil {
		return nil, GetSiblingContextResult{}, err
	}

	byAgent := map[string]*Sibling{}
	for _, l := range allLocks {
		if l.AgentID == args.AgentID {
			continue
		}
		sib := byAgent[l.AgentID]
		if sib == nil {
			sib = &Sibling{AgentID: l.AgentID}
			byAgent[l.AgentID] = sib
		}
		sib.Locks = append(sib.Locks, l)
	}
	for _, p := range activePlans {
		sib := byAgent[p.AgentID]
		if sib == nil {
			sib = &Sibling{AgentID: p.AgentID}
			byAgent[p.AgentID] = sib
		}
		sib.Plans = append(sib.Plans, p)
	}

	out := GetSiblingContextResult{Siblings: make([]Sibling, 0, len(byAgent))}
	for _, sib := range byAgent {
		out.Siblings = append(out.Siblings, *sib)
	}
	return textResult(out), out, nil
}

// textResult mirrors the typed output as a compact JSON TextContent block —
// gives clients that only read Content (older or non-schema-aware) a readable payload.
func textResult(v any) *sdk.CallToolResult {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return &sdk.CallToolResult{
			Content: []sdk.Content{&sdk.TextContent{Text: fmt.Sprintf("marshal error: %v", err)}},
			IsError: true,
		}
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(b)}}}
}
