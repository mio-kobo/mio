# Mio

<p align="center">
  <a href="assets/mascot/mio-and-mochi.png">
    <img src="assets/mascot/mio-and-mochi.webp" alt="Mio 抱着 Mochi 小团子：蓝白长发、蓝眼睛、佩戴 M 发饰的语言形象" width="310" />
  </a>
</p>

<p align="center"><strong>小巧的核心，广阔的能力。</strong></p>
<p align="center"><em>Minimal Core · Modular Bunko · Pay for What You Use</em></p>

<p align="center">
  <a href="https://github.com/ExMikuPro/Mio/actions/workflows/mio-ci-release-with-plugin.yml"><img alt="CI" src="https://github.com/ExMikuPro/Mio/actions/workflows/mio-ci-release-with-plugin.yml/badge.svg" /></a>
  <a href="https://github.com/ExMikuPro/Mio/releases"><img alt="Release" src="https://img.shields.io/github/v/release/ExMikuPro/Mio?include_prereleases&label=release" /></a>
  <img alt="Status: experimental" src="https://img.shields.io/badge/status-experimental-blue" />
</p>

**Mio** 是一门正在开发的静态强类型编程语言，采用日语罗马音关键字，重视**数学正确性、资源效率、源码兼容性**和可按需使用的标准库。当前公开开发版使用 Go 编写编译器及 Mochi 字节码虚拟机。

> **项目状态：** 当前公开 Release 仍是 Alpha，不建议用于生产环境。仓库源码、开发分支与 Release 可能处于不同开发阶段；实际可用功能以对应分支源码和测试为准。

## Mio 与 Mochi

图中的少女是 **Mio**，她抱着的白色小团子是 **Mochi**。Mochi 也是 Mio 第一代字节码执行系统的名字。

- **v1 目标：** 完成由 Mio 编写的编译器自举，仍通过 Mochi VM 执行。
- **v2 目标：** 编译器直接生成原生机器码，不再依赖 Mochi VM 执行用户程序。
- **不变的约定：** 即使 v2 替换掉字节码执行架构，Mochi 依然是 Mio 项目角色形象与发展历史的一部分。

点击上方图片可以查看原始高清角色图。

## 快速开始

从源码构建需要 Go（仓库当前基线使用 Go 1.22.2 验证）：

```bash
git clone https://github.com/ExMikuPro/Mio.git
cd Mio
go build -o bin/mio ./cmd/mio
./bin/mio version
```

新建 `hello.mio`：

```mio
chant("Hello, Mio!");
```

运行、检查与编译：

```bash
./bin/mio check hello.mio
./bin/mio run hello.mio
./bin/mio build hello.mio
./bin/mio exec hello.mochi
./bin/mio disasm hello.mochi
```

标准库示例：

```mio
yobu "kiso/math";

kaku value: float64 = 144.0;
chant(math.sqrt(value));
```

> **Release 提示：** 当前已发布的 Alpha 安装包只包含 CLI 等基础文件，不包含完整 Bunko；从仓库构建时请保留 `bunko/`，或依工具文档配置 `MIO_BUNKO` 指向正确的标准库根目录。

## 语言与编译器能力

| 领域 | 当前仓库已实现的主要能力 |
| --- | --- |
| 语法 | `kaku`、`waza`、`moshi`、`sore`、`aida`、`kaesu` 等 |
| 类型 | 静态强类型、显式数值转换、`T?`、泛型与类型约束 |
| 数值 | `int8/16/32/64`、`uint8/16/32`、`float32/64`、`rune` |
| 数据结构 | 动态数组、Map、结构体、关联数据枚举、闭包 |
| 包系统 | 目录即包；`yobu` 导入、`koukai` 显式导出、`teisu` 编译期常量 |
| 工具链 | Lexer / Parser / TypeChecker / Compiler、Mochi VM、反汇编 |
| 扩展接口 | MMA 原生模块、字节数组边界、不透明资源句柄 |
| 开发工具 | `mio test`、结构化诊断、三语言 i18n、JetBrains IDE 插件 |

**Mochi 当前最高格式为 v14**，支持既有 v1–v14 格式读取。编译器按实际使用的特性生成相应最低兼容格式。详细规则见 [语言规范](docs/LANGUAGE.md) 与 [字节码文档](docs/MOCHI.md)。

## Bunko 标准库

