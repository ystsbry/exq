#!/usr/bin/env bash
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/../../lib/require-dotnet.sh"
cd "$(sandbox_root)"
require_dotnet

# .editorconfig に従ってソースを書き換える。
dotnet format HelloExq.sln
