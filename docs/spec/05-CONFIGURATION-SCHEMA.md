# Athenaeum v0.1 Configuration Specification

## 1. Configuration layers

Effective configuration is produced from:

1. project file: `<workspace>/athenaeum.toml`;
2. optional user override: `<user-config>/athenaeum/workspaces/<workspace-key>.toml`;
3. approved environment variables and CLI flags.

Precedence:

```text
CLI > environment > user override > project config > defaults
```

Only fields marked **user-overridable** may be changed by the user override. Security and workspace membership fields remain project-owned unless changed by CLI at launch.

## 2. Project configuration

```toml
schema_version = 1
name = "Flashbulb"
root = "."

include = [
  "README.md",
  "docs/**/*.md",
  "reports/**/*.md"
]

exclude = [
  "**/node_modules/**",
  "**/vendor/**",
  "**/target/**",
  ".git/**"
]

[documents]
raw_html = false
wiki_links = true
footnotes = true
callouts = true
math = true
mermaid = true
front_matter = ["yaml", "toml"]
max_editable_bytes = 10485760
large_file_warning_bytes = 2097152

[assets]
directory = "assets"
allow_remote = true
paste_naming = "date-hash"

[annotations]
shared_directory = ".athenaeum/shared"
default_visibility = "personal"

[search]
enabled = true
index_code_blocks = true
index_front_matter = true

[git]
enabled = true

[security]
writable = [
  "README.md",
  "docs/**/*.md",
  "reports/**/*.md",
  "assets/**",
  ".athenaeum/shared/**"
]
allow_external_reads = false

[[groups]]
id = "design"
title = "Design"
patterns = ["docs/design/**/*.md"]

[[groups]]
id = "operations"
title = "Operations"
patterns = ["docs/operations/**/*.md", "runbooks/**/*.md"]

[relationships.front_matter]
fields = ["related", "implements", "supersedes"]
```

## 3. User override

Permitted fields:

```toml
[ui]
theme = "system" # system | light | dark
font_size = 15
editor_wrap = true
restore_session = true

[editing]
autosave = false
autosave_delay_ms = 1500

[annotations]
default_visibility = "personal"

[remote_assets]
load_automatically = true
```

A user override MUST NOT expand:

- included files;
- writable paths;
- external-read authority;
- remote bind settings;
- raw HTML permission.

## 4. CLI

Required commands:

```text
athenaeum open [path-to-athenaeum.toml]
athenaeum serve [path-to-athenaeum.toml]
athenaeum validate [path-to-athenaeum.toml]
athenaeum version
```

Expected flags:

```text
--no-open
--bind <address>
--port <number>
--remote
--auth-token-file <path>
--log-level <level>
--safe-mode
```

Rules:

- `open` opens the browser unless `--no-open`.
- `serve` never opens the browser.
- `--remote` requires non-loopback bind plus token file.
- `--safe-mode` disables Git, remote assets, raw HTML, Mermaid, and user overrides; documents remain readable and editable within normal write rules.

## 5. Environment variables

Keep the environment surface intentionally small:

```text
ATHENAEUM_CONFIG
ATHENAEUM_BIND
ATHENAEUM_PORT
ATHENAEUM_AUTH_TOKEN_FILE
ATHENAEUM_LOG_LEVEL
ATHENAEUM_NO_OPEN
```

Environment variables MUST NOT define include globs or write permissions.

## 6. Validation rules

- `schema_version` is required.
- Unknown top-level fields are errors in v0.1.
- Unknown fields inside extension-capable maps may be warnings only when explicitly documented.
- Duplicate group IDs are errors.
- Include patterns that match no files are warnings.
- Writable patterns outside the root are errors.
- The shared annotation directory must resolve inside the workspace.
- The asset directory must resolve inside the workspace.
- Raw HTML plus remote mode produces a high-severity warning and SHOULD be rejected unless an explicit unsafe override is added in a future version.

## 7. Schema artifact

A machine-readable JSON Schema is provided at `schemas/athenaeum.schema.json`. The Go validator remains authoritative when platform path rules cannot be represented in JSON Schema.