Bunko 源码放在 `bunko/`，导入路径使用 `kiso/`；算法尽可能以 Mio 本身实现，确需宿主能力时使用受限原生接口。

| 模块 | 主要能力 |
| --- | --- |
| `kiso/math` | 数值比较、整数数学、浮点基础运算与常量 |
| `kiso/array` | 动态数组、追加、容量管理与浅克隆 |
| `kiso/bytes` | 字节比较、复制与 ByteBuffer |
| `kiso/utf8` | 严格 UTF-8 编解码与码点统计 |
| `kiso/string` | 字符串操作与 StringBuilder |
| `kiso/convert` | 整数、布尔、精确 IEEE 754 浮点文本转换 |
| `kiso/io` | Reader、Writer、短读写和流式复制 |
| `kiso/fs` | 同步文件和目录操作 |
| `kiso/path` | `/` 风格逻辑路径处理 |
| `kiso/encoding/binary` | 大小端整数与 LEB128 |
| `kiso/testing` | 测试断言与错误报告 |

**不用的模块不应被无谓加载，不用的函数尽可能不进入最终产物。** 详见 [Bunko 文档](docs/BUNKO.md)。

## 测试与国际化

Mio 支持同包 `_test.mio` 测试文件，正式构建不会包含测试代码：

```bash
./bin/mio test ./...
./bin/mio test ./... --run=test_gcd
./bin/mio test ./... --lang=zh-CN
./bin/mio test ./... --lang=ja-JP
./bin/mio test ./... --lang=en-US
```

Go 侧开发测试分级：

```bash
go test -short ./... -count=1
go test ./... -count=1
go test -race -short ./... -count=1
MIO_STRESS_TEST=1 go test ./... -count=1
```

测试与国际化规则见 [TESTING.md](docs/TESTING.md) 和 [I18N.md](docs/I18N.md)。诊断提供 human、short、json 格式；JSON 的字段与错误码保持稳定，不随语言切换。

## 编辑器支持

**[JetBrains / IntelliJ IDEA / GoLand](editors/jetbrains/README.md)**：Mio Language Support 插件目前已有语法高亮、补全、跳转定义、引用查找、CLI 诊断、运行配置、新项目创建以及 SDK 管理等功能。插件当前 0.6.0 的具体 IDE 版本与构建方法见链接文档。

**VS Code / LSP**：长期计划。预计复用 Mio 编译器前端，在统一语言服务中提供诊断、补全与跳转，而不是重新编写另一套编译器。

## 路线图与源码兼容

**Mio v1.0.0（开发中）**：使用 Mio 编写完整的 Lexer、Parser、Resolver、TypeChecker 和 Mochi 编译器，并完成自编译验证。

当前 `Mio-1.0.0` *本地开发分支*已完成纯 Mio 的 Lexer、Parser 和 Resolver 阶段差分验收，下一阶段为 TypeChecker。这些自举成果若尚未推送，不属于 GitHub 默认 `main` 的公开文件，不能误认为当前 Release 已包含。

**Mio v2.0.0（远期规划）**：完全自举、Native AOT、独立 Runtime / GC、可选多核并行、async/await 和隐式上下文。v2 不以 Mochi VM 作为用户程序执行后端。

**兼容承诺目标：** 经过 v1.0 正式稳定的 Mio 用户源码和 Bunko 公共 API，在 v2 应能不修改用户代码而直接重新编译。标准库内部算法、VM 与原生宿主实现以及 Mochi 二进制格式不纳入这一承诺。

版本采用独立分支开发；通过验收后，直接将对应版本分支设置为 GitHub 默认分支，不强制合并到 `main`。

## 文档导航

- [语言语法与类型规则](docs/LANGUAGE.md)
- [Bunko 标准库](docs/BUNKO.md)
- [MMA 原生模块接口](docs/MMA.md)
- [Mochi 字节码](docs/MOCHI.md)
- [测试框架](docs/TESTING.md)
- [诊断国际化](docs/I18N.md)
- [JetBrains 插件](editors/jetbrains/README.md)

## 许可证与插画

目前仓库尚未包含 `LICENSE` 文件，请在正式公开许可前确定代码许可证。

Mio 与 Mochi 的角色插画属于单独的项目形象素材。请明确插画作者、署名要求及商用/再分发授权范围；**不能默认将角色图与未来代码许可证视为同一许可**。
