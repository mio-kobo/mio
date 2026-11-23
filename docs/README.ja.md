[English](../README.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md)

---

<p align="center">
  <a href="../assets/mascot/mio-and-mochi.png">
    <img src="../assets/mascot/mio-and-mochi.webp" alt="Mio and Mochi" width="260" />
  </a>
</p>

<h1 align="center">Mio</h1>

<p align="center"><strong>A small core, a wide world of mathematics and programs.</strong></p>

<p align="center">
  <a href="https://github.com/mio-kobo/mio/releases">Releases</a>
  · <a href="https://github.com/mio-kobo/mio/actions">ビルド状況</a>
  · <a href="https://marketplace.visualstudio.com/items?itemName=exmikupro.mio-language-support">VS Code</a>
  · <a href="https://plugins.jetbrains.com/plugin/35197-mio-language-support">JetBrains</a>
  · <a href="https://mio.kaelexvp.cc">ドキュメント</a>
</p>

**Mio** は、ローマ字の日本語をキーワードに使う静的強型付けプログラミング言語です。明示的な数値セマンティクスと小さなコア、そして必要なときにだけ読み込む標準ライブラリ **Bunko** を軸に設計されています。

> **`v2.2.0`（開発中）：** `v2.1.*` の次期安定版として、ネイティブ AOT の対象を 6 プラットフォームへ広げ、ネイティブ並行処理とイベントループのプリミティブを追加しています。

```mio
yobu "kiso/math";

waza square(value: float64) -> float64 {
    kaesu value * value;
}

kaku area = square(12.0);
chant(area);
chant(math.sqrt(area));
```

## 特徴とツールチェーン

- **型システム：** 固定幅整数、浮動小数点数、ジェネリクス、Null 許容型、構造体、列挙型、クロージャ。
- **6 プラットフォームのネイティブ AOT：** Mach-O（`darwin-arm64`、`darwin-amd64`）、静的 ELF（`linux-amd64`、`linux-arm64`）、PE32+（`windows-amd64`、`windows-arm64`）。
- **Mochi VM と WebAssembly：** コンパクトなバイトコードをリファレンス VM とブラウザ上のプレイグラウンドで実行可能。
- **正確な GC（Precise GC）：** Shadow Frame によるルート追跡、専用マネージドアリーナ、クロスページ割り当てを採用（`darwin-arm64` は世代別 GC、他ターゲットは正確なマーク＆スイープ）。[Runtime ABI 仕様 RA-1](runtime-abi.md) に準拠。
- **モジュール式 Bunko ライブラリ：** `yobu` で必要な `kiso/` モジュールだけを読み込み、使わないランタイム機能のコストを避けられます。
- **多言語 CLI：** BCP 47 フォールバック対応の 39 ロケール、自動テーマ切替（`--lang`、`--theme`、`NO_COLOR`）、機械可読な安定出力に対応。詳細は [docs/i18n.md](i18n.md) を参照してください。

## はじめに

### Full 版と Core 版

どちらの配布版にも `run`、`build`、`test`、`lsp` コマンドが含まれます。

- **Full 版（`mio`）：** Bunko 標準ライブラリを内蔵した自己完結型の実行ファイル（Windows は `mio.exe`）。
- **Core 版（`mio-core`）：** Bunko を内蔵しない軽量版コンパイラ。`--bunko-root` または環境変数 `MIO_BUNKO` で外部の Bunko ツリーを指定します。

### インストール

#### macOS / Linux（Homebrew）

```bash
# Full 版
brew install mio-kobo/mio/mio

# Core 版
brew install mio-kobo/mio/mio-core
```

#### Debian / Ubuntu（APT）

1. 署名用公開鍵を取得し、フィンガープリントを確認します：

```bash
curl -fsSLo mio-archive-keyring.gpg \
  [https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg](https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg)

gpg --show-keys --with-fingerprint mio-archive-keyring.gpg
```

2. リポジトリを追加してインストールします：

