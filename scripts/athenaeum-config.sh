#!/bin/sh
# Generates an athenaeum.toml for a directory by inspecting what is in it.
#
#   athenaeum-config <dir>                 # print a config for <dir>
#   athenaeum-config --write <dir>         # write <dir>/athenaeum.toml
#   athenaeum-config --write --registry ~/dev/*/   # a workspace per repo
#
# It scopes the config to Markdown, excludes the usual build and dependency
# noise, and turns each immediate sub-directory that contains Markdown into a
# document group so the Map Room is organised out of the box. The result is
# meant to be a good starting point you then edit, not a locked artifact.
#
# Options:
#   -w, --write            Write athenaeum.toml into each <dir> instead of stdout.
#   --force                Overwrite an existing athenaeum.toml.
#   --registry [FILE]      Also register each <dir> as a workspace (implies
#                          --write). FILE defaults to the per-OS registry path.
#   --name NAME            Workspace name (single directory only; default: the
#                          directory's name, humanised).
#   -h, --help             This help.
set -eu

me=$(basename "$0")

usage() {
  # Print the leading comment block (after the shebang), stripping "# ".
  awk 'NR==1 {next} /^#/ {sub(/^# ?/, ""); print; next} {exit}' "$0"
  exit "${1:-0}"
}

# --- argument parsing --------------------------------------------------------

write=0
force=0
registry=0
registry_file=""
name_override=""
set -- "$@"
dirs=""

while [ $# -gt 0 ]; do
  case "$1" in
    -w | --write) write=1 ;;
    --force) force=1 ;;
    --registry)
      registry=1
      write=1
      # An optional argument: consume the next token only if it is not a flag
      # and not an existing directory (a directory is a positional target).
      if [ $# -gt 1 ] && [ "${2#-}" = "$2" ] && [ ! -d "$2" ]; then
        registry_file="$2"
        shift
      fi
      ;;
    --name)
      [ $# -gt 1 ] || { echo "$me: --name needs a value" >&2; exit 2; }
      name_override="$2"
      shift
      ;;
    -h | --help) usage 0 ;;
    --) shift; break ;;
    -*) echo "$me: unknown option '$1'" >&2; usage 2 ;;
    *) dirs="$dirs
$1" ;;
  esac
  shift
done
# Any tokens after -- are directories too.
for d in "$@"; do dirs="$dirs
$d"; done

# Trim the leading newline and count the targets.
dirs=$(printf '%s' "$dirs" | sed '/^$/d')
[ -n "$dirs" ] || { echo "$me: no directory given" >&2; usage 2; }
count=$(printf '%s\n' "$dirs" | wc -l | tr -d ' ')

if [ "$count" -gt 1 ] && [ "$write" -eq 0 ]; then
  echo "$me: several directories given; add --write (each config goes into its directory)" >&2
  exit 2
fi
if [ -n "$name_override" ] && [ "$count" -gt 1 ]; then
  echo "$me: --name applies to a single directory only" >&2
  exit 2
fi

# --- helpers -----------------------------------------------------------------

# Directory and file names that are never documents worth including or grouping.
is_noise() {
  case "$1" in
    node_modules | vendor | target | dist | build | out | bin | obj | \
    .git | .github | .svn | .hg | .next | .svelte-kit | .nuxt | .cache | \
    .venv | venv | env | __pycache__ | .tox | .mypy_cache | .pytest_cache | \
    coverage | .idea | .vscode | .DS_Store | tmp | temp) return 0 ;;
    .*) return 0 ;; # any other dot-directory
    *) return 1 ;;
  esac
}

# Escape a value for a TOML basic string.
toml_escape() {
  printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
}

# Lower-case, dash-separated slug of a name, for a group id.
slugify() {
  printf '%s' "$1" | tr '[:upper:]' '[:lower:]' \
    | sed 's/[^a-z0-9]\{1,\}/-/g; s/^-//; s/-$//'
}

# Human title from a directory name: dashes and underscores become spaces and
# each word is capitalised.
humanize() {
  printf '%s' "$1" | sed 's/[-_]\{1,\}/ /g' \
    | awk '{for(i=1;i<=NF;i++)$i=toupper(substr($i,1,1)) substr($i,2)}1'
}

# Does the tree under $1 contain a file matching the extension glob $2 (default
# any Markdown), skipping the heaviest noise directories?
has_ext() {
  find "$1" \( -type d \( -name node_modules -o -name vendor -o -name .git \
      -o -name target -o -name dist -o -name build \) -prune \) -o \
      -type f -name "$2" -print 2>/dev/null | head -n1 | grep -q .
}

has_markdown() {
  has_ext "$1" '*.md' || has_ext "$1" '*.markdown'
}

# Detect which Markdown extensions the tree actually uses, so the generated
# patterns match real files and validate without a "matches nothing" warning.
# Sets EXTS to a space-separated list like "md" or "md markdown".
detect_exts() {
  EXTS=""
  has_ext "$1" '*.md' && EXTS="md"
  has_ext "$1" '*.markdown' && EXTS="${EXTS:+$EXTS }markdown"
  # With no Markdown at all, still emit .md so the file is well-formed; the
  # caller has already warned that it will match nothing.
  [ -n "$EXTS" ] || EXTS="md"
}

