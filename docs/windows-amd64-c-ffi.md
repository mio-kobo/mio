# Windows AMD64 原生 C ABI / FFI

Mio 的 Windows AMD64 原生程序现在可以通过 PE32+ 导入表调用固定原型的 C DLL 函数。此功能仅用于 Mio → C，目标必须是 `windows-amd64`。编译和执行路径保持纯 Mio；没有 CGo、外部 COFF 链接器或 Go Runtime 依赖。C 编译器仅用于构建验收夹具。

## 后端审计与接入位置

`selfhost/cli` 经 `selfhost/pipeline/windows.mio` 使用 `kiso/aot/lowerwinrt`，包括普通构建、Mochi 输入、测试镜像和 GC stress。`selfhost/windows-compiler`、`selfhost/native-aot-windows` 也使用该后端。`lowerwin` 仍是单独的小型标量后端，继续保留原有能力限制；它能对其支持的标量程序调用同一 C ABI 桥。浮点、堆对象和闭包程序使用正式的 `lowerwinrt` 路径。

两个 Windows AMD64 后端共同使用 `kiso/aot/win64abi`，原型协议共同使用 `kiso/encoding/win64c`。现有 AMD64 编码器、PE Writer、`ImportLibrary`、`Patch`、IAT、`rel32` 与帧指针展开记录全部复用。没有修改 PE Writer、Runtime ABI、GC 布局或普通 Mio 调用约定。其他后端仅增加目标拒绝检查，没有增加 Windows C ABI lowering。

## 声明与跨包调用

```mio
waza current_process_id() -> uint32 = c "kernel32.dll" "GetCurrentProcessId";
waza current_process() -> cptr = c "kernel32.dll" "GetCurrentProcess";
waza process_id(handle: cptr) -> uint32 = c "kernel32.dll" "GetProcessId";
chant(current_process_id() == process_id(current_process()));
```

`c` 是签名后的上下文标记，不是新保留关键字。必须声明 DLL、全部参数类型和返回类型，以分号结束，不允许 Mio 函数体。第二个字符串可省略，此时导出符号使用声明的 Mio 名称。`koukai waza` 可以从其他包引用，绑定、可见性、重复名称和参数检查沿用普通声明规则。相同 DLL/导出的兼容声明可共用 IAT，冲突原型会拒绝。

```mio
koukai waza mio_c_no_args() -> int32 = c "mio_ffi_fixture.dll";
```

DLL 必须是 1–255 字节的 ASCII 基名并以 `.dll` 结尾；不允许路径、冒号、NUL、控制字符、空白或协议分隔符。导出名也限 1–255 字节，精确保留大小写。编译器不检查本机是否安装了该 DLL，便于交叉构建；不存在的 DLL/导出由 Windows Loader 以 `0xC0000135` / `0xC0000139` 拒绝。

## LLP64 类型映射

| C / Windows 类型 | Mio 声明类型 | 宽度 |
| --- | --- | --- |
| `void` | `unit`，仅返回值 | 无 |
| `int8_t` / `uint8_t` | `int8` / `uint8` | 8 |
| `int16_t` / `uint16_t` | `int16` / `uint16` | 16 |
| `int32_t` / `uint32_t` | `int32` / `uint32` | 32 |
| `int64_t` / `uint64_t` | `int64` / `uint64` | 64 |
| `short` / `unsigned short` | `int16` / `uint16` | 16 |
| `int`、`long`、`BOOL` | `int32` | 32 |
| `unsigned int`、`unsigned long`、`DWORD` | `uint32` | 32 |
| `long long` | `int64` | 64 |
| `float` / `double` | `float32` / `float64` | 32 / 64 |
| `void *`、不透明 `HANDLE` | `cptr` | 64 |
| `size_t`、`uintptr_t` | `uint64` | 64 |
| `intptr_t` | `int64` | 64 |
| `wchar_t` | `uint16` | 16 |

