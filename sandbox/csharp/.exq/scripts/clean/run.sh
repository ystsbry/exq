#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/../../lib/require-dotnet.sh"
cd "$(sandbox_root)"
require_dotnet

dotnet clean HelloExq.sln
# dotnet clean は obj/ を残すので、生成物を完全に消したいとき用に削除する。
find src tests -type d \( -name bin -o -name obj \) -prune -exec rm -rf {} +
echo "Removed bin/ and obj/"
