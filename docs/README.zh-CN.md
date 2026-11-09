[English](../README.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md)

---

<p align="center">
  <a href="../assets/mascot/mio-and-mochi.png">
    <img src="../assets/mascot/mio-and-mochi.webp" alt="Mio 与 Mochi" width="260" />
  </a>
</p>

<h1 align="center">Mio</h1>

<p align="center"><strong>小巧的核心，广阔的数学与程序世界。</strong></p>
<p align="center"><em>Minimal Core · Modular Bunko · Pay for What You Use</em></p>

<p align="center">
  <a href="https://github.com/mio-kobo/mio/releases">下载 / Releases</a>
  · <a href="https://github.com/mio-kobo/mio/actions">构建状态 / Actions</a>
  · <a href="https://marketplace.visualstudio.com/items?itemName=exmikupro.mio-language-support">VS Code</a>
  · <a href="https://plugins.jetbrains.com/plugin/35197-mio-language-support">JetBrains</a>
  · <a href="https://mio.kaelexvp.cc">官方文档 / Docs</a>
</p>

**Mio** 是一门采用日语罗马字关键字的**静态强类型编程语言**。它强调明确的数值语义、精简的语言核心，以及按需导入的 **Bunko** 标准库。

Mio 2.1 的原生 AOT 编译目标为 macOS ARM64、Linux AMD64 和 Windows AMD64。

> 最新稳定版为 [v2.0.0](https://github.com/mio-kobo/mio/releases/tag/v2.0.0)，已在 macOS ARM64 上通过验证。最新公开发布的候选版为 [v2.1.0-rc.1](https://github.com/mio-kobo/mio/releases/tag/v2.1.0-rc.1)。[`Mio-2.1.1` 开发分支](https://github.com/mio-kobo/mio/tree/Mio-2.1.1) 正在筹备 v2.1.1，正式版尚未发布。

改动与发布验证进度见 [v2.1.0 发布说明草案](releases/v2.1.0.md)。

## 语言与工具链

- **静态类型系统**：固定宽度整数、浮点数、泛型、Nullable、结构体、枚举与闭包。
- **Mochi** 字节码可在参考虚拟机中运行。
- **Native AOT**：macOS ARM64 Mach-O、Linux AMD64 静态 ELF 和 Windows AMD64 PE32+。
- **精确 GC**：Shadow Frame 根追踪、独立托管 arena 和跨页对象分配。
- **Bunko 标准库**：使用 `yobu` 按需导入 `kiso/` 模块。

Mio 与 Mochi 也是项目中的两位角色：Mio 是上方的少女，Mochi 是她怀里的小团子。Mochi 的名字同时用于字节码执行系统。

## 快速开始

### 获取 Mio

可从 [GitHub Releases](https://github.com/mio-kobo/mio/releases) 下载已发布的版本。Mio 2.1.1 的 **Full** 是内嵌完整 Bunko 标准库的单个自包含 `mio` 可执行文件（Windows 为 `mio.exe`）。**Core** 是不内嵌 Bunko 的精简编译器。两者均提供 `test` 和 `lsp` 等内置开发命令。Full 无需外部 Bunko 目录；Core 可通过 `--bunko-root` 或 `MIO_BUNKO` 使用单独安装的 Bunko。

预编译发行包无需 Go、Python 或外部开发工具链。macOS 候选版使用 ad-hoc 签名，未经过 Apple 公证，可能受到 Gatekeeper 限制。源码自举与原生验证详见[测试指南](testing.md)。

## 平台与规划

下表说明开发分支的支持情况，已通过的检查见[三平台原生 CI 结果](https://github.com/mio-kobo/mio/actions/runs/42372415752)。

| 平台或能力 | 当前状态 |
| --- | --- |
| macOS ARM64 原生 AOT | 主要目标，已通过 M18 和候选版验证。`Mio-2.1` 的原生 CI 在 `macos-15` 上运行：CLI 原生固定点、`mio run` 与进程协议、全部包的 `mio test`、Runtime ABI 契约与差分语料。 |
| Linux x86-64 原生（纯 Mio，静态 ELF） | 实验性：CI 中编译器/CLI 原生固定点、`mio run` 与进程协议、全部包的 `mio test`、Runtime ABI 契约与差分通过；`chant` 浮点值尚不支持（编译期报错，`convert.format_float64/32` 给出与各平台一致的文本） |
| Windows x86-64 原生（纯 Mio，PE32+） | 实验性。CLI 原生固定点、`mio run` 与进程协议（CreateProcessW）、全部包的 `mio test`、Runtime ABI 契约与差分语料均已通过 CI 验证。阻塞式 IPv4 `kiso/net/tcp` 由 Winsock 提供支持，CI 要求通过原生包测试及双进程回显测试。公开发布的 RC1 尚不支持 Windows TCP。浮点值仍不能直接用 `chant` 输出。 |
| Web / 浏览器 Playground | 在 WebAssembly 版参考虚拟机中运行 Mochi 字节码。[Web CI](https://github.com/mio-kobo/mio/actions/runs/42372415737) 验证原生 CLI 生成的字节码能否在该虚拟机中运行，并在 Chromium 中测试 Playground。 |
| Bunko 标准库 | 全部库公开函数都有原生测试（PR CI 的 bunko-coverage 检查）；逐函数、逐平台结果见 [Bunko 测试矩阵](../tests/bunko-matrix/bunko-test-matrix.md) |
| Linux ARM64 | 规划中 |
| 分代 GC | darwin-arm64 已实现（Object ABI v1）；x86-64 为非分代精确标记清除。两者满足同一 [Runtime ABI 契约 RA-1](runtime-abi.md) |
| High / Balanced / Low GC 策略、并发 GC | 规划中 |

[冻结的 Darwin ARM64 GF4 种子](https://github.com/mio-kobo/mio/releases/tag/gf4-seed-v1-immutable)已作为不可变的 GitHub Release 发布。GitHub Actions 仍未确认 v2.1.0 的生产级 Trusted GF4 自举通过。

完整语言教程、Bunko API、CLI 参考和运行时文档见 [Mio 官方文档网站](https://mio.kaelexvp.cc)。

## 贡献、社区与许可证

贡献流程见 [贡献指南](CONTRIBUTING.zh-CN.md)，原生测试优先与统一入口见 [测试架构](testing.md)。安全漏洞请按照 [安全政策](SECURITY.zh-CN.md) 私密报告；社区交流请参阅 [行为准则](CODE_OF_CONDUCT.zh-CN.md)。

Mio 程序代码采用 [MIT License](../LICENSE)。[`assets/mascot/`](../assets/mascot/) 中的 Mio 与 Mochi 角色插画**不属于 MIT License 授权范围**，其版权及使用许可另行适用。
