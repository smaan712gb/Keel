// Package store owns the SQLite connection and schema migrations for Keel.
package store

import (
	"database/sql"
	_ "embed"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// TimeFormat is the canonical timestamp format for all Keel tables.
// SQLite sorts ISO-8601 strings lexicographically, which matches chronological order.
const TimeFormat = "2006-01-02T15:04:05.000Z07:00"

// DB wraps a *sql.DB with Keel-specific helpers.
type DB struct {
	*sql.DB
}

// Open opens (and migrates) the Keel state database at the given path.
// Callers typically pass .keel/state.db under a repo root.
func Open(path string) (*DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve db path: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", abs)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if _, err := sqlDB.Exec(schemaSQL); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &DB{DB: sqlDB}, nil
}

// FormatTime renders a time in the canonical ISO-8601 format used across Keel tables.
func FormatTime(t time.Time) string {
	return t.UTC().Format(TimeFormat)
}

// ParseTime parses a timestamp previously written by FormatTime.
func ParseTime(s string) (time.Time, error) {
	return time.Parse(TimeFormat, s)
}
