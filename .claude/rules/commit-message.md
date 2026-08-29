# コミットメッセージ規約

[Conventional Commits v1.0.0](https://www.conventionalcommits.org/) に準拠し、説明は**日本語**で書く。

## フォーマット

```
<type>[(scope)][!]: <説明（日本語）>

[任意の本文（日本語）]

[任意のフッター]
```

- **type**（必須）: 下記一覧から選ぶ
- **scope**（任意）: 影響範囲。このリポジトリでは `internal/` 配下のモジュール名や機能名を使う（例: `tui`, `daemon`, `herdr`, `workflow`, `store`, `review`）
- **説明**（必須）: 日本語で簡潔に。末尾に句点（。）を付けない
- **本文**（任意）: 「何を・なぜ」変えたかを日本語で書く。1 行 72 文字以内で折り返す
- **フッター**（任意）: `BREAKING CHANGE: <説明>` や `Closes #123` などの参照

## type 一覧

| type | 使う場面 |
|---|---|
| `feat` | 新機能の追加 |
| `fix` | バグ修正 |
| `docs` | ドキュメント・ルールのみの変更 |
| `style` | フォーマット・空白の調整（ロジック変更なし） |
| `refactor` | バグ修正でも新機能でもないコード改善 |
| `perf` | パフォーマンス改善 |
| `test` | テストの追加・修正 |
| `build` | ビルドシステムや依存関係の変更 |
| `ci` | CI/CD 設定の変更 |
| `chore` | 雑務・メンテナンス |
| `revert` | 以前のコミットの取り消し |

## 破壊的変更（Breaking Change）

後方互換性を壊す変更（CLI インターフェースの変更、`.exq` フォーマットの非互換変更など）の場合:

- type/scope の後に `!` を付ける
- フッターに `BREAKING CHANGE:` を追加して詳細と移行方法を書く

## 例

```
feat(daemon): exq と exqd を繋ぐ UNIX ソケット JSON プロトコルとクライアントを追加
```

```
build!: Makefile を廃止し開発スクリプトを .exq/scripts/ へ移行

BREAKING CHANGE: make ターゲットは廃止した。exq run <name> を使うこと。
```

## その他のルール

- 1 コミット 1 論理単位。無関係な変更を混ぜず、複数コミットに分割する
- push 済みコミットは `--amend` せず、修正コミットを積んで対応する
- `--force` 系の push はしない
