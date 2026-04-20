// Package plans stores agents' declared intent. In M1 plans are recorded and
// surfaced via get_sibling_context; active reconciliation ("two agents want to
// add logging — pick one") is deferred to M2.
package plans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/smaan712gb/keel/internal/store"
)

// ActivePlanWindow caps how far back get_sibling_context looks. Plans older
// than this are treated as stale unless they have active locks backing them.
const ActivePlanWindow = 30 * time.Minute

// Plan is one row surfaced by List.
type Plan struct {
	PlanID      string     `json:"plan_id"`
	AgentID     string     `json:"agent_id"`
	Summary     string     `json:"summary"`
	Files       []string   `json:"files"`
	DeclaredAt  time.Time  `json:"declared_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

type Store struct {
	db  *store.DB
	now func() time.Time
}

func New(db *store.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

// DeclareParams is the input to Declare.
type DeclareParams struct {
	RepoRoot string
	AgentID  string
	Summary  string
	Files    []string
}

// Declare records a plan and returns its id. Each call creates a new row —
// callers wanting to supersede a prior plan should Complete the old one first.
func (s *Store) Declare(ctx context.Context, p DeclareParams) (string, error) {
	if p.RepoRoot == "" || p.AgentID == "" || p.Summary == "" {
		return "", errors.New("repo_root, agent_id, and summary are required")
	}
	filesJSON, err := json.Marshal(p.Files)
	if err != nil {
		return "", fmt.Errorf("marshal files: %w", err)
	}
	id := uuid.NewString()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO plans (id, repo_root, agent_id, summary, files_json, declared_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, p.RepoRoot, p.AgentID, p.Summary, string(filesJSON), store.FormatTime(s.now()))
	if err != nil {
		return "", fmt.Errorf("insert plan: %w", err)
	}
	return id, nil
}

// Complete marks a plan completed. Returns false if the plan doesn't exist,
// doesn't belong to the given agent, or was already completed.
func (s *Store) Complete(ctx context.Context, planID, agentID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE plans SET completed_at = ?
		 WHERE id = ? AND agent_id = ? AND completed_at IS NULL
	`, store.FormatTime(s.now()), planID, agentID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListActive returns plans declared within ActivePlanWindow that are not yet
// completed, optionally excluding one agent (useful for sibling-context queries).
func (s *Store) ListActive(ctx context.Context, repoRoot, excludeAgent string) ([]Plan, error) {
	cutoff := store.FormatTime(s.now().Add(-ActivePlanWindow))
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, agent_id, summary, files_json, declared_at, completed_at
		  FROM plans
		 WHERE repo_root = ?
		   AND completed_at IS NULL
		   AND declared_at >= ?
		   AND (? = '' OR agent_id != ?)
		 ORDER BY declared_at DESC
	`, repoRoot, cutoff, excludeAgent, excludeAgent)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Plan
	for rows.Next() {
		var (
			p          Plan
			filesJSON  string
			declared   string
			completedN *string
		)
		if err := rows.Scan(&p.PlanID, &p.AgentID, &p.Summary, &filesJSON, &declared, &completedN); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(filesJSON), &p.Files)
		p.DeclaredAt, _ = store.ParseTime(declared)
		if completedN != nil {
			t, _ := store.ParseTime(*completedN)
			p.CompletedAt = &t
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
