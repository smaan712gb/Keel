# Changelog

All notable changes to Keel will land here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] — first tagged release

First public release: a single-binary MCP server that gives AI coding agents
a shared coordination spine over a local SQLite store.

### Added
- **Six MCP tools** (frozen contract):
  `acquire_lock`, `release_lock`, `heartbeat`, `list_locks`, `declare_plan`,
  `get_sibling_context`.
- **Whole-file lock manager** with leased locks (default 300s, max 3600s),
  `heartbeat` extension, and a 30s background reaper that refuses to revive
  expired leases. `acquire_lock` returns
  `{status, holder:{agent_id, plan_summary, eta_seconds}, lease_expires_at}`
  on contention so the caller can wait or pivot.
- **Plan store** that records one-sentence agent intent and surfaces it via
  `get_sibling_context`. Active dedupe is intentionally deferred to M2.
- **CLI** — `keel init`, `keel serve`, `keel status [--repo --json]`,
  `keel acquire <repo> <file> <agent> [--plan --eta --lease --json]`,
  `keel release <lock_id> <agent>`. Exit codes for `acquire`:
  `0` acquired, `2` busy, `1` error.
- **Agent configs**: `examples/claude-code/CLAUDE.md` and
  `examples/cursor/{README.md,mcp.json}` wire both agents end-to-end.
- **Demo**: `examples/demo/race.sh` reproduces a two-agent contention flow;
  `examples/demo/keel.tape` renders a GIF for the README via
  [vhs](https://github.com/charmbracelet/vhs).
- **Install**: `install.sh` downloads a verified prebuilt binary from the
  GitHub release for Linux and macOS (amd64/arm64); Windows installs via
  `go install` or the release archive.
- **Release automation**: GoReleaser + GitHub Actions publish multi-platform
  archives and a `checksums.txt` on every `v*` tag.
- **CI**: `go vet`, `gofmt`, `go build`, `go test` on Linux / macOS / Windows
  on every push and PR.

### Deliberately deferred to M2
- GitHub App with a "Coordinated by Keel" PR footer.
- Deterministic merge queue + test / scope gates.
- Per-agent cost circuit breakers (token + dollar caps).
- Active plan reconciliation (pick-a-winner when two agents declare
  overlapping work).
- Line-range locks (requires an AST-aware scope model).

[Unreleased]: https://github.com/smaan712gb/keel/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/smaan712gb/keel/releases/tag/v0.1.0
