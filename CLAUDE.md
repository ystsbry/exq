# exq 開発ガイド

このファイルと `.claude/rules/` はこのリポジトリの開発者向けであり、exq のユーザーに配布されるものではない。

## 開発コマンド

ビルド・テスト等の開発コマンドは `.exq/scripts/` に exq フォーマットで定義されている。`exq <name>` で実行する（例: `exq build`, `exq test`, `exq fmt`, `exq vet`。まとめて実行するなら `exq run check`）。開発用コマンドを追加する場合も直接シェルスクリプトを散らかさず、この形式（`.exq/scripts/<name>/` に `command.toml` + `run.sh`）に従う。

## ルールの書き方

- トピック別のルールは `.claude/rules/<topic>.md` に 1 ファイル 1 トピックで置く
- 特定パスの編集時だけ効かせたいルールは、frontmatter の `paths:`（glob パターン）で適用範囲を指定する
- この CLAUDE.md には概要とポインタのみを書き、ルール本文は `.claude/rules/` に置く

## ルール一覧

- `.claude/rules/skills.md`: スキルの配置規約（ユーザー配布物と開発者専用の使い分け・SKILL.md の書式）
- `.claude/rules/branch-naming.md`: ブランチ命名規約（`<type>/<kebab-case>` 形式）
- `.claude/rules/commit-message.md`: コミットメッセージ規約（Conventional Commits・日本語）
