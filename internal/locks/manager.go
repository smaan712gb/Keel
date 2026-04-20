// Package locks owns whole-file region locks, lease/heartbeat semantics, and the
// orphan-reaper loop. M1 only supports file-path granularity; line ranges land in M2.
package locks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/smaan712gb/keel/internal/store"
)

const (
	DefaultLeaseSeconds = 300 // 5 minutes
	MaxLeaseSeconds     = 3600
)

// Status values returned by Acquire. Frozen public contract — add, never remove.
const (
	StatusAcquired = "acquired"
	StatusBusy     = "busy"
)

// Holder describes the agent currently holding a file's lock, surfaced when a
// competing agent's Acquire call returns StatusBusy.
type Holder struct {
	AgentID        string    `json:"agent_id"`
	PlanSummary    string    `json:"plan_summary"`
	ETASeconds     *int      `json:"eta_seconds,omitempty"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}

// AcquireResult is the contract returned to MCP callers. Fields populated depend on Status.
type AcquireResult struct {
	Status         string    `json:"status"`
	LockID         string    `json:"lock_id,omitempty"`
	LeaseExpiresAt time.Time `json:"lease_expires_at,omitempty"`
	Holder         *Holder   `json:"holder,omitempty"`
}

// ActiveLock is one row surfaced by ListActive / get_sibling_context.
type ActiveLock struct {
	LockID         string    `json:"lock_id"`
	FilePath       string    `json:"file_path"`
	AgentID        string    `json:"agent_id"`
	PlanSummary    string    `json:"plan_summary"`
	ETASeconds     *int      `json:"eta_seconds,omitempty"`
	AcquiredAt     time.Time `json:"acquired_at"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
}

// Manager is safe for concurrent use; the underlying SQLite connection is
// serialized (MaxOpenConns=1) so ACID semantics hold across goroutines.
type Manager struct {
	db  *store.DB
	now func() time.Time
}

func New(db *store.DB) *Manager {
	return &Manager{db: db, now: func() time.Time { return time.Now().UTC() }}
}

