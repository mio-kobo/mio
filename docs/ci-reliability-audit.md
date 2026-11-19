# Mio 2.1：统一 CI 可靠性审计

日期：2026-11-04。工作分支 `Mio-2.1`，起点及本地 HEAD 为 `dc8cc30f74fa85ff73544c6f498ac2659bcf1791`；修复尚未提交或推送。开始时工作树干净。没有修改其他贡献者分支、合并 PR、触发远端工作流、变更 Release workflow、语言语义、Core、GC 或正式 AOT 实现。

结论：本地基础设施审计与修复已完成；**当前还没有足够的原生执行证据支持合并这些 PR**。框架自检成功不能替代语言测试成功。本文与 `build/ci-reliability-audit/` 是本次证据；旧 `testing-audit.md` 是历史记录，不混用其测试数量或运行结论。

## 1. 实际机制与依赖

已阅读 CONTRIBUTING.md、docs/testing.md、全部四份激活 workflow、scripts/ci 下的脚本，以及当前所有 26 个模块的 test.sh 和 ci-module.json。

```text
ci-pr.yml / ci-full.yml
  -> bash scripts/ci/run.sh TARGET all quick/full
  -> Python runner.discover(): internal / bunko / selfhost / tools / scripts/ci
  -> adjacent ci-module.json + test.sh
  -> common.sh -> ci_check / ci_native / ci_native_and_crosscheck
  -> compiler builds + module log + per-check stdout/stderr + report.json
```

Python 标准库负责调度及协议；Bash、Python、Git、Go/gofmt 是开发依赖。原生模块还需要 macOS ARM64、codesign 和主机提供的 Native Seed。参考模块构建现有 Go 工具；它们不用于正式原生构建。工具缺失为 NOT_RUN；真实执行非零、超时为 FAIL；平台/等级策略为 SKIP；没有执行、缺少必需检查、损坏报告为 NOT_RUN。退出码 0/1/2 分别代表通过、失败、存在环境或基础设施问题；混合失败时总状态保留 FAIL，退出码可能为 2。

Native Seed SHA256 固定为 `be555a5e51380a0151c3e4b17d57dd79120123ab170dae3103146d32ab5a0c79`（bootstrap-v2.1.0-seed.1 发布资产 mio-v2.1.0-darwin-arm64；旧 GF4 Seed 已弃用），与既有约定一致。runner 在首次原生构建前验证文件与摘要，不下载或自动替换 Seed；然后仅构建一次当前工作树 selfhost/cli。后续原生检查复用该产物，显式传当前 Bunko root。构建有独立命令、日志、Seed/产物哈希；构建失败为 FAIL，依赖测试为 NOT_RUN。缺少或错误 Seed 不执行编译器/测试，返回 NOT_RUN/2。本次真实 Seed/签名及原生构建 **NOT_RUN**；自检仅用临时夹具验证协议，不是信任真实 Seed 的证据。

## 2. 远端问题及准确分类

使用 GitHub connector 只读 GET 查询 `/actions/runs?per_page=50`，并读取所列 run 的 jobs、历史 job logs、四个 PR 的元数据与完整变更文件列表；另读取公开运行页面确认审批原因。精简快照：`build/ci-reliability-audit/github-snapshot.json`。

