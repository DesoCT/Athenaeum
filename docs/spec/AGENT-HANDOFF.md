# Athenaeum Agent Handoff

Use this prompt when assigning work to an implementation agent.

---

You are implementing Athenaeum v0.1 from the attached specification pack.

Before coding, read:

1. `00-PRODUCT-CONSTITUTION.md`
2. `01-PRODUCT-REQUIREMENTS-V0.1.md`
3. `02-SYSTEM-ARCHITECTURE.md`
4. `03-DATA-FILESYSTEM-SECURITY.md`
5. `04-UX-INTERACTION-SPEC.md`
6. `06-ACCEPTANCE-TESTS.md`
7. `07-AGENT-OPERATING-RULES.md`
8. all existing ADRs

Your assigned scope is: **[INSERT PHASE OR REQUIREMENT IDS]**.

Requirements:

- Produce a short implementation plan mapped to requirement and acceptance-test IDs.
- Do not reinterpret locked decisions.
- Do not implement excluded capabilities.
- Prefer a complete vertical slice over scaffolding.
- Add regression, integration, and UI tests appropriate to the change.
- Use temporary fixture workspaces for all write tests.
- Preserve path security, atomic writes, and version checks.
- Add an ADR proposal instead of silently deviating.

At completion report:

- requirement IDs completed;
- files changed;
- architecture decisions made;
- commands and tests run;
- test output summary;
- known limitations;
- screenshots for UI work;
- confirmation that no chat, AI, MCP, semantic search, Git mutation, plugin framework, or collaboration system was introduced.

Do not mark the work complete while any assigned MUST acceptance criterion fails.
