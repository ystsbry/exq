# C# (.NET) での exq 利用例

C# プロジェクトで exq を使うとどうなるかを、実際に動かして確かめるための最小サンプル。
`dotnet` CLI 中心のエコシステム（ソリューション・ビルド・xUnit・format）で
exq のコマンドがどう並ぶかを確認できる。

## 前提

**.NET SDK 8.0 (LTS) 以上**がインストール済みであること。SDK 自体の導入はこのサンプルの
スコープ外なので、未インストールなら先に入れておく:

- https://dotnet.microsoft.com/download/dotnet/8.0

```sh
# 例: sudo なしでユーザーローカルに入れる場合
curl -sSL https://dot.net/v1/dotnet-install.sh | bash -s -- --channel 8.0
export DOTNET_ROOT="$HOME/.dotnet"
export PATH="$DOTNET_ROOT:$DOTNET_ROOT/tools:$PATH"
```

SDK が無い状態で `setup.sh` や各コマンドを実行すると、必要なバージョンと入手先を
案内するエラーで停止する。

exq 本体のインストールは[リポジトリルートの README](../../README.md) を参照。

## セットアップ

`.exq/` はこのリポジトリにコミット済みなので `exq init` は不要。
`setup.sh` は依存関係を復元するだけで、何度実行しても同じ結果になる（冪等）。

```sh
cd sandbox/csharp
./setup.sh
```

## 試せる操作

```sh
exq list        # コマンド一覧
exq             # TUI（←/→ でタブ切替、enter で実行、q で終了）
```

| コマンド | 内容 |
| --- | --- |
| `build` | `dotnet build` でソリューション全体をビルドする |
| `test` | `dotnet test` で xUnit テストを実行する |
| `test-filter` | `dotnet test --filter <式>` でテストを絞り込む（**引数あり**） |
| `format` | `.editorconfig` に従って `dotnet format` で整形する |
| `format-check` | `dotnet format --verify-no-changes` で検査のみ行う（差分があれば非0終了） |
| `clean` | `dotnet clean` に加えて `bin/` `obj/` を削除する |
| `run-app` | `dotnet run` でコンソールアプリを実行する（**引数あり**） |

### 引数付きコマンド

`[[args]]` を定義したコマンドの挙動（TUI の入力フォーム、CLI の `--` 以降の値渡し）自体は
C# 固有ではないので、詳細は[リポジトリルートの README の「実行時引数」](../../README.md#実行時引数)を参照。
ここでは `test-filter` / `run-app` で実際に試せる:

```sh
# 絞り込み実行: 5 件中 3 件だけ走る
exq run test-filter -- "FullyQualifiedName~WithoutName"

# アプリに名前を渡す
exq run run-app -- exq      # => Hello, exq!
exq run run-app             # => Hello, world!（空ならフォールバック）
```

## 動作確認の手順

上から順に実行すると、一通りの挙動を確認できる。

```sh
cd sandbox/csharp
./setup.sh                  # .NET SDK 8.0.x を検出し restore が完了する

exq list                    # 上表の 7 コマンドが並び、引数付きには (args: ...) が付く
exq run build               # Build succeeded.
exq run test                # Passed! - Failed: 0, Passed: 5
exq run test-filter -- "FullyQualifiedName~WithoutName"
                            # Passed! - Failed: 0, Passed: 3  ← 5 件から絞り込まれる
exq run run-app -- exq      # Hello, exq!
exq run format-check        # 差分なしで成功する
exq run clean               # bin/ obj/ が消え、クローン直後の状態に戻る
./setup.sh                  # 再度 restore すれば同じように動く（冪等）

exq                         # TUI を開き、上記コマンドが一覧に出ることを確認する
                            # 引数付きコマンドを enter すると入力フォームが開く
```

exq が未インストールでも、各コマンドは直接実行できる:

```sh
bash .exq/scripts/test/run.sh
bash .exq/scripts/test-filter/run.sh "FullyQualifiedName~WithoutName"
```

## 構成

```
sandbox/csharp/
├── .exq/
│   ├── lib/require-dotnet.sh    # SDK 検査の共通ヘルパー（scripts/ の外なので一覧に出ない）
│   └── scripts/<name>/
│       ├── command.toml         # 説明と [[args]]
│       └── run.sh               # 実行エントリポイント
├── src/HelloExq/                # コンソールアプリ（Greeter + Program）
├── tests/HelloExq.Tests/        # xUnit テスト（5 件）
├── HelloExq.sln
├── .editorconfig                # dotnet format の結果を環境によらず決定的にする
└── setup.sh
```

各 `run.sh` は自身のパスから `sandbox/csharp` を解決して `cd` するため、
exq をどのディレクトリから起動しても、直接 `bash` で叩いても同じように動く。
