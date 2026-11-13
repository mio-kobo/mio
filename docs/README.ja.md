[English](../README.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md)

---

<p align="center">
  <a href="../assets/mascot/mio-and-mochi.png">
    <img src="../assets/mascot/mio-and-mochi.webp" alt="Mio and Mochi" width="260" />
  </a>
</p>

<h1 align="center">Mio</h1>

<p align="center"><strong>A small core, a wide world of mathematics and programs.</strong></p>
<p align="center"><em>最小限のコア · モジュール式 Bunko · 使った分だけ</em></p>

<p align="center">
  <a href="https://github.com/mio-kobo/mio/releases">Releases</a>
  · <a href="https://github.com/mio-kobo/mio/actions">ビルド状況</a>
  · <a href="https://marketplace.visualstudio.com/items?itemName=exmikupro.mio-language-support">VS Code</a>
  · <a href="https://plugins.jetbrains.com/plugin/35197-mio-language-support">JetBrains</a>
  · <a href="https://mio.kaelexvp.cc">ドキュメント</a>
</p>

**Mio** は、ローマ字表記の日本語をキーワードに採用した静的強型付けプログラミング言語です。明示的な数値セマンティクス、コンパクトな言語コア、そして必要なときにインポートして使う標準ライブラリ **Bunko** を重視しています。

> **開発状況（`v2.2.0` 開発中）：** Mio 2.2 は現在活発に開発が進められており、`v2.1.*` に代わる次期安定版リリースとなる予定です。ネイティブ事前コンパイル（AOT）の対象を **6つのプラットフォーム** に拡大し、ネイティブ並行処理およびイベントループのプリミティブを備えています。

```mio
yobu "kiso/math";

waza square(value: float64) -> float64 {
    kaesu value * value;
}

kaku area = square(12.0);
chant(area);
chant(math.sqrt(area));
```

## 言語仕様とツールチェーン

- **静的型システム：** 固定幅整数、浮動小数点数、ジェネリクス、Null 許容型、構造体、列挙型、クロージャをサポート。
- **6プラットフォーム対応のネイティブ AOT：** Mach-O（`darwin-arm64`、`darwin-amd64`）、静的 ELF（`linux-amd64`、`linux-arm64`）、PE32+（`windows-amd64`、`windows-arm64`）を出力。
- **Mochi VM と WebAssembly：** コンパクトな **Mochi** バイトコードはリファレンス VM およびブラウザ上のプレイグラウンドで動作。
- **正確な GC（Precise GC）：** Shadow Frame によるルート追跡、専用のマネージドアリーナ、クロスページオブジェクト割り当てを採用（`darwin-arm64` では世代別 GC、その他のターゲットでは正確なマーク＆スイープ GC を実装）し、[Runtime ABI 仕様 RA-1](runtime-abi.md) に準拠。
- **モジュール式 Bunko 標準ライブラリ：** `yobu` を使って `kiso/` モジュールを必要な分だけインポートでき、未使用のランタイム機能に対するオーバーヘッドが発生しません。
- **多言語対応 CLI：** BCP 47 フォールバック規則に基づく 39 ロケール、適応型ヘルプテーマ（`--lang`、`--theme`、`NO_COLOR`）、機械可読な安定出力をサポート。詳細は [docs/i18n.md](i18n.md) を参照してください。

## はじめに

### 配布エディション：Full と Core

Mio には 2 つの配布形態があり、どちらにも `run`、`build`、`test`、`lsp` などの開発用コマンドが統合されています：

- **Full 版（`mio`）：** Bunko 標準ライブラリを完全に内蔵した単一の自己完結型 `mio` 実行ファイル（Windows では `mio.exe`）。
- **Core 版（`mio-core`）：** Bunko を内蔵しない軽量なコンパイラバイナリ。`--bunko-root` オプションまたは `MIO_BUNKO` 環境変数で外部の Bunko ツリーを指定して使用します。

### インストール

#### macOS / Linux（Homebrew）

```bash
# Full 版をインストール
brew install mio-kobo/mio/mio

# または Core 軽量版をインストール
brew install mio-kobo/mio/mio-core
```

#### Debian / Ubuntu（APT）

1. 署名用公開鍵をダウンロードし、信頼できる別の経路でフィンガープリントを確認します：

```bash
curl -fsSLo mio-archive-keyring.gpg \
  https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg

gpg --show-keys --with-fingerprint mio-archive-keyring.gpg
```

2. リポジトリを追加して Mio をインストールします：

