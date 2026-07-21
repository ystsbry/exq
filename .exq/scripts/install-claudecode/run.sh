#!/usr/bin/env bash
set -euo pipefail
src="$(git rev-parse --show-toplevel)/plugin"
skills_dir="${CLAUDE_SKILLS_DIR:-$HOME/.claude/skills}"
target="$skills_dir/exq"
mkdir -p "$skills_dir"
if [ -L "$target" ]; then
  rm -f "$target"
elif [ -e "$target" ]; then
  echo "skip: $target already exists (not a symlink)"
  exit 0
fi
ln -s "$src" "$target"
echo "Linked $target -> $src"
echo "Restart Claude Code, then verify with: claude plugin list"
