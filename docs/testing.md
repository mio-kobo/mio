# Mio CI V2：测试架构

**Mio's native testing framework is the primary testing mechanism. External tools and other programming languages are permitted for supplementary testing and independent cross-validation only.**

本分支为 `Mio-2.1`。普通模块、标准库、Core、AOT 和 GC 首先使用 Mio 原生测试；关键模块再运行具有独立判断能力的检查。Go 参考实现及 Python/Shell 仅用于补充测试、交叉验证和基础设施自检。框架不支持的类型应记录限制，不能因外部语言方便而替换 Mio 测试。

## 结构

```text
scripts/ci/
  run.sh / runner.py           发现模块、环境检查、日志与 JSON 汇总
  common.sh                   模块协议及 ci_native / ci_check
  selftest.sh / selftest.py    负向自检
  verify_native.py            独立原生输出、ABI、GC 遥测
  release_isolation.py        发布源码与包清单隔离检查
  test.sh                     quality 基础设施检查
  native_platforms.py         三平台 Native 验证驱动（.github/workflows/native.yml）
tests/runtime-abi/run.py       Runtime ABI 契约（RA-1）与 VM/Native 差分语料的逐目标运行器
selfhost/cli/test.sh           core：Mio 测试 → 独立原生验证
selfhost/test.sh               selfhost：原生测试全集
selfhost/bootstrap/test.sh     Mio 测试 → 原生 ABCD 固定点及复核
bunko/kiso/aot/test.sh          AOT：Mio 测试 → 原生产物/ABI 检查
bunko/kiso/runtime/gc/test.sh   GC：Mio 测试 → 实际 GC 遥测
bunko/kiso/**/test.sh           各个已有 Mio 测试的标准库包
internal/test.sh               go-reference，补充参考实现
tools/native-ci/test.sh        原生 archive 测试 → 既有生产流水线 → 独立清单检查
```

每个入口旁的 `ci-module.json` 是注册信息。调度自动发现 internal、bunko、selfhost、tools 和 scripts/ci 下的清单，无需修改中央模块列表。源码目录不迁移。Actions 负责调度、依赖安装和证据上传，测试业务由仓库脚本负责。

## 使用接口与平台

```bash
bash scripts/ci/selftest.sh
bash scripts/ci/run.sh macos-arm64 core quick
bash scripts/ci/run.sh macos-arm64 aot unit
bash scripts/ci/run.sh macos-arm64 gc quick
bash scripts/ci/run.sh macos-arm64 all quick
bash scripts/ci/run.sh macos-arm64 all full
bash bunko/kiso/math/test.sh macos-arm64 unit
bash scripts/ci/run.sh linux-amd64 go-reference quick
# 需要原有生产 Seed、冻结输入和可用的 macOS 沙箱：
bash scripts/ci/run.sh macos-arm64 native-release release
```

参数为 `<target> <module[,module...]|all> <suite>`，`all` suite 等价于 `full`；逗号分隔选择会在同一次运行中复用编译器和工具，逐模块保留日志与失败。模块单独调用为 `<target> [suite]`，默认 quick。脚本内部切换到仓库根目录。示例是已实现的接口；本次 Windows 环境未执行 macOS/Linux 流程，实际状态见 [调度审计](ci-scheduling-audit.md)，不能把示例视为通过声明。

| target | 实际主机 | 能力 |
| --- | --- | --- |
| macos-arm64 | Darwin arm64 | Mio 原生框架、AOT、ABI、GC、自举及辅助检查（Seed 模块）；native.yml 的 darwin-arm64 原生验证 |
| linux-amd64 | Linux x86_64 | Go/VM 补充参考测试；native.yml 的 linux-amd64 原生验证（编译器/CLI 固定点、mio test、契约、差分） |
| windows-amd64 | Windows x64 | 仅 native.yml 的 windows-amd64 原生验证（CLI 固定点、`mio run`/进程协议、`mio test` 全部包、契约、差分）；`run.sh` 统一入口未接入 |

会核对真实 OS 和架构。交叉编译不等于原生执行：darwin/windows 的 Stage A 由 Linux 原生 CLI 交叉构建，只在对应 runner 上执行后才计为 PASS。

开发驱动要求 Bash、Python 3.9+、Git；参考测试另需 Go（版本见 go.mod）。原生测试先用主机提供的 `MIO_NATIVE_SEED` 构建当前工作树的 `selfhost/cli`，整次运行只构建一次，再由各模块复用当前源码编译器。GF4 Seed 的 SHA256 必须为：

