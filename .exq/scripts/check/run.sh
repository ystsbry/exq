#!/usr/bin/env bash
# gofmt / go vet / go test をまとめて実行し、どれか失敗したら非0で終了する。
set -euo pipefail

root="$(git rev-parse --show-toplevel)"
cd "$root"

status=0

echo "== gofmt =="
unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
  echo "gofmt が必要なファイル:"
  echo "$unformatted"
  status=1
else
  echo "OK"
fi

echo "== go vet =="
if go vet ./...; then
  echo "OK"
else
  status=1
fi

echo "== go test =="
if go test ./...; then
  echo "OK"
else
  status=1
fi

if [ "$status" -ne 0 ]; then
  echo
  echo "check: NG（上記の失敗を確認してください）"
fi
exit "$status"
