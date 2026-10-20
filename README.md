<p align="center">
  <a href="assets/mascot/mio-and-mochi.png">
    <img src="assets/mascot/mio-and-mochi.webp" alt="Mio 与 Mochi" width="260" />
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

**Mio V2** 在 Mochi 字节码与虚拟机之外，已经实现面向 **macOS Apple Silicon（ARM64）** 的原生 AOT 编译，并完成纯 Mio 原生 AOT 编译器的多代自举验证。

> **发布状态：** `v2.0.0` 正在准备正式 GitHub Release，尚未发布为 Stable。当前已验证的首发平台是 macOS ARM64；Linux、Windows 和分代 GC 属于后续规划。

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

预编译发行包**不要求安装 Go**；从源码重新构建工具链和生成首次自举 Seed 仍需要 Go。目前发行候选采用 **ad-hoc 签名，未经过 Apple notarization**，安装时可能受到 Gatekeeper 限制。

### Hello, Mio!

创建 `hello.mio`：

```mio
chant("Hello, Mio!");
```

安装 CLI 后，在包含该文件的目录中执行：

```bash
mio version
mio check hello.mio
mio run hello.mio
```

### 使用 Bunko

```mio
yobu "kiso/math";

kaku value: float64 = 144.0;
chant(math.sqrt(value));
```

`bunko/` 是标准库的物理目录；源码中的导入路径使用 `kiso/math`，**不是** `bunko/kiso/math`。

## 原生自举

在 **M18** 验收中，Mio 完成了 `Seed → Stage 1 → 2 → 3 → 4 → 5` 的原生编译链。Stage 1–4 均已实际执行，Stage 2/3/4 的未签名 Mach-O 产物逐字节一致；Stage 5 已生成并签名。

正式自举链中，**Go AOT 只负责生成初始 Seed**，此后的核心原生代码生成由纯 Mio 实现。256 MiB 受限堆自编译还验证了真实 GC 触发后，输出仍与固定点一致。

这证明了 Mio 原生 AOT 后端的自托管能力，**不代表从 `.mio` 源码开始的完整工具链已完全摆脱 Go**。

## 平台与规划

| 平台或能力 | 当前状态 |
| --- | --- |
| macOS ARM64 原生 AOT | 已通过 M18 与发行候选验证 |
| Linux ARM64 / x86-64 | 规划中 |
| Windows x86-64 | 规划中 |
| 分代 GC、High / Balanced / Low | 规划中，未包含在 v2.0.0 |

完整语言教程、Bunko API、CLI 参考和运行时文档由 **Mio 官方文档网站** 统一维护。

## 许可证

Mio 程序代码采用 [MIT License](LICENSE)。[`assets/mascot/`](assets/mascot/) 中的 Mio 与 Mochi 角色插画**不属于 MIT License 授权范围**，其版权及使用许可另行适用。
