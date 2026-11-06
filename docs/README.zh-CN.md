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

**Mio V2** 在 Mochi 字节码与虚拟机之外，已经实现面向 **macOS Apple Silicon（ARM64）** 的原生 AOT 编译，并完成纯 Mio 原生 AOT 编译器的多代自举验证。`Mio-2.1` 开发分支的编译器、三个原生后端（darwin-arm64、linux-amd64、windows-amd64）和运行时全部由 Mio 编写；Go 编写的 AOT 后端已删除。

> **发布状态：** `v2.0.0` 的已验证首发平台是 macOS ARM64。Linux x86-64 与 Windows x86-64 原生后端在 `Mio-2.1` CI 中通过原生自举固定点与 Runtime ABI 契约测试，但仍属**实验性**，尚未作为正式发布平台提供。

## 语言与工具链

- **静态类型系统**：固定宽度整数、浮点数、泛型、Nullable、结构体、枚举与闭包。
- **Mochi v14**：字节码格式及 VM 执行路径。
- **Native AOT**：ARM64 机器码与 Mach-O 原生程序生成。
- **精确 GC**：Shadow Frame 根追踪、独立托管 arena 和跨页对象分配。
- **Bunko 标准库**：使用 `yobu` 按需导入 `kiso/` 模块。

Mio 与 Mochi 也是项目中的两位角色：Mio 是上方的少女，Mochi 是她怀里的小团子。Mochi 的名字同时用于字节码执行系统。

## 快速开始

### 获取 Mio

正式发布后，可从 [GitHub Releases](https://github.com/mio-kobo/mio/releases)

预编译发行包**不要求安装 Go**；在没有受信任 Native Seed 的环境中从源码首次自举时，Stage A 由 Go 参考前端与参考 VM 托管纯 Mio 后端生成（go-hosted，不视为 Seed），之后各代均为原生 Mio 程序。目前发行候选采用 **ad-hoc 签名，未经过 Apple notarization**，安装时可能受到 Gatekeeper 限制。

## 平台与规划

| 平台或能力 | 当前状态 |
| --- | --- |
| macOS ARM64 原生 AOT | 已通过 M18 与发行候选验证；`Mio-2.1` 的原生 CI 在 `macos-15` 上运行：CLI 原生固定点、`mio run` 与进程协议、全部包的 `mio test`、Runtime ABI 契约与差分（结果见 Actions） |
| Linux x86-64 原生（纯 Mio，静态 ELF） | 实验性：CI 中编译器/CLI 原生固定点、`mio run` 与进程协议、全部包的 `mio test`、Runtime ABI 契约与差分通过；`chant` 浮点值尚不支持（编译期报错，`convert.format_float64/32` 给出与各平台一致的文本） |
| Windows x86-64 原生（纯 Mio，PE32+） | 实验性：CI 中 CLI 原生固定点、`mio run` 与进程协议（CreateProcessW）、全部包的 `mio test`、Runtime ABI 契约与差分通过；`kiso/net/tcp`（尚无 Winsock 层）与 `chant` 浮点值尚不支持 |
| Bunko 标准库 | 全部库公开函数都有原生测试（PR CI 的 bunko-coverage 检查）；逐函数、逐平台结果见 [Bunko 测试矩阵](../tests/bunko-matrix/bunko-test-matrix.md) |
| Linux ARM64 | 规划中 |
| 分代 GC | darwin-arm64 已实现（Object ABI v1）；x86-64 为非分代精确标记清除。两者满足同一 [Runtime ABI 契约 RA-1](runtime-abi.md) |
| High / Balanced / Low GC 策略、并发 GC | 规划中 |

完整语言教程、Bunko API、CLI 参考和运行时文档由 **Mio 官方文档网站** 统一维护。

## 贡献、社区与许可证

贡献流程见 [贡献指南](CONTRIBUTING.zh-CN.md)，原生测试优先与统一入口见 [测试架构](testing.md)。安全漏洞请按照 [安全政策](SECURITY.zh-CN.md) 私密报告；社区交流请参阅 [行为准则](CODE_OF_CONDUCT.zh-CN.md)。

Mio 程序代码采用 [MIT License](../LICENSE)。[`assets/mascot/`](../assets/mascot/) 中的 Mio 与 Mochi 角色插画**不属于 MIT License 授权范围**，其版权及使用许可另行适用。
