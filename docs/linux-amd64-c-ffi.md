# Linux x86-64 原生 C ABI

Mio 在 Linux x86-64（AMD64）上可以从原生 ELF 程序调用固定原型的 C 函数，遵循 System V AMD64 ABI。它与 [Darwin ARM64 原生 C ABI](darwin-arm64-c-ffi.md) 共用同一套 Mio 语言接口：同样的 `waza ... = c "符号";` 声明、同样的类型表、同样的诊断，只是后端不同。含 C 声明的构建需要 Linux x86-64 主机和系统 C 编译器驱动（`cc`、`gcc` 或 `clang`）；运行生成的程序只需要 Linux 和程序所链接的共享库。没有 C 声明的程序继续由 Mio 自带的 ELF writer 生成静态可执行文件，产物与此前逐字节相同。

## 声明和调用

```mio
waza process_id() -> int32 = c "getpid";
chant(process_id());
```

```sh
mio build pid.mio -o pid
./pid
```

编译器检查 Mio 调用是否符合声明，开发者负责使声明与 C 头文件中的真实原型一致。符号使用 C 源码拼写，不加前导下划线（Mach-O 才加）。`main` 是 Mio 可执行文件的入口，保留给编译器。

| C 类型 | Mio 声明类型 |
| --- | --- |
| `void` | `unit`，仅用于返回值 |
| `int8_t`、`int16_t`、`int32_t`、`int64_t` | `int8`、`int16`、`int32`、`int64` |
| `uint8_t`、`uint16_t`、`uint32_t`、`uint64_t` | `uint8`、`uint16`、`uint32`、`uint64` |
| `float`、`double` | `float32`、`float64` |
| `_Bool` | `bool` |
| `void *` | `cptr`，不透明的原生数据指针 |

Linux x86-64 使用 LP64：`int` 为 32 位，`long` 和 `long long` 为 64 位，指针和 `size_t` 为 64 位，所以 `long` 对应 `int64`，`size_t` 对应 `uint64`。这是 glibc 与 musl 在 x86-64 上共同的数据模型。每个 Mio 类型保留各自的宽度和符号，不会被当作无类型的 64 位整数。

`uint64` 可以传递、返回和比较相等性。显式 `uint64(x)` 将 Mio 整数按模 2⁶⁴ 转换，`int64(u)` 将全部 64 位按补码解释。`cptr(0)` 构造空指针，其他 `cptr` 值由 C 提供。

## 链接

| `mio build` 参数 | 用途 |
| --- | --- |
| `--link <file>` | 按顺序传递已存在的 `.o`、`.a` 或 `.so`（含 `libfoo.so.1` 形式） |
| `-L <directory>`、`-Ldirectory` | 添加库搜索路径 |
| `-l <name>`、`-lname` | 按名称链接库，例如 `-lm` |
| `--rpath <path>` | 添加运行时搜索路径，支持 `$ORIGIN` |

这些参数与 Darwin 共用同一套命令行约定，可以重复，顺序按给出的顺序原样传给链接器，所以静态库要放在引用它的目标文件之后。`-F` 和 `-framework` 是 Darwin 的 Framework 机制，在 Linux 上直接报 `E_CABI`。`--link` 的文件、`-L` 的目录在构建开始前检查存在性。

```c
#include <stdint.h>
int32_t add_numbers(int32_t a, int32_t b) { return a + b; }
```

```mio
waza add(a: int32, b: int32) -> int32 = c "add_numbers";
chant(add(20, 22));
```

```sh
cc -c add.c -o add.o
mio build add.mio --link add.o -o add             # 目标文件
ar -rcs libadd.a add.o
mio build add.mio -L . -ladd -o add               # 静态库
cc -shared -fPIC add.c -o libadd.so
mio build add.mio -L . -ladd --rpath '$ORIGIN' -o add   # 共享库
```

