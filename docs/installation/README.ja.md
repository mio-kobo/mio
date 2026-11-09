# Mio 2.1 のインストール

パッケージマネージャーへの公開はまだ有効になっていません。以下のコマンドは、
検証済みの安定版と配布リポジトリの PR が承認された後に使用できます。
`2.1.1-pr.<sha>` は候補ビルドです。

Full は完全な Bunko 標準ライブラリーを組み込んだ単一の自己完結型ネイティブ
コンパイラーで、統合 CLI ツールを備え、オフラインで開発できます。Core は
Bunko を組み込まない小型コンパイラーで、`kiso/` のインポートには外部 Bunko
が必要です。どちらも追加ランタイムの取得は不要で、独立した開発ツールは
同梱していません。

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
自己完結型の Inno インストーラーは実際の bin をユーザー PATH に追加します。
Full は Bunko を内蔵するため外部ディレクトリーは不要です。Core は外部 Bunko を
必要とします。Windows Runner でファイル一覧とコマンドを検証します。
アンインストール時はこの PATH 項目を削除します。
Microsoft のコミュニティーリポジトリへの初回提出は手動レビューが必要です。

## Windows AMD64：Chocolatey

初回提出のコミュニティーレビュー完了後に利用できます。
[公式手順](https://docs.chocolatey.org/en-us/choco/setup/)で Chocolatey を導入し、
管理者 PowerShell で実行してください。

```powershell
choco install mio
mio version
choco upgrade mio
choco uninstall mio
# Core（更新・削除にも mio-core を指定）：
choco install mio-core
mio-core version
```

Full と Core は異なるコマンドで共存できます。検証済みの公式 ZIP を Chocolatey の
`lib` 配下に配置し、`bin` のプロセス Shim から実際のコンパイラーを起動します。
Full は Bunko を内蔵するため隣接ディレクトリーを必要としません。
Chocolatey 導入後は端末を開き直して PATH を更新してください。同じ種類を WinGet と Chocolatey の両方で導入しないでください。
Full は導入後にオフラインで開発でき、Core は外部 Bunko が必要です。
プロジェクトと設定はパッケージディレクトリーの外に保存してください。更新時には
内容を置換し、削除時にはこのディレクトリーを削除します。

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
Windows AMD64 のネイティブコンパイラは、Winsock を通じてブロッキング IPv4 の `kiso/net/tcp` をサポートします。

Bunko は `--bunko-root`、`MIO_BUNKO`、Full 実行ファイル内蔵の Bunko、Core と旧配置向けの
実行ファイルから見た `../bunko` の順に検索します。無効な明示パスや環境変数は
エラーになります。空の `MIO_BUNKO` もエラーです。Full の `mio` 実行ファイルは
単体で別のディレクトリーや互換 OS・CPU のコンピューターへ移動しても `kiso/...` を
インポートできます。Core には外部 Bunko が必要です。

[English](README.md) · [简体中文](README.zh-CN.md)
