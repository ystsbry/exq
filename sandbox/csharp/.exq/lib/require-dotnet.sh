#!/usr/bin/env bash
# 各コマンドの run.sh から source して使う共通ヘルパー。
# .exq/scripts/ でも .exq/workflows/ でもないので exq のコマンド探索には現れない。

# この sandbox が前提とする .NET SDK のメジャーバージョン (LTS)。
REQUIRED_DOTNET_MAJOR=8

# sandbox/csharp の絶対パスを返す。exq 経由でも
# `bash .exq/scripts/<name>/run.sh` の直接実行でも同じ結果になる。
sandbox_root() {
  cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd
}

# dotnet CLI の有無と SDK バージョンを検査する。満たさなければ案内を出して非0終了。
require_dotnet() {
  if ! command -v dotnet >/dev/null 2>&1; then
    cat >&2 <<EOF
error: dotnet CLI が見つかりません。

この sandbox は .NET SDK ${REQUIRED_DOTNET_MAJOR}.0 (LTS) 以上を前提としています。
SDK のインストールは exq のスコープ外なので、手動で導入してください:

  https://dotnet.microsoft.com/download/dotnet/${REQUIRED_DOTNET_MAJOR}.0

  # 例: 公式スクリプトで sudo なしにユーザーローカルへ入れる場合
  curl -sSL https://dot.net/v1/dotnet-install.sh | bash -s -- --channel ${REQUIRED_DOTNET_MAJOR}.0
  export DOTNET_ROOT="\$HOME/.dotnet"
  export PATH="\$DOTNET_ROOT:\$DOTNET_ROOT/tools:\$PATH"
EOF
    return 1
  fi

  local version major
  # SDK が無く runtime だけの環境では --version が失敗するので、その場合も弾く。
  if ! version="$(dotnet --version 2>/dev/null)"; then
    echo "error: dotnet はありますが SDK が見つかりません (.NET SDK ${REQUIRED_DOTNET_MAJOR}.0 以上が必要)" >&2
    echo "       https://dotnet.microsoft.com/download/dotnet/${REQUIRED_DOTNET_MAJOR}.0" >&2
    return 1
  fi

  major="${version%%.*}"
  if [ -z "$major" ] || [ "$major" -lt "$REQUIRED_DOTNET_MAJOR" ]; then
    echo "error: .NET SDK ${REQUIRED_DOTNET_MAJOR}.0 以上が必要です (検出: ${version})" >&2
    echo "       https://dotnet.microsoft.com/download/dotnet/${REQUIRED_DOTNET_MAJOR}.0" >&2
    return 1
  fi
}
