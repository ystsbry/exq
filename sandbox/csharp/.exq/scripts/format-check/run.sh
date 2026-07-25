#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/../../lib/require-dotnet.sh"
cd "$(sandbox_root)"
require_dotnet

# 書き換えずに検査だけ行う（CI 的な使い方の例）。
dotnet format HelloExq.sln --verify-no-changes
