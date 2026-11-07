[English](../CHANGELOG.md) | [简体中文](CHANGELOG.zh-CN.md) | [日本語](CHANGELOG.ja.md)

---

# 更新日志

## 2.1.0 — 未发布

- Windows AMD64 通过 Winsock 支持阻塞式 IPv4 TCP。原生 CI 要求通过 TCP 包测试及双进程回显测试。
- 稳定版 Core 和 Full 从标签对应的源码构建。分发的编译器哈希与 GF4 自举及原生固定点绑定，生成包管理器元数据前须验证签名证据。
- 验证状态详见 [v2.1.0 发布说明草案](releases/v2.1.0.md)。

## 2.1.0-rc.1 — 2026-11-07

### 发行包与发布状态

- 首个 2.1 候选版，不是正式稳定版。macOS ARM64 是主要目标；Linux 和 Windows AMD64 仍为实验性。
- 三平台提供 Core / Full 压缩包，Windows 另提供自包含安装程序。Full 携带外置 Bunko 源码；Core 导入标准库时需要外部 Bunko。安装后不依赖 Go 或 Python。
- 支持基于真实可执行文件位置查找 Bunko、显式覆盖优先级、安装迁移、集成工具和真实包管理器生命周期验证。
- RC 在原生编译前写入准确版本与源码提交，并运行完整候选验证。正式包管理器发布仍拒绝预发布版，并要求真实的 Trusted Seed PASS 证据。
- 已知限制：Windows TCP 不可用；Linux/Windows 不支持直接 `chant` 浮点值（可用 `convert.format_float64/32`）；macOS 为 ad-hoc 签名，未经 notarization。并发 Old-GC 默认未开启。

### Runtime ABI 与原生工具链

- Runtime ABI 契约 **RA-1**（[docs/runtime-abi.md](runtime-abi.md)、`bunko/kiso/runtime/abi`）：为 darwin-arm64、linux-amd64 和 windows-amd64 规定统一的保证，包括精确根追踪、对象生命周期、内存安全和所有引用写入路径。现有的两种对象布局被明确命名为不同配置：Darwin 上的 Object ABI v1（分代 GC），以及 Linux/Windows 上的 x64-marksweep-v1。本次没有修改对象布局。
- `mio build --gc-stress` 会为目标平台选择相应的 GC 压力模式。
- 契约测试（`tests/runtime-abi`）和 VM/Native 差分测试集（`tests/native-differential`）在三个目标上原生运行，分别覆盖默认模式与 `--gc-stress` 模式。
- 修复了共享原生类型分析中的两个问题：自引用的 Nullable 结构体在递归类型合并时丢失可空性，导致 `nil` 判断被折叠为“非空”；一个最初被识别为 `mu` 的全局变量在首次读取字段时分析失败。这些问题影响全部原生后端。
- 修复了 Linux 和 Windows 上运行时陷阱信息长度异常的问题：`nil reference`、`index out of range` 曾被截断，`invalid call` 曾过长。

- **行为变更（darwin-arm64）：** 原生程序退出时不再自动打印最后一个顶层表达式语句的值。所有目标上的编译程序现在只输出显式的 `chant` 内容；参考实现中的 `mio run` 仍保留类似 REPL 的显示行为。
- Darwin 运行时：操作系统内存压力级别不再每次分配都查询，而是最多每 256 次分配查询一次，并在每轮 GC 后查询。此前每次分配调用一次 `sysctlbyname`，使分配密集型程序约三分之一的时间耗在内核中。该优化使用了原先未使用的元数据字（偏移 2880）。

### CI

- `.github/workflows/native.yml`（Full CI 也会调用）：在 `ubuntu-24.04`、`macos-15` 和 `windows-2025` 上验证原生编译器和 CLI 的固定点、原生 `mio test`、Runtime ABI 契约及差分测试集；同时记录 Runner 的系统与架构、Stage A 的 SHA-256 以及各阶段哈希。Stage A 是 go-hosted（Go 参考 VM 运行纯 Mio 的 Linux lowering）；受信任的 GF4 Native Seed 被记录为 `NOT_RUN`。

### 移除

- 移除了 Go AOT 后端及工具（`internal/aot`，包括 Go+C 的 Linux Runtime blob、`cmd/mochi-aot`、`cmd/mochi-aot-audit`、`examples/aot_m8..m10` 生成器）、Go M18/BCD 脚本、归档的 Go 发布工作流，以及 CI 模块 `go-aot-reference`、`legacy-bootstrap`、`go-aot-linux-amd64`、`go-linux-native` 和 `linux-runtime-reference`。这些原生能力的覆盖已转移到纯 Mio 的原生工作流。Go 参考前端和 VM 仍保留，分别作为 go-hosted Stage A 的宿主与 VM 对照实现。
