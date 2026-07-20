# ADR-0001: Bootstrap token lifetime

- **Status:** Accepted — option A approved by the owner 2026-07-20
- **Date:** 2026-07-18
- **Affects:** R14, spec 03 section 10, acceptance A3
- **Raised by:** Phase 0 implementation

## Current rule

Spec 03 section 10 states:

> The server MUST create a high-entropy session secret at startup. The browser
> receives it through the one-time launch URL or an equivalent bootstrap
> mechanism, then uses secure same-site session state.

The phrase "one-time launch URL" is ambiguous between two readings:

1. the URL is issued once, at launch, and remains redeemable while the process
   runs;
2. the token is single-use and is consumed by the first redemption.

## What Phase 0 implements

Reading 1. The bootstrap token is generated once per process, is redeemable any
number of times while that process runs, and each redemption mints a distinct
session ID. The redirect to `/` strips the token from the address bar and
browser history immediately after redemption.

## Why

Reading 2 breaks ordinary local use. A user who opens a second browser, uses a
private window, or clears cookies has no way back into a running workspace
without restarting the process. Athenaeum is a local command centre that is
expected to stay running for hours, so single-use redemption converts a routine
action into a restart.

The residual risk is bounded. The token is 32 random bytes, never logged
(enforced by `TestA3BootstrapTokenNotInLogs`), and reachable only over loopback
in the default mode. The realistic exposure is another local user reading the
launch URL from the process table or a shell scrollback — and that user could
equally read the workspace files directly.

## Why this is not a free choice

Under reading 2 the exposure window closes at first redemption, which is
strictly better against scrollback capture. That is a real security difference,
so it is the owner's decision, not the implementer's.

## Options

| Option | Behaviour | Cost |
|---|---|---|
| A (implemented) | Token redeemable for the process lifetime | Longer exposure window |
| B | Single-use token; re-bootstrap requires restart | Hostile to normal use |
| C | Single-use token plus a `--reissue-token` command or a signal that prints a fresh URL | Best of both; more surface to build and test |

## Decision

**Option A is adopted for v0.1**, approved by the owner on 2026-07-20.

Revisit if remote mode ever shares the same bootstrap path. If a closed
exposure window becomes desirable, **C** is the target and belongs in Phase 6
alongside the rest of remote hardening — not **B**, which degrades the core
local workflow that constitution C1 protects.

## Impact if changed

- `internal/security.SessionManager.RedeemBootstrap` gains consumption state.
- `internal/app` gains a reissue path (option C).
- Acceptance A3 gains a redemption-exhaustion case.

No stored data format changes, so this can be revisited without migration.
