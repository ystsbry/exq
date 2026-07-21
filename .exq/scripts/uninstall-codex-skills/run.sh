#!/usr/bin/env bash
set -euo pipefail
src="$(git rev-parse --show-toplevel)/plugin/skills"
dest="${AGENTS_HOME:-$HOME/.agents}/skills"
for d in "$src"/*/; do
  name="$(basename "$d")"
  target="$dest/$name"
  if [ -L "$target" ]; then
    rm -f "$target"
    echo "Removed $target"
  fi
done
