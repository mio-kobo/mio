# Mio

Mio 是一个使用 Go 实现的静态强类型实验性编程语言。`v0.1.0-alpha.1` 提供完整的源码检查、字节码编译和虚拟机执行链路，适合试用语言核心语法与 Mochi 字节码格式。

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
- `build`：生成 Mochi v2 文件。
- `exec`：执行 Mochi v1 或 v2 文件。
- `disasm`：显示常量、主指令流和函数指令流。

## v0.1 已实现

- `int64`、`bool`、`string`、`nil` 和 `unit` 类型语义。
- `kaku` 变量声明、赋值和词法作用域。
- `moshi` / `sore` 条件分支。
- 顶层 `waza` 函数、静态参数与返回类型、递归和相互递归。
- `kaesu` 返回语句。
- 算术、比较、相等、逻辑运算和短路求值。
- `chant` 内置输出函数。
- Mochi v1 兼容读取及 Mochi v2 函数表。
- VM 栈限制、递归限制、指令预算和运行时字节码检查。

完整语法和类型规则见 [docs/LANGUAGE.md](docs/LANGUAGE.md)，字节码格式见 [docs/MOCHI.md](docs/MOCHI.md)。

## 尚未实现

当前版本没有循环、集合、用户定义类型、模块、导入、闭包、异常处理、包管理器、调试器或稳定的外部嵌入 API。这些能力属于后续版本规划，不是 v0.1 的承诺。

## 开发验证

```bash
go test ./... -count=1
go test -race ./...
go vet ./...
git diff --check
```

## 许可证

仓库当前没有 `LICENSE` 文件。作者需要在公开发布前决定许可证；在许可证确定前，不应把本仓库描述为已采用某个开源许可证。