// NormalizeFilePath canonicalises a relative file path: forward slashes,
// no leading "./", no trailing slash. Callers should pre-strip repo_root.
func NormalizeFilePath(p string) string {
	p = filepath.ToSlash(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimRight(p, "/")
	return p
}

// AcquireParams is the input to Acquire. LeaseSeconds defaults to DefaultLeaseSeconds.
type AcquireParams struct {
	RepoRoot     string
	FilePath     string
	AgentID      string
	PlanSummary  string
	ETASeconds   *int
	LeaseSeconds int
}

// Acquire attempts to take a whole-file lock. On contention the result carries
// the current holder's declared plan and ETA so the caller can decide to wait
// or pivot — the user's locked M1 semantic.
func (m *Manager) Acquire(ctx context.Context, p AcquireParams) (*AcquireResult, error) {
	if p.RepoRoot == "" || p.FilePath == "" || p.AgentID == "" {
		return nil, errors.New("repo_root, file_path, and agent_id are required")
	}
	lease := p.LeaseSeconds
	if lease <= 0 {
		lease = DefaultLeaseSeconds
	}
	if lease > MaxLeaseSeconds {
		lease = MaxLeaseSeconds
	}
	filePath := NormalizeFilePath(p.FilePath)

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := m.now()
	nowStr := store.FormatTime(now)

	var (
		holderAgent   string
		holderPlan    string
		holderETA     sql.NullInt64
		holderExpires string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT agent_id, plan_summary, eta_seconds, lease_expires_at
		  FROM locks
		 WHERE repo_root = ?
		   AND file_path = ?
		   AND released_at IS NULL
		   AND lease_expires_at > ?
		 LIMIT 1
	`, p.RepoRoot, filePath, nowStr).Scan(&holderAgent, &holderPlan, &holderETA, &holderExpires)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query active lock: %w", err)
	}
	if err == nil {
		expires, _ := store.ParseTime(holderExpires)
		h := &Holder{
			AgentID:        holderAgent,
			PlanSummary:    holderPlan,
			LeaseExpiresAt: expires,
		}
		if holderETA.Valid {
			eta := int(holderETA.Int64)
			h.ETASeconds = &eta
		}
		return &AcquireResult{Status: StatusBusy, Holder: h, LeaseExpiresAt: expires}, nil
	}

	id := uuid.NewString()
	expiresAt := now.Add(time.Duration(lease) * time.Second)
	var etaVal any
	if p.ETASeconds != nil {
		etaVal = *p.ETASeconds
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO locks (id, repo_root, file_path, agent_id, plan_summary, eta_seconds, acquired_at, lease_expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, p.RepoRoot, filePath, p.AgentID, p.PlanSummary, etaVal, nowStr, store.FormatTime(expiresAt))
	if err != nil {
		return nil, fmt.Errorf("insert lock: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &AcquireResult{Status: StatusAcquired, LockID: id, LeaseExpiresAt: expiresAt}, nil
}

// Release marks the lock released. Returns false if the lock doesn't exist,
// belongs to a different agent, or was already released/expired.
func (m *Manager) Release(ctx context.Context, lockID, agentID string) (bool, error) {
	if lockID == "" || agentID == "" {
		return false, errors.New("lock_id and agent_id are required")
	}
	res, err := m.db.ExecContext(ctx, `
		UPDATE locks
		   SET released_at = ?
		 WHERE id = ?
		   AND agent_id = ?
		   AND released_at IS NULL
	`, store.FormatTime(m.now()), lockID, agentID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Heartbeat extends the lease on an active lock. Refuses to revive expired leases —
// that's the orphan-reaper's job, and silently extending an expired lock would let a
// ghost agent keep a file hostage.
func (m *Manager) Heartbeat(ctx context.Context, lockID, agentID string, leaseSeconds int) (time.Time, bool, error) {
	if lockID == "" || agentID == "" {
		return time.Time{}, false, errors.New("lock_id and agent_id are required")
	}
	if leaseSeconds <= 0 {
		leaseSeconds = DefaultLeaseSeconds
	}
	if leaseSeconds > MaxLeaseSeconds {
		leaseSeconds = MaxLeaseSeconds
	}
	now := m.now()
	newExpiry := now.Add(time.Duration(leaseSeconds) * time.Second)
	res, err := m.db.ExecContext(ctx, `
		UPDATE locks
		   SET lease_expires_at = ?
		 WHERE id = ?
		   AND agent_id = ?
		   AND released_at IS NULL
		   AND lease_expires_at > ?
	`, store.FormatTime(newExpiry), lockID, agentID, store.FormatTime(now))
	if err != nil {
		return time.Time{}, false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return time.Time{}, false, nil
	}
	return newExpiry, true, nil
}

// ListActive returns every lock that is currently held (not released, not expired) in a repo.
func (m *Manager) ListActive(ctx context.Context, repoRoot string) ([]ActiveLock, error) {
	nowStr := store.FormatTime(m.now())
	rows, err := m.db.QueryContext(ctx, `
		SELECT id, file_path, agent_id, plan_summary, eta_seconds, acquired_at, lease_expires_at
		  FROM locks
		 WHERE repo_root = ?
		   AND released_at IS NULL
		   AND lease_expires_at > ?
		 ORDER BY acquired_at ASC
	`, repoRoot, nowStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ActiveLock
	for rows.Next() {
		var (
			al        ActiveLock
			etaN      sql.NullInt64
			acqStr    string
			expireStr string
		)
		if err := rows.Scan(&al.LockID, &al.FilePath, &al.AgentID, &al.PlanSummary, &etaN, &acqStr, &expireStr); err != nil {
			return nil, err
		}
		al.AcquiredAt, _ = store.ParseTime(acqStr)
		al.LeaseExpiresAt, _ = store.ParseTime(expireStr)
		if etaN.Valid {
			eta := int(etaN.Int64)
			al.ETASeconds = &eta
		}
		out = append(out, al)
	}
	return out, rows.Err()
}

// Reap marks every lock whose lease has expired as released, pinned to its
// expiry time rather than "now" so historical queries stay accurate. Returns
// the number of leases reaped.
func (m *Manager) Reap(ctx context.Context) (int, error) {
	nowStr := store.FormatTime(m.now())
	res, err := m.db.ExecContext(ctx, `
		UPDATE locks
		   SET released_at = lease_expires_at
		 WHERE released_at IS NULL
		   AND lease_expires_at <= ?
	`, nowStr)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
