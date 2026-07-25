#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/../../lib/require-dotnet.sh"
cd "$(sandbox_root)"
require_dotnet

# $1: filter（command.toml の [[args]] 定義順）。dotnet test --filter にそのまま渡す。
# 例: FullyQualifiedName~WithoutName / FullyQualifiedName~GreeterTests
filter="${1:-}"
if [ -z "$filter" ]; then
  echo "filter が空のため全テストを実行します" >&2
  dotnet test HelloExq.sln
else
  dotnet test HelloExq.sln --filter "$filter"
fi