```bash
sudo install -m 644 -D mio-archive-keyring.gpg /etc/apt/keyrings/mio.gpg

printf '%s\n' 'deb [arch=amd64 signed-by=/etc/apt/keyrings/mio.gpg] [https://mio-kobo.github.io/mio-apt](https://mio-kobo.github.io/mio-apt) stable main' \
  | sudo tee /etc/apt/sources.list.d/mio.list

sudo apt update
sudo apt install mio       # Core 版は `mio-core`
mio version
```

アップデート：

```bash
sudo apt update && sudo apt install --only-upgrade mio
```

#### Windows（Chocolatey）

```powershell
# Full 版
choco install mio

# Core 版
choco install mio-core
```

#### ビルド済みバイナリとソースからのビルド

全 6 ターゲット向けのアーカイブを [GitHub Releases](https://github.com/mio-kobo/mio/releases) で配布しています。Go や Python、外部ツールチェーンなしで動作します。

> **macOS での注意：** GitHub Releases のプレリリース版はアドホック署名のみで公証（Notarization）を受けていません。Gatekeeper にブロックされた場合は「システム設定」から実行を許可してください。ソースからのビルドは [テストドキュメント](testing.md) を参照してください。

### 実行例

```bash
mio run hello.mio
mio test
```

## プラットフォームサポートとロードマップ

Mio 2.2 では、6 つのネイティブターゲットすべてでコンパイラビルド、セルフホスト不動点、Runtime ABI、GC、ブロッキング／ノンブロッキング TCP、Bunko 標準ライブラリを CI で検証しています。

| ターゲット | コア AOT・GC・Bunko | ブロッキング／ノンブロッキング TCP | Reactor / イベントループ | ネイティブスレッド・Channel |
| :--- | :---: | :---: | :---: | :---: |
| `linux-amd64` | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み |
| `darwin-arm64` | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み |
| `linux-arm64` | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み | 🚧 計画中 |
| `darwin-amd64` | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み | 🚧 計画中 |
| `windows-amd64` | ✅ 検証済み | ✅ 検証済み | 🚧 計画中 | 🚧 計画中 |
| `windows-arm64` | ✅ 検証済み | ✅ 検証済み | 🚧 計画中 | 🚧 計画中 |

*未対応の並行処理や Reactor 機能はシングルスレッドへフォールバックせず、コンパイル時または実行時に明示的にエラー停止（Fail-closed）します。15 項目の CI パリティマトリクスとバックログは [6 プラットフォーム・パリティマトリクス](parity/README.md) と [Bunko テストマトリクス](../tests/bunko-matrix/bunko-test-matrix.md) を参照してください。*

### 今後の予定

- **Web プレイグラウンド：** WebAssembly リファレンス VM 上で Mochi バイトコードを実行し、CI でネイティブ CLI との互換性を確認。
- **並行処理と Reactor の拡充：** ネイティブスレッド・同期・Channel を `linux-arm64`、`darwin-amd64`、Windows へ広げ、Windows 版 Reactor バックエンドを実装。
- **GC の強化：** High / Balanced / Low モードと並行 GC の追加。

## ドキュメントとマスコット

言語チュートリアル、Bunko API リファレンス、CLI マニュアル、ランタイム仕様は [Mio 公式ドキュメント](https://mio.kaelexvp.cc) にまとまっています。

**Mio**（上部イラストの少女）と **Mochi**（抱かれている相棒で、バイトコード VM と同じ名前）はプロジェクトのマスコットです。

## コントリビュート・セキュリティ・ライセンス

- 開発手順とネイティブ優先のテスト：[貢献ガイド](../CONTRIBUTING.md)、[テストドキュメント](testing.md)
- 脆弱性の非公開報告：[セキュリティポリシー](../SECURITY.md)
- コミュニティ規範：[行動規範](../CODE_OF_CONDUCT.md)

Mio のソースコードは [MIT ライセンス](../LICENSE) で公開しています。[`assets/mascot/`](../assets/mascot/) 内の Mio と Mochi のイラストは **コードの MIT ライセンスの対象外** であり、別途著作権と利用条件が適用されます。
