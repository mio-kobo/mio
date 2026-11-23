# Darwin ARM64 原生 C ABI

Linux x86-64 的对应实现见 [Linux x86-64 原生 C ABI](linux-amd64-c-ffi.md)；两个目标共用同一套 Mio 声明语法和类型表。

Mio 在 Apple Silicon macOS 上可以从原生 Mach-O 程序调用固定原型的 C 函数。使用 C ABI 的构建需要 Xcode 或 Command Line Tools 提供的 macOS SDK、Clang 和 Apple 链接器。运行生成的程序只需要 macOS 和程序所链接的动态库。普通 Mio 构建继续由 Mio 自带的 Mach-O writer 和签名器完成。

## 声明和调用

在 `waza` 签名末尾使用 `= c "符号";` 声明外部函数。每个参数和返回值都必须标注类型，`koukai` 可以让其他 Mio 包调用该声明。符号使用 C 源码拼写，Mach-O 前导下划线由编译器添加。`main` 是 Mio 可执行文件的入口，保留给编译器。

```mio
waza process_id() -> int32 = c "getpid";
chant(process_id());
```

```sh
mio build pid.mio --target darwin-arm64 -o pid
./pid
```

编译器检查 Mio 调用是否符合声明，开发者负责使声明与 C 头文件中的真实原型一致。链接器使用符号名解析库中的实现。原型冲突会报错，调用需要的符号缺失会导致链接失败，Apple 工具链的诊断会保留在 stderr。

| C 类型 | Mio 声明类型 |
| --- | --- |
| `void` | `unit`，仅用于返回值 |
| `int8_t`、`int16_t`、`int32_t`、`int64_t` | `int8`、`int16`、`int32`、`int64` |
| `uint8_t`、`uint16_t`、`uint32_t`、`uint64_t` | `uint8`、`uint16`、`uint32`、`uint64` |
| `float`、`double` | `float32`、`float64` |
| `_Bool` | `bool` |
| `void *` | `cptr`，不透明的原生数据指针 |

Darwin ARM64 使用 LP64，C 的 `int` 对应 `int32`，`long` 对应 `int64`，`size_t` 对应 `uint64`。第一版的 `uint64` 可以传递、返回和比较相等性。显式 `uint64(x)` 将 Mio 整数按模 2⁶⁴ 转换，`int64(u)` 将全部 64 位按补码解释。`cptr(0)` 构造空指针，其他 `cptr` 值由 C 提供。Mio 可比较原生指针是否相等，解引用通过有正确原型的 C 函数完成。

## 链接用户库

```c
#include <stdint.h>

int32_t add_numbers(int32_t a, int32_t b) {
    return a + b;
}
```

```mio
waza add(a: int32, b: int32) -> int32 = c "add_numbers";
chant(add(20, 22));
```

```sh
xcrun --sdk macosx clang -arch arm64 -c add.c -o add.o
mio build add.mio --link add.o -o add
./add
```

| `mio build` 参数 | 用途 |
| --- | --- |
| `--link <file>` | 按顺序传递已存在的 ARM64 `.o`、`.a` 或 `.dylib` |
| `-L <directory>`、`-Ldirectory` | 添加库搜索路径 |
| `-l <name>`、`-lname` | 按名称链接库 |
| `-F <directory>`、`-Fdirectory` | 添加 Framework 搜索路径 |
| `-framework <name>` | 链接 Framework，例如 `CoreFoundation` |
| `--rpath <path>` | 添加运行时搜索路径，支持 `@loader_path`、`@executable_path` |

参数可以重复。请把静态库放在引用它的目标文件之后。动态库的 install name 决定 dyld 查找位置，`@rpath` 库可以配合 `--rpath` 使用。系统库 libSystem 随系统工具链链接。Mio 接受编译后的目标文件，C 源码由用户用 Clang 编译。

`mio run`、`mio exec` 和 `mio test` 可以调用默认链接的系统 C 符号。显式链接用户库时使用 `mio build` 并执行其产物。跨目标构建在发现 C 声明时会报告 `E_CABI`（Linux x86-64 只能在 Linux x86-64 主机上原生构建）。Go Reference 可以检查原型和调用，实际执行到 C 调用时会给出错误。

## 编译器与链接器

