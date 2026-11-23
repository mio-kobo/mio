# kiso/aot/pe

纯 Mio Windows AMD64 PE32+ executable / DLL writer。接收机器码与数据，不包含指令编码器、语言 lowering、宿主 I/O、外部链接器或 OS Adapter；不依赖未合并的 #4/#5。`dynamic_library` 接受显式 `.text` 偏移导出列表，生成排序的 `.edata`、DLL 标志及零入口点；语言入口与 ABI 限制见 `docs/windows-dll.md`。

## 接口

`executable(ImageSpec) -> ImageResult` 返回完整文件 bytes、layout 和 `PEError?`。失败返回空 bytes，不返回部分镜像。调用者负责保存成功结果。

| ImageSpec 字段 | 含义 |
| --- | --- |
| `text`, `rdata`, `data` | 原始已初始化字节；text 必须非空 |
| `zero_fill` | data 后的虚拟零填充长度，不分配对应文件字节（raw alignment padding 除外） |
| `entry` | text 内的入口字节偏移 |
| `imports` | `ImportLibrary {name, symbols}`，按输入顺序导入命名符号 |
| `patches` | `Patch {section, offset, kind, target, target_offset}` |
| `unwind` | 按 begin 升序、互不重叠的 `UnwindFunction` |

按顺序生成 `.text` RX、可选 `.rdata` R、`.data` RW、`.idata` RW、`.pdata`/`.xdata` R、`.reloc` R/discardable。空节省略；纯 BSS 的 data 节没有 raw payload 或 raw pointer。RVA、VA、文件偏移独立计算。

固定 ImageBase `0x140000000`、FileAlignment 512、SectionAlignment 4096、console subsystem、NX compatible。header 随节数计算，时间戳/保留字节为零，输出确定。file 和 virtual image 各限 64 MiB（含 headers/对齐）；最多 256 DLL、总计 4096 symbols、4096 patches、4096 unwind records。

### 补丁与重定位

source section 只能是 `text_section`、`rdata_section`、`data_section`；offset 必须落在已初始化字节内并容纳整个字段，禁止重叠。target 通常为上述三节，target_offset 可指向 BSS；target=`import_section` 时，target_offset 是按 DLL/符号输入顺序扁平化的 **IAT 索引**，自动跳过各 DLL 的 null thunk。

| kind | 写入值 |
| --- | --- |
| `rva32` | 目标 RVA，4 字节 |
| `rel32` | 目标 RVA 减去补丁字段结束位置的 RVA，signed 4 字节 |
| `va64` | ImageBase + 目标 RVA，8 字节；自动生成 DIR64 relocation |

没有绝对补丁时声明 RELOCS_STRIPPED、关闭 dynamic/high-entropy base；有 va64 时排序 source RVA、按 4 KiB page 分组、奇数 WORD 项补 ABSOLUTE，启用 dynamic/high-entropy base。调用者不能嵌入未登记的绝对地址并期待自动识别。

### 导入和展开的限制

`.idata` 包含 descriptors、独立 ILT/IAT、DLL names、2-byte 对齐的 hint/name records，所有列表有终止项。接受 1..255 字节 printable ASCII 名称，拒绝空 DLL/符号列表、重复的完全同名 DLL/符号、NUL、非 ASCII。DLL 名按字节比较，不作大小写/路径归一化。不支持 ordinal/delay imports、resources、TLS、COFF objects 或通用链接；显式函数导出由 `dynamic_library` 支持。

`UnwindFunction {begin,end,prolog_size,stack_size,stack_offset}` 的 begin/end 是 text 偏移，end 不包含在内；stack_offset 是单条栈分配指令结束时相对函数入口的偏移。仅支持无 frame pointer、无 nonvolatile saves 的单次分配：

- stack_size=0：零 codes，stack_offset 必须为 0。
- 8..128：UWOP_ALLOC_SMALL。
- 136..524280：UWOP_ALLOC_LARGE (OpInfo=0)，大小必须是 8 的倍数。

