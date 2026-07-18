# ADR-0002: Every route requires a session, including the frontend shell

- **Status:** Accepted
- **Date:** 2026-07-18
- **Affects:** R14, acceptance A3
- **Raised by:** Phase 0 implementation

## Current rule

Acceptance A3 requires that Athenaeum "binds only to loopback and rejects
requests without a valid session". It does not say which routes are in scope.

## Decision

Every route except `/bootstrap` requires a valid session cookie. That includes:

- `/api/v1/health`;
- the SPA shell at `/` and every static asset.

## Why

Two weaker designs were considered and rejected.

**Public health endpoint.** Convenient for readiness probes, but it discloses
the workspace name, version, and remote-mode state to any local process that
can reach the port. Requirement N6 wants observability; it does not want an
unauthenticated information endpoint. Tests bootstrap first, which costs one
extra request and keeps A3 literally true.

**Public static assets.** Serving the SPA shell to unauthenticated callers is
close to harmless — it is our own compiled code — but it makes A3 read as
"rejects *some* requests without a session", which is exactly the kind of
quiet weakening constitution C8 exists to prevent. The stricter rule is also
simpler to state and to test.

## Consequences

- A readiness probe must bootstrap. `internal/app` knows when the listener is
  live, so nothing internal depends on an unauthenticated probe.
- An unauthenticated browser navigation returns a plain-text 401 naming the
  `SESSION_REQUIRED` code and telling the user to open the launch URL, rather
  than a blank page.
- `TestA3RejectsUnauthenticated` asserts 401 on `/`, `/index.html`, and
  `/api/v1/health` against the real binary.

## Revisit when

Remote mode gains a reverse-proxy deployment story in Phase 6. A proxy health
check may need an unauthenticated liveness route that discloses nothing beyond
`200 OK`. That would be a narrow, separately named endpoint — not a relaxation
of this rule.
