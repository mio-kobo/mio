# Mio 2.1 のインストール

パッケージマネージャーへの公開はまだ有効になっていません。以下のコマンドは、
検証済みの安定版と配布リポジトリの PR が承認された後に使用できます。
`2.1.0-pr.<sha>` は候補ビルドです。

Full はネイティブコンパイラー、対応済みの Bunko 標準ライブラリー、統合 CLI
ツールを含み、オフラインで開発できます。Core は同一バイト列のコンパイラーを
使用し、インポート可能な Bunko を含みません。標準ライブラリーに依存しない
プログラムをコンパイルできます。追加のランタイムの取得は不要です。
現時点では独立した開発ツールは同梱していません。

## macOS ARM64：Homebrew

```sh
brew tap mio-kobo/mio
brew install mio             # Core: brew install mio-core
mio version
brew upgrade mio             # Core: mio-core
brew uninstall mio
```

両方とも `mio` を提供するため競合します。切り替える前に元のパッケージを削除してください。

## Windows AMD64：WinGet

```powershell
winget install --id MioKobo.Mio --exact
mio version
winget upgrade --id MioKobo.Mio --exact
winget uninstall --id MioKobo.Mio --exact
# Core:
winget install --id MioKobo.Mio.Core --exact
mio-core version
```

Full のコマンドは `mio`、Core は `mio-core` です。インストール先は独立しています。
Core の更新・削除には `MioKobo.Mio.Core` を指定します。
ZIP Portable による Bunko ディレクトリー全体の保持を Windows Runner で検証します。
Microsoft のコミュニティーリポジトリへの初回提出は手動レビューが必要です。

## Linux AMD64：APT

リポジトリーの有効化後に専用の公開鍵を取得し、別の信頼できる経路で公開された
フィンガープリントと照合してください。

```sh
curl -fsSLo mio-archive-keyring.gpg https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg
gpg --show-keys --with-fingerprint mio-archive-keyring.gpg
sudo install -m 644 -D mio-archive-keyring.gpg /etc/apt/keyrings/mio.gpg
printf '%s\n' 'deb [arch=amd64 signed-by=/etc/apt/keyrings/mio.gpg] https://mio-kobo.github.io/mio-apt stable main' | sudo tee /etc/apt/sources.list.d/mio.list
sudo apt-get update
sudo apt-get install mio     # または mio-core
sudo apt-get update && sudo apt-get install --only-upgrade mio
sudo apt-get remove mio
```

Linux AMD64 のみ対応しています。Full と Core には競合・置換関係を設定しています。
切り替え時は APT の削除内容を確認してください。`apt-key` や署名検証の無効化は使いません。
ビルドツールはパッケージの実行時依存関係に含まれません。

## オフライン開発と制限

```sh
mio build hello.mio --target=linux-amd64 -o hello
./hello
mio check math.mio
```

macOS では `darwin-arm64`、Windows では `windows-amd64` を指定します。
デフォルトは現在のホストターゲットです。`run` と `exec` はホストターゲットを使用します。
check/build/run/exec/test/lsp/disasm の統合ツールは三つのプラットフォームで検証します。
Windows には Winsock ランタイム層がなく、`kiso/net/tcp` は NOT_APPLICABLE です。

Bunko は `--bunko-root`、`MIO_BUNKO`、実際の実行ファイルから見た `../bunko`
の順に検索します。無効な明示パスや環境変数はエラーになります。空の `MIO_BUNKO`
もエラーです。Full の同梱ライブラリーを使う場合は変数を解除してください。
配置を変更する際はインストールディレクトリー全体をコピーしてください。

[English](README.md) · [简体中文](README.zh-CN.md)
