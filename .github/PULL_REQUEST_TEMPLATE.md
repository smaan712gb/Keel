<!-- Thanks for contributing to Keel. -->

## What & why

<!-- One paragraph: what this PR changes and why. -->

## Contract check

- [ ] No rename or removal of existing MCP tool names, arguments, or response fields.
- [ ] No change to the `acquire_lock` busy response shape (`{ status, holder, lease_expires_at }`).
- [ ] Exit codes for `keel acquire` unchanged (`0` acquired, `2` busy, `1` error).

## M1 / M2 scope

- [ ] This change fits the M1 scope (locks, plans, MCP tools, CLI, demo, install).
- [ ] If it touches an M2-deferred area (GitHub App, merge queue, cost caps, plan reconciliation, line-range locks), the PR explains why it must land now.

## Tests

- [ ] `go test ./...` passes.
- [ ] New behaviour has a test, or the PR explains why no test is practical.
