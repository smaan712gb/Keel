# Keel + Cursor

Cursor discovers MCP servers from `~/.cursor/mcp.json` (global) or
`.cursor/mcp.json` checked into a repo.

## Install

    keel init                          # prints config to paste
    cp mcp.json ~/.cursor/mcp.json

Restart Cursor. In Settings → MCP you should see `keel` listed with six
tools: `acquire_lock`, `release_lock`, `heartbeat`, `list_locks`,
`declare_plan`, `get_sibling_context`.

## Prompt snippet for Cursor Rules

Paste this into your Cursor Rules (`.cursor/rules/keel.mdc` or Settings →
Rules for AI) so Cursor coordinates through Keel on every edit:

> Before modifying any file in this repo, call `mcp_keel_acquire_lock` with the
> absolute `repo_root`, the `file_path` relative to it, a stable `agent_id`
> (`cursor-<session>`), a one-sentence `plan_summary`, and an `eta_seconds`
> estimate. If the response is `busy`, read the holder's `plan_summary` and
> either wait, pivot to a different file, or ask the user. Call
> `mcp_keel_release_lock` immediately after each file is saved.