| 证据 | 观察 | 分类及结论 |
| --- | --- | --- |
| [Full 41811793218](https://github.com/mio-kobo/mio/actions/runs/41811793218)，head dc8cc30 | macOS job 126512589951 queued，runner_name 为空，标签 self-hosted/macOS/ARM64/mio-native；Linux job 已分配 ubuntu-24.04 Runner 并 in_progress | Native 测试 NOT_RUN，尚未分配匹配执行器；不能归因于语言失败。不是整个 Actions 服务完全没有执行 |
| [#6 Native 41808929510](https://github.com/mio-kobo/mio/actions/runs/41808929510) | 同类 macOS queued/未分配；Supplementary Linux 已成功 | Linux 补充 PASS 不能代替 Native 证据 |
| [#7 Native 41810938512](https://github.com/mio-kobo/mio/actions/runs/41810938512) 及同提交的 Auxiliary 41810938764 | action_required，无 job；公开页面明确等待维护者批准 | 工作流等待批准，测试 NOT_RUN；没有测试失败日志 |
| [#5 41764211407](https://github.com/mio-kobo/mio/actions/runs/41764211407) | action_required，无 job；公开页面同样明确等待维护者批准 | 工作流等待批准，测试 NOT_RUN |
| [旧基线 41603555344](https://github.com/mio-kobo/mio/actions/runs/41603555344)，head 11820c1 | Linux 真实执行并失败：Resolver、Typechecker、Codegen 的 SelfAndBunkoSources | 已知源码/参考测试基线 FAIL，不能改称 Runner 故障 |
| [旧基线 41808615038](https://github.com/mio-kobo/mio/actions/runs/41808615038)，head 6bcdf33 | Linux 同类三项源码失败；bootstrap job 126501824259 日志明确 `scripts/check-native-bootstrap.sh: No such file or directory` | 源码 FAIL 与 CI 缺脚本必须分开。缺脚本已在当前 HEAD 的前次 CI V2 改造中恢复，不是本次新增修复 |
| [#4 41668876679](https://github.com/mio-kobo/mio/actions/runs/41668876679)，head 015b0f8 | 运行确实失败；PR 只增加 x86_64/ELF 文件 | 不可仅凭红灯归咎于 #4。旧基线已有相同 Core 失败；没有声称每个 macOS 失败均完成逐项归因 |

审计期间再次查询最新 Full，仍为 macOS queued、Linux in_progress。这是读到的快照，不是本次修改后的工作流运行结果。管理员 Runner 清单/在线状态、Runner group 授权、服务进程与仓库审批策略均 **NOT_RUN**：当前读取不能确定是 Runner 未注册、离线、标签不匹配、权限还是繁忙。共享 concurrency 还可能增加等待；不能据此猜定唯一机器原因。job timeout 也不能保证未分配 Runner 的等待及时结束。

`git diff 11820c1 HEAD -- selfhost/codegen selfhost/resolver selfhost/typecheck bunko/kiso/string` 及 `git diff 6bcdf33 HEAD -- ...` 均为空，确认这些已知失败涉及的源码没有在当前提交中变化。本次在当前工作树重新执行 Resolver 单项，仍以退出码 1 报 `package kiso/string resolve errors=1`。Typechecker/Codegen 当前完整重跑 **NOT_RUN**；其历史日志已读取。`old_sweep_enabled undefined`、`__const limit` 在本次读取片段/本地命令中未重新复现，不能宣布已修复或直接归因于这些 PR。

## 3. 本次发现及最小修复

| 问题 | 修改与结果 |
| --- | --- |
| 发现机制只扫描清单，新增 Bunko 测试漏交清单可完全消失 | discover 拒绝无相邻 ci-module.json 的 Bunko `*_test.mio`；缺入口、未知 target、无效 covered_by 同样拒绝。inventory 可在 Windows 静态查看平台计划 |
| arm64/macho 有 required independent，却在 all 中整个模块被 covered_by 跳过 | 只允许纯 native 单层模块委托递归检查；两个 Writer/encoder 子模块的原生与独立层现在均实际调度 |
| 模块可仅记录 Go/command 成功却声称 native | 检查记录增加 kind，模块增加 engine；native 模块没有 Mio/native 记录则补 NOT_RUN。保留 reference/supplementary 标识 |
| 普通 Go JSON 中一个 PASS 可与必需项的 SKIP 同时出现 | 新增显式 go-required 检查类型：任一 SKIP 或无测试为 NOT_RUN；普通 Go 平台基线保留原明细。它是接入手段，未把历史所有合法 SKIP 强行改为失败 |
| 缺 Seed/编译器时按 suite 添加的检查没有完整列出 | 现在将 required_checks 与 suite_checks 全部补成 NOT_RUN；wrong-seed 自检确认编译器和测试均未启动 |
| module exit 2 与实际 FAIL 并存时模块标签可能误成 NOT_RUN | 真实失败记录优先决定模块 FAIL，同时保留基础设施退出码；总报告不掩盖失败 |
| 并行检查追加报告存在 Windows 写竞争 | 首轮自检实际捕获失败记录丢失：预期 FAIL 却得到 NOT_RUN。使用 msvcrt/fcntl 进程锁串行追加；原并行负向用例复跑成功 |
| artifact 路径可能收集自托管主机保留的其他运行结果 | Quick/Full 结果按 run ID/attempt/target 隔离；ABCD 目录也使用本次 key，上传不再使用所有历史 ci-abcd-* 通配 |
| 基础设施检查混在长测试后，缺少统一可配置的合并门槛 | PR 新增独立 hosted infrastructure job，以及依赖该 job 和两平台 quick matrix 的 `Mio CI / required` gate。任何失败、取消、跳过都不能使 gate 成功 |
| 报告缺少明确源码身份 | 增加 source_revision 与 source_worktree_dirty；本地未提交修复的结果不能误标为纯 HEAD 已验证 |

本次改动只涉及测试驱动、两个普通 CI workflow、bootstrap 测试入口的证据目录、文档及其 .gitignore 白名单。没有删除重要测试、放宽断言、加入 `|| true` 或 continue-on-error；发布工作流与正式构建路径未改变。

另有既存问题：release-candidate.yml 仍引用当前分支缺失的 scripts/release_automation.py / test_release_automation.py。它属于被要求保持不变的发布策略入口，本次只报告，不修复；实际执行 NOT_RUN。

## 4. 自动发现和平台调度

当前发现 26 个模块；当前所有 Bunko 测试包都有相邻清单。脚本也有 parent 级集成模块（例如 selfhost 的 full 递归测试）；不能把它描述为每个 Go 包或每个无测试库都具备独立入口。

以下是 inventory 的静态计划，**不是语言测试运行数量**：

| 平台 / suite | RUN | DELEGATED | SKIP |
| --- | ---: | ---: | ---: |
| Linux AMD64 quick | 2 | 0 | 24 |
| Linux AMD64 full | 2 | 0 | 24 |
| macOS ARM64 quick | 19 | 2 | 5 |
| macOS ARM64 full | 23 | 2 | 1 |

Linux 的两个 RUN 是 go-reference 与 quality；没有 Mio Linux 原生 Seed 验收。委托仅为 lower/types 与 lower 的 native 递归层；报告仍为 SKIP 并注明父模块。macOS full 唯一策略 SKIP 是单独显式请求的 native-release。Windows 原生矩阵不存在：真实请求返回 NOT_RUN/2。

## 5. 四个 PR 的状态

| PR | 独立入口 | 原生/独立验证状态 | 后续要求 |
| --- | --- | --- | --- |
| [#4](https://github.com/mio-kobo/mio/pull/4) AMD64/ELF | 两包均缺 test.sh/manifest | 本次 NOT_RUN；已有 Mio 测试和手工 objdump/ELF 证据不等于自动接入 | 增加两入口；自动化 objdump 与 ELF reader/真实加载检查 |
| [#5](https://github.com/mio-kobo/mio/pull/5) CPU/OS | 两包均缺；Go runtime-chain 没有独立 Linux Quick 注册 | 本次 NOT_RUN；等待批准且依赖 #4 | 先 #4、更新分支；增加两原生入口及 Linux runtime-chain reference 入口 |
| [#6](https://github.com/mio-kobo/mio/pull/6) PE32+ | 已有 macOS native + independent，未设置 covered_by | 本次 Seed 验证 NOT_RUN；PR 自报 Windows/VM 结果未在本次重跑 | 更新至 CI 修复，建议使用 ci_native_and_crosscheck；补 Seed 验收及单独 Windows 镜像执行证据 |
| [#7](https://github.com/mio-kobo/mio/pull/7) Linux reference AOT | 缺独立入口 | 本次 NOT_RUN；等待批准；Linux Full 会包含，PR Quick 当前会漏 | 注册 Linux reference 模块与必需 objdump/blob 检查；匹配 gcc 14.2.0；补 macOS 受影响回归 |

完整 test.sh / ci-module.json 接入模板、路径和必要边界见 [PR 接入方案](ci-pr-integration.md)。模板只在文档中，没有把待合并模块植入本分支。#7 实际嵌入 C runtime/blob 的架构定位也需要维护者决定；不能因为它有参考测试就视为满足正式纯 Mio 实现边界。

## 6. 本次实际执行

环境：Windows AMD64，Python 3.14.5、已有 Git for Windows Bash/Git、仓库已有 .cache/toolchain 下的 Go/actionlint。开始时 Git/Bash/Go 没有全部位于 PATH，使用现有路径，没有新增正式依赖。驱动脚本与完整命令/退出码存于 `build/ci-reliability-audit/validate.py`、`validation.json` 和各 stdout/stderr 日志。

| 实际命令 / 检查 | 状态 | 结果解释 |
| --- | --- | --- |
| `bash scripts/ci/selftest.sh` | PASS | 38 项，0 SKIP；包含真实 assert False、退出 9 的并行子检查、pipefail、超时/后代清理、空测试、缺必需检查、假原生记录、错误 Seed 等。负向子进程确实非零，而自检对这些预期断言成功 |
| `python scripts/ci/runner.py inventory` | PASS（静态） | 26 模块、四种平台/suite 计划；没有启动编译器 |
| `.cache/toolchain/bin/actionlint.exe .github/workflows/ci-pr.yml .github/workflows/ci-full.yml` | PASS（静态） | 不能替代 GitHub 实际执行 |
| `bash -n` 对全部 CI/module Shell 脚本 | PASS | 30 个入口/脚本；完整枚举见 validate.py |
| Python `ast.parse` 对 scripts/ci/*.py | PASS | 语法检查 |
| `python scripts/ci/release_isolation.py` | PASS（静态） | 不等于实际 release/archive 验证 |
| `go vet ./...` | PASS | 开发静态检查 |
| `git diff --check` | PASS | 修改完整性检查 |
| `bash scripts/ci/run.sh macos-arm64 all quick` | NOT_RUN / 2 | Windows 主机拒绝不匹配目标；拒绝机制检查 PASS |
| `bash scripts/ci/run.sh windows-amd64 all quick` | NOT_RUN / 2 | 平台尚未接入；拒绝机制检查 PASS |
| `bash bunko/kiso/aot/arm64/test.sh macos-arm64 quick` | NOT_RUN / 2 | 单模块同样不能绕过宿主检查 |
| `go test -json ./selfhost/resolver -run '^TestMioResolverSelfAndBunkoSources$' -count=1 -timeout 2m` | FAIL / 1 | 重新确认当前工作树的 kiso/string Resolver 基线失败；日志 resolver-baseline.jsonl |
| 新修复的 macOS Native Quick/Full、当前源码构建、真实 Seed 及负向 Mio 断言 | NOT_RUN | 无对应主机/Seed；模拟协议测试不代替此项 |
| 本地 Linux Quick/Full、PR #4–#7 语言/ELF/PE 执行、macOS 长压力/GC/FFI | NOT_RUN | 本次未执行这些平台测试 |
| 修改后的 GitHub Actions gate 与 artifact 上传 | NOT_RUN | 未推送或触发运行 |
| Go 全仓库回归、Typechecker/Codegen 全部当前重跑、正式打包/安装 | NOT_RUN | 本次没有执行，不引用旧报告充作本次 PASS |

修复过程首轮自检存在并发报告用例失败，已定位并修复；首次 actionlint 也发现 job env 不允许 runner context，已改为合法且隔离的 build 路径并复验。最终记录以 validation.json 为准，不隐去初次发现的问题。

## 7. 仍需人工处理与合并顺序

1. 检查 self-hosted macOS ARM64 服务在线、标签/Runner group/仓库权限、容量和共享 concurrency；主机提供固定 GF4 Seed。本文不更改 Runner 配置或发布凭据。
2. 维护者审查并批准 #5/#7 等 fork workflow；这与恢复 Runner 是两个独立动作。没有批准就没有运行证据。
3. 应用并提交本次 CI 修复后，在 GitHub 跑真正的 macOS/Linux Quick/Full，审查 report 的源码 SHA、dirty 状态、compiler builds、native/independent/supplementary 层、所有 NOT_RUN/SKIP 及 artifact。分支规则应 required `Mio CI / required`；规则当前配置未验证/未修改（NOT_RUN）。
4. 在单独任务中处理已知 Core 失败；本任务保持失败，不能为绿色 CI 放宽语言测试。Full 基线尚未通过时不要宣称已取得完整合并证据。
5. 推荐先审查/提交 CI 基础设施修复，再完成 #4 接入和两层证据，合入 #4；#5 更新到该基线、补入口及 Linux chain 后再合入。#6 不依赖 #4/#5，可在其原生/Windows 证据齐备后独立并行审查。#7 最后评估：虽无 #4/#5 代码硬依赖，仍需参考实现边界决定、匹配工具链的必需检查和 macOS 回归。

没有自动合并或自动推送。本地基础设施 PASS 与远端/原生 NOT_RUN 均如实保留。

## 后续补充：保留并发组中的等待任务

按维护者补充要求，Quick/Full 的 job 级 ci-resource 并发组增加 `queue: max`。Full 的外层 ci-full 并发组也增加该配置，避免等待任务尚未进入资源并发组就被后续运行替换。Quick 外层仍保留取消同一 PR 旧版本的策略，不与 queue: max 混用。

[GitHub 官方并发文档](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)说明：cancel-in-progress: false 默认仍只保留一个 pending；queue: max 可保留最多 100 个等待任务。该改动解决等待任务被替换，不能恢复离线 Runner。

本次补充验证：PyYAML BaseLoader 解析两个 YAML 并断言三个并发组的 queue/cancel 配置，PASS；git diff --check，PASS。本地旧版 actionlint 对三个 queue 字段报告不识别，因此上文及 validation.json 的 actionlint PASS 仅适用于补充前版本，不能用于声称当前完整工作流已 lint 通过。GitHub 实际排队执行验证仍为 NOT_RUN，未推送或触发运行。