```text
4c3f7dcbea536d756c3c4a93c18a999412625184bd5c90123f1c16a0cd9e36da
```

该约定来自现有 `tests/m19ef/README.md`、ABCD 和生产流水线，不新增 Go Seed，也不下载不明原生产物。所有 Mio 原生测试显式传递当前 `--bunko-root`，避免读到 Seed 内嵌的历史库。缺少 Seed 或哈希不符返回 NOT_RUN/2，不降级到 Go 冒充原生。系统 `codesign` 只用于签名验证；真正的原生代码生成、链接、签名由现有 Mio 实现负责。

## 模块与等级

| ID | suites | 内容 |
| --- | --- | --- |
| core | quick, unit, full | `selfhost/pipeline` 原生 Mio 测试；独立编译/执行、9 参数内部 ABI、输出和错误断言检查 |
| aot | quick, unit, full | `bunko/kiso/aot/...` 原生测试；独立 Mach-O、签名和输出检查 |
| gc | quick, unit, full | `bunko/kiso/runtime/gc` 原生测试；独立 1 MiB 堆容器工作负载及 GC 遥测 |
| selfhost | unit, full | 原生 `selfhost/...` 全集 |
| bootstrap | bootstrap, full | 先原生 codegen 测试，再运行并独立复核现有 `tests/m19ef/bootstrap-abcd.sh` |
| native-release | release | 原生 archive 测试，复现正式生产沙箱和构建/安装检查，再独立审核真实包 |
| quality | quick, full | gofmt/vet/whitespace、负向自检、发布隔离静态检查 |
| go-reference | quick, unit, full | 补充 Go/VM 回归；full 保留 VM 回归和 `go test` 全仓库（`Mio/selfhost/bootstrap` 除外，见下） |
| go-bootstrap-reference | full（standalone） | `go test ./selfhost/bootstrap`：参考 VM 中的 Compiler A→B→C→D 自编译，Linux 约 56 分钟、macOS 更久；独立长时间模块，Full 用单独的 job 运行 |
| kiso/* | quick, unit, full | 每个已有 `*_test.mio` 包的原生测试；实际清单见各模块注册文件 |

标准库已接入 aot/arm64、aot/lower、aot/lower/types、aot/macho、array、bytes、convert、crypto/sha256、encoding/binary、encoding/json、encoding/mochi、fs、io、math、net/tcp、os、path、regexp、regexp/unicode_data、runtime/cpu/aarch64、runtime/os/darwin、runtime/target、string、testing、utf8，以及单列的 gc。`all` 调度时仅声明 `required_checks=["native"]` 的 AOT 子包可通过 `covered_by` 委托父级递归检查，单独报告 SKIP 和委托原因；arm64、macho 的独立 oracle 必须运行。显式选择子模块仍运行自己的入口。

完整 Quick 包含已有库测试和 Core/AOT/GC 的基本双重验证。PR 先由 `scripts/ci/plan.py` 生成计划：文档修改只跑 hosted quality；普通标准库 `*_test.mio` 局部修改跑 Core、受影响包及 Go/VM 基线；所有实现代码、Core/ABI/GC/AOT、测试调度/清单、未知或无法获取的 diff 均回退完整 Quick。没有启用缩小范围的依赖图猜测。每次计划保存选中项 NOT_RUN、未选中项 SKIP 和原因，实际执行后再验收，不能将计划当作测试通过。

`ci-pr.yml` 的 hosted infrastructure 统一运行 quality；Linux 运行计划中的补充参考模块；GitHub-hosted `macos-15` 在 Native Verification 暂缓期间运行适用 macOS 的补充（Go / VM 参考）模块，Native Verification 启用后另有独立的 native job 运行原生模块。避免 Quick 在 macOS 再跑相同 quality 和跨平台 Go/VM 基线；Full 仍保留两平台上的适用回归。Quick 不运行完整自举或生产验证。Go 参考工具由模块可选的 `reference_tools` 声明，在一次运行内按需构建并复用，go-reference 只需 mio；其他模块缺省保留原三工具。不跨提交缓存编译器或测试 PASS。

Full 增加完整 selfhost、原生 ABCD、Go/VM 参考全量测试，以及三平台 Native 验证（`native` job 调用 `native.yml`）。自举顺序执行，Go 包并行度为 1 或 2。原生路径使用受信任 Seed 构建的当前源码编译器。`ci-full.yml` 对 `Mio-2.1` push、手动及每周调度调用 `all full`，保留完整 Go/VM 参考检查，并调用 `native.yml`，不使用 continue-on-error。Go AOT 后端（internal/aot、mochi-aot）及其 go-aot-reference、legacy-bootstrap、go-aot-linux-amd64、go-linux-native、linux-runtime-reference 模块已删除，其原生覆盖由 native.yml 承担（见下节）。定时任务显式 checkout `Mio-2.1`；手动仅允许 Mio-2.1，其他 ref 由 hosted policy job 拒绝并标 NOT_RUN。Full 不根据 PR 影响范围缩减。

并发：macOS 现在使用 GitHub-hosted `macos-15`，每个 job 都是全新的独立环境，因此取消了 `ci-resource-macos-arm64-shared` 这种为保护单台共享主机而设的全局串行组，job 级并发组已全部移除。Quick 外层按 PR number 分组并取消同 PR 旧提交（`cancel-in-progress: true`），不同 PR 的 Quick 互不影响；Full 外层仍按 ref 分组，`cancel-in-progress: false`、`queue: max`，等待中的 Full 不会被替换，等待数上限为 100，不会无限增长。hosted macOS 并行 job 数受 GitHub 账户配额限制。Release 工作流不变，仍使用既有持久化主机。

`native-release release` 是显式发布验证，因生产冻结输入和较高资源需求，不加入每个 PR。正式 `.github/workflows/release.yml` 保持不变。

## 两层验证与覆盖边界

每个关键模块的 `required_checks` 声明原生层和独立层；原生层使用 `kind=mio`，报告为 `layer=native`。独立层为单独命令，报告为 `layer=independent`。某层未执行时补记 NOT_RUN，不会自动继承另一层的 PASS。补充 Go 检查为 `layer=supplementary`。

Core/AOT 独立路径调用正式 Native Seed 编译 9 参数函数、数组和字符串；验证 ARM64 Mach-O 文件头和签名，再在空 PATH 环境执行，比较固定 stdout、空 stderr 和退出码。同时构造故意失败的 Mio 测试，并独立核查非零退出及 JSON failed 计数。重复调用 Mio 测试不算独立验证；这里的输出 oracle 不调用 testing.expect。

GC 独立路径复用现有 `automatic-roots/builder.mio`，由纯 Mio 生成受限堆程序。检查容器工作负载输出 `4797`，并解码 64 字节遥测：minor collections 必须大于 0，根发布、注册/退出和资源获取/释放必须满足已有协议。这是有实际回收的有限场景，不能代表长期、多 mutator、并发 old sweep 或所有内存策略均得到验证。

ABCD 继续使用现有、带冻结源和哈希/签名复核的脚本；不重写自举算法。依赖 Go AOT 的历史 BCD/M18 gate 已随 Go AOT 后端删除（它们只能验证已废弃的 Go 引导路径，且在当前源码上已不可复现）；纯 Mio 的固定点由 native.yml 在三个平台上验证。

Bunko 每个库公开函数（`koukai waza`）都必须有覆盖它的原生测试：`scripts/ci/bunko_matrix.py --check`（quality 的 bunko-coverage 检查）从源码生成函数清单并核对 `*_test.mio`、`tests/native-differential` 用例与 `bunko_matrix_coverage.json` 中登记的 runner 协议（子进程、exec、exit、测试通道）。native.yml 的 summary 用三平台原生报告生成逐函数矩阵（`bunko-test-matrix.json/.md`，提交版见 [tests/bunko-matrix/bunko-test-matrix.md](../tests/bunko-matrix/bunko-test-matrix.md)），覆盖的库函数在任一平台非 PASS（UNSUPPORTED 需登记技术原因）即失败。部分 nativefs/sign 辅助包通过原生库/集成路径间接覆盖；不创建空入口。selfhost 阶段全集有原生测试，但尚未逐阶段配置独立 oracle。外部 FFI/完整 ABI、长期 GC 压力、新平台后端仍未覆盖。原有 E5/E6 等脚本保留，其硬编码 Seed、固定 fixture、分支或恢复要求不被本次静默改写；未接入项应明确记录 NOT_RUN。

## 模块接口

新模块只需添加相邻的 `test.sh` 和 `ci-module.json`，并保证真的有测试：

```json
{"module":"kiso/example","targets":["macos-arm64"],"suites":["quick","full","unit"],"required_checks":["native"]}
```

脚本设置 CI_MODULE，source 公共脚本，定义 `ci_module(target,suite)`，末尾调用 `ci_main "$@"`。参考 `bunko/kiso/math/test.sh`。普通模块先调用 `ci_native native <package>`；关键模块再用 `ci_check command <independent-name> <argv...>` 调用独立 oracle。Go 只能用于明确的补充检查。禁止空脚本、`true` 占位或 `|| true` 掩盖失败。

公共层提供 MIO_CI_MIO（由当前源码构建的原生编译器）、MIO_CI_SEED（已验证的初始 Seed，仅供既有 ABCD/生产流程）、MIO_CI_TOOLS（仅开发参考工具）、MIO_BUNKO、MIO_CI_MODULE_DIR。`ci_check go` 要求 Go JSON lines，`ci_check mio` 要求 Mio JSON，且均拒绝无测试/全跳过。不使用 eval 或 Shell 字符串拼接执行用户参数。

## 报告与排错

每次创建独立 `build/ci-results/<timestamp>-<id>/report.json`。schema_version=1，含 target、module、suite、status、duration_ms、checks、artifacts；模块 checks 内分别保存两层状态、command、exit_code、stdout/stderr 路径，Go 测试还保存逐测试状态。Mio stdout 保留原始完整 JSON。

| 状态 | 含义 |
| --- | --- |
| PASS | 定义的必需检查确实执行并通过 |
| FAIL | 执行失败或超时 |
| SKIP | 明确的平台/等级策略或委托跳过，附原因 |
| NOT_RUN | 环境/参数/协议问题、空测试或必需检查未执行 |

0 为所有选定必需检查通过；1 为测试失败；2 为环境、参数或基础设施错误。显式指定不支持的组合返回 2。`all` 可以包含明确策略 SKIP，但不能把它们合并为 PASS；某个必需检查 NOT_RUN 不得成功。Go 的条件跳过保留明细，部分通过不代表覆盖所有原生路径。

先查看 report.reason，再看 module.log 和独立 stdout/stderr。Python 捕获真实退出码，避免 tee 吞掉失败；POSIX 超时终止进程组，嵌套检查器转发终止信号；Windows 自检通过 taskkill /T /F 清理进程树。中断也返回非零并记录 NOT_RUN，不能把部分完成的运行写成 PASS。默认单检查超时 2400 秒、模块 7200 秒；Full Go 全集检查为 6600 秒，Go 包自身为 60 分钟，避免原有自举测试因少于 30 分钟的测试期限而跳过，支持 MIO_CI_CHECK_TIMEOUT/MIO_CI_MODULE_TIMEOUT；MIO_CI_RESULTS 可更改报告父目录。PYTHON 可指定解释器。生产环境不需要这些变量。

CI 命令可以在相同平台/架构本地原样执行，不依赖 GitHub 环境变量。Release 的受信任 Seed 仍由既有主机提供；开发用 Quick/Full 暂不使用 Seed（见下文“暂行措施”）。缺少 Seed 时不得宣称原生 CI 已运行。

## 外部工具隔离与发布验证

外部工具只能作为 Development / CI Dependencies；不得加入 Core/标准库正式实现、链接进编译器或运行时、成为发行版的运行依赖。依赖方向为测试工具调用 Mio，Mio 不反向调用测试工具。新增测试驱动不参与正式源码构建或自举，也不改变历史引导架构。

`python scripts/ci/release_isolation.py` 静态检查：正式工作流无新增外部依赖安装；保留空 PATH 和 production.sb；Core 导入图不引入测试目录；嵌入资源不含外部驱动；生产 bundle 仅包含 mio/LICENSE/README.txt。这个静态 PASS 不能替代实际发布构建。

`native-release release` 先执行原生 archive 测试，再在原有生产沙箱内运行纯 Mio 流水线（PATH 为空，不能执行 Python/Go/Shell），由现有 Seed 完成源码重建、固定点、原生测试、打包、安装、已安装编译器编译和应用运行。最后在沙箱外独立验证真实 tar.gz 只含允许的三个普通文件、无外部运行时标记。外部审计脚本不会进入包。若生产冻结 fixture 或 Seed 不齐，则如实失败/NOT_RUN，不能用伪造 tar 包声称 Release 通过。

## 负向自检

`bash scripts/ci/selftest.sh` 无需 Go/Seed。Windows 可直接 `python scripts/ci/selftest.py`，若 PATH 上无 Bash 则管道及真实模块调度标为 SKIP。覆盖错误断言、错误输出、异常退出、缺工具、错误参数/平台/组合、超时、空 Go/Mio 测试、全跳过、子失败聚合、框架假阳性、混入测试驱动的包，以及失败命令经过 tee。

真实端到端用例会调用 Bash 模块和检查器，核对成功/失败/空模块的 0/1/2、最终 JSON 和日志。聚合其他分支使用注入子进程结果验证协议。Core oracle 另使用真实 Mio 框架运行故意失败断言。这样证明故意错误会返回非零；本次没有提交、推送或触发远端 CI，不能宣称已观察 GitHub 状态变红。

## CI V2 新增协议与验证

`report.json` 的 `builds` 单独记录当前源码编译器及参考工具构建命令、耗时、日志和状态；模块因构建失败而无法执行时为 NOT_RUN，构建失败仍令总报告 FAIL。`summary` 分别统计四种模块状态，`exit_code` 保存驱动退出码。参数错误、依赖缺失、空模块、全跳过、无效 JSON 和缺少必需层均不能得到成功结果。测试失败与基础设施错误并存时总状态保持 FAIL，退出码 2 仍表示存在基础设施问题。

ARM64 编码器、Mach-O Writer 可以单独执行原生测试及独立 backend oracle。该 oracle 直接调用模块生成固定指令字及含 NUL 的常量，Python 比较固定 ARM64 words、Mach-O 头、768 字节入口及常量布局，签名后执行生成程序，要求退出码 42；另执行九参数 ABI/输出检查。Core 的独立检查还拒绝非法词法、语法、未定义符号和类型错误。Runtime CPU 模块具有原生 IR 测试与实际运行程序交叉检查；GC 保留真实回收遥测。关键模块共用 `ci_native_and_crosscheck`，原生层失败后仍尝试独立层，最终返回失败。

`ci-module.json` 可声明 `requires`（该模块额外工具）、`required_checks`（全部 suite 的必需检查）和 `suite_checks`（按 suite 增加必需检查）。可选字段 `standalone: true` 表示长时间独立模块：`all` 不运行它（在报告中记为带原因的 SKIP），只在被显式点名时运行；`module_timeout`（秒，≤21600）覆盖该模块的整体超时。缺失依赖记录 NOT_RUN/2；单独请求不支持的平台/等级也返回 2。模块 ID 从仓库相邻清单发现，不依赖 GitHub 环境。

尚未独立接入的库包包括 array、bytes、convert、fs、os、string、testing、utf8、aot/lower/nativefs、aot/sign、runtime/ir 和 regexp/unicode_data：当前没有自己的 `*_test.mio`，通过调用者部分覆盖，不能声称逐包独立测试通过。`net/tcp.before-patch` 是历史快照；E5/E6 长压力、并发 GC、外部 FFI ABI 尚未整体接入。未跟踪的 PE 开发文件属于用户正在进行的工作，不由此次 CI 改造注册或修改。

工作流和本地验证证据见 [本次审计](testing-audit.md)。macOS 原生新 oracle 未在本 Windows 主机执行，不能把静态检查通过等同于原生通过。

Windows 开发 checkout 的逐字节 Unicode diagnostic golden 由 `.gitattributes` 固定 LF；Go 格式检查仅在临时副本规范 CRLF，不改变任何源码或断言。框架负向自检现有 47 项，记录真实跳过，测试取消不会生成成功报告。

## 可靠性审计补充

`python scripts/ci/runner.py inventory` 在任何开发主机静态列出全部模块及 Linux/macOS Quick/Full 的 RUN / SKIP / DELEGATED 计划，不执行语言测试。Bunko 中新增 `*_test.mio` 而没有相邻清单会阻止调度并返回 NOT_RUN/2；缺少 test.sh、无效目标或委托关系也不能成功。

报告增加 source_revision、source_worktree_dirty、模块 engine 和检查 kind。native 模块必须确实记录 Mio 原生检查，参考 Go 的成功不能代替它。JSONL 追加使用跨进程锁，防止并行检查丢失记录。需要强制执行的 Go 检查使用 `ci_check go-required <name> ...`：任何 SKIP 或空测试都返回 NOT_RUN/2；普通 go 检查保留平台基线跳过，不能由其 PASS 推断全部子测试已运行。

PR workflow 的 `Mio CI / required`（Supplementary CI Gate；名称保持不变以兼容分支保护）根据计划汇总 infrastructure、reference、macos（hosted 补充）作业；Native Verification 启用时同时要求 native 作业。它核验执行报告中的模块集合、源码 SHA、平台、suite 和结果。必需 job 跳过、取消或报告缺失/过期不能成功。文档范围允许显式 SKIP 语言测试，其 PASS 只代表文档所需检查。维护者应在分支规则中将 gate 设为 required；本地修改不会更改远端规则。暂缓期间 gate 的 PASS 不代表 Native Verification 成功，报告的 `native_verification` 字段固定为 NOT_RUN，并在 Step Summary 与 `::warning` 中显示。独立 infrastructure job 不依赖 Native Seed；Quick/Full artifact 名称和目录包含 run ID、attempt、target 或用途，gate 仅下载小型 report artifact，完整日志仍保留。ABCD 上传也只匹配本次运行的目录。

本次问题分类、实际命令与 NOT_RUN 边界见 [可靠性审计](ci-reliability-audit.md)；未合并模块的 test.sh/ci-module.json 方案见 [PR 接入方案](ci-pr-integration.md)。历史 testing-audit.md 保留为上次工作的记录，不能替代本次运行证据。

Quick / Full / Release 三级设计、原架构审计、资源对比和管理员部署清单见 [三级 CI 调度审计](ci-scheduling-audit.md)。本地 actionlint 1.7.7 需显式兼容已受 GitHub 支持的 concurrency.queue；不能把未经兼容的 raw lint 失败写成 PASS。

## 三平台 Native 验证（native.yml）

`.github/workflows/native.yml`（PR、`runtime-abi/**` push、手动，以及 Full 的 `native` job）运行 `scripts/ci/native_platforms.py`，每项检查记录 engine：`native`（原生 Mio 镜像在该 runner 上执行）、`reference`（Go 参考 VM）、`go-hosted`（Stage A 引导主机）、`host`（runner 自身检查）。状态为 PASS / FAIL / SKIP / NOT_RUN / NOT_APPLICABLE，只有 PASS 计为通过；必需检查非 PASS 时 `Native / required` 失败。

| 平台 / runner | 检查 |
| --- | --- |
| linux-amd64 / `ubuntu-24.04` | 实际 OS/arch；**Stage A = Go 参考前端 + Go VM 中运行的纯 Mio lowerlinux（go-hosted，不是 Seed）**；原生 compiler A→B→C→D（B=C=D）；compiler B 构建 CLI，CLI A→B→C→D（B=C=D）；CLI 冒烟；全部含 `*_test.mio` 包的原生 `mio test`（跳过记为 SKIP 并单列原因）；Runtime ABI 契约与差分语料（默认与 `--gc-stress`）及其 VM 差分；`linux-runtime-chain` oracle；由 CLI B、C 两代交叉构建 darwin/windows Stage A（必须一致，SHA-256 写入 manifest，非 PR 时做 Artifact Attestation） |
| darwin-arm64 / `macos-15` | 实际 OS/arch；Stage A SHA-256 与 Linux job 输出及 manifest 三方一致；原生 CLI A→B→C→D（B=C=D），A=B（交叉构建与本机构建逐字节一致）；冒烟；全部包的原生 `mio test`（此平台任何 SKIP 都是失败）；契约与差分（默认与 `--gc-stress`）。此路径不使用 Go |
| windows-amd64 / `windows-2025` | 同 darwin 的 Stage A 校验、CLI 固定点、A=B、冒烟、契约与差分；`mio test` 为 NOT_APPLICABLE（Windows 运行时未实现进程 spawn，同一批包在 Linux/macOS 原生运行）。此路径不使用 Go |

受信任 GF4 Native Seed 在每个报告中固定为 NOT_RUN：B/C/D 固定点成立与 Seed 的独立可信性是两个不同结果。报告（`report.json`、逐检查日志、契约/差分结果）按 run ID/attempt 上传，`Native / required` 汇总三平台并写入 Step Summary。

## 暂行措施：Native Verification 推迟（临时，仅开发用 CI）

**Native Seed 目前未准备好**：受信任的 GF4 Native Seed（SHA-256 `4c3f7dcbea536d756c3c4a93c18a999412625184bd5c90123f1c16a0cd9e36da`，runner 仍强制校验）无法被 GitHub-hosted Runner 取得；仓库内没有已批准的 Seed 分发来源。为了让 `ci-pr.yml` 与 `ci-full.yml` 先迁移到 GitHub-hosted Runner 并正常运行，需要 Seed 的验证被推迟。这是**临时措施**，不改变 `release.yml`。

- **Native Verification 的状态是 NOT_RUN。** 所有 `engine=native` 模块（Core、AOT、GC、selfhost、ABCD、标准库原生测试及 `kiso/aot/x86-64` 等）不会启动，也不会被写成 PASS 或 SKIP 之外的成功状态；计划与报告列出它们并标 NOT_RUN。
- **补充测试成功不等于 Native 测试成功。** 暂缓期间 `Mio CI / required` 只是 Supplementary CI Gate：infrastructure（quality、自检、发布隔离静态检查）、Linux 参考模块、hosted `macos-15` 上的 Go / VM 参考模块。它通过时报告仍写 `native_verification: NOT_RUN`。
- **不做的事：** 不下载或生成任何 Seed，不用 Go 编译器或参考 VM 代替 Native Seed，不运行未验证的二进制，不把参考结果写成 Native 结果。
- **Quick：** `scripts/ci/plan.py plan` 在暂缓时把原生模块移入 `deferred_native_modules`（NOT_RUN），`macos_modules` 为 macOS 上可执行的补充模块。**Full：** `plan.py full` 为 Linux/macOS 只选 `engine != native` 的模块，原生模块列为 NOT_RUN 并写入 Step Summary 与 `full-plan` artifact。
- **重新启用条件：** (1) 确定并批准受信任 Seed 的分发来源，且在运行前校验固定 SHA-256；(2) 在 `ci-pr.yml` 的 native job 与 Full 的 macOS job 前加入准备 Seed 并导出 `MIO_NATIVE_SEED` 的步骤（不得向 fork PR 提供生产机密）；(3) 设置仓库变量 `MIO_NATIVE_VERIFICATION=enabled`。启用后 Quick 计划恢复 `native_modules`，gate 要求 native job 与其执行报告，Full 恢复 `all full`。仅设置变量而没有 Seed 时，runner 返回 NOT_RUN/2，作业失败，不会成功。
- **仍保留的风险：** 暂缓期间合并所依据的只有补充测试，原生编译器、AOT、GC 与自举的回归不会被 CI 发现；`Mio CI / required` 为绿色容易被误读；hosted `macos-15` 的并行额度和 6 小时作业上限可能限制 Full；`queue: max` 需 GitHub 支持，本地 actionlint 1.7.7 不认识它。维护者合并涉及编译器/AOT/GC 的 PR 前仍应在受信任的 macOS ARM64 环境手动运行 `bash scripts/ci/run.sh macos-arm64 all quick`（或 full）。
- 不要把 “Native Verification” 加入必需检查，直到它真正启用：被跳过的必需检查在 GitHub 上会被当作通过。

## Full 失败修复记录（run 41825096644，ff64fd0）

下表为对该次 Full 失败的逐项归因。所有结论来自下载的 `report.json` 与 artifact、对 11820c1 / dc8cc30 / 9c35560 / ff64fd0 的本地复现；未在 macOS 上重新执行的内容标为 NOT_RUN。

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| macOS `infrastructure`：`test_timeout`、`test_timeout_kills_descendants` 抛 `PermissionError: [Errno 1]` | `runner.execute` 在超时后先 `killpg(SIGTERM)`，再 `killpg(SIGKILL)`。进程组领导者因 SIGTERM 退出但尚未被回收时，macOS 对只剩僵尸进程的进程组返回 EPERM（Linux 返回成功或 ESRCH）。 | `kill_tree` 将该 EPERM 视为“组内已无存活进程”；仍有存活后代时 `killpg` 会成功，后代照常被杀。测试条件不变；新增自检用注入的 EPERM 复现，并确认修复前的 runner 在同一注入下抛出同一错误。macOS 实机 NOT_RUN。 |
| 6 个 `internal/lsp` 测试失败（Linux、macOS） | 不是 9c35560 之前就存在的基线问题：这些测试在 11820c1、dc8cc30、ff64fd0 上直接 `go test ./internal/lsp` 全部通过。runner 为所有子命令导出 `MIO_BUNKO=<仓库>/bunko`，该变量会覆盖测试自建临时工作区的库根，6 个测试因此找不到其临时 `bunko`。设置 `MIO_BUNKO` 后可本地精确复现这 6 个失败。 | Go 检查（`go`、`go-required`）不再继承 `MIO_BUNKO`（与开发者直接运行一致）；Mio 命令仍使用它。在 runner 环境下 `internal/lsp` 107 项通过。 |
| `Resolver/Typechecker/Codegen SelfAndBunkoSources` 报 `package kiso/string resolve errors=1` | 既有基线问题（11820c1 起就失败），根因是测试数据过期而非 runner：测试里 `kiso/string` 的依赖表缺 `kiso/array`（`string.mio` 导入了 array），而 `selfhost/lexer|parser|resolver|typecheck|codegen` 导入的 `selfhost/progress` 不在包列表里，纯 Mio resolver 因此报未解析导入。 | 只更正三个测试里的包依赖数据（补 `kiso/array` 依赖与 `selfhost/progress` 包）；断言不变。三项现已通过（resolver 3.8 s、typecheck 134 s、codegen 448 s，Linux）。 |
| `selfhost/bootstrap` 在 `repository` 检查里 3600 s 超时（macOS）/ 约 56 分钟（Linux，通过） | `go test ./...` 包含 Go 自举自编译，单项就接近一小时，并受 `-timeout 60m` 限制。 | 拆为独立模块 `go-bootstrap-reference`（`standalone`，`module_timeout` 11400 s，go test `-timeout 175m`）和 `ci-full.yml` 中单独的 `bootstrap` job（两个平台各一个，`timeout-minutes: 210`，不使用 continue-on-error）。`repository` 检查改为 `go list ./...` 去掉 `Mio/selfhost/bootstrap` 其余全部包。测试范围不变：Full 仍然运行该测试。 |
| `legacy-bcd`：`Bootstrap hash mismatch: build/compiler-A.mochi (b18049e2…)` | 不是非确定性，也不是环境：固定哈希 `b5904e8f…` 是 f398065 时代 Compiler A 的哈希（在 f398065 上重建得到 `b5904e8f…`）；从 d4a11b3 起源码变化，Compiler A 变为 `b18049e2…`（二分确认；重复构建两次，Linux/macOS CI 都得到同一值）。 | **未修改**。冻结哈希属于历史基线，只有维护者可以决定重定基线（或让该 gate 针对其历史提交运行）。该 gate 在当前源码上必然失败；报告保持 FAIL。 |
| `legacy-m18`：`git-ci-ancestor` exit 1 | `check-native-aot-fixed-point.py` 的默认祖先 `8b53a25` 不在 Mio-2.1 历史中（历史迁移后无法到达）。迁移后的等价提交是 f398065（同作者、同时间、同标题；是 Mio-2.1 的祖先，树仅少 30 个文档/插件文件）。脚本注明“历史迁移后需显式覆盖”。 | `legacy/test.sh` 显式传 `--ci-ancestor f398065`。该等价性是推断，需维护者确认；之后 m18 能否通过 macOS NOT_RUN。 |
| `TestM18SelfCompilerVMDeterminism`、`TestM18Stage4Differential/*`：找不到 `native-aot.mochi`、`stage4.native` | 级联：这些 Go 测试读取 `legacy-m18` 生成的证据目录，而 `legacy-m18` 在第一步（祖先检查）就失败，证据从未生成。 | `legacy-m18` 失败时不再启动这些测试，记录为 NOT_RUN（模块仍因 `legacy-m18` 的 FAIL 而失败）；成功时照常运行。测试本身未改。 |
| macOS `go-aot-reference` 133 项失败（`static string data exceeds __const limit`、`old_sweep_enabled` 未定义） | 未修复，也不在本次范围：`m19b_generational_test.go` 复制固定的 10 个 `lower/*.mio` 文件，缺少 `old_sweep.mio` 等，导致 `old_sweep_enabled` 未定义（该函数定义在 `old_sweep.mio`）；`darwinarm64/string.go` 对静态字符串数据设了 16 KiB 页上限，而内嵌的 Mio 源码已超过。这些测试在 Linux 上被跳过，无法在此验证，是否为 9c35560 之前的基线也无法在 macOS 复核。 | 记录为已知问题，修复需要 macOS 执行；不放宽测试。 |

与必需 gate 的关系：`bootstrap` job 属于 Full（push 到 Mio-2.1、手动、周计划），不属于 PR Quick；PR 的必需检查 `Mio CI / required` 不依赖它，也不会被它延迟。Full 工作流的结果是各 job 的总和（`Full / linux-amd64`、`Full / macos-arm64`、两个 `Full / bootstrap / …`），任何一个失败都使该次 Full 失败。若要把 Full 设为合并前必需，需要维护者另行配置分支规则。

注：上表中的 `legacy-bcd`、`legacy-m18`、`legacy-m18-reference` 与 `go-aot-reference` 已随 Go AOT 后端一起删除（Mio-2.1 Runtime ABI 统合，见 CHANGELOG.md）；这些行仅作历史记录。
