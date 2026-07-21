#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
# $1: prefix（command.toml の [[args]] 定義順）
prefix="${1:-$HOME/.local}"
if [ ! -x bin/exq ]; then
  echo "bin/exq がありません。先に build を実行してください" >&2
  exit 1
fi
install -d "$prefix/bin"
install -m 0755 bin/exq "$prefix/bin/exq"
echo "Installed exq to $prefix/bin/exq"
