# ブランチ命名規約

ブランチは必ず最新化した `main` から切り、`<type>/<説明>` の形式で命名する。

```
<type>/<kebab-case の英語短文>
```

## type 一覧

コミットメッセージの type（`.claude/rules/commit-message.md` 参照）と揃える。ブランチの主目的となる変更の type を使う。

| type | 使う場面 |
|---|---|
| `feat/` | 新機能の追加 |
| `fix/` | バグ修正 |
| `docs/` | ドキュメント・ルールのみの変更 |
| `refactor/` | 挙動を変えないコード改善 |
| `test/` | テストの追加・修正 |
| `build/` | ビルド・依存関係の変更 |
| `ci/` | CI/CD 設定の変更 |
| `chore/` | 雑務・メンテナンス |

## 説明部分のルール

- 小文字ケバブケース、英語で簡潔に（3〜5 語程度）
- 何をするブランチか判別できる具体性を持たせる（`fix/bug` のような曖昧な名前は不可）

## 例

```
feat/daemon-socket-client
fix/tui-scroll-overflow
docs/git-convention-rules
ci/claude-review-comment-only
```