不能沿用 Unix LP64 的 `long` 宽度；`char` 的符号由实际 C 原型决定，应显式选择 `int8` 或 `uint8`。C `_Bool` 与 Windows `BOOL` 不同：这一版不接受 Mio `bool` 作为外部原型类型；需要 `_Bool` 时请提供固定整数原型的 C 包装函数，不能将其声明成 `BOOL`。

`uint64` 在这一版支持完整 64 位传输、返回和相等比较；不提供无符号算术或大小比较。`uint64(integer)` 按模 2⁶⁴ 转换，`int64(value)` 按补码解释全部位。这不等于用 `int64` 代替所有 C 类型：窄整数、浮点、指针在源码中均有不同类型，窄整数返回独立规范化，浮点不做整数数值转换。

## 参数、Shadow Space 和栈

Windows x64 与 System V 使用不同的参数寄存器、寄存器保存规则和栈布局。这里实现 Microsoft x64 Calling Convention：

| 从零开始的位置 | 整数 / 指针 | 浮点 |
| --- | --- | --- |
| 0 | RCX | XMM0 |
| 1 | RDX | XMM1 |
| 2 | R8 | XMM2 |
| 3 | R9 | XMM3 |
| ≥4 | 栈上的 8 字节槽 | 栈上的 8 字节槽 |

例如 `(int64, float64, int32, float32)` 使用 RCX、XMM1、R8、XMM3。寄存器编号取决于参数位置，不能分别累加整数和浮点参数。

桥从既有 Mio 参数槽读取值，不改变普通函数参数布局。调用前先保存 RBP，再以 RBP 为锚将 RSP 向下对齐至 16，分配 `align16(32 + max(0,n-4)*8)` 字节。前 32 字节始终留给 C 被调用者保存寄存器参数，即使没有参数也不能省略。第 5 个参数在调用前的 `[RSP+32]`，进入 C 后是 `[RSP+40]`；后续每槽 8 字节。所有 C `CALL` 前保证 `RSP % 16 == 0`。

当前原型限制最多 256 个参数，使临时栈区低于一页，避免新增未经实现的栈探测需求。返回后用既有 `lea rsp,[rbp]; pop rbp; ret` 恢复 Mio 栈。RAX 用于整数/指针，XMM0 用于浮点；`unit` 不读取返回寄存器。窄有符号返回符号扩展，无符号返回零扩展；float32/float64 按位搬运，保留负零和 NaN payload。

## 寄存器与展开信息

桥只使用 RAX、RCX、RDX、R8–R11 和 XMM0–XMM3 等 volatile 寄存器。RBP 保存后恢复。RBX、RDI、RSI、R12–R15、XMM6–XMM15 都不被桥改写；RDI/RSI 在 Windows 是 nonvolatile，不能套用 System V 的保存规则。Mio 活跃操作数与局部值仍在原有栈槽里，因此不需要另设通用寄存器分配器的 call-clobber 模型。

每个桥使用现有 PE `FrameFunction`，序言为 `push rbp; mov rbp,rsp`，展开码为 PUSH_NONVOL RBP 与 SET_FPREG。其后的动态栈对齐依赖帧指针恢复，不伪造固定 ALLOC 展开码。没有扩展 Writer 的展开格式。验收通过真实 Windows `RtlLookupFunctionEntry` / `RtlVirtualUnwind`，在不同操作数深度下验证 RIP、RSP、RBP 的恢复；另一真实 PE 探针逐个检查全部非易失 GPR 和 XMM6–XMM15 的低 128 位。

## Mochi、DLL Import 与确定性

沿用 Mochi 现有空指令 Native Stub 的名称槽，使用保留协议：

```text
$win64c|DLL|导出名|返回类型|参数类型...
```

它是显式外部元数据，不是普通 Mio 函数名称。Go 与纯 Mio reader 都验证原型、空函数体、局部槽数、捕获数和返回标志。函数表中的声明保留元数据，但不生成 Mio 函数体。调用继续使用既有 FunctionID 和调用指令，原生后端为可达外部声明生成共享桥。Go VM 真正调用该 stub 时返回 `E_FFI_RUNTIME`；未调用的合法声明可以保留。

