# exq 開発ガイド

このファイルと `CLAUDE.md`・`.claude/rules/` はこのリポジトリの開発者向けであり、exq のユーザーに配布されるものではない。

## 開発コマンド

ビルド・テスト等の開発コマンドは `.exq/scripts/` に exq フォーマットで定義されている。`exq <name>` で実行する（例: `exq build`, `exq test`, `exq fmt`, `exq vet`。まとめて実行するなら `exq run check`）。開発用コマンドを追加する場合も直接シェルスクリプトを散らかさず、この形式（`.exq/scripts/<name>/` に `command.toml` + `run.sh`）に従う。

## 開発ルール

開発ルールは `.claude/rules/` にトピック別の Markdown で置いている。Claude Code は自動で読み込むが、それ以外のエージェントは自動では読み込まないため、**作業を始める前に `.claude/rules/` 配下のファイルを読むこと**。

- frontmatter に `paths:`（glob パターン）があるルールは、そのパターンにマッチするファイルを扱うときのみ適用する
- ルールを追加・変更するときは `.claude/rules/<topic>.md` に 1 ファイル 1 トピックで置く
