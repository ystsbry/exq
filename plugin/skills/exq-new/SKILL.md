---
name: exq-new
description: ローカル環境専用コマンドを exq フォーマット (.exq/commands/<name>/ に command.toml + run.sh) で作成する。「exq コマンドを作って」「ローカル専用コマンドを追加して」「exq-new」などと言われたら使う。引数は作りたいコマンドの説明（任意で --name <コマンド名>）。
---

# exq-new

やりたいことの説明から、exq が実行できる形式のローカル専用コマンドを生成する。

## 入力

```
# Claude Code（exq プラグイン経由）
/exq:exq-new <やりたいことの説明>
/exq:exq-new <やりたいことの説明> --name <コマンド名>

# OpenAI Codex CLI
$exq-new <やりたいことの説明>
$exq-new <やりたいことの説明> --name <コマンド名>
```

- `<やりたいことの説明>` (必須): コマンドにさせたい処理の自然言語説明
- `--name <コマンド名>` (任意): 生成するコマンド名。省略時は説明から決める

## exq フォーマット

1 コマンド = カレントディレクトリの `.exq/commands/<name>/` ディレクトリ:

```
.exq/
└── commands/
    └── <name>/
        ├── command.toml   # メタデータ
        └── run.sh         # 実行エントリポイント
```

**制約（exq 本体の検証仕様に準拠）:**

- `<name>`: 小文字ケバブケース推奨。パス区切り (`/`)・`.`・`..` は不可
- `run.sh`: 実行権限必須 (`chmod +x`)。shebang で任意の言語を使える（bash / python / node など）。
  作業ディレクトリは **ユーザーが exq を実行したディレクトリ**（`.exq/commands/<name>/` ではない）
- `command.toml`: 最低限 `description` を持つ。実行時に引数が必要なら `[[args]]` を順に定義する

```toml
description = "コマンドの一行説明（TUI と exq list に表示される）"

# 実行時引数が必要な場合のみ。定義順に $1, $2, ... として run.sh へ渡される。
[[args]]
key = "env"
description = "デプロイ先環境 (dev / prod)"

[[args]]
key = "service"
description = "対象サービス名（空なら全サービス）"
```

`[[args]]` を定義すると、TUI では選択後にキーごとの入力フォームが表示され、
CLI では `exq run <name> -- <values...>` で定義順に値を渡せる。
空欄の値も空文字列として位置を保って渡される。

## 手順

### 1. 引数パース

説明文を取り出す。`--name` があればコマンド名として保持。無ければ説明から英小文字ケバブケースの短い名前を決める（例: 「テスト DB をリセットする」→ `reset-test-db`）。

### 2. 初期化の確認

```bash
exq init
```

`exq init` は冪等（再実行しても `.exq/` や exclude 行は重複しない）なので、無条件に実行してよい。

`exq` が `$PATH` に無い場合は手動で同等の処理を行う:

```bash
mkdir -p .exq/commands
excl="$(git rev-parse --git-common-dir)/info/exclude"
grep -qxF '.exq/' "$excl" 2>/dev/null || { mkdir -p "$(dirname "$excl")"; echo '.exq/' >> "$excl"; }
```

### 3. 既存コマンドとの衝突チェック

`.exq/commands/<name>/` が既に存在する場合は**上書きせず**、ユーザーに確認を取る（別名を提案するか、上書きの明示的な了承を得る）。

### 4. run.sh の生成

説明された処理を実装したスクリプトを `.exq/commands/<name>/run.sh` に書く。

- 1 行目は shebang。既定は `#!/usr/bin/env bash` + `set -euo pipefail`。処理内容に適した言語があればそちらの shebang を使う
- 作業ディレクトリはユーザーの cwd である前提で書く（リポジトリルートが必要なら `git rev-parse --show-toplevel` で解決する）
- 実行時に可変にしたい値（対象環境・サービス名など）があれば `command.toml` に `[[args]]` を定義し、
  run.sh 冒頭で定義順にキー名の変数へ受ける。値は TUI フォーム / `exq run <name> -- <values...>` の
  両方から定義順の位置引数で渡ってくる。空欄は空文字列で届くのでデフォルト値は `${1:-default}` 形式で書く

  ```bash
  # $1: env, $2: service（command.toml の [[args]] 定義順）
  env="${1:?env is required}"
  service="${2:-}"
  ```
- 破壊的な処理（削除・リセット・強制 push 等）を含む場合は、スクリプト内に確認プロンプトか `--yes` フラグを入れる
- シークレットや環境固有の絶対パスをハードコードしない。必要なら環境変数を参照し、未設定時は分かるエラーを出す

書き終えたら必ず実行権限を付ける:

```bash
chmod +x .exq/commands/<name>/run.sh
```

### 5. command.toml の生成

```toml
description = "<TUI 一覧で一目で分かる一行説明>"
```

引数を使うスクリプトにした場合は、run.sh の `$1, $2, ...` と同じ順で `[[args]]` を定義する
（key はスクリプト内の変数名と揃え、description には TUI フォームで迷わない説明を書く）。

### 6. 検証

```bash
bash -n .exq/commands/<name>/run.sh   # bash の場合の構文チェック
exq list                              # 一覧に載ることを確認
```

副作用が無い読み取り専用コマンドであれば `exq run <name>` で試走してよい。副作用があるものは試走せず、ユーザーに委ねる。

`exq` が `$PATH` に無い場合は `exq list` の代わりにディレクトリ構成と実行権限を確認する:

```bash
ls -l .exq/commands/<name>/
```

### 7. 報告

ユーザーに以下を報告する:

```
Created .exq/commands/<name>/
- command.toml: <description>
- run.sh: <何をするスクリプトか一行>

Run with: exq run <name>   (or pick it in the exq TUI)
```

## 注意事項

- `.exq/` 配下は `.git/info/exclude` により git 管理外。コミット・PR の対象にしない
- 既存コマンドの run.sh を読んで流儀（言語、エラーハンドリング）が分かる場合はそれに合わせる
- 説明が曖昧でスクリプトの中身を決められない場合のみユーザーに質問する。多少の曖昧さは合理的なデフォルトで埋めてよい
