# Mio

Mio 是一门静态强类型编程语言，使用日语罗马音关键字，提供模块化 Bunko 标准库、Mochi VM 和 Native AOT 工具链。当前源码版本为 **2.0.0**，正式发布状态以 [GitHub Releases](https://github.com/mio-kobo/mio/releases) 为准。

V2 提供 macOS ARM64 原生编译、精确 Mark-Sweep GC 和 Shadow Frame。M18 纯 Mio Mochi→Mach-O 编译器已完成原生自托管：Stage 2/3/4 逐字节一致，并通过 256 MiB 堆上的真实 GC 自编译见证。Native AOT 支持范围与 VM 不完全相同，详见[发行说明](docs/releases/v2.0.0.md)。

目前唯一经过发行验收的平台是 **macOS Apple Silicon（ARM64）**；尚未建立最低 macOS 版本承诺。

从 [Releases](https://github.com/mio-kobo/mio/releases) 选择可用的 V2 版本，下载对应归档及 `SHA256SUMS`。以下以 `2.0.0` 为例，仅在该版本发布后适用；RC 请替换为完整 RC 版本号。安装目录须为新目录：

```sh
shasum -a 256 -c SHA256SUMS
mkdir -p "$HOME/.local/opt"
tar -xzf mio-2.0.0-darwin-arm64.tar.gz -C "$HOME/.local/opt"
export MIO_HOME="$HOME/.local/opt/mio-2.0.0-darwin-arm64"
export PATH="$MIO_HOME:$PATH"
export MIO_BUNKO="$MIO_HOME/bunko"
mio --version
```

保留完整安装目录。预编译发行包使用时不需要安装 Go；其中 CLI、VM、LSP 与 Go AOT 工具包含 Go runtime。纯 Mio `native-aot` 及其生成的原生程序不依赖 Go runtime。源码重建工具链和首次 Seed 需要 Go（基线见 `go.mod`）。当前使用 ad-hoc 签名，尚未提供 Developer ID 签名或公证。

将以下代码保存为 `hello.mio`：

```mio
chant("Hello, Mio!");
```

```sh
mio run hello.mio
```

完整文档由[官方文档网站](https://mio.kaelexvp.cc)统一维护。发布元数据与必要验收记录保留在源码仓库。

代码采用 [MIT License](LICENSE)。`assets/mascot/` 中的 Mio 与 Mochi 角色插画不属于 MIT 授权，其使用许可需单独取得。
