#!/usr/bin/env bash
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
# 書き換えはせず検査のみ。差分があるファイルを列挙して非0終了する。
out="$(gofmt -l .)"
if [ -n "$out" ]; then
  echo "gofmt が必要なファイル:"
  echo "$out"
  exit 1
fi
echo "gofmt OK"
