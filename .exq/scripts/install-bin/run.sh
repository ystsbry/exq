#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
# $1: prefix（command.toml の [[args]] 定義順）
prefix="${1:-$HOME/.local}"
for bin in exq exqd; do
  if [ ! -x "bin/$bin" ]; then
    echo "bin/$bin がありません。先に build を実行してください" >&2
    exit 1
  fi
done
install -d "$prefix/bin"
for bin in exq exqd; do
  install -m 0755 "bin/$bin" "$prefix/bin/$bin"
  echo "Installed $bin to $prefix/bin/$bin"
done