所有声明先验证，只有可达声明进入导入表。外部导入按 DLL/名称确定性排序；DLL 身份不区分大小写，保留选定拼写，导出名严格区分大小写；同 DLL 合并，同导出去重。既有 Runtime 的 DLL/符号顺序保留。新增 KERNEL32 符号可能移动第二个 DLL 的扁平 IAT 索引，因此最终统一重映射 Runtime 补丁，再消解外部补丁。真实 Winsock 与 C FFI 混合用例覆盖该情况。

所有调用使用 PE Writer 管理的 `call [rip+rel32]` 和 `import_section`，没有未登记的绝对 C 地址或额外重定位。

## 指针所有权与 GC 边界

`cptr` 是不透明、非 GC 管理的数据指针。Mio 只能通过 `cptr(0)` 构造空指针，其他值由 C 返回；可以保存并传回 C，或比较相等，不支持整数地址构造、解引用和指针算术。静态 C 数据由 DLL 管理；C 分配的内存必须按库约定调用对应释放函数，释放后不得再次使用。句柄按对应 API 的所有权协议管理。

不允许把 Mio String、Array、Map、Struct、Closure 或 GC 对象转换成 `cptr`。没有自动 `char*` 转换、编码转换、Pinning、GC 地址获取或托管内存异步借用。这使 C 调用不改变 Shadow Frame 和 GC Root：Mio 根留在调用者原有槽中，桥不分配托管内存、不触发 GC。C 回调、C → Mio 和异步借用托管内存均没有协议，不能使用此接口实现。原生指针的分配、保存与释放约定仍由库的调用者负责。

## 明确不支持

Variadic、不完整原型、C Struct/Union 按值、函数指针回调、C++ ABI/异常/RTTI、COM vtable、`__vectorcall`、SIMD 聚合、C → Mio、DLL 导出、通用 `.lib`/`.obj` 链接、延迟导入、序号导入、动态 LoadLibrary/GetProcAddress FFI。标准 Win64 `__cdecl` / `__stdcall` 不生成 32 位 x86 栈清理逻辑。

开发者必须根据 C 头文件声明真实原型。PE 导出表没有类型信息，编译器无法证明任意第三方 DLL 的实现与声明一致；原型写错或 DLL 违反 ABI 仍可能导致进程损坏。Windows Loader 决定依赖 DLL 的实际查找位置，这一版不增加新的 DLL 搜索策略。

## 测试和 CI

```text
python tests/ffi/windows-amd64/run.py --mio <Native Mio CLI.exe> --output build/ffi/windows-amd64
python tests/runtime-abi/run.py --mio <Native Mio CLI.exe> --target windows-amd64 --output build/ffi/runtime-abi
python tests/runtime-abi/run.py --suite differential --mio <Native Mio CLI.exe> --target windows-amd64 --output build/ffi/native-differential
```

驱动查找现有 MSVC 或 clang-cl/Windows SDK，构建真实 DLL，通过 Native CLI 编译 Mio，再执行生成的 PE。可用 ml64 时还运行 volatile 破坏、全部 nonvolatile 保存和真实 Windows 展开探针。工具缺失返回 `SKIP` / `NOT_RUN` 和非零退出码，不回退 VM 或模拟器。报告记录每个检查、stdout/stderr、命令退出码、编译器/DLL/PE SHA-256 与独立 PE 导入/展开检查。

`scripts/ci/native_platforms.py` 的 Windows AMD64 follower 使用同一个 Native CLI 执行该驱动，报告名为 `windows-c-ffi`；其原有 workflow 会上传报告和日志，非 PASS 的必需检查使 CI 失败。继续沿用现有候选编译器与 Seed 验证流程，本改动没有新增 Seed 身份、下载源或验证旁路。可信 Seed 验证与本地原生行为验证应分别报告。