生成 DWORD 对齐的 xdata 和排序的 RUNTIME_FUNCTION 表。不支持其他展开操作/handler，也不反汇编以验证 metadata 是否符合序言。后端仍须保证序言、尾声、非易失寄存器保存和 metadata 一致。这不是完整 Windows ABI lowering。

## 验证

原生模块入口遵循已有约定，未修改 GitHub Actions：

```bash
bash bunko/kiso/aot/pe/test.sh macos-arm64 unit
```

需要项目既有受信任 Native Seed。先运行 `pe_test.mio`，再由 Seed 编译并执行 Mio fixture builder，由 Python 标准库读者独立检查产物。原生路径不降级到 Go。

Windows 开发机的明确 reference 补充路径（Go 只构建现有参考工具）：

```text
go build -o build/pe-writer/mio-reference.exe ./cmd/mio
python tests/pe-writer/run.py --mio build/pe-writer/mio-reference.exe --mode reference --execute
```

`run.py` 保存日志、compiler/writer SHA256 和分层 report.json。`--execute` 要求真实 Windows AMD64，核查四个夹具的退出码 42/43/44/45 及空 stdout/stderr；省略时执行状态是 NOT_RUN。纯结构检查：

```text
python tests/pe-writer/verify.py build/pe-writer
```

Mio builder 使用测试专用固定指令序列，不参与正式编译路径。夹具涵盖相对导入调用、绝对 IAT 地址、跨页 relocation blocks、多 DLL、纯 BSS 末尾零读取、small/large unwind 和空可选目录。独立 oracle 必须拒绝 7 个故意损坏的 PE。

2026-11-04 Windows X64 实测：14 项 Mio 测试在 Go/VM 参考工具中 PASS；6 个 PE 独立结构检查 PASS；4 个 Windows `.exe` 执行 PASS。Mio **原生**测试、macOS/Linux 原生回归、实际异常展开、强制非首选基址加载为 NOT_RUN；完整 Windows 源语言 AOT、GC 和 Selfhost 未实现。没有新增或修改 GitHub Actions workflow。

依据：[Microsoft PE/COFF](https://learn.microsoft.com/en-us/windows/win32/debug/pe-format)、[x64 exception handling](https://learn.microsoft.com/en-us/cpp/build/exception-handling-x64)。

### 2026-11-05 接入修复验证

修复分支合入 `Mio-2.1` 的 `8757f70`，保留 PE Writer 原始提交；解决 `.gitignore` 冲突时保留双方所有文档例外。模块入口使用现有 `ci_native_and_crosscheck`，原生层失败后仍收集独立层结果，最终保留非零状态。参考测试逐项检查结果，出现 skipped 时报告 NOT_RUN 并返回非零；清理/读取输入前先覆盖旧报告，同时移除旧 native builder，避免复用上次成功证据。

Windows AMD64 开发验证（Go/Python 仅为补充工具）：

- `go build -o build/pe-writer/mio-reference.exe ./cmd/mio`：PASS，当前源码参考工具。
- `python tests/pe-writer/test_driver.py`：PASS，8 项驱动正向/负向检查；模拟检查不是原生编译/执行证据。
- `python tests/pe-writer/run.py --mio build/pe-writer/mio-reference.exe --mode reference --execute`：参考 VM 14 项 PASS；6 个镜像结构 PASS；7 个损坏镜像全部拒绝；4 个 Windows fixture 退出码/输出 PASS。
- `bash -n bunko/kiso/aot/pe/test.sh`、`git diff --check`：PASS。
- `python scripts/ci/selftest.py`：FAIL，59 项中 58 项通过，1 项 ERROR。未修改的 `test_timeout_kill_survives_macos_eperm_for_zombie_only_groups` 在 Windows 读取不存在的 `os.killpg`，抛出 AttributeError；没有跳过或修改此检查。
- Mio native 模块测试、native fixture builder、macOS/Linux 原生回归、实际异常展开、强制非首选基址加载、发布验证及远端 CI：NOT_RUN。

本次未增加 Windows 源语言 AOT 或 OS Adapter；Windows fixture 执行 PASS 仅证明这些 Mio writer 产物可以在本机执行。