`mio run`、`mio exec`、`mio test` 可以调用默认链接的 libc 符号。`libm` 等需要显式参数的库使用 `mio build -lm`。跨目标构建发现 C 声明时报告 `E_CABI`：本机没有目标平台的 C 工具链，也不会把交叉编译当作原生执行。

## System V AMD64 调用规则

Mio 内部调用约定保持不变：参数由调用者压栈，返回值在 RAX，浮点以位模式存放在 64 位槽中。每个 C 外部函数是一个普通的 Mio 函数，体内由 C 调用桥完成 Mio 槽到 System V 的转换。

### 整数参数寄存器

整数、`bool` 和指针按顺序进入 `RDI`、`RSI`、`RDX`、`RCX`、`R8`、`R9`，共 6 个。窄整数在装入寄存器前由桥扩展到完整 64 位：有符号类型符号扩展，无符号类型和 `bool` 零扩展。psABI 只要求调用者扩展到 32 位，扩展到 64 位对所有编译器都是安全的。

### SSE 参数寄存器

`float32` 和 `float64` 按顺序进入 `XMM0`–`XMM7`，共 8 个。`float32` 使用 `movd`，只有低 32 位有效；`float64` 使用 `movq`。

### 两个寄存器组独立耗尽

整数组和浮点组各自计数。例如 `double calculate(int64_t a, double b, int32_t c, float d)` 中，`a` 在 `RDI`，`b` 在 `XMM0`，`c` 在 `RSI`，`d` 在 `XMM1`。整数寄存器用完后，只有整数参数上栈，浮点参数仍然使用剩余的 XMM；反之亦然。分类逻辑是 `kiso/aot/lowerlinux` 中的纯函数 `classify`，有独立的单元测试。

### 栈参数布局

寄存器耗尽的参数上栈，按声明顺序排列，第一个栈参数位于 CALL 处的 `RSP+0`。**每个栈参数固定占一个 8 字节槽**，不论类型大小，窄类型的高位由桥填成扩展后的值。这与 Darwin ARM64 不同：Darwin 的栈参数按自然大小紧凑排列，只是整个参数区向上取整到 16 字节。整个参数区向上取整到 16 字节的倍数。

### 栈对齐

System V 要求 CALL 指令执行前 `RSP` 为 16 字节的倍数；CALL 压入 8 字节返回地址后，被调用者入口处 `(RSP + 8)` 为 16 的倍数。Mio 的操作数栈随意推进 `RSP`，进入桥时对齐没有保证，所以桥在自己的帧里执行 `and rsp, -16`，再预留 16 字节倍数的栈参数区。返回后用 `mov rsp, rbp` 恢复，所以不需要记录原 `RSP`，嵌套调用和任意深度的 Mio 帧都同样正确。测试使用只读取入口 `RSP` 的汇编探针在不同 Mio 帧深度、不同待求值操作数数量和不同栈参数个数下验证。

### 返回值

| C 类型 | 返回位置 | Mio 侧处理 |
| --- | --- | --- |
| 整数、指针 | `RAX` | 有符号类型符号扩展，无符号类型零扩展到完整 64 位；高位被丢弃，因为 psABI 不保证它们 |
| `bool` | `RAX` 低 8 位 | 规范为 0 或 1 |
| `float` | `XMM0` 低 32 位 | 位模式搬入 `RAX` |
| `double` | `XMM0` | 位模式搬入 `RAX` |
| `void` | 无 | 无 |

### 寄存器保存

`RBX`、`RBP`、`R12`–`R15` 是被调用者保存寄存器，由 C 保证保留。`RAX`、`RCX`、`RDX`、`RSI`、`RDI`、`R8`–`R11` 以及全部 `XMM` 寄存器可能被 C 破坏。Mio 的执行模型不跨调用在寄存器中保留值：操作数栈和局部变量都在栈帧里，每个 Mio 函数本身也任意使用这些寄存器，所以调用桥不需要额外保存。测试夹具中的 `mio_clobber` 把全部调用者保存寄存器和 `XMM0`–`XMM15` 改写成固定值，Mio 在其前后保持的值必须不变。