Full 的 `selfhost/resources/bunko_data.mio` 已用现有纯 Mio `selfhost/resourcegen.generate` 在 Git LF 源码输入上重新生成，119 个生产模块的内容、长度与 SHA-256 完整匹配。新增模块不能只存在于磁盘 Bunko，而遗漏内嵌发行路径。CI 在隔离目录中用 Full 默认内嵌 Bunko 编译并执行 FFI；Core 保留显式外部 Bunko 路径。

2026-11-23 本机 Windows AMD64 验收详见测试目录 README。Linux AMD64 已在 Ubuntu WSL 实际执行 RA-1 16 项和差分 64 项，另 2 项为原有 NOT_APPLICABLE。Darwin 原生执行和远端完整 CI 未完成时保持 `NOT_RUN`，交叉构建本身不能代替执行。

依据：[Microsoft x64 Calling Convention](https://learn.microsoft.com/en-us/cpp/build/x64-calling-convention)、[Microsoft x64 ABI Conventions](https://learn.microsoft.com/en-us/cpp/build/x64-software-conventions)。

## 修改文件清单

以下为本次实现、集成与测试的源码清单，生成的二进制和本机日志不提交：

```text
.gitignore
bunko/kiso/aot/lower/lower.mio
bunko/kiso/aot/lower/types/types.mio
bunko/kiso/aot/lowerlinux/lowerlinux.mio
bunko/kiso/aot/lowerwin/lowerwin.mio
bunko/kiso/aot/lowerwinrt/lowerwinrt.mio
bunko/kiso/aot/win64abi/bridge.mio
bunko/kiso/encoding/mochi/decode.mio
bunko/kiso/encoding/mochi/win64c_test.mio
bunko/kiso/encoding/win64c/prototype.mio
bunko/kiso/encoding/win64c/ci-module.json
bunko/kiso/encoding/win64c/prototype_test.mio
bunko/kiso/encoding/win64c/test.sh
docs/windows-amd64-c-ffi.md
internal/ast/ast.go
internal/compiler/builtin.go
internal/compiler/compiler.go
internal/ffi/win64.go
internal/incremental/compiler.go
internal/incremental/fingerprint.go
internal/lsp/semantic.go
internal/mochi/validate.go
internal/parser/parser.go
internal/typecheck/checker.go
internal/typecheck/definitions.go
internal/typecheck/ffi_test.go
internal/typecheck/signature.go
internal/typecheck/type.go
internal/vm/vm.go
scripts/ci/native_platforms.py
selfhost/codegen/codegen_diff_test.go
selfhost/codegen/compiler.mio
selfhost/codegen/ffi_diff_test.go
selfhost/parser/statement.mio
selfhost/parser/types.mio
selfhost/pipeline/compiler.mio
selfhost/pipeline/linux.mio
selfhost/resolver/resolver.mio
selfhost/resolver/resolver_diff_test.go
selfhost/resources/bunko_data.mio
selfhost/typecheck/checker.mio
selfhost/typecheck/environment.mio
selfhost/typecheck/expression.mio
selfhost/typecheck/statement.mio
selfhost/typecheck/type.mio
selfhost/typecheck/typecheck_diff_test.go
tests/ffi/windows-amd64/README.md
tests/ffi/windows-amd64/backend_probe.mio
tests/ffi/windows-amd64/cases/cross_package/api/api.mio
tests/ffi/windows-amd64/cases/cross_package/main.mio
tests/ffi/windows-amd64/cases/float_bits.mio
tests/ffi/windows-amd64/cases/heap.mio
tests/ffi/windows-amd64/cases/pointers.mio
tests/ffi/windows-amd64/cases/registers.mio
tests/ffi/windows-amd64/cases/scalar_backend.mio
tests/ffi/windows-amd64/cases/scalars.mio
tests/ffi/windows-amd64/cases/system.mio
tests/ffi/windows-amd64/cases/winsock.mio
tests/ffi/windows-amd64/fixtures/mio_ffi_fixture.c
tests/ffi/windows-amd64/fixtures/probes.asm
tests/ffi/windows-amd64/fixtures/unwind.c
tests/ffi/windows-amd64/run.py
tests/ffi/windows-amd64/scalar_builder.mio
```
