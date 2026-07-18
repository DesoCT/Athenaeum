---
title: Runbook
---

# Runbook

## Launching

```bash
athenaeum open examples/athenaeum.toml
```

Athenaeum binds to loopback, prints a launch URL carrying a one-time bootstrap
token, and opens a browser. `serve` does the same without opening a browser.

## Safe mode

```bash
athenaeum serve examples/athenaeum.toml --safe-mode
```

Safe mode disables Git, remote assets, raw HTML, and Mermaid. Documents remain
readable and editable under the normal write rules.

## Validating configuration

```bash
athenaeum validate examples/athenaeum.toml
```

Exits non-zero with an actionable message when the configuration is wrong.
