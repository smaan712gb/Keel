# Working in this repo with Keel

Other agents may be editing this repo concurrently. Before you write to any file,
coordinate through Keel via the MCP tools listed below.

## Expected workflow

1. **Declare intent.** Call `declare_plan` with a one-sentence summary and the files
   you expect to touch. This is advisory — it does not lock anything — but it lets
   sibling agents see what you're about to do and skip duplicate work.

2. **Check for siblings.** Call `get_sibling_context` to see what other agents are
   currently holding or planning. If a sibling's plan overlaps with yours, either
   pivot or coordinate via your user.

3. **Acquire before writing.** Call `acquire_lock` for every file you're about to
   modify. Pass your own `agent_id`, a `plan_summary`, and an `eta_seconds`
   estimate.
   - `status: "acquired"` → proceed.
   - `status: "busy"` → the response includes the holder's id, plan, and ETA.
     Wait, pivot to a different file, or ask the user.

4. **Heartbeat for long work.** Default lease is 5 minutes. If you expect to take
   longer, call `heartbeat` before the lease expires.

5. **Release when done.** Call `release_lock` immediately after your edit is
   committed to disk. Crashed agents are reaped after their lease expires; do
   not rely on that path in normal operation.

## Agent id

Use a stable id unique to your session, e.g. `claude-code-{session_id}`.
`repo_root` should be the absolute path to the repo root.

## M1 constraints

- Region locks are whole-file only. Line-range locks are coming in M2.
- Plan reconciliation is record-and-surface only. If two agents declare
  overlapping plans, Keel does not pick a winner yet — the user does.
