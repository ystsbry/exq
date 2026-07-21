#!/usr/bin/env bash
set -euo pipefail
skills_dir="${CLAUDE_SKILLS_DIR:-$HOME/.claude/skills}"
target="$skills_dir/exq"
if [ -L "$target" ]; then
  rm -f "$target"
  echo "Removed $target"
else
  echo "not installed (symlink): $target"
fi
