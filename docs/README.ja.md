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

Mio 2.1 のネイティブ AOT コンパイル対象は macOS ARM64、Linux AMD64、Windows AMD64 です。

> 最新の安定版は [v2.0.0](https://github.com/mio-kobo/mio/releases/tag/v2.0.0) で、macOS ARM64 で検証済みです。公開済みの最新リリース候補は [v2.1.0-rc.1](https://github.com/mio-kobo/mio/releases/tag/v2.1.0-rc.1) です。[開発ブランチ `Mio-2.1.2`](https://github.com/mio-kobo/mio/tree/Mio-2.1.2) では、未公開の v2.1.2 に向けて開発を進めています。

変更点とリリースに向けた検証状況は、[v2.1.0 リリースノートの草案](releases/v2.1.0.md)を参照してください。

## 言語とツールチェーン

- **静的型システム：** 固定幅整数、浮動小数点数、ジェネリクス、Nullable、構造体、列挙型、クロージャ。
- **Mochi v14：** バイトコード形式と VM 実行経路。
- **Native AOT：** macOS ARM64 の Mach-O、Linux AMD64 の静的 ELF、Windows AMD64 の PE32+。
- **正確な GC：** Shadow Frame によるルート追跡、専用の管理対象 arena、ページをまたぐオブジェクト割り当て。
- **Bunko 標準ライブラリ：** `yobu` によって `kiso/` モジュールを必要に応じてインポート。

Mio と Mochi はプロジェクトのマスコットでもあります。上の女の子が Mio、腕に抱かれた小さな相棒が Mochi です。Mochi の名前はバイトコード実行システムにも使われています。

## はじめに

### Mio の入手

公開済みの配布物は [GitHub Releases](https://github.com/mio-kobo/mio/releases) から入手できます。Mio 2.1.1 の **Full** は、Bunko 標準ライブラリー全体を組み込んだ単一の自己完結型 `mio` 実行ファイル（Windows では `mio.exe`）です。**Core** は Bunko を組み込まない小型のコンパイラーです。どちらも `test` や `lsp` などの統合開発コマンドを備えます。Full は外部 Bunko を必要とせず、Core は `--bunko-root` または `MIO_BUNKO` で別途インストールした Bunko を利用できます。

ビルド済みの配布物は Go、Python、外部の開発ツールチェーンなしで動作します。macOS のリリース候補には ad-hoc 署名が使われています。Apple の公証は受けていないため、Gatekeeper によって制限される場合があります。ソースからのブートストラップとネイティブ検証については [テストガイド](testing.md)を参照してください。

## プラットフォームと計画

以下は開発ブランチでの対応状況です。検証の詳細は [3 プラットフォームのネイティブ CI の結果](https://github.com/mio-kobo/mio/actions/runs/42372415752)を参照してください。

| プラットフォーム・機能 | 現在の状況 |
| --- | --- |
| macOS ARM64 Native AOT | 主要ターゲット。M18 とリリース候補の検証を通過。`Mio-2.1` のネイティブ CI（`macos-15`）で CLI のネイティブ固定点、`mio run` とプロセスプロトコル、全パッケージの `mio test`、Runtime ABI 契約と差分コーパスを実行。 |
| Linux x86-64 ネイティブ（Mio、静的 ELF） | 実験段階。コンパイラ／CLI のネイティブ固定点、`mio run` とプロセスプロトコル、全パッケージの `mio test`、Runtime ABI 契約と差分コーパスが CI で通過。浮動小数点値の `chant` は未対応（コンパイル時エラー。`convert.format_float64/32` は全プラットフォームで同じ文字列を返す）。 |
| Windows x86-64 ネイティブ（Mio、PE32+） | 実験段階。CLI のネイティブ固定点、`mio run` とプロセスプロトコル（CreateProcessW）、全パッケージの `mio test`、Runtime ABI 契約と差分コーパスが CI で通過。Winsock によるブロッキング IPv4 の `kiso/net/tcp` に対応し、CI ではネイティブのパッケージテストと 2 プロセス間のエコーテストを必須としています。公開済みの RC1 では Windows の TCP は未対応。浮動小数点値の直接の `chant` 出力も未対応。 |
| Web / ブラウザー版 Playground | WebAssembly 版の参照 VM で Mochi バイトコードを実行。[Web CI](https://github.com/mio-kobo/mio/actions/runs/42372415737) では、ネイティブ CLI が生成したバイトコードとの互換性を確認し、Chromium で Playground をテスト。 |
| Bunko 標準ライブラリ | すべてのライブラリ公開関数にネイティブテストあり（PR CI の bunko-coverage チェック）。関数別・プラットフォーム別の結果は [Bunko テストマトリクス](../tests/bunko-matrix/bunko-test-matrix.md)。 |
| Linux ARM64 | 計画中。 |
| 世代別 GC | darwin-arm64 では実装済み（Object ABI v1）。x86-64 では非世代型の正確な mark-and-sweep GC を使用。双方が [Runtime ABI 契約 RA-1](runtime-abi.md) に準拠。 |
| High / Balanced / Low GC モード、並行 GC | 計画中。 |

[凍結済みの Darwin ARM64 用 GF4 シード](https://github.com/mio-kobo/mio/releases/tag/gf4-seed-v1-immutable)は、変更不可の GitHub Release として公開されています。v2.1.0 の本番用 Trusted GF4 ブートストラップについては、GitHub Actions での成功はまだ確認されていません。

言語チュートリアル、Bunko API、CLI リファレンス、ランタイムの詳しい資料は [Mio 公式ドキュメントサイト](https://mio.kaelexvp.cc) で管理しています。

## コントリビューション・セキュリティ・ライセンス

貢献方法は [コントリビューションガイド](CONTRIBUTING.ja.md)、ネイティブテストの優先方針と実行方法は [テスト資料](testing.md) を参照してください。脆弱性は [セキュリティポリシー](SECURITY.ja.md) に従って非公開で報告し、コミュニティの交流には [行動規範](CODE_OF_CONDUCT.ja.md) が適用されます。

Mio のプログラムコードには [MIT License](../LICENSE) が適用されます。ただし、[`assets/mascot/`](../assets/mascot/) にある Mio と Mochi のイラストは**コードの MIT License の対象外**であり、著作権と利用条件は別途定められています。