### 红区

System V 允许叶函数使用 `RSP` 以下 128 字节的红区。Mio 不在 `RSP` 以下保存任何数据，调用桥在调用前只会进一步降低 `RSP`，C 看到的 `RSP` 以下都是死区，所以 C 的 CALL 和被调用者的红区不会破坏 Mio 的栈帧。

### 可变参数

第一版不实现可变参数函数，声明中带 `...` 会在编译期以 `E_CABI` 拒绝，不会生成普通调用。ABI 对此另有要求：调用可变参数函数时，调用者必须把 `AL` 设为用到的 XMM 寄存器个数（上界即可）。将来支持时，桥要在 `call` 前写 `AL`，并让浮点参数同时遵守变参提升规则。

## ELF 与动态链接

Mio 生成的机器码本身是位置无关的 `rip` 相对代码，只有对外部符号的 `call` 需要链接器。构建时 Mio 把代码写进私有目录，生成一个汇编文件：Mio 机器码用 `.incbin` 原样放进 `.text`，只在每个外部调用处插入 `call 符号@PLT`，由系统汇编器生成 `R_X86_64_PLT32` 重定位。随后调用系统 C 编译器驱动完成汇编和链接，并保留 stderr 上的链接器诊断和退出状态。

- 符号解析：未定义符号按链接顺序在目标文件、静态库成员和共享库中解析。找不到符号时链接失败，诊断中包含缺失的符号名。
- 动态链接：外部函数经 PLT 和 GOT 调用，默认惰性绑定（`JUMP_SLOT` 重定位），由 `ld-linux-x86-64.so.2` 解析。程序依赖 libc 和用户链接的共享库。
- 非 PIE：Mio 运行时的全局数据和堆元数据位于固定地址 `0x10000000`，所以可执行文件用 `-no-pie` 链接，数据放进独立的 `.mio_bss`（NOBITS）段，用 `--section-start` 固定地址，ELF 为 `ET_EXEC`。这与没有 C 声明的静态 ELF 的布局保持一致。可执行栈被禁用（`GNU_STACK` 为 `RW`）。
- 入口：libc 的 `_start` 调用 `main(argc, argv, envp)`。Mio 入口把 `RSP` 放回 `argv - 8`，即 Linux 进程入口处期望的位置，之后只经 `exit` 结束，不会返回 libc 启动代码。
- 退出：程序正常结束和 `os.exit` 调用 libc 的 `exit`，C 的 stdio 缓冲得以冲刷，`atexit` 处理程序得以运行。运行时陷阱（如越界）仍直接 `exit_group`，不运行 C 清理。Mio 的 `chant` 直接写文件描述符，不经过 C 缓冲，所以 C 的 `printf` 输出和 Mio 的输出在管道上的先后由 C 缓冲决定；需要交错时让 C 在返回前 `fflush`。

编译器驱动按 `MIO_CC`、`CC`、`PATH` 中的 `cc`、`gcc`、`clang` 的顺序选择，不写死发行版路径。

测试环境：Debian 13，glibc 2.41，GCC 14.2，GNU ld 2.44。Clang 的验收在装有 Clang 的主机上运行，没有则报告 SKIP。musl：**未验证**。理论上只依赖标准的 `main` 入口、PLT 和 `exit`，但没有在 musl 环境中构建和运行，不声明支持。

## 指针和 GC

Mio GC 管理的字符串、数组、结构体、Map 和闭包不能作为 C 参数或返回类型，类型检查器在编译期拒绝。C 指针指向 C 控制的存储，Mio GC 不追踪该存储。谁分配、何时失效、用哪个函数释放，由被调用库的契约决定；传递指针本身不转移所有权。使用前按库的约定检查空指针，释放后应丢弃所有指向同一存储的 Mio 引用。Mio 只比较指针是否相等，解引用通过有正确原型的 C 函数完成。不允许整数与指针之间的任意转换，唯一的构造是 `cptr(0)`。

