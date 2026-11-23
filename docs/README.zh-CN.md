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
  · <a href="https://github.com/mio-kobo/mio/actions">构建状态</a>
  · <a href="https://marketplace.visualstudio.com/items?itemName=exmikupro.mio-language-support">VS Code</a>
  · <a href="https://plugins.jetbrains.com/plugin/35197-mio-language-support">JetBrains</a>
  · <a href="https://mio.kaelexvp.cc">官方文档</a>
</p>

**Mio** 是一门以日语罗马音为关键字的静态强类型编程语言。语言核心保持精简，强调显式的数值语义，标准库 **Bunko** 按需导入。

> **`v2.2.0`（开发中）：** 作为 `v2.1.*` 的后续稳定版，Mio 2.2 将原生 AOT 编译扩展到六个平台，并加入原生并发与事件循环原语。

```mio
yobu "kiso/math";

waza square(value: float64) -> float64 {
    kaesu value * value;
}

kaku area = square(12.0);
chant(area);
chant(math.sqrt(area));
```

## 语言与工具链

- **类型系统：** 支持定宽整数、浮点数、泛型、可空类型、结构体、枚举和闭包。
- **六平台原生 AOT：** 支持 Mach-O（`darwin-arm64`、`darwin-amd64`）、静态 ELF（`linux-amd64`、`linux-arm64`）以及 PE32+（`windows-amd64`、`windows-arm64`）。
- **Mochi 虚拟机与 WebAssembly：** 紧凑的字节码可在参考虚拟机和浏览器演练场中运行。
- **精确垃圾回收：** 采用 Shadow Frame 根追踪、专用托管内存区域和跨页对象分配（`darwin-arm64` 为分代 GC，其余平台为精确标记-清除），遵循 [Runtime ABI 规范 RA-1](runtime-abi.md)。
- **模块化 Bunko 标准库：** 通过 `yobu` 按需引入 `kiso/` 模块，不引入多余的运行时开销。
- **多语言 CLI：** 支持 39 种语言环境（BCP 47 回退）、自适应帮助主题（`--lang`、`--theme`、`NO_COLOR`）和稳定的机器可读输出。详见 [docs/i18n.md](i18n.md)。

## 快速开始

### Full 与 Core 发行版

两个版本均包含 `run`、`build`、`test`、`lsp` 命令：

- **Full 版（`mio`）：** 单个自包含可执行文件（Windows 下为 `mio.exe`），已内嵌完整 Bunko 标准库。
- **Core 版（`mio-core`）：** 不内嵌 Bunko 的精简编译器，需通过 `--bunko-root` 或环境变量 `MIO_BUNKO` 指定外部 Bunko 目录。

### 安装

#### macOS / Linux（Homebrew）

```bash
# Full 版
brew install mio-kobo/mio/mio

# Core 版
brew install mio-kobo/mio/mio-core
```

#### Debian / Ubuntu（APT）

1. 下载签名公钥并核对指纹：

```bash
curl -fsSLo mio-archive-keyring.gpg \
  [https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg](https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg)

gpg --show-keys --with-fingerprint mio-archive-keyring.gpg
```

2. 添加软件源并安装：

```bash
sudo install -m 644 -D mio-archive-keyring.gpg /etc/apt/keyrings/mio.gpg

printf '%s\n' 'deb [arch=amd64 signed-by=/etc/apt/keyrings/mio.gpg] [https://mio-kobo.github.io/mio-apt](https://mio-kobo.github.io/mio-apt) stable main' \
  | sudo tee /etc/apt/sources.list.d/mio.list

sudo apt update
sudo apt install mio       # 如需精简版，可安装 `mio-core`
mio version
```

后续更新：

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

#### 预编译包与源码构建

也可以直接从 [GitHub Releases](https://github.com/mio-kobo/mio/releases) 下载六个平台的预编译压缩包，无需安装 Go、Python 或其他工具链。

> **macOS 说明：** GitHub Releases 上的预发布版本仅做临时签名（ad-hoc signing），未经过 Apple 公证；如被 Gatekeeper 拦截，可在「系统设置」中手动允许运行。从源码构建请参考 [测试文档](testing.md)。

### 运行

```bash
mio run hello.mio
mio test
```

## 平台支持与路线图

Mio 2.2 在 CI 中对六个原生目标平台验证了编译器构建、自举不动点、Runtime ABI、GC、阻塞/非阻塞 TCP 以及 Bunko 标准库。

| 目标平台 | 核心 AOT、GC 与 Bunko | 阻塞与非阻塞 TCP | Reactor / 事件循环 | 原生线程与 Channel |
| :--- | :---: | :---: | :---: | :---: |
| `linux-amd64` | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 |
| `darwin-arm64` | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 |
| `linux-arm64` | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 | 🚧 计划中 |
| `darwin-amd64` | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 | 🚧 计划中 |
| `windows-amd64` | ✅ 已验证 | ✅ 已验证 | 🚧 计划中 | 🚧 计划中 |
| `windows-arm64` | ✅ 已验证 | ✅ 已验证 | 🚧 计划中 | 🚧 计划中 |

*尚未支持的并发或 Reactor 特性会在编译期或运行期直接报错（Fail-closed），不会静默退回单线程模拟。完整的 15 项特性对齐矩阵与待办见 [六平台特性对齐矩阵](parity/README.md) 与 [Bunko 测试矩阵](../tests/bunko-matrix/bunko-test-matrix.md)。*

### 后续规划

- **Web 演练场：** 在 WebAssembly 参考虚拟机中运行 Mochi 字节码，并在 CI 中与原生 CLI 校验字节码兼容性。
- **并发与 Reactor 补齐：** 将原生多线程、同步原语和 Channel 扩展到 `linux-arm64`、`darwin-amd64` 及 Windows，并补全 Windows 的 Reactor 后端。
- **GC 改进：** 增加 High / Balanced / Low 三档 GC 模式及并发 GC。

## 文档与看板娘

语言教程、Bunko API 参考、CLI 手册和运行时规范见 [Mio 官方文档站](https://mio.kaelexvp.cc)。

**Mio**（顶部插画中的少女）和 **Mochi**（她怀里的小家伙，与字节码虚拟机同名）是本项目的吉祥物。

## 贡献、安全与许可证

- 贡献流程与原生优先测试规范：[贡献指南](../CONTRIBUTING.md)、[测试文档](testing.md)
- 漏洞私密报告：[安全政策](../SECURITY.md)
- 社区行为准则：[行为准则](../CODE_OF_CONDUCT.md)

Mio 源代码基于 [MIT 许可证](../LICENSE) 开源。[`assets/mascot/`](../assets/mascot/) 下的 Mio 与 Mochi 插画**不在代码 MIT 许可证范围内**，其版权与使用条款另行规定。