Self-hosted 解析器把外部符号放在函数声明上，类型检查器只允许固定的标量原型。Codegen 将原型写入 Mochi 的外部函数记录。记录没有 Mio 指令体，`$cabi::` 前缀把它与已有 MMA 和普通 Mio 函数区分开。Native lowering 将该记录的参数和返回值约束为标量，防止它们被当作 GC 根。

Mio 内部调用把每个值作为一个机器字传递，浮点位模式也经过通用寄存器。C 调用桥从 Mio 入参槽读值，按签名使用两组独立的寄存器槽。

1. 整数、`bool` 和指针进入 `x0–x7`。对于小于 32 位的寄存器参数，调用者按符号扩展或零扩展到 `Wn`。
2. `float32` 和 `float64` 分别进入 `s0–s7` 和 `d0–d7`。
3. 各寄存器组独立耗尽。溢出参数按声明顺序写入栈，使用本身的大小和自然对齐。整个传参区向上对齐到 16 字节。
4. 调用桥沿用 Mio 函数序言保存 FP、LR 和所用的被调用者保存寄存器。桥通过 `BL` 调用 C，Darwin 保留的 `x18` 不参与传参。整数、布尔和指针从 `x0` 取回并规范化窄值，浮点值从 `s0` 或 `d0` 原样搬回 Mio 的机器字表示。当前标量各占一个返回寄存器，多寄存器返回用于本版范围以外的类型。
5. 在线程程序中，调用桥在进入 C 前公布现有 Mio 根并进入阻塞区。返回值保存在桥的私有栈槽内，直到恢复 mutator 状态。

现有自带 Mach-O writer 负责内部地址修补。线程版在固定数据槽中绑定 libSystem 的预定义导入。扩展 writer 来链接任意 C 库时，需要支持 archive 成员选择、动态库和 Framework 的定位、外部重定位、动态绑定及签名布局。FFI 路径使用 Apple 工具链完成这些步骤。Mio 自行生成指令和内部跳转，系统汇编器只为 Mio 已生成的跨 section 地址引用和外部 `BL` 生成 Mach-O 重定位，链接器解析符号、生成需要的 stub 和动态加载信息，并提供 ad-hoc 签名。ARM64 本地链接以 macOS 11.0 为最低部署目标，所用 SDK 和用户库仍可能有各自的系统版本要求。程序正常结束时调用 libSystem 的 `exit`，由 C 库刷新 stdio。

ABI 的栈布局和窄值扩展规则见 [Apple ARM64 说明](https://developer.apple.com/documentation/xcode/writing-arm64-code-for-apple-platforms)和 [AAPCS64](https://github.com/ARM-software/abi-aa/blob/main/aapcs64/aapcs64.rst)。

## 指针和 GC

Mio GC 管理的字符串、数组、结构体、Map 和闭包不能作为 C 参数或返回类型。C 指针指向 C 控制的存储，Mio GC 不追踪该存储。谁分配、何时失效、用哪个函数释放，由被调用库的契约决定。传递指针本身不转移所有权。使用前按库的约定检查空指针，释放后应丢弃所有指向同一存储的 Mio 引用。

首版仅支持 Darwin ARM64 的 Mio → C。可变参数、结构体或 union 按值、函数指针和回调、C → Mio、C++ ABI、Objective-C Runtime 和 Swift ABI 均在范围之外。系统链接器对 Mach-O 地址重定位的加数有大小限制，超大 section 的链接可能因重定位超界失败。

下一版可先明确 C 字符串的分配、编码和释放规则，再增加显式字节缓冲区互操作。聚合类型、可变参数和 C → Mio 回调应各自设计和验证。

## 测试

```sh
MIO_NATIVE_SEED=/path/to/mio-v2.1.0-darwin-arm64 \
  bash scripts/ci/run.sh macos-arm64 kiso/aot/cabi quick
```

CI 先校验 Seed 并从当前源码重建 Native 编译器。Mio 单元测试检查原型元数据，独立验收使用 Clang 产出的目标文件、静态库和动态库，加上 libSystem 与 CoreFoundation，执行生成的签名程序。每条命令、原始输出及 PASS、FAIL、SKIP、NOT_RUN 状态进入报告目录。其他目标在该验收中只验证编译和错误诊断。

Native 工作流以非必需检查 `darwin-c-abi` 在 macOS ARM64 上运行同一脚本，日志位于 `logs/c-abi`。该检查要求报告中没有 FAIL 和 NOT_RUN，在没有 `--baseline` 编译器时唯一允许的 SKIP 是 `ordinary-binary-identity`，它不计为通过。
