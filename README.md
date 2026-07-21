# exq

リポジトリにコミットせず、ローカル環境専用のコマンドを管理・実行するツール。

コマンドはカレントディレクトリの `.exq/` 配下に置かれ、`.git/info/exclude` によって
Git 管理から除外されるため、`.gitignore` を汚さずに自分専用のコマンドを持てる。

## インストール

```sh
bash .exq/scripts/install/run.sh                 # ~/.local/bin/exq にインストール
bash .exq/scripts/install/run.sh /usr/local      # PREFIX を指定する場合
```

このリポジトリ自身の開発スクリプトも `.exq/` で管理している（dogfooding）。
exq インストール後は `exq run install` などで同じスクリプトを TUI / CLI から実行できる。

## 使い方

```sh
exq init                       # ./.exq/scripts と ./.exq/workflows を作成し、.git/info/exclude に .exq/ を追記
exq                            # TUI を開く（enter: 実行 / d: 削除 / q: 終了）
exq list                       # コマンド一覧
exq run <name> [-- <values...>] # コマンドを実行（-- 以降は引数として $1, $2, ... に渡る）
exq remove <name>              # コマンドを削除（-y で確認スキップ）
```

## コマンドフォーマット

1 コマンド = `.exq/scripts/<name>/` ディレクトリ。ワークフロー（スクリプトの組み合わせ）は `.exq/workflows/<name>/` に置く。名前は scripts / workflows を通して一意。

```
.exq/
├── scripts/
│   └── hello/
│       ├── command.toml   # メタデータ
│       └── run.sh         # 実行エントリポイント（実行権限必須、shebang で任意の言語）
└── workflows/
    └── pre-pr/
        └── command.toml   # steps 定義（run.sh なし）
```

旧構成（`.exq/commands/`）は `exq init` の再実行で `scripts/` へ自動移行される。

`command.toml`:

```toml
description = "コマンドの説明"

# 実行時引数が必要な場合のみ。定義順に $1, $2, ... として run.sh へ渡される。
[[args]]
key = "env"
description = "デプロイ先環境 (dev / prod)"

[[args]]
key = "service"
description = "対象サービス名（空なら全サービス）"
```

`run.sh` はユーザーが exq を実行したディレクトリを作業ディレクトリとして実行される。

### 実行時引数

`[[args]]` を定義したコマンドは:

- **TUI**: enter で選択すると、キーと説明が一覧で並ぶ入力フォームが開く
  （tab/↑↓ で移動、enter で実行、esc で一覧に戻る）
- **CLI**: `exq run <name> -- <values...>` で定義順に値を渡す

どちらもシェルを介さず定義順の位置引数（`$1`, `$2`, ...）として渡り、
空欄の値も空文字列として位置を保つ。`[[args]]` の無いコマンドは従来どおり
enter で即実行される。

### ワークフロー

複数のスクリプトを決まった順で実行する組み合わせを `.exq/workflows/<name>/command.toml` に定義できる:

```toml
description = "PR 提出前チェック一式"
steps = ["fmt", "vet", "test"]
```

- `exq run <name>` または TUI から単体スクリプトと同様に実行できる（一覧にはステップ構成が `(steps: fmt → vet → test)` の形で表示される）
- 実行中は `[2/3] vet` のように進捗が表示され、完了後にステップごとの成否・所要時間のサマリが出る:

  ```
  ✓ fmt  0.3s
  ✗ vet  0.4s (exit 1)
  - test (skipped)
  ```

- ステップが失敗（非0終了）した時点で中断され、残りは skipped になる。exq は失敗ステップの終了コードで終了する
- steps に存在しない名前・ワークフロー（入れ子）・実行権限の無いスクリプト・未宣言の `${key}` が含まれる場合は、**実行前の検証**でエラーになる（途中まで走ってから気づかない）

#### ステップへの引数受け渡し

ワークフロー自身に `[[args]]` を宣言し、steps 内で `${key}` として参照すると、
実行時の値（TUI フォーム / `exq run <name> -- <values...>`）がステップに渡る:

```toml
description = "exq バイナリをビルドしてインストールする"
steps = ["build", "install-bin ${prefix}"]

[[args]]
key = "prefix"
description = "インストール先 PREFIX（空なら ~/.local）"
```

- steps のエントリは `"スクリプト名 引数..."` の形式（空白区切り）。`${key}` はトークン全体でも
  `--prefix=${key}` のような埋め込みでもよく、空白を含む値も1引数のまま渡る
- 値が未入力のキーは空文字列になる（スクリプト側は `${1:-default}` で既定値を持てる）
- **注意**: TOML の制約上、`steps` は `[[args]]` より**前**に書くこと（テーブルの後の
  トップレベルキーはテーブル側に解釈される）

## Skill（コマンド生成の AI 支援）

やりたいことを伝えると exq フォーマットのコマンドを生成する skill を同梱している。
Claude Code / OpenAI Codex CLI が **同じ `plugin/` を共有**する:

```
plugin/
├── .claude-plugin/plugin.json    # Claude Code 用プラグイン定義
├── .codex-plugin/plugin.json     # Codex 用プラグイン定義
└── skills/
    └── exq-new/SKILL.md          # 共有（1 コピーのみ）

.agents/plugins/marketplace.json  # Codex 用ローカルマーケットプレース（./plugin を参照）
```

| ランタイム | インストール先 | 呼び出し |
| --- | --- | --- |
| Claude Code | `~/.claude/skills/exq`（skills-dir plugin として自動ロード） | `/exq:exq-new <説明>` |
| OpenAI Codex CLI | プラグイン（marketplace 経由）or `~/.agents/skills/`（fallback） | `$exq-new <説明>` |

```sh
# Claude Code 用: plugin/ を ~/.claude/skills/exq にシンボリックリンク
# （claude 再起動後 `claude plugin list` で確認）
exq run install-claudecode
exq run uninstall-claudecode

# Codex 用: 同じ plugin/ をローカル marketplace 経由でプラグイン登録
exq run install-codex          # = codex plugin marketplace add <repo>
exq run uninstall-codex
```

> **Codex の注意点**
> - marketplace 経由では Codex がプラグインを `~/.codex/plugins/cache/` に**コピー**する
>   （symlink ではない）ため、SKILL.md を編集したら再インストールが必要。
> - plugin 未対応の codex バージョン向け fallback として `exq run install-codex-skills` /
>   `exq run uninstall-codex-skills` も用意している（`~/.agents/skills/` へのディレクトリ
>   symlink。ファイル単位の symlink は loader に落とされる: openai/codex#15756。
>   symlink なので編集が即反映される）。

## 開発

開発用スクリプトは `.exq/scripts/` にコミットされており、exq 自身で実行する（dogfooding）。

```sh
exq run build   # bin/exq をビルド
exq run test    # go test ./...
exq run vet     # go vet
exq run fmt     # gofmt
exq run check   # fmt チェック + vet + test をまとめて実行
```

exq が未インストールでも `bash .exq/scripts/<name>/run.sh` で直接実行できる。

### UI の確認（storybook 的な起動）

`exq demo` は一時ディレクトリにサンプルコマンドを展開して TUI を開く。
実環境の `.exq/` には一切触れないので、削除や実行も安全に試せる
（終了時に一時ディレクトリごと破棄される）。

```sh
exq demo              # サンプルデータ入りで TUI を起動
exq demo --empty      # 空状態の表示を確認
exq demo --snapshot   # 全 UI 状態（browse / empty / confirm-delete / error）を
                      # stdout にレンダリングして終了（TTY 不要）
```
