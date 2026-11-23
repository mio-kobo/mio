# Windows AMD64 Native C FFI 验收

本套件必须使用真实 Windows AMD64、Native Mio CLI、MSVC 或 clang-cl/Windows SDK。`run.py` 自动读取已安装的 VS/SDK；不会安装工具，不使用 Go、CGo、VM 或模拟器替代 Native 调用。ml64 用于寄存器和展开探针，缺失时明确报告 SKIP。

```text
python tests/ffi/windows-amd64/run.py --mio <native-cli.exe> --output build/ffi/windows-amd64
```

`fixtures/mio_ffi_fixture.c` 提供固定宽度整数、按位置混合浮点、超过四参数的加权运算、void、静态指针、完整 uint64、高位边界、负零、NaN payload、Shadow Space 与栈对齐探针。`probes.asm` 实际破坏 volatile GPR/XMM；`unwind.c` 调用 Windows 自身的 RtlVirtualUnwind 检查生产桥帧。

Full 发行版另加 `--check-embedded`，在隔离目录中移除 Bunko 环境变量、不指定 `--bunko-root`，编译运行新增模块和真实 DLL 调用；CI 对 Full CLI 强制执行这两项检查。Core 发行版继续使用显式外部 Bunko。

`backend_probe.mio` 是测试用纯 Mio PE builder：在真实进程内设置并检查全部 nonvolatile GPR 和 XMM6–XMM15 的低 128 位、操作数与 RSP。它只为被测生产桥发布真实展开记录，测试入口直接调用 ExitProcess。该探针不向用户提供 C → Mio 功能。`scalar_builder.mio` 单独调用小型 `lowerwin`，验证它和正式 Heap 后端共用同一个桥。

用例还覆盖系统 API、导入去重、多 DLL、未使用声明、跨包公开声明、Winsock IAT 重映射、递归、闭包、GC stress、错误目标、错误类型/参数数量、GC 对象指针转换、冲突原型，以及 Loader 的 DLL/导出缺失错误。Loader 负向测试只禁止阻塞 CI 的系统错误对话框，仍检查真实 NTSTATUS。

报告分开记录 `*-build`、`*-pe`、`*-native`。结构检查通过不代表执行通过；缺少 C 工具或 Native CLI 时返回非零，状态保持 SKIP/NOT_RUN。每条命令保存 stdout/stderr 与退出码；`report.json` 保存编译器、DLL 和生成 PE 的 SHA-256。不要将输出目录的二进制提交到仓库。

2026-11-23 本机 Windows AMD64 验证：

| 检查 | 结果 |
| --- | --- |
| MSVC 真实 C DLL / 原生 PE 调用、系统 DLL、标量后端、Heap/GC、寄存器、展开、负向和 PE 检查 | PASS（98 个分层检查） |
| Runtime ABI RA-1：8 用例 × 普通 / GC stress | PASS（16） |
| Native differential：普通 / GC stress | PASS（64），NOT_APPLICABLE（2，原有 GC 压力排除项） |
| Go Reference / Self-hosted 前端、Codegen、Mochi、包加载、增量编译、格式化回归 | PASS |
| 新 FFI 原型元数据差分与 VM Unsupported Runtime 诊断 | PASS |
| Native 原型单元测试 / Mochi 完整包测试 | PASS（1 / 10；文件测试需正常临时目录写权限） |
| Native CI 的 `windows-c-ffi` 接入函数在本机执行 | PASS（98 项通用检查 + 2 项 Full 内嵌默认检查） |
| 纯 Mio 重生成内嵌 Bunko / LF 源码完整性 / 打包单元测试重放 | PASS（119 模块；Ubuntu WSL 打包测试 68 项） |
| Native 编译器固定点（连续自编译产物一致） | PASS |
| 远端完整 Native CI | NOT_RUN（提交 PR 后由既有 workflow 运行） |
| Linux AMD64 原生执行回归（Ubuntu WSL，Windows Native CLI 交叉构建 ELF 编译器） | PASS：RA-1 16，差分 64，NOT_APPLICABLE 2 |
| Darwin 原生执行回归 | NOT_RUN；错误目标拒绝检查 PASS |
| 可信 Native Seed 身份验证 | NOT_RUN（本地沿用已有原生候选，不将固定点等同于可信 Seed） |
| 全套 LSP Go 测试 | FAIL：未修改的 `TestPathFromURI` 将 `/tmp/a b.mio` 硬编码为 POSIX 路径，Windows 得到 `\tmp\a b.mio`；与 FFI 无关，其余专项结果不据此冒充全套通过 |

本机日志位于执行时指定的 `build/ffi/` 子目录。正式接入点是 `scripts/ci/native_platforms.py` 的 `windows-c-ffi` 必需检查；CI 会保留完整日志与 JSON，而不是读取这份说明作为成功证据。
