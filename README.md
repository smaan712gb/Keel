# Keel

[![ci](https://github.com/smaan712gb/keel/actions/workflows/ci.yml/badge.svg)](https://github.com/smaan712gb/keel/actions/workflows/ci.yml)
[![release](https://img.shields.io/github/v/release/smaan712gb/keel?sort=semver)](https://github.com/smaan712gb/keel/releases)
[![license](https://img.shields.io/github/license/smaan712gb/keel)](LICENSE)

**The coordination plane for AI coding agents.**

Run Claude Code, Cursor, Codex, Windsurf, OpenClaw, Paperclip on the same
repo — without them corrupting each other's work.

Keel is a single-binary MCP server. Every agent you care about already speaks
MCP; Keel gives them a shared spine for *who is editing what, right now, and
why*.

> Git was for humans coordinating on code. Keel is for agents.

---

## What Keel does (M1)

Six MCP tools exposing five primitives, all backed by a local SQLite store.

| Tool | What it does |
| --- | --- |
| `acquire_lock` | Atomically take a whole-file lock. On contention, returns the holder's declared plan + ETA so the caller can wait or pivot. |
| `release_lock` | Drop a lock held by the calling agent. |
| `heartbeat` | Extend a live lock's lease (refuses to revive an already-expired lease). |
| `list_locks` | Every lock currently held in a repo. |
| `declare_plan` | One-sentence description of what you're about to do (advisory, surfaced to siblings). |
| `get_sibling_context` | What every *other* agent in the repo is currently holding or planning. |

Under the hood:

- **Leased locks** — default 5 minutes, extended via `heartbeat`. Orphans are
  reaped by a background loop so a crashed agent can't brick the repo.
- **File-path granularity only in M1.** Line-range locks need an AST and are
  explicitly deferred to M2 — a footgun if shipped early.
- **Recorded plans, not reconciled plans.** `declare_plan` persists intent;
  `get_sibling_context` surfaces it. Active dedupe (picking a winner when two
  agents want to "add logging") is M2.

---

## Install

One-liner (Linux, macOS — amd64 and arm64):

    curl -fsSL https://raw.githubusercontent.com/smaan712gb/keel/main/install.sh | bash

The script downloads the latest release, verifies its SHA-256 against
`checksums.txt`, and drops the `keel` binary into `~/.local/bin`. Set
`KEEL_INSTALL_DIR` or `KEEL_VERSION` to override.

Windows (or any host with Go 1.22+):

    go install github.com/smaan712gb/keel/cmd/keel@latest

Or grab the archive for your platform from the
[releases page](https://github.com/smaan712gb/keel/releases).

Then:

    keel init

That creates `~/.keel/` and prints the MCP registration snippet for Claude
Code and Cursor. Paste it in, restart the agent, and you're wired.

### Claude Code (one-liner)

    claude mcp add keel -- keel serve

### Cursor

Add to `~/.cursor/mcp.json`:

    {
      "mcpServers": {
        "keel": { "command": "keel", "args": ["serve"] }
      }
    }

Sample configs and a CLAUDE.md prompt that teaches agents the workflow live in
[`examples/`](examples/).

---

## See it work

Prereqs for the race demo: `bash` and `jq` on PATH. Windows users can run it
under WSL or Git Bash.

    keel init                        # once
    bash examples/demo/race.sh       # from the repo root

Two agents race for `payments.ts`. Keel serializes them deterministically:

    [claude-code] acquire_lock  payments.ts  (eta 6s) → ACQUIRED
    [cursor]      acquire_lock  payments.ts         → BUSY
                  held by claude-code — "add logging to chargeCard"  eta=6s
    [claude-code] release_lock                      → released
    [cursor]      retry                             → ACQUIRED
    [done]        coordinated by keel — 2 agents, 1 file, 0 conflicts

A GIF renders from [`examples/demo/keel.tape`](examples/demo/keel.tape) via
[vhs](https://github.com/charmbracelet/vhs).

---

## How agents use it

1. `declare_plan` — tell siblings what you're about to do.
2. `get_sibling_context` — see what they're doing.
3. `acquire_lock` before you write. If `busy`, read the holder's plan and
   decide: wait, pivot, or surface to the user.
4. `heartbeat` while you work, if the task runs long.
5. `release_lock` the moment the file is written.

Drop [`examples/claude-code/CLAUDE.md`](examples/claude-code/CLAUDE.md) into
your repo root (or the Cursor rules snippet in
[`examples/cursor/README.md`](examples/cursor/README.md)) and the agent will
follow this loop without further prompting.

---

## CLI

    keel init                                      Prepare state dir + print MCP config
    keel serve                                     Run the MCP server over stdio
    keel status [--repo=PATH] [--json]             Show active locks + plans
    keel acquire <repo> <file> <agent> [flags]     Manual acquire (scripting / debug)
    keel release <lock_id> <agent>                 Manual release

Exit codes for `keel acquire`: `0` acquired, `2` busy, `1` error — so shell
scripts can branch cleanly. Flags for `acquire`: `--plan=TEXT`, `--eta=SECS`,
`--lease=SECS` (default 300, max 3600), `--json`.

---

## Architecture

    cmd/keel/             CLI entrypoint (init, serve, status, acquire, release)
    internal/store/       SQLite connection + schema (embedded .sql)
    internal/locks/       Lock manager: acquire, release, heartbeat, reap
    internal/plans/       Plan store: declare, complete, list-active
    internal/mcp/         MCP server + 6 tool handlers (frozen contract)
    examples/             Claude Code + Cursor configs, demo fixture, vhs tape

State lives globally at `~/.keel/state.db` so one daemon serves every repo you
work in. Each tool call carries `repo_root`; the server has no notion of a
"current repo."

---

## What's in M2 (deliberately *not* in M1)

- GitHub App that posts a "Coordinated by Keel" footer on every PR + gates
  merges behind test + scope checks.
- Deterministic merge queue.
- Per-agent cost circuit breakers (token + dollar caps per task).
- Active plan reconciliation: pick a winner when two agents declare
  overlapping work.
- Line-range locks (needs an AST-aware scope model).

Not on the roadmap: Keel does not ship a sandbox (partner with
Northflank/Blaxel), an observability layer (Braintrust/Dash0 already own it),
or its own agent. Keel is the neutral spine — the moat is the neutrality.

---

## Status

Early. Feedback lands at
[github.com/smaan712gb/keel/issues](https://github.com/smaan712gb/keel/issues).
