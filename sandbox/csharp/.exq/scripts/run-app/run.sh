#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/../../lib/require-dotnet.sh"
cd "$(sandbox_root)"
require_dotnet

# $1: name（command.toml の [[args]] 定義順）。空なら引数なしで実行する。
name="${1:-}"
if [ -z "$name" ]; then
  dotnet run --project src/HelloExq
else
  dotnet run --project src/HelloExq -- "$name"
fi
