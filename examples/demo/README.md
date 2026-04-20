# Demo: two agents, one file

`race.sh` simulates what happens when two coding agents try to modify the same
file at the same time. Keel serializes them deterministically — the second
agent receives the first agent's plan and ETA and can decide whether to wait
or pivot.

## Run it

Prereqs: `keel` on PATH, `jq` installed, bash (Linux, macOS, WSL, or Git Bash
on Windows).

    keel init                        # once per machine
    bash examples/demo/race.sh

Expected output (timestamps and IDs will differ):

    [claude-code] acquire_lock  payments.ts  (eta 6s)
    {
      "status": "acquired",
      "lock_id": "df737d05-…",
      "lease_expires_at": "…"
    }
    [cursor]      acquire_lock  payments.ts  — 'refactor validate()'
    BUSY      held by claude-code — "add logging to chargeCard"  eta=6s  lease=58s
    [claude-code] release_lock  df737d05-…
    released df737d05
    [cursor]      retry acquire_lock
    ACQUIRED  lock=3f81f8fa  lease=59s
    [done]    coordinated by keel — 2 agents, 1 file, 0 conflicts

## Produce the GIF

The README embeds a short GIF rendered from `keel.tape` with
[vhs](https://github.com/charmbracelet/vhs):

    vhs examples/demo/keel.tape

Output lands at `examples/demo/keel.gif`. vhs requires ffmpeg and ttyd;
install per the vhs README.
