# Keel

The coordination plane for AI coding agents.

Run Claude Code, Cursor, Codex, Windsurf, OpenClaw, and Paperclip on
the same repo — without them stepping on each other.

## Primitives

- Region locks — atomic checkout of files and ranges
- Plan reconciliation — agents submit intent before writing
- Sibling context — each agent sees what others are doing
- Deterministic merge queue — test-gated, scope-checked PRs
- Per-agent cost circuit breakers — kill runaway loops

## Install

    curl -fsSL https://keel.dev/install | sh

## Status

Early. Ship feedback at github.com/<owner>/keel/issues.
```
