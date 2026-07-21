#!/usr/bin/env bash
set -euo pipefail
# $1: prefix（command.toml の [[args]] 定義順）
prefix="${1:-$HOME/.local}"
rm -f "$prefix/bin/exq"
echo "Removed $prefix/bin/exq"
