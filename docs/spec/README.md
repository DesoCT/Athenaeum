# Athenaeum v0.1 Specification Pack

**Status:** Approved build contract  
**Version:** 0.1.0  
**Date:** 2026-07-18  
**Product:** Athenaeum  
**Primary workspace:** Map Room

This pack is the normative handoff for implementation agents. An agent must not reinterpret a locked decision, add an out-of-scope capability, or weaken an acceptance criterion without an approved amendment.

## Reading order

1. `00-PRODUCT-CONSTITUTION.md`
2. `01-PRODUCT-REQUIREMENTS-V0.1.md`
3. `02-SYSTEM-ARCHITECTURE.md`
4. `03-DATA-FILESYSTEM-SECURITY.md`
5. `04-UX-INTERACTION-SPEC.md`
6. `05-CONFIGURATION-SCHEMA.md`
7. `06-ACCEPTANCE-TESTS.md`
8. `07-AGENT-OPERATING-RULES.md`
9. `08-DELIVERY-PLAN.md`
10. `09-DECISION-REGISTER.md`
11. `AGENT-HANDOFF.md`

## Normative language

The terms **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are requirements keywords.

- **MUST / MUST NOT:** release-blocking.
- **SHOULD / SHOULD NOT:** expected unless an ADR records a justified exception.
- **MAY:** optional and non-blocking.

## Scope summary

Athenaeum v0.1 is a local-first command centre for configured Markdown workspaces. It provides document navigation, rich rendering, source editing, search, annotations, notes, explicit relationships, external-change safety, and read-only Git context.

Athenaeum v0.1 is **not** a chat product, memory system, semantic knowledge graph, collaboration service, cloud service, WYSIWYG editor, or Git client.

## External memory integration

A future external knowledge or memory integration is intentionally deferred. Previous assumptions about Flashbulb or MCP are non-normative. The owner will provide new system documentation before that integration is designed or built.
