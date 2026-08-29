# exq

リポジトリにコミットせず、ローカル環境専用のコマンドを管理・実行するツール。

コマンドはカレントディレクトリの `.exq/` 配下に置かれ、`.git/info/exclude` によって
Git 管理から除外されるため、`.gitignore` を汚さずに自分専用のコマンドを持てる。

## インストール

```sh
bash .exq/scripts/build/run.sh                        # bin/exq と bin/exqd をビルド
bash .exq/scripts/install-bin/run.sh                  # ~/.local/bin へ配置
bash .exq/scripts/install-bin/run.sh /usr/local       # PREFIX を指定する場合
```

`exq` は CLI / TUI 本体、`exqd` は[バックグラウンドジョブ](#バックグラウンド実行とスケジュール)を
実行する常駐デーモン。バックグラウンド実行・スケジュール実行を使わないなら `exq` だけでよい。

このリポジトリ自身の開発スクリプトも `.exq/` で管理している（dogfooding）。
exq インストール後は `exq run build` などで同じスクリプトを TUI / CLI から実行できる。

## 使い方

```sh
exq init                       # ./.exq/scripts と ./.exq/workflows を作成し、.git/info/exclude に .exq/ を追記
exq                            # TUI を開く（←/→: タブ切替, enter: 実行, b: バックグラウンド, s: スケジュール, d: 削除, q: 終了）
exq list                       # コマンド一覧
exq run <name> [-- <values...>] # コマンドを実行（-- 以降は引数として $1, $2, ... に渡る）
exq remove <name>              # コマンドを削除（-y で確認スキップ）
```

バックグラウンド実行・スケジュール実行のコマンド（`exq run --bg` / `jobs` / `logs` /
`stop` / `schedule` / `daemon`）は[後述](#バックグラウンド実行とスケジュール)。

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

## バックグラウンド実行とスケジュール

時間のかかるコマンドを投げっぱなしにしたり、定期実行したりできる。実体は
デーモン `exqd` で、投入されたジョブを**自身の子プロセスとして実行する**ため、
投入元の端末やペインを閉じてもジョブは走り続ける。

```
  exq (CLI/TUI) ─┐
                 ├─ UNIX ソケット ─▶ exqd ─▶ .exq/scripts/<name>/run.sh
  systemd timer ─┘                            （投入時のディレクトリで実行）
  （スケジュール発火 = exq run --bg の自動実行）
```

「いつ動かすか」は exqd ではなく **systemd user timer** に任せている。スケジュール
実行は「timer が代行してくれる手動バックグラウンド投入」に過ぎず、ジョブの状態・
ログは手動投入とまったく同じ場所に集まる。

### セットアップ

```sh
exq daemon install   # ~/.config/systemd/user/exqd.service を生成して起動
exq daemon status    # unit の状態とソケットの疎通を確認
exq daemon restart   # exq を入れ替えた後（バージョン不一致時）に実行
```

- systemd **user** unit として動く。ジョブは自分のリポジトリ・権限・環境で走り、
  root 常駐は一切しない
- **WSL2 では `/etc/wsl.conf` に `[boot] systemd=true` が必要**。
  未設定の場合は `exq daemon install` が設定方法を添えて明示的に失敗する
- ログアウト後もスケジュールを動かすには `loginctl enable-linger <user>` が必要
  （`exq daemon install` が案内する）

### バックグラウンド実行

```sh
exq run <name> --bg [-- <values...>]   # exqd に投入して job id を即返す
exq jobs                               # ジョブ一覧（状態・開始時刻・所要時間）
exq logs <job-id> [-f]                 # ログ閲覧（-f は tail -f 相当）
exq stop <job-id>                      # 停止（プロセスグループに SIGTERM → 猶予後 SIGKILL）
exq jobs --prune                       # 完了したジョブの記録とログを一括削除
```

TUI では scripts / workflows 一覧で **`b`**（引数フォーム内では `ctrl+b`）で投入し、
**jobs タブ**で状態を確認できる（enter でログ表示、`r` で更新）。

ジョブの状態とログは `~/.local/state/exq/jobs/<job-id>/`（`job.json` +
`output.log`、`$XDG_STATE_HOME` 準拠）に残る。**ファイルとして残るため、
exqd が止まっていても `exq logs` / `exq jobs --prune` は使える。**

### スケジュール実行

```sh
exq schedule add <name> --on-calendar "<式>" [-- <values...>]
exq schedule list                      # 登録済み一覧（次回発火時刻・直近の投入結果つき）
exq schedule remove <id>               # 停止して unit を削除（-y で確認スキップ）
```

式は systemd の **OnCalendar 構文**をそのまま使う（cron 式ではない）:

```sh
exq schedule add test   --on-calendar "Mon..Fri 09:00"
exq schedule add backup --on-calendar "daily"
exq schedule add deploy --on-calendar "*-*-01 03:00" -- prod
```

- 登録時に `systemd-analyze calendar` で検証するので、書式ミスはその場で分かる
- `~/.config/systemd/user/exq-sched-<id>.{timer,service}` を生成する。
  **この unit ファイルが唯一の記録**であり、exq 側に別の登録簿は持たない
  （`systemctl --user list-timers` で見えるものと食い違わない）
- `Persistent=true` を付けているので、マシン停止中に逃した発火は次回起動時に 1 度だけ実行される

TUI では scripts / workflows 一覧で **`s`** を押すと登録フォーム（OnCalendar 式 +
`[[args]]` の値）が開き、**schedules タブ**で一覧・削除（`d`）・詳細（enter で
生成された unit とジョブ履歴）を確認できる。

### 注意点

- **環境変数**: ジョブは端末のシェル環境ではなく **systemd user session の環境**で動く。
  PATH や direnv 由来の変数は同期実行と異なりうるので、必要なものは `run.sh` 側で
  読み込むこと
- **exqd の再起動**: 再起動時に running のまま残っていたジョブは orphaned として
  failed 扱いになる（子プロセスは systemd の cgroup ごと止まるため、再アタッチはしない）
- **多重起動**: 同じスケジュールのジョブがまだ動いているときの発火はスキップされ、
  `skipped` として履歴に残る（手動投入はスキップされない）
- **プロジェクトの削除・移動**: timer は残り、発火のたびに作業ディレクトリ不在で失敗する。
  `exq schedule list` が `(!)` で警告するので `exq schedule remove` で片付ける
- **ログの肥大化**: 自動ローテーションはしない。`exq jobs --prune` で完了ジョブを掃除する

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
exq run build   # bin/exq と bin/exqd をビルド
exq run test    # go test ./...
exq run vet     # go vet
exq run fmt     # gofmt
exq run check   # fmt チェック + vet + test をまとめて実行
```

exq が未インストールでも `bash .exq/scripts/<name>/run.sh` で直接実行できる。

### AI コードレビュー

PR を作成・更新すると、[revu](https://github.com/ystsbry/revu) ベースの AI コードレビューが
自動で実行される（`.github/workflows/claude-review.yml`）。

- claude-code-action が revu の `review-pr` スキルでレビュー下書きを生成し、
  `revu submit` が GitHub PR レビューとして投稿する
- PR コメントに `/claude review` と書くと任意のタイミングで再実行できる
- draft PR と `skip-review` ラベル付きの PR はスキップされる
- 前提: リポジトリシークレット `CLAUDE_CODE_OAUTH_TOKEN` の登録

### UI の確認（storybook 的な起動）

`exq demo` は一時ディレクトリにサンプルコマンドを展開して TUI を開く。
実環境の `.exq/` には一切触れないので、削除や実行も安全に試せる
（終了時に一時ディレクトリごと破棄される）。

```sh
exq demo              # サンプルデータ入りで TUI を起動
exq demo --empty      # 空状態の表示を確認
exq demo --snapshot   # 全 UI 状態（browse / jobs / schedules / フォーム / empty /
                      # confirm-delete / error）を stdout にレンダリングして終了（TTY 不要）
```
