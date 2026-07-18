# Athenaeum Agent Operating Rules

These rules govern every implementation agent working from this pack.

## 1. Read before coding

An agent MUST read, in order:

1. Product Constitution;
2. Product Requirements;
3. Architecture;
4. Data/Security Rules;
5. relevant UX and acceptance sections;
6. existing ADRs;
7. repository contribution instructions.

The agent MUST state which requirement IDs it is implementing in its plan or commit message.

## 2. Scope discipline

An agent MUST:

- implement only assigned requirements;
- preserve all exclusions;
- avoid speculative abstractions;
- avoid unrelated refactors;
- stop and write an ADR proposal when a locked decision blocks correct implementation.

An agent MUST NOT:

- add chat, AI, MCP, semantic search, or external knowledge code;
- add Git write operations;
- introduce multi-root support;
- replace sidecar formats with a database;
- make the search database authoritative;
- silently widen path permissions;
- add a plugin framework.

## 3. Vertical-slice rule

Prefer complete vertical slices over disconnected scaffolding. A slice includes:

- backend domain logic;
- API contract;
- frontend interaction;
- tests;
- error states;
- documentation.

Dead stubs and placeholder screens are not considered progress unless the delivery plan explicitly calls for them.

## 4. Test discipline

For each MUST requirement, the agent MUST add or update tests at the lowest useful level:

- unit tests for pure rules;
- integration tests for filesystem, SQLite, Git, and HTTP boundaries;
- browser/end-to-end tests for user workflows;
- platform-specific tests where path semantics differ.

A bug fix MUST include a failing regression test before or alongside the fix.

## 5. Data safety

Test code MUST use temporary workspaces and fixture repositories. It MUST NOT operate on the developer's actual repository or home documents.

Write-path code MUST be reviewed for:

- traversal;
- symlink escape;
- stale versions;
- interrupted writes;
- permission errors;
- external changes;
- recovery cleanup.

## 6. API discipline

- API errors use stable codes.
- API paths accept document IDs, not arbitrary absolute paths.
- Mutation requests include an expected revision/version.
- New endpoints require typed frontend clients and contract tests.
- Breaking API changes require an ADR before v0.1 freeze.

## 7. Dependency discipline

Before adding a dependency, the agent MUST record:

- reason;
- alternatives considered;
- licence;
- runtime/build impact;
- whether it introduces CGO, a subprocess, network activity, or native packaging complexity.

No dependency may introduce telemetry or network calls by default.

## 8. Commit discipline

Each commit SHOULD:

- address one coherent slice;
- reference requirement IDs;
- keep generated files separate where practical;
- leave tests green;
- avoid drive-by formatting of unrelated files.

Suggested commit format:

```text
feat(search): add incremental FTS indexing [R7, F1-F3]
```

## 9. Completion report

An agent's handoff MUST include:

- implemented requirement IDs;
- files changed;
- commands run;
- test results;
- known limitations;
- deviations or ADRs;
- screenshots for visible UI changes;
- explicit confirmation that excluded systems were not introduced.

## 10. Definition of done

A feature is done only when:

- behaviour is implemented end to end;
- happy path and failure states are tested;
- keyboard and accessibility behaviour is covered;
- error messages are actionable;
- security boundaries are preserved;
- relevant docs and config examples are updated;
- no acceptance criterion regresses.
