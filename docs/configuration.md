# Configuring a workspace

A workspace is a folder of Markdown plus one `athenaeum.toml` beside it. That
file is the whole configuration: which documents Athenaeum can see, which it may
write to, and which rendering features are switched on.

Your files stay exactly where they are and stay authoritative. Athenaeum never
moves them into a database of its own.

## The smallest useful file

Drop this next to your notes and you are done:

```toml
schema_version = 1
name = "My Notes"
root = "."

include = ["**/*.md"]
```

Then:

```bash
athenaeum open athenaeum.toml
```

Everything else has a sensible default. Check a file before launching with:

```bash
athenaeum validate athenaeum.toml
```

That reports every problem at once, naming the field and what to do about it,
and exits non-zero if anything is wrong.

## Choosing which documents appear

```toml
include = [
  "README.md",
  "docs/**/*.md",
  "notes/**/*.md",
]

exclude = [
  "**/node_modules/**",
  "**/vendor/**",
  "archive/**",
]
```

`**` matches across directories, `*` does not. A file must match an `include`
pattern to be visible at all, and anything matching `exclude` is dropped even if
an include would have caught it.

A document that is excluded is invisible to the whole application — the tree,
quick open, and search alike. It cannot be reached by typing a path either.

If a pattern matches nothing, `validate` warns rather than fails: an empty
`reports/**` is usually a typo, but occasionally it is a directory you have not
created yet.

## Which files Athenaeum may change

This is the setting most worth understanding, because it decides what a save is
allowed to touch.

```toml
[security]
writable = [
  "notes/**/*.md",
  "assets/**",
]
```

**If you omit `writable` entirely, every included document is editable.** That is
the sensible default for your own notes.

When you do set it, it becomes the complete list. Anything outside opens
read-only, the Save control disappears, and the status reads
`Read-only (not writable)`. That is deliberate rather than a failure — it is how
you protect files you want to read but never edit by accident:

```toml
include = ["**/*.md"]

[security]
# Reference material stays read-only; only the journal can be edited.
writable = ["journal/**/*.md", "assets/**"]
```

A pattern here can never point outside the workspace root. `validate` rejects
that rather than silently ignoring it.

## Images and pasted files

```toml
[assets]
directory = "assets"        # where pasted and dropped images are stored
allow_remote = true         # load images from the internet
paste_naming = "date-hash"  # or "original" to keep the original file name
```

Paste or drag an image anywhere on a document and it is written into
`directory` with a relative Markdown link inserted for you. If the name is
already taken you are asked what to do — Athenaeum never overwrites silently.

Remember to make the asset directory writable, or the paste is refused:

```toml
[security]
writable = ["notes/**/*.md", "assets/**"]
```

`allow_remote = false` blocks images hosted elsewhere. Remote images are always
marked in the preview and are fetched without cookies or referrer.

## Rendering features

```toml
[documents]
raw_html = false            # leave off unless you trust every author
wiki_links = true           # [[other-note]]
footnotes = true
callouts = true             # > [!NOTE]
math = true                 # $inline$ and $$display$$
mermaid = true              # ```mermaid diagrams
front_matter = ["yaml", "toml"]

max_editable_bytes = 10485760       # 10 MB; larger files open read-only
large_file_warning_bytes = 2097152  # 2 MB; warn above this
```

Rendered Markdown is always sanitised. `raw_html = true` permits inert markup
such as `<b>`; scripts, event handlers, and unsafe URLs are removed regardless.

## Grouping documents

Groups appear as sections on the Map Room home and as a search filter.

```toml
[[groups]]
id = "reference"
title = "Reference"
patterns = ["docs/reference/**/*.md"]

[[groups]]
id = "journal"
title = "Journal"
patterns = ["journal/**/*.md"]
```

## Search and Git

```toml
[search]
enabled = true
index_code_blocks = true
index_front_matter = true

[git]
enabled = true
```

The search index lives in your cache directory, never in the workspace. Deleting
it loses nothing — it rebuilds from your files.

`git.enabled` adds a Git-state filter to search when the workspace is inside a
repository. Athenaeum only ever reads: it cannot commit, push, or change
anything in your repository.

## A complete example

```toml
schema_version = 1
name = "Field Notes"
root = "."

include = ["README.md", "notes/**/*.md", "reference/**/*.md"]
exclude = ["**/node_modules/**", ".git/**"]

[documents]
raw_html = false
wiki_links = true
callouts = true
math = true
mermaid = true

[assets]
directory = "assets"
allow_remote = true

[search]
enabled = true

[git]
enabled = true

[security]
# Reference material is read-only on purpose.
writable = ["notes/**/*.md", "assets/**"]

[[groups]]
id = "reference"
title = "Reference"
patterns = ["reference/**/*.md"]
```

## When something is not working

| What you see | What it means |
|---|---|
| A document is missing | No `include` pattern matches it, or an `exclude` does |
| `Read-only (not writable)` | It is outside `security.writable` |
| `Read-only` | The file is not UTF-8, or is above `max_editable_bytes` |
| A pasted image is refused | The asset directory is not in `security.writable` |
| A remote image does not load | `assets.allow_remote` is false, or `--safe-mode` is on |
| Everything is read-only | You set `writable` and it does not match your documents |

`athenaeum validate athenaeum.toml` diagnoses most of these before you launch.

## Running with everything risky switched off

```bash
athenaeum serve athenaeum.toml --safe-mode
```

Disables Git, remote assets, raw HTML, and Mermaid. Documents stay readable and
editable under the normal write rules.

---

The normative specification for every field, including the ones this guide
leaves out, is in [docs/spec/05-CONFIGURATION-SCHEMA.md](spec/05-CONFIGURATION-SCHEMA.md).
