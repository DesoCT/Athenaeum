#!/usr/bin/env bash
#
# Mechanically enforces the v0.1 exclusions in spec 01 section 6 and acceptance
# test L, so an excluded system cannot enter the release by accident.
#
# The check deliberately inspects declared dependencies and executed commands
# rather than prose: the specification and code comments discuss these excluded
# systems constantly, and a naive text search would flag every mention.

set -euo pipefail

cd "$(dirname "$0")/.."

failures=0

fail() {
  echo "EXCLUSION VIOLATION: $1" >&2
  failures=$((failures + 1))
}

# --- 1. No AI, MCP, or embedding dependencies in the Go module ---------------

forbidden_go_modules=(
  "github.com/sashabaranov/go-openai"
  "github.com/anthropics"
  "modelcontextprotocol"
  "github.com/mark3labs/mcp-go"
  "github.com/philippgille/chromem-go"
  "github.com/pgvector"
  "github.com/milvus-io"
  "github.com/weaviate"
  "github.com/qdrant"
)

for module in "${forbidden_go_modules[@]}"; do
  if grep -qiF "$module" go.mod 2>/dev/null; then
    fail "go.mod requires $module (AI, MCP, or embedding dependency)"
  fi
done

# --- 2. No AI, MCP, chat, or embedding dependencies in the frontend ----------

forbidden_npm_packages=(
  "openai"
  "@anthropic-ai/"
  "@ai-sdk/"
  "\"ai\":"
  "langchain"
  "@modelcontextprotocol/"
  "@xenova/transformers"
  "onnxruntime"
)

for package in "${forbidden_npm_packages[@]}"; do
  if grep -qiE "$package" web/package.json 2>/dev/null; then
    fail "web/package.json depends on $package (AI, MCP, chat, or embedding dependency)"
  fi
done

# --- 3. No Git mutation commands anywhere in the source ---------------------
#
# R12 and D-019 restrict v0.1 to status, diff, log, and blame. This looks for
# the mutating subcommands appearing as quoted arguments, which is how they
# would reach exec.Command.

git_mutations=(
  "add" "commit" "push" "pull" "fetch" "reset" "checkout" "switch"
  "rebase" "merge" "cherry-pick" "revert" "stash" "tag" "branch"
  "clean" "rm" "mv" "apply" "restore"
)

source_dirs=(cmd internal)

for subcommand in "${git_mutations[@]}"; do
  # Matches a quoted subcommand on a line that also names git execution.
  if grep -rnE "\"(git-)?${subcommand}\"" "${source_dirs[@]}" 2>/dev/null \
      | grep -iE "exec\.|Command|gitArgs|runGit" >/dev/null 2>&1; then
    fail "a Git mutation subcommand ('${subcommand}') is passed to git execution"
  fi
done

# --- 4. No shell invocation for Git (spec 02 section 3.10) ------------------

if grep -rnE 'exec\.Command\("(sh|bash|zsh|cmd|powershell)"' "${source_dirs[@]}" 2>/dev/null; then
  fail "a shell is invoked directly; spec 02 section 3.10 forbids it"
fi

# --- 5. No chat surface in the frontend ------------------------------------

if find web/src -iname "*chat*" -print -quit 2>/dev/null | grep -q .; then
  fail "web/src contains a chat component"
fi

# --- Result ----------------------------------------------------------------

if [ "$failures" -gt 0 ]; then
  echo "" >&2
  echo "$failures exclusion violation(s). See spec 01 section 6 and acceptance L." >&2
  exit 1
fi

echo "exclusion check passed: no chat, AI, MCP, embedding, or Git-mutation code"
