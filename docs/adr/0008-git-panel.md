# ADR-0008: Read-only Git panel

- **Status:** Accepted — approved by the owner 2026-07-21
- **Date:** 2026-07-21
- **Affects:** R12, D-019, spec 02 section 3.10, acceptance J1–J4
- **Raised by:** Phase 5 implementation (Git panel)

## Context

Phase 3 built the `gitview` adapter but limited it to per-file status, for the
search filter. Phase 5 completes R12: status, working-tree diff, history, and
blame, read-only. The mechanism preventing mutation (D-019) already existed —
an enforced subcommand allow-list — so the decisions here are about extending it
safely and about how the panel behaves, not about whether Git may write (it may
not, ever, in v0.1).

## Decisions

### 1. The allow-list widens by exactly three read-only subcommands

`diff`, `log`, and `blame` join `rev-parse` and `status`. Every one is
inspection only. Two tests hold the line: `TestOnlyReadOnlySubcommandsAreReachable`
proves no mutating subcommand runs (J3), and `TestAllowListIsExhaustive` fails if
the set changes without a deliberate edit — so a future widening is reviewed, not
silent. Commands run through `exec.Command` with an argument vector and a `--`
separator before any path, never a shell, so no document id can be read as a flag
or a command.

### 2. Status is cached in the background; diff, history, and blame run live

`git status` on a large repository is slow, so it is refreshed off the request
path and served from a snapshot (unchanged from Phase 3, requirement N2). Diff,
history, and blame are per-document and cheap, so they run synchronously on the
request. Blame is additionally fetched only on demand in the UI, because it is
the largest payload and rarely the first thing wanted.

### 3. Unavailability is a flag, not an error

Every Git endpoint returns `{"available": false}` with HTTP 200 when there is no
repository or no `git` on PATH, and the panel renders an explanation. A missing
Git must never look like a fault, because core functionality does not depend on
it (acceptance J4, constitution C1). A per-file operation on an untracked or
uncommitted file likewise returns empty rather than erroring — "no history yet"
is a normal state.

## Consequences

- The read operations live in `internal/gitview/read.go`, beside the existing
  status adapter, and are exposed through `GET /api/v1/git/{status,diff,history,blame}`.
- The frontend adds a **Git** tab to the right context panel (shown only when
  the workspace reports Git capability), and the Map Room home gains a **Changed**
  section from the same status snapshot (spec 04 section 3).
