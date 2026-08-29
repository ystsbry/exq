# スキルの配置規約

このリポジトリには「ユーザーへの配布物」と「開発者専用」の 2 種類のスキル置き場がある。**「exq のユーザーに配布するものか」で置き場所を決める。**

| 区分 | 置き場所 | 読み込まれ方 |
|---|---|---|
| ユーザー向け（配布物） | `plugin/skills/<name>/SKILL.md` | ユーザーが `exq install-claudecode` / `exq install-codex` / `exq install-codex-skills` を実行して各ツールに登録する |
| 開発者向け（このリポジトリ専用） | `.claude/skills/<name>/SKILL.md` | Claude Code がプロジェクトスキルとして自動発見する。インストール作業は不要 |

## ユーザー向けスキル（配布物）を追加・変更するとき

- 実体は `plugin/skills/<name>/SKILL.md` に置く。
- `plugin/.codex-plugin/plugin.json` は `"skills": "./skills/"` でディレクトリごと参照しているため、スキルの追加だけならマニフェストの編集は不要。
- ただしスキル構成が変わって説明と乖離した場合は、以下 3 ファイルの `description` を揃えて更新する:
  - `plugin/.claude-plugin/plugin.json`
  - `plugin/.codex-plugin/plugin.json`
  - `.agents/plugins/marketplace.json`
- 配布先には Claude Code と Codex 両方のユーザーが含まれるため、SKILL.md に呼び出し記法を書く場合は両方を併記し（Claude Code は `/exq:<name>`、Codex は `$<name>`）、ツール固有の機能に依存する手順は書かない。
- 動作確認は自環境へのリンクで行う: Claude Code は `exq install-claudecode`、Codex は `exq install-codex`（プラグイン）または `exq install-codex-skills`（スキルのみ）。

## 開発者向けスキルを追加・変更するとき

- 実体は `.claude/skills/<name>/SKILL.md` に置く。Claude Code 専用でよい（Codex 対応は不要）。
- 開発者向けスキルを配布経路（`plugin/`・marketplace）に含めないこと。ユーザーにも有用と判断したら `plugin/skills/` へ移動して配布物に昇格させる。

## SKILL.md の書式（両区分共通）

- YAML frontmatter に `name` と `description` を必ず書く。
  - `name`: ディレクトリ名と一致させる。小文字ケバブケース。
  - `description`: 何をするスキルかと、トリガーとなる発話例（「〜して」「`<name>`」など）を含める。
- 本文には目的・入力・手順を書く。