```bash
sudo install -m 644 -D mio-archive-keyring.gpg /etc/apt/keyrings/mio.gpg

printf '%s\n' 'deb [arch=amd64 signed-by=/etc/apt/keyrings/mio.gpg] https://mio-kobo.github.io/mio-apt stable main' \
  | sudo tee /etc/apt/sources.list.d/mio.list

sudo apt update
sudo apt install mio       # Full 版（Core 版の場合は `sudo apt install mio-core`）
mio version
```

アップデート方法：

```bash
sudo apt update && sudo apt install --only-upgrade mio
```

#### Windows（Chocolatey）

```powershell
# Full 版をインストール
choco install mio

# または Core 軽量版をインストール
choco install mio-core
```

#### ビルド済みバイナリとソースからのビルド

全 6 ターゲット向けのビルド済みアーカイブは [GitHub Releases](https://github.com/mio-kobo/mio/releases) からも入手でき、Go や Python、外部のツールチェーンなしでそのまま動作します。

> **macOS での注意：** GitHub Releases のプレリリース版バイナリはアドホック署名であり公証（Notarization）を受けていないため、Gatekeeper によって制限される場合があります。その際は「システム設定」から実行を許可してください。ソースからのビルド手順は [テストドキュメント](testing.md) をご覧ください。

### クイック実行

```bash
mio run hello.mio
mio test
```

## プラットフォームサポートとロードマップ

Mio 2.2 では、全 6 つのネイティブターゲットにおいて、ネイティブコンパイラのビルド、セルフホスティング不動点、Runtime ABI、GC の正当性、ブロッキング／ノンブロッキング TCP、および Bunko 標準ライブラリの動作を CI で検証しています。

| ターゲット | コア AOT・GC・Bunko | ブロッキング／ノンブロッキング TCP | Reactor / イベントループ | ネイティブスレッド・Channel |
| :--- | :---: | :---: | :---: | :---: |
| `linux-amd64` | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み |
| `darwin-arm64` | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み |
| `linux-arm64` | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み | 🚧 計画中 |
| `darwin-amd64` | ✅ 検証済み | ✅ 検証済み | ✅ 検証済み | 🚧 計画中 |
| `windows-amd64` | ✅ 検証済み | ✅ 検証済み | 🚧 計画中 | 🚧 計画中 |
| `windows-arm64` | ✅ 検証済み | ✅ 検証済み | 🚧 計画中 | 🚧 計画中 |

*未サポートの並行処理および Reactor 機能は、シングルスレッドへの暗黙的なフォールバックを行わず、コンパイル時または実行時に明示的なエラー（Fail-closed）として停止します。全 15 項目の CI パリティマトリクスとバックログの詳細は、[6プラットフォーム・パリティマトリクス](parity/README.md) および [Bunko テストマトリクス](../tests/bunko-matrix/bunko-test-matrix.md) をご覧ください。*

### その他のターゲットと今後の予定

- **Web / ブラウザプレイグラウンド：** WebAssembly リファレンス VM 上で Mochi バイトコードを実行し、CI にてネイティブ CLI とのバイトコード互換性を検証しています。
- **並行処理と Reactor の全プラットフォーム対応：** ネイティブマルチスレッド、スレッド同期、Channel を `linux-arm64`、`darwin-amd64`、Windows へ拡大し、Windows 版 Reactor バックエンドを完成させます。
- **GC の機能強化：** High / Balanced / Low の各 GC モードおよび並行 GC の実装。

## ドキュメントとマスコット

[Mio 公式ドキュメントサイト](https://mio.kaelexvp.cc) では、言語チュートリアル、Bunko API リファレンス、CLI マニュアル、ランタイム仕様を公開しています。

**Mio と Mochi** は本プロジェクトのマスコットキャラクターでもあります。ページ上部のイラストに描かれている少女が **Mio**、彼女が抱えている小さな相棒が **Mochi**（Mio のバイトコード実行システムと同じ名前）です。

## コントリビュート・セキュリティ・ライセンス

- コントリビュートの手順は [CONTRIBUTING.md](../CONTRIBUTING.md)、ネイティブ優先のテストプロセスは [testing.md](testing.md) をご覧ください。
- セキュリティ上の脆弱性報告は [SECURITY.md](../SECURITY.md)、コミュニティの行動規範は [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md) に従ってください。

Mio のソースコードは [MIT ライセンス](../LICENSE) のもとで公開されています。[`assets/mascot/`](../assets/mascot/) に含まれる Mio と Mochi のイラストは **コードの MIT ライセンスの対象外** であり、著作権および利用条件は別途定められています。
