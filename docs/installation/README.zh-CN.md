# 安装 Mio 2.1

包管理器发布目前尚未启用。下列命令须等待维护者发布已验证的正式版本，
并审核分发仓库的 PR 后使用。`2.1.0-pr.<sha>` 仅为候选构建。

Full 包含原生编译器、当前支持的 Bunko 标准库和集成 CLI 工具，可以离线开发。
Core 使用字节完全相同的编译器，不附带可导入的 Bunko，能够编译不依赖标准库的程序。
两者都不需要额外下载运行时，目前也没有附带独立开发工具。

## macOS ARM64：Homebrew

```sh
brew tap mio-kobo/mio
brew install mio             # Core：brew install mio-core
mio version
brew upgrade mio             # Core：mio-core
brew uninstall mio
```

两种包都提供 `mio`，因此互相冲突。切换前请卸载原包。

## Windows AMD64：WinGet

```powershell
winget install --id MioKobo.Mio --exact
mio version
winget upgrade --id MioKobo.Mio --exact
winget uninstall --id MioKobo.Mio --exact
# Core：
winget install --id MioKobo.Mio.Core --exact
mio-core version
```

Full 使用 `mio` 命令，Core 使用 `mio-core`，安装目录相互独立。
升级和卸载 Core 时请使用 `MioKobo.Mio.Core`。
自包含 Inno 安装程序保留完整 Bunko 目录，将真实 bin 目录加入用户 PATH。
Windows Runner 验证安装后的文件清单及命令行为；卸载时移除自身的 PATH 项。
首次向微软社区仓库提交清单仍需人工审核。

## Linux AMD64：APT

仓库启用后，下载专用公钥，并通过独立可信渠道核对维护者公布的指纹：

```sh
curl -fsSLo mio-archive-keyring.gpg https://mio-kobo.github.io/mio-apt/mio-archive-keyring.gpg
gpg --show-keys --with-fingerprint mio-archive-keyring.gpg
sudo install -m 644 -D mio-archive-keyring.gpg /etc/apt/keyrings/mio.gpg
printf '%s\n' 'deb [arch=amd64 signed-by=/etc/apt/keyrings/mio.gpg] https://mio-kobo.github.io/mio-apt stable main' | sudo tee /etc/apt/sources.list.d/mio.list
sudo apt-get update
sudo apt-get install mio     # 或 mio-core
sudo apt-get update && sudo apt-get install --only-upgrade mio
sudo apt-get remove mio
```

只支持 Linux AMD64。两个包声明冲突和替换关系；切换时请检查 APT 的卸载提示。
不使用 `apt-key`，也不要关闭签名验证。正式包没有构建工具运行依赖。

## 离线使用与限制

```sh
mio build hello.mio --target=linux-amd64 -o hello
./hello
mio check math.mio
```

macOS 和 Windows 分别使用 `darwin-arm64` 和 `windows-amd64`。
CLI 默认选择当前主机目标；`run` 和 `exec` 必须使用主机目标。
三个平台均验证集成的 check/build/run/exec/test/lsp/disasm 工具。
Windows 尚无 Winsock 运行时层，`kiso/net/tcp` 标记为 NOT_APPLICABLE。

Bunko 按 `--bunko-root`、`MIO_BUNKO`、真实可执行文件相邻的 `../bunko` 顺序查找。
显式路径或环境变量无效时直接报错；空的 `MIO_BUNKO` 也是错误。
使用 Full 内置标准库时请取消该变量。迁移安装目录时应复制完整目录树。

[English](README.md) · [日本語](README.ja.md)
