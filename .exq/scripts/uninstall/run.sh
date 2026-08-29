#!/usr/bin/env bash
set -euo pipefail
# $1: prefix（command.toml の [[args]] 定義順）
prefix="${1:-$HOME/.local}"
for bin in exq exqd; do
  rm -f "$prefix/bin/$bin"
  echo "Removed $prefix/bin/$bin"
done
