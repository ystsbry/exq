#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
# cgo を使わないので、QEMU 環境での gcc 絡みのトラブルを避けるため既定で無効化。
# 必要なら CGO_ENABLED=1 で上書き可能。
export CGO_ENABLED="${CGO_ENABLED:-0}"
mkdir -p bin
go build -o bin/exq ./cmd/exq
echo "Built bin/exq"
