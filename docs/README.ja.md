[English](../README.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md)

---

<p align="center">
  <a href="../assets/mascot/mio-and-mochi.png">
    <img src="../assets/mascot/mio-and-mochi.webp" alt="Mio と Mochi" width="260" />
  </a>
</p>

<h1 align="center">Mio</h1>

<p align="center"><strong>小さなコアから、数学とプログラムの広い世界へ。</strong></p>
<p align="center"><em>Minimal Core · Modular Bunko · Pay for What You Use</em></p>

<p align="center">
  <a href="https://github.com/mio-kobo/mio/releases">リリース</a>
  · <a href="https://github.com/mio-kobo/mio/actions">ビルド状況</a>
  · <a href="https://marketplace.visualstudio.com/items?itemName=exmikupro.mio-language-support">VS Code</a>
  · <a href="https://plugins.jetbrains.com/plugin/35197-mio-language-support">JetBrains</a>
  · <a href="https://mio.kaelexvp.cc">公式ドキュメント</a>
</p>

**Mio** は、日本語のローマ字表記をキーワードに使う**静的・強い型付けのプログラミング言語**です。数値の意味を明確にし、言語コアを小さく保ちながら、必要なものだけを取り込める **Bunko** 標準ライブラリを備えています。

**Mio V2** は Mochi バイトコードと仮想マシンに加え、**macOS Apple Silicon（ARM64）** 向けのネイティブ AOT コンパイルを実装し、Mio 製 AOT コンパイラの複数世代にわたるセルフホスト検証を完了しています。開発ブランチ `Mio-2.1` のコンパイラ、ランタイム、および 3 つのネイティブバックエンド（`darwin-arm64`、`linux-amd64`、`windows-amd64`）は Mio で実装されています。Go 製 AOT バックエンドは削除されました。

> **リリース状況：** `v2.0.0` で検証済みの最初の対応環境は macOS ARM64 です。Linux x86-64 と Windows x86-64 のネイティブバックエンドは、`Mio-2.1` の CI でセルフホストの固定点検証と Runtime ABI 契約テストを通過していますが、まだ**実験段階**であり、正式対応プラットフォームとしてはリリースされていません。

## 言語とツールチェーン

- **静的型システム：** 固定幅整数、浮動小数点数、ジェネリクス、Nullable、構造体、列挙型、クロージャ。
- **Mochi v14：** バイトコード形式と VM 実行経路。
- **Native AOT：** ARM64 マシンコードとネイティブ Mach-O 実行ファイルの生成。
- **正確な GC：** Shadow Frame によるルート追跡、専用の管理対象 arena、ページをまたぐオブジェクト割り当て。
- **Bunko 標準ライブラリ：** `yobu` によって `kiso/` モジュールを必要に応じてインポート。

Mio と Mochi はプロジェクトのマスコットでもあります。上の女の子が Mio、腕に抱かれた小さな相棒が Mochi です。Mochi の名前はバイトコード実行システムにも使われています。

## はじめに

### Mio の入手

配布されているビルドは [GitHub Releases](https://github.com/mio-kobo/mio/releases) から確認できます。

ビルド済みの配布物には **Go のインストールは不要**です。信頼済み Native Seed がない環境でソースから初回ブートストラップを行う場合、Stage A は Go の参照フロントエンドと参照 VM 上で Mio のバックエンドを動かして生成します（*go-hosted* であり、信頼済み Seed ではありません）。以降の世代はネイティブ Mio プログラムとして動作します。現行のリリース候補は **ad-hoc 署名のみで Apple の notarization を受けていない**ため、macOS の Gatekeeper によってインストールが制限される場合があります。

## プラットフォームと計画

| プラットフォーム・機能 | 現在の状況 |
| --- | --- |
| macOS ARM64 Native AOT | M18 とリリース候補の検証を通過。`Mio-2.1` のネイティブ CI（`macos-15`）で CLI のネイティブ固定点、`mio run` とプロセスプロトコル、全パッケージの `mio test`、Runtime ABI 契約と差分コーパスを実行（結果は Actions を参照）。 |
| Linux x86-64 ネイティブ（Mio、静的 ELF） | 実験段階。コンパイラ／CLI のネイティブ固定点、`mio run` とプロセスプロトコル、全パッケージの `mio test`、Runtime ABI 契約と差分コーパスが CI で通過。浮動小数点値の `chant` は未対応（コンパイル時エラー。`convert.format_float64/32` は全プラットフォームで同じ文字列を返す）。 |
| Windows x86-64 ネイティブ（Mio、PE32+） | 実験段階。CLI のネイティブ固定点、`mio run` とプロセスプロトコル（CreateProcessW）、全パッケージの `mio test`、Runtime ABI 契約と差分コーパスが CI で通過。`kiso/net/tcp`（Winsock 層なし）と浮動小数点値の `chant` は未対応。 |
| Bunko 標準ライブラリ | すべてのライブラリ公開関数にネイティブテストあり（PR CI の bunko-coverage チェック）。関数別・プラットフォーム別の結果は [Bunko テストマトリクス](../tests/bunko-matrix/bunko-test-matrix.md)。 |
| Linux ARM64 | 計画中。 |
| 世代別 GC | darwin-arm64 では実装済み（Object ABI v1）。x86-64 では非世代型の正確な mark-and-sweep GC を使用。双方が [Runtime ABI 契約 RA-1](runtime-abi.md) に準拠。 |
| High / Balanced / Low GC モード、並行 GC | 計画中。 |

言語チュートリアル、Bunko API、CLI リファレンス、ランタイムの詳しい資料は [Mio 公式ドキュメントサイト](https://mio.kaelexvp.cc) で管理しています。

## コントリビューション・セキュリティ・ライセンス

貢献方法は [コントリビューションガイド](CONTRIBUTING.ja.md)、ネイティブテストの優先方針と実行方法は [テスト資料](testing.md) を参照してください。脆弱性は [セキュリティポリシー](SECURITY.ja.md) に従って非公開で報告し、コミュニティの交流には [行動規範](CODE_OF_CONDUCT.ja.md) が適用されます。

Mio のプログラムコードには [MIT License](../LICENSE) が適用されます。ただし、[`assets/mascot/`](../assets/mascot/) にある Mio と Mochi のイラストは**コードの MIT License の対象外**であり、著作権と利用条件は別途定められています。
