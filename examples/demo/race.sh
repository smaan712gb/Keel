#!/usr/bin/env bash
# Two-agent race: both want to edit payments.ts. Keel serializes them.
#
# Usage: from the repo root, run:
#     bash examples/demo/race.sh
#
# Requires: keel on PATH, `keel init` run once, and `jq`.
set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
  echo "error: 'jq' is required for this demo (parses keel --json output)." >&2
  echo "install: https://stedolan.github.io/jq/download/" >&2
  exit 1
fi

REPO="$(cd "$(dirname "$0")/fixture" && pwd)"
FILE="payments.ts"

say() { printf "\033[1;36m[%s]\033[0m %s\n" "$1" "$2"; }

say "setup" "Cleaning any prior state for this demo..."
keel status --repo "$REPO" --json >/dev/null

# --- Agent A (Claude Code) grabs the lock first ---
say "claude-code" "declare_plan  — 'add logging to chargeCard'"
say "claude-code" "acquire_lock  payments.ts  (eta 6s)"
A_OUT=$(keel acquire "$REPO" "$FILE" claude-code \
  --plan="add logging to chargeCard" --eta=6 --lease=60 --json)
echo "$A_OUT"
A_LOCK=$(echo "$A_OUT" | jq -r '.lock_id')
if [[ -z "$A_LOCK" || "$A_LOCK" == "null" ]]; then
  echo "error: could not parse lock_id from acquire output" >&2
  exit 1
fi

# --- Agent B (Cursor) tries a moment later ---
sleep 1
say "cursor" "acquire_lock  payments.ts  — 'refactor validate()'"
if keel acquire "$REPO" "$FILE" cursor \
     --plan="refactor validate()" --eta=30 --lease=60; then
  say "cursor" "unexpectedly acquired — demo failed"; exit 1
fi

# --- Agent A finishes its edit and releases ---
sleep 3
say "claude-code" "writing payments.ts ..."
sleep 1
say "claude-code" "release_lock  $A_LOCK"
keel release "$A_LOCK" claude-code

# --- Agent B retries and succeeds ---
say "cursor" "retry acquire_lock  payments.ts"
B_OUT=$(keel acquire "$REPO" "$FILE" cursor \
  --plan="refactor validate()" --lease=60 --json)
echo "$B_OUT"
B_LOCK=$(echo "$B_OUT" | jq -r '.lock_id')
if [[ -z "$B_LOCK" || "$B_LOCK" == "null" ]]; then
  echo "error: could not parse lock_id from retry output" >&2
  exit 1
fi
say "cursor" "release_lock  $B_LOCK"
keel release "$B_LOCK" cursor

say "done" "coordinated by keel — 2 agents, 1 file, 0 conflicts"
