# exq

リポジトリにコミットせず、ローカル環境専用のコマンドを管理・実行するツール。

コマンドはカレントディレクトリの `.exq/` 配下に置かれ、`.git/info/exclude` によって
Git 管理から除外されるため、`.gitignore` を汚さずに自分専用のコマンドを持てる。

## インストール

```sh
make install PREFIX=$HOME/.local
```

## 使い方

```sh
exq init                       # ./.exq/scripts と ./.exq/workflows を作成し、.git/info/exclude に .exq/ を追記
exq                            # TUI を開く（enter: 実行 / d: 削除 / q: 終了）
exq list                       # コマンド一覧
exq run <name> [-- <values...>] # コマンドを実行（-- 以降は引数として $1, $2, ... に渡る）
exq remove <name>              # コマンドを削除（-y で確認スキップ）
```

## コマンドフォーマット

1 コマンド = `.exq/scripts/<name>/` ディレクトリ。ワークフロー（スクリプトの組み合わせ。実行は未実装）は `.exq/workflows/<name>/` に置く。名前は scripts / workflows を通して一意。

```
.exq/
├── scripts/
│   └── hello/
│       ├── command.toml   # メタデータ
│       └── run.sh         # 実行エントリポイント（実行権限必須、shebang で任意の言語）
└── workflows/             # steps 定義によるワークフロー置き場（実行機能は今後対応）
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
make install-claudecode
make uninstall-claudecode

# Codex 用: 同じ plugin/ をローカル marketplace 経由でプラグイン登録
make install-codex             # = codex plugin marketplace add <repo>
make uninstall-codex
```

> **Codex の注意点**
> - marketplace 経由では Codex がプラグインを `~/.codex/plugins/cache/` に**コピー**する
>   （symlink ではない）ため、SKILL.md を編集したら再インストールが必要。
> - plugin 未対応の codex バージョン向け fallback として `make install-codex-skills` /
>   `make uninstall-codex-skills` も用意している（`~/.agents/skills/` へのディレクトリ
>   symlink。ファイル単位の symlink は loader に落とされる: openai/codex#15756。
>   symlink なので編集が即反映される）。

## 開発

```sh
make build   # bin/exq をビルド
make test    # go test ./...
make vet     # go vet
make fmt     # gofmt
```

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
