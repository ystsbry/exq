# exq コーディングガイドライン（レビュー観点）

revu の review-pr skill が一般観点に追加で適用する、exq 固有のレビュー指針。

## Go コード

- コメント・識別子は英語。コメントは「コードから読み取れない制約や意図」だけを書く（変更の経緯や次の行の説明は書かない）
- gofmt 必須（CI の format ジョブで検証される）。lint は golangci-lint v2 の standard セット
- CLI/TUI の出力系では `fmt.Fprint` 系の戻り値チェックを免除している（.golangci.yml の errcheck 設定）。それ以外の error は握りつぶさない
- cgo (`import "C"`) は導入しない（リリースが CGO_ENABLED=0 固定のため）
- 子プロセスの実行はシェルを介さず `exec.Command` に引数配列で渡す（インジェクション防止・空白を含む引数の保全）

## パッケージ構成

- `cmd/exq/` は cobra のコマンド層のみ。ロジックは `internal/` に置く
- `internal/store`: `.exq/` の探索・初期化・削除。`internal/command`: オンディスクフォーマット。`internal/runner`: 実行。`internal/workflow`: steps 合成。`internal/tui`: 画面
- TUI の実行フローは「TUI が終了して端末を明け渡してから実行する」設計を崩さない

## .exq フォーマット / TOML

- コマンドは `.exq/scripts/<name>/`（command.toml + run.sh）、ワークフローは `.exq/workflows/<name>/`（command.toml のみ）。名前は両者を通して一意
- **TOML の制約: `steps` などのトップレベルキーは `[[args]]` テーブルより前に書く**（テーブルの後のトップレベルキーはテーブル側に解釈される）
- 壊れた command.toml は一覧から隠さず、寛容に扱う（description 空で表示を継続）

## シェルスクリプト（.exq/scripts）

- `#!/usr/bin/env bash` + `set -euo pipefail` を既定とする
- リポジトリルートが必要な処理は `git rev-parse --show-toplevel` で解決する（cwd 前提にしない）
- 引数は `${1:-default}` 形式で受ける（ワークフローから空文字列が渡り得る）
- 破壊的な処理には確認プロンプトまたは `--yes` を設ける

## UX / 出力の慣行

- ユーザー向けメッセージには次の行動が分かるヒントを含める（例: 「run `exq init` to migrate」）
- 進捗・枠などの装飾出力は stderr、データは stdout（パイプ利用を汚さない）
- 終了コードは子プロセス・失敗ステップのものを伝播する
- 既存の表示スタイル（lipgloss の色番号・カード/タブの描画慣行）と一貫させる

## テスト

- 新しい挙動には必ずテストを付ける（`t.TempDir` + `git init` で実リポジトリを作る流儀）
- 失敗系・エッジケース（空・重複・パストラバーサル・中断）を優先して書く
- TUI は model の Update/View を直接叩くテスト（bubbletea の Program は起動しない）

## 互換性

- `.exq/` のオンディスクフォーマット変更は後方互換（自動移行 + 旧構成検出時の案内）を必須とする
- CLI のフラグ・サブコマンドの破壊的変更はコミットメッセージで BREAKING CHANGE を明示する
