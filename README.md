# Mio

Mio 是一个使用 Go 实现的静态强类型实验性编程语言。当前开发版提供完整的源码检查、字节码编译和虚拟机执行链路，适合试用语言核心语法与 Mochi 字节码格式。

这是首次公开实验版本。语法、诊断信息和字节码之外的工具接口仍可能变化，不建议用于生产环境。

## 安装

从源码构建需要 Go。本版本已使用 Go 1.22.2 验证。

```bash
cd Mio
go build -o bin/mio ./cmd/mio
./bin/mio version
```

也可以运行 `scripts/build-release.sh`，为支持的平台生成发布压缩包。构建结果写入 `dist/`。

## 快速开始

创建 `hello.mio`：

```mio
chant("Hello, Mio!");
```

检查并运行：

```bash
./bin/mio check hello.mio
./bin/mio run hello.mio
```

编译为 Mochi 字节码，再执行或反汇编：

```bash
./bin/mio build hello.mio
./bin/mio exec hello.mochi
./bin/mio disasm hello.mochi
```

`examples/` 包含变量、条件分支、函数、递归和局部作用域示例。

## CLI

```text
mio check <file.mio>
mio run <file.mio>
mio build [-o <file.mochi>] <file.mio>
mio exec <file.mochi>
mio disasm <file.mochi>
mio version
mio help
```

- `check`：执行词法、语法、名称和静态类型检查。
- `run`：检查源码，编译为字节码并执行。
- `build`：按所用特性生成兼容的 Mochi 字节码，固定宽度整数使用 Mochi v11。
- `exec`：执行 Mochi v1 至 v11 文件。
- `disasm`：显示常量、主指令流和函数指令流。

## 已实现

- `int8`、`int16`、`int32`、`int64`、`uint8`、`uint16`、`uint32`、`float32`、`float64`、`bool`、`string`、`T?`、`unit` 和一维同质数组类型语义。
- `kaku` 变量声明、赋值和词法作用域。
- `moshi` / `sore` 条件分支。
- 顶层 `waza` 函数、静态参数与返回类型、递归和相互递归。
- 泛型函数、泛型结构体、泛型枚举、函数类型与无捕获具名函数值。
- 不可变值语义结构体、带关联数据的枚举、穷尽 `awase` 分支。
- 受约束的 `Map<K, V>`、共享修改及返回 `V?` 的安全读取。
- `kaesu` 返回语句。
- 算术、比较、相等、逻辑运算和短路求值。
- `chant` 内置输出函数。
- 数组字面量、变量推断、索引读取、共享元素修改、长度查询，以及数组函数参数和返回值。
- Mochi v1 至 v11 兼容读取；v11 追加固定宽度整数常量与转换指令。
- VM 栈限制、递归限制、指令预算和运行时字节码检查。

完整语法和类型规则见 [docs/LANGUAGE.md](docs/LANGUAGE.md)，字节码格式见 [docs/MOCHI.md](docs/MOCHI.md)。

标准库的物理目录、公开命名空间和实现前置条件见 [docs/BUNKO.md](docs/BUNKO.md)。

## 尚未实现

当前版本没有字符串修改、切片、多维数组、复合值相等比较、模块、闭包、HKT、型变、trait、异常处理、包管理器、调试器或稳定的外部嵌入 API。这些能力属于后续版本规划。

## 开发验证

```bash
go test ./... -count=1
go test -race ./...
go vet ./...
git diff --check
```

## 许可证

仓库当前没有 `LICENSE` 文件。作者需要在公开发布前决定许可证；在许可证确定前，不应把本仓库描述为已采用某个开源许可证。
