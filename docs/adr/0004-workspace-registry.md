# ADR-0004: A workspace registry, without multi-root

- **Status:** Accepted — approved by the owner 2026-07-20
- **Date:** 2026-07-20
- **Affects:** R1, R2, spec 05 section 1, D-006
- **Raised by:** owner request to keep several documentation repositories in one
  place and move between them

## The request

Register several folders — typically the `docs/` of separate Git repositories —
in one place, and move between them without restarting.

## Why this needs a decision

Multi-root is the most firmly closed door in the pack:

- **D-006** — "One primary root with globs in v0.1; multi-root deferred" (Locked)
- **R1** — "v0.1 MUST NOT implement unrelated multi-root workspaces"
- **Spec 01 section 6** lists "multi-root workspaces" under explicit exclusions
- **Spec 09** names multi-root as future work *requiring a new decision*

Building the obvious version of this request would cross that line four times
over, so the shape matters more than the feature.

## Decision

Athenaeum gains a **workspace registry**: a launcher, not a mount table.

The owner's analogy is apt and worth recording: it is a kubeconfig with
contexts. A file lists named targets, exactly one is active, a picker switches
between them, and an explicit argument overrides the lot the way
`kubectl --context` does. `ATHENAEUM_CONFIG` already plays the part of
`KUBECONFIG`.

The analogy also marks the one deliberate difference. `kubectl config
use-context` writes back to persist `current-context`; this registry never
does. See "Why the registry is read-only" below.

1. `<user-config>/athenaeum/workspaces.toml` lists workspaces by name and path.
2. Athenaeum **only reads** it. The user edits it; the application never
   rewrites it.
3. Opening a workspace from the registry is exactly equivalent to launching
   with its path. **One root, one write boundary, one index, one session.**
4. Leaving a workspace returns to the picker. Nothing from a previous workspace
   remains loaded.
5. Search, the file tree, quick open, and relationships **never span
   workspaces**. Each sees only the active root.

## Why this is not multi-root

D-006 and R1 are about what a *running workspace* contains. Under this design a
session still has exactly one root, and every rule that follows from that is
untouched: the write boundary is the active workspace's, document IDs stay
relative to a single canonical root, and the index projects one corpus.

The registry is closer to a shell's list of directories than to a mount table.
It changes which workspace you open, not what a workspace *is*.

The test that keeps it honest: **if a feature would make two roots visible at
the same moment, it is multi-root and belongs behind an amendment to D-006.**
Cross-workspace search is the obvious example, and is deliberately excluded.

## Why the registry is read-only to the application

The owner chose a hand-edited file, and that choice avoids a real problem.

Spec 03 section 1 defines five data classes and their authorities. A registry
the application writes to fits none of them: it is not workspace content, not a
sidecar, not a disposable projection, and not session state. Adding an
application-writable file outside every workspace would create a sixth
authority class, which constitution C3 requires to be explicit and which no
requirement currently sanctions.

Reading from `<user-config>/athenaeum/` is already sanctioned — spec 05
section 1 puts the per-workspace user override there — so this adds no new
authority at all.

The cost is that adding a workspace means editing a file. That is consistent
with a product whose stated preference is "visible state and inspectable files
over opaque automation" (C8).

## Consequences

- A new `internal/registry` package: load, validate, resolve `~`, report a
  missing or unreadable entry without failing the others.
- Resolution order for `athenaeum open`, chosen so that **no command that works
  today behaves differently**:

  1. an explicit path — open it, unchanged;
  2. no path but `./athenaeum.toml` exists — open it, unchanged;
  3. no path and no local config — show the picker, where today this is simply
     an error.

  `--pick` forces the picker regardless of the working directory, which is how
  one drills out from a shell already inside a workspace. `athenaeum
  workspaces` lists the registry without opening a browser.

  An earlier draft of this ADR had the picker take precedence over a local
  `athenaeum.toml`. That would have quietly broken the common flow of running
  `athenaeum open` from inside a repository, so it was rejected: a convenience
  feature must not change the meaning of an existing command.
- The Map Room gains a way back to the picker, and the command bar names the
  active workspace.
- Registry entries are validated on load: a path that does not exist, or holds
  no `athenaeum.toml`, is shown as unavailable with the reason rather than
  silently omitted.

## Rejected alternatives

**True multi-root.** What was actually asked for, and it would need D-006
amended. It also forces answers to questions v0.1 has deliberately not
answered: per-root write boundaries, document ID collisions between roots, what
a relative link across roots means, and index partitioning. Worth doing one day,
on purpose, not as a side effect of a convenience feature.

**Scanning directories for `athenaeum.toml`.** Less to maintain, but it reads
broadly across the filesystem and the list changes without the user editing
anything — both at odds with C8.

**Letting the application append to the registry.** Convenient, and rejected
for the authority reason above.

## Revisit when

Someone genuinely needs to search across repositories. That is the point where
multi-root earns its complexity, and it should arrive as an amendment to D-006
with the collision and boundary questions answered — not by widening this
registry until it becomes a mount table by accident.
