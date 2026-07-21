#!/usr/bin/env bash
set -euo pipefail
src="$(git rev-parse --show-toplevel)/plugin/skills"
dest="${AGENTS_HOME:-$HOME/.agents}/skills"
mkdir -p "$dest"
for d in "$src"/*/; do
  name="$(basename "$d")"
  target="$dest/$name"
  if [ -L "$target" ]; then
    rm -f "$target"
  elif [ -e "$target" ]; then
    echo "skip: $target already exists (not a symlink)"
    continue
  fi
  ln -s "${d%/}" "$target"
  echo "Linked $target -> ${d%/}"
done
echo "Restart Codex to pick up the new skills."