# Print an indented, comma-terminated quoted glob per detected extension.
# $1 = leading indent, $2 = path prefix (e.g. "**" or "docs/**").
glob_lines() {
  for ext in $EXTS; do
    printf '%s"%s/*.%s",\n' "$1" "$(toml_escape "$2")" "$ext"
  done
}

# Print the detected globs as an inline array body: "a/**/*.md", "a/**/*.markdown"
glob_inline() {
  sep=""
  for ext in $EXTS; do
    printf '%s"%s/*.%s"' "$sep" "$(toml_escape "$1")" "$ext"
    sep=", "
  done
}

# The default registry file, following os.UserConfigDir semantics.
default_registry() {
  case "$(uname -s)" in
    Darwin) printf '%s/Library/Application Support/athenaeum/workspaces.toml' "$HOME" ;;
    *) printf '%s/athenaeum/workspaces.toml' "${XDG_CONFIG_HOME:-$HOME/.config}" ;;
  esac
}

# --- config generation -------------------------------------------------------

# Emit an athenaeum.toml for directory $1 to stdout.
emit_config() {
  target=$1
  base=$(basename "$(cd "$target" && pwd)")
  if [ -n "$name_override" ]; then
    ws_name=$name_override
  else
    ws_name=$(humanize "$base")
  fi

  if ! has_markdown "$target"; then
    echo "$me: warning: no Markdown found under $target; the include patterns will match nothing" >&2
  fi
  detect_exts "$target"
  git_note="# Set git.enabled to false if this directory is not a Git repository."
  [ -d "$target/.git" ] && git_note="# This directory is a Git repository, so the Git panel and filter work here."

  cat <<EOF
# Generated by $me for $(cd "$target" && pwd)
# Edit freely: this is a starting point, not a locked file.
# Validate with: athenaeum validate $target

schema_version = 1
name = "$(toml_escape "$ws_name")"
root = "."

include = [
$(glob_lines "  " "**")
]

exclude = [
  "**/node_modules/**",
  "**/vendor/**",
  "**/target/**",
  "**/dist/**",
  "**/build/**",
  "**/.venv/**",
  "**/__pycache__/**",
  "**/.next/**",
  "**/.svelte-kit/**",
  ".git/**",
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
$git_note
enabled = true

[security]
writable = [
$(glob_lines "  " "**")
  "assets/**",
  ".athenaeum/shared/**",
]
allow_external_reads = false
EOF

  # One group per immediate sub-directory that holds Markdown, with unique ids.
  seen_ids=""
  for sub in "$target"/*/; do
    [ -d "$sub" ] || continue
    sub_name=$(basename "$sub")
    is_noise "$sub_name" && continue
    has_markdown "$sub" || continue

    id=$(slugify "$sub_name")
    [ -n "$id" ] || id="group"
    # Disambiguate a collision by appending a counter.
    unique=$id; n=2
    while printf '%s\n' "$seen_ids" | grep -qx "$unique"; do
      unique="${id}-${n}"; n=$((n + 1))
    done
    seen_ids="$seen_ids
$unique"

    # Per-subdirectory extensions, so a group's patterns match its own files and
    # never warn (the workspace-wide include/writable were emitted above).
    detect_exts "$sub"
    printf '\n[[groups]]\nid = "%s"\ntitle = "%s"\npatterns = [%s]\n' \
      "$(toml_escape "$unique")" "$(toml_escape "$(humanize "$sub_name")")" \
      "$(glob_inline "$sub_name/**")"
  done

  cat <<'EOF'

[relationships.front_matter]
fields = ["related", "implements", "supersedes"]
EOF
}

# --- registry ----------------------------------------------------------------

# Append a workspace entry to the registry file unless its path is already there.
register() {
  path=$1 name=$2 file=$3
  mkdir -p "$(dirname "$file")"
  [ -f "$file" ] || : > "$file"
  abs=$(cd "$path" && pwd)
  if grep -qF "path = \"$abs\"" "$file" 2>/dev/null; then
    echo "$me: $abs is already registered" >&2
    return
  fi
  {
    printf '\n[[workspace]]\nname = "%s"\npath = "%s"\n' \
      "$(toml_escape "$name")" "$(toml_escape "$abs")"
  } >> "$file"
  echo "$me: registered '$name' -> $abs in $file"
}

# --- main --------------------------------------------------------------------

[ "$registry" -eq 1 ] && [ -z "$registry_file" ] && registry_file=$(default_registry)

printf '%s\n' "$dirs" | while IFS= read -r dir; do
  [ -n "$dir" ] || continue
  if [ ! -d "$dir" ]; then
    echo "$me: not a directory: $dir" >&2
    exit 1
  fi

  if [ "$write" -eq 1 ]; then
    out="$dir/athenaeum.toml"
    if [ -e "$out" ] && [ "$force" -eq 0 ]; then
      echo "$me: $out exists; pass --force to overwrite" >&2
      exit 1
    fi
    emit_config "$dir" > "$out"
    echo "$me: wrote $out"
  else
    emit_config "$dir"
  fi

  if [ "$registry" -eq 1 ]; then
    base=$(basename "$(cd "$dir" && pwd)")
    reg_name=${name_override:-$(humanize "$base")}
    register "$dir" "$reg_name" "$registry_file"
  fi
done
