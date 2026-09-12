#!/usr/bin/env bash
# sandbox/csharp のセットアップ。何度実行しても同じ結果になる（冪等）。
#
# .exq/ はこのリポジトリにコミット済みなので exq init は不要。
# ここでやるのは .NET SDK の確認と依存関係の復元だけ。
set -euo pipefail

source "$(dirname "${BASH_SOURCE[0]}")/.exq/lib/require-dotnet.sh"
cd "$(sandbox_root)"
require_dotnet

echo "==> .NET SDK $(dotnet --version)"

# dotnet restore は復元済みなら何もしないので、そのまま繰り返し実行できる。
echo "==> dotnet restore"
dotnet restore HelloExq.sln

cat <<'EOF'

セットアップ完了。

  exq list                                  # 利用できるコマンドを確認
  exq                                       # TUI を開く
  exq run build                             # ビルド
  exq run test                              # テスト
  exq run test-filter -- "FullyQualifiedName~WithoutName"   # 引数付きの例
  exq run run-app -- exq                    # アプリ実行

exq が未インストールでも、各コマンドは直接実行できる:

  bash .exq/scripts/test/run.sh
EOF