调用桥不是 GC 安全点：C 不能回调 Mio，调用期间没有 Mio 轮询，所以不会发生回收，Mio 栈上的根保持原位；回收只发生在 Mio 函数入口和循环回边。因此调用桥不需要帧描述符，也不会让 C 看到未追踪的引用。C 函数可能阻塞，当前是单个 mutator，阻塞期间整个程序等待，不影响回收正确性。

含 C 调用的程序不能同时使用 OS 线程：线程运行时用裸 `clone` 和 FS 基址 TLS，会破坏 libc 的线程状态，构建时以 `E_CABI` 拒绝。

## 当前不支持

- 结构体或联合按值传递和返回、聚合返回（`E_CABI`）。
- 可变参数函数（`E_CABI`）。
- 函数指针和 C → Mio 回调、`long double`、复数、向量类型。
- C++ ABI。
- Linux ARM64、macOS x86-64、Windows 上的 C 声明：编译期报 `E_CABI`，不回退到 Mio 的内部调用约定。
- 含 C 调用的程序使用 OS 线程。
- 交叉构建含 C 声明的程序。
- musl（未验证）。

## 与 Darwin ARM64 的差异

| | Linux x86-64 | Darwin ARM64 |
| --- | --- | --- |
| 整数参数寄存器 | `RDI RSI RDX RCX R8 R9`（6 个） | `x0`–`x7`（8 个） |
| 浮点参数寄存器 | `XMM0`–`XMM7` | `s0`–`s7` / `d0`–`d7` |
| 栈参数 | 每个 8 字节槽 | 按自然大小对齐紧凑排列 |
| 窄整数 | 调用者扩展，桥扩展到 64 位 | 调用者扩展到 `Wn` |
| 对齐 | CALL 前 `RSP` 16 字节对齐，需要动态 `and` | `SP` 始终 16 字节对齐 |
| 目标文件 | ELF，`R_X86_64_PLT32` | Mach-O，`BRANCH26` |
| 链接器 | 系统 C 驱动（GNU ld、lld 等） | Apple clang 和链接器 |
| 线程 | 与 C 调用互斥 | 通过阻塞区与 C 调用共存 |

两个后端互不引用：Linux 的分类和发射在 `kiso/aot/lowerlinux/cabi_sysv.mio`，Darwin 的在 `kiso/aot/lower/cabi_darwin.mio`；共享的只有声明、类型、符号和原型校验（`kiso/aot/cabi`）。`darwin-arm64` 目标选择 Apple ARM64 规则，`linux-amd64` 选择 System V AMD64 规则，其他目标没有后端。

## 测试

```sh
python3 tests/ffi/linux-amd64/run.py --mio /path/to/current-source/mio --output build/ffi-linux
```

或通过统一入口：

```sh
MIO_NATIVE_SEED=... bash scripts/ci/run.sh linux-amd64 kiso/aot/cabi quick
```

测试使用主机的 GCC 和（如果有）Clang 编译夹具，生成目标文件、静态库和共享库，再用从当前源码构建的 Mio 编译程序、实际执行并核对输出。覆盖整数、有符号和无符号窄整数、`float`、`double`、混合参数、超过 6 个整数参数、超过 8 个浮点参数、栈参数位置和对齐、整数/浮点/void 返回、无参函数、连续调用、寄存器保存、原生指针、静态库、共享库、libc 和 libm，以及 `--gc-stress`。ELF 检查使用 `readelf` 和 `ldd`。负向测试覆盖其他目标、变参、结构体按值、回调、无效指针转换、缺失符号、缺失库、错误的链接参数和线程组合。每条命令、原始输出及 PASS、FAIL、SKIP、NOT_RUN、NOT_APPLICABLE 状态进入报告目录。
