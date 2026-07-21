#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
# $1: prefix（command.toml の [[args]] 定義順）
prefix="${1:-$HOME/.local}"
export CGO_ENABLED="${CGO_ENABLED:-0}"
mkdir -p bin
go build -o bin/exq ./cmd/exq
install -d "$prefix/bin"
install -m 0755 bin/exq "$prefix/bin/exq"
echo "Installed exq to $prefix/bin/exq"
