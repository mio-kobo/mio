[English](../README.md) | [简体中文](README.zh-CN.md) | [日本語](README.ja.md)

---

<p align="center">
  <a href="../assets/mascot/mio-and-mochi.png">
    <img src="../assets/mascot/mio-and-mochi.webp" alt="Mio and Mochi" width="260" />
  </a>
</p>

<h1 align="center">Mio</h1>

<p align="center"><strong>A small core, a wide world of mathematics and programs.</strong></p>
<p align="center"><em>极简核心 · 模块化 Bunko · 按需付费</em></p>

<p align="center">
  <a href="https://github.com/mio-kobo/mio/releases">Releases</a>
  · <a href="https://github.com/mio-kobo/mio/actions">构建状态</a>
  · <a href="https://marketplace.visualstudio.com/items?itemName=exmikupro.mio-language-support">VS Code</a>
  · <a href="https://plugins.jetbrains.com/plugin/35197-mio-language-support">JetBrains</a>
  · <a href="https://mio.kaelexvp.cc">官方文档</a>
</p>

**Mio** 是一门使用罗马音日语作为关键字的静态强类型编程语言。它强调显式的数值语义、精简的语言核心，以及按需导入的 **Bunko** 标准库。

> **版本状态（`v2.2.0` 开发中）：** Mio 2.2 目前正处于积极开发阶段，即将替代 `v2.1.*` 成为下一个正式稳定版本，将原生提前编译（AOT）拓展至**六大平台**，并引入原生并发与事件循环原语。

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

- **静态类型系统：** 支持定宽整数、浮点数、泛型、可空类型、结构体、枚举以及闭包。
- **六大平台原生 AOT：** 支持 Mach-O（`darwin-arm64`、`darwin-amd64`）、静态 ELF（`linux-amd64`、`linux-arm64`）以及 PE32+（`windows-amd64`、`windows-arm64`）。
- **Mochi 虚拟机与 WebAssembly：** 紧凑的 **Mochi** 字节码可在参考虚拟机及浏览器在线演练场中运行。
- **精确垃圾回收（Precise GC）：** 采用 Shadow Frame 根追踪、专用托管内存区域与跨页对象分配（`darwin-arm64` 已启用分代 GC，其余平台采用精确标记-清除算法），完全遵循 [Runtime ABI 规范 RA-1](runtime-abi.md)。
- **模块化 Bunko 标准库：** 通过 `yobu` 按需导入 `kiso/` 模块，无需为未使用的运行时特性付出额外开销。
- **本地化 CLI：** 支持 39 种语言环境（遵循 BCP 47 回退规则）、自适应帮助主题（`--lang`、`--theme`、`NO_COLOR`）以及稳定的机器可读输出格式。详见 [docs/i18n.md](i18n.md)。

## 快速开始

### 发行版说明：Full 与 Core

Mio 提供两种发行版本，均集成了 `run`、`build`、`test` 与 `lsp` 等开发命令：

- **Full 版（`mio`）：** 单个自包含的 `mio` 可执行文件（Windows 下为 `mio.exe`），完整内嵌了 Bunko 标准库。
- **Core 版（`mio-core`）：** 体积更小的编译器二进制文件，不包含内嵌的 Bunko 库，需通过 `--bunko-root` 参数或 `MIO_BUNKO` 环境变量指定外部 Bunko 目录。

### 安装方式

#### macOS / Linux（Homebrew）

```bash
# 安装 Full 完整版
brew install mio-kobo/mio/mio

# 或安装 Core 精简版
brew install mio-kobo/mio/mio-core
```

#### Debian / Ubuntu（APT）

1. 下载签名公钥并通过独立可信渠道核对密钥指纹：

```bash
curl -fsSLo mio-archive-keyring.gpg \
  https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg

gpg --show-keys --with-fingerprint mio-archive-keyring.gpg
```

2. 添加软件源并安装 Mio：

```bash
sudo install -m 644 -D mio-archive-keyring.gpg /etc/apt/keyrings/mio.gpg

printf '%s\n' 'deb [arch=amd64 signed-by=/etc/apt/keyrings/mio.gpg] https://mio-kobo.github.io/mio-apt stable main' \
  | sudo tee /etc/apt/sources.list.d/mio.list

sudo apt update
sudo apt install mio       # 安装 Full 版（如需精简版请运行 `sudo apt install mio-core`）
mio version
```

后续更新：

```bash
sudo apt update && sudo apt install --only-upgrade mio
```

#### Windows（Chocolatey）

```powershell
# 安装 Full 完整版
choco install mio

# 或安装 Core 精简版
choco install mio-core
```

#### 预编译压缩包与源码构建

你也可以直接从 [GitHub Releases](https://github.com/mio-kobo/mio/releases) 下载全六端预编译二进制文件，无需安装 Go、Python 或其他外部工具链即可运行。

> **macOS 二进制说明：** GitHub Releases 上的预发布版本使用临时签名（ad-hoc signing）且未经过 Apple 公证，如遇 Gatekeeper 拦截，请在「系统设置」中手动允许运行。从源码构建请参阅 [测试文档](testing.md)。

### 快速运行

```bash
mio run hello.mio
mio test
```

## 平台支持与路线图

Mio 2.2 在 CI 中完成了六大原生目标平台的编译器构建、自举不动点（Self-hosting fixed point）、Runtime ABI、GC 正确性、阻塞与非阻塞 TCP 以及 Bunko 标准库的全面验证。

| 目标平台 | 核心 AOT、GC 与 Bunko | 阻塞与非阻塞 TCP | Reactor / 事件循环 | 原生线程与 Channel |
| :--- | :---: | :---: | :---: | :---: |
| `linux-amd64` | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 |
| `darwin-arm64` | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 |
| `linux-arm64` | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 | 🚧 计划中 |
| `darwin-amd64` | ✅ 已验证 | ✅ 已验证 | ✅ 已验证 | 🚧 计划中 |
| `windows-amd64` | ✅ 已验证 | ✅ 已验证 | 🚧 计划中 | 🚧 计划中 |
| `windows-arm64` | ✅ 已验证 | ✅ 已验证 | 🚧 计划中 | 🚧 计划中 |

*尚未支持的并发或 Reactor 特性会在编译期或运行期严格报错（Fail-closed），不会静默降级为单线程模拟。完整的 15 项特性 CI 对齐矩阵与待办清单，请参阅 [六平台特性对齐矩阵](parity/README.md) 与 [Bunko 测试矩阵](../tests/bunko-matrix/bunko-test-matrix.md)。*

### 其他目标与后续规划

- **Web / 浏览器演练场：** 在 WebAssembly 参考虚拟机中运行 Mochi 字节码，并在 CI 中与原生 CLI 保持字节码兼容性校验。
- **并发与 Reactor 全平台对齐：** 将原生多线程、线程同步与 Channel 拓展至 `linux-arm64`、`darwin-amd64` 及 Windows 平台，并补齐 Windows 平台的 Reactor 后端。
- **GC 增强：** 实现 High / Balanced / Low 三档 GC 模式以及并发 GC。

## 文档与看板娘

[Mio 官方文档站](https://mio.kaelexvp.cc) 提供了完整的语言教程、Bunko API 参考、CLI 手册以及运行时规范说明。

**Mio 与 Mochi** 也是本项目的看板娘与吉祥物：**Mio** 是页面顶部插画中的少女，她怀里的小家伙则是 **Mochi**——与 Mio 的字节码执行系统同名。

## 贡献、安全与许可证

- 贡献流程请参阅 [CONTRIBUTING.md](../CONTRIBUTING.md)，原生优先的测试规范请参阅 [testing.md](testing.md)。
- 漏洞私密报告请遵循 [SECURITY.md](../SECURITY.md)，社区行为准则请参阅 [CODE_OF_CONDUCT.md](../CODE_OF_CONDUCT.md)。

Mio 源代码基于 [MIT 许可证](../LICENSE) 开源。[`assets/mascot/`](../assets/mascot/) 目录下的 Mio 与 Mochi 插画**不受代码 MIT 许可证覆盖**，其版权与使用条款另行规定。
