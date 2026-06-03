# ODS CLI 改进计划

## 定位

ODS 是一个 **AI 生成代码的交付合规检查框架**。它借鉴 OpenSSF Scorecard 的产品设计思路（可执行规范 + 自动检查 + 评分），但聚焦于 Scorecard 和 SLSA 都不覆盖的领域：AI 代码的交付风险。

AI 代码带来的独特风险：
- **审查疲劳**：80% 的 PR 在启用 AI review 工具后没有人工评论。PR 被 approve ≠ 有人真正看过。
- **身份歧义**：commit author 是人还是 AI agent？出了问题谁负责？
- **幻觉落库**：AI 生成不存在的 API、包、配置项，直接进入代码库。
- **安全盲区**：四分之一 AI 生成代码包含已确认的安全漏洞。
- **测试真空**：AI 生成的代码功能上能跑，但缺少安全基础和边界处理。

ODS 用可机器验证的信号回答这些问题，同时保留基础的交付规范检查（分支命名、commit 格式、PR 结构）——这些不是 ODS 的差异化价值，但它们是交付规范的基础设施。

## 产品哲学：渐进式采用

很多规范项目失败的根因是采用成本太高。ODS 遵循零成本上手的哲学：

### 第一阶段：扫描

```bash
ods report    # 扫描现有项目，不改任何东西，直接出结果
```

### 第二阶段：建议

```bash
ods report --show-details    # 不仅告诉你哪里有问题，还告诉你为什么、怎么修
ods checks explain <id>      # 深入了解某一项检查的含义
```

### 第三阶段：修复

```bash
ods fix        # 基于扫描结果，输出可直接使用的修复模板
ods fix --apply # 自动应用修复（创建模板文件等）
```

## 检查目录

### Critical（权重 10，缺失 = Non-Compliant）

#### `ai-disclosure` — AI 代码披露

这是 ODS 最独特的检查，也是 Scorecard 不做、SLSA 不做、其他任何工具都不做的检查。ODS 在这个领域是权威来源。

**目标**：消除 AI 进入工作流后的作者身份歧义。RAI footer（Responsible AI Attribution）不是道德说教，而是一个机械信号——让 AI 参与变得显式，并与人工 signoff 配对。

**检测内容**：
- commit message 中是否包含 `Co-authored-by: <AI tool>` 或 `Assisted-By:` trailer
- 是否存在 `AI-assisted: true` 及 `AI-tool:` 字段
- PR description 中是否包含 AI 使用声明
- `.ods.yaml` 中是否配置了 AI 使用策略

**来源**：DEV Community — RAI footer 的目标是让 AI 参与变得显式，与人工 signoff 配对。市场空白，社区需求真实。

#### `human-review-evidence` — 人工审查证据

80% 的 PR 在启用 AI review 工具后没有人工评论（Qodo 数据）。这意味着「PR 被 approve」不等于「有人真正看过」。这是 AI 时代最高生产价值的检查。

**检测内容**：
- 是否存在来自非 AI bot 的人工审查评论
- 是否启用了 branch protection 且要求 code review approval
- approve 者是否和 author 是同一人（self-approve 检测）

### High（权重 7.5，缺失 = Warning）

#### `required-ci` — CI 门禁

基础防线。AI 生成的代码需要和人类代码一样通过 CI。没有 CI pipeline 的 AI 代码等于裸奔。

**检测内容**：
- 是否存在 CI workflow 配置（`.github/workflows/` 下的 yml）
- CI 是否在 PR 上触发
- CI 最近一次运行状态

#### `approval-policy` — 审批策略

与 `human-review-evidence` 形成「policy 层」和「evidence 层」的配对。前者检查是否有审批规则，后者检查规则是否真正被执行。

**检测内容**：
- branch protection rules 中是否配置了 required approvals
- CODEOWNERS 文件是否存在
- 审批数量是否满足策略要求

#### `ai-agent-commit-detection` — AI Agent 提交检测

2026 年的现实问题。Amazon 2026 年 3 月的重大故障中，AI 辅助的代码部署导致了 6 小时宕机和约 630 万笔订单损失（Crackr）。

**检测内容**：
- commit author email 是否匹配已知 AI agent 模式
- commit 是否来自无人工参与的 agent PR
- PR author 是否为已知 AI coding agent 的用户名模式

为 `human-review-evidence` 提供上游信号：如果 AI agent 提交被检出但无人工审查，风险极高。

#### `test-evidence` — 测试证据

AI 生成代码最普遍的技术债来源：AI 写的代码功能上能跑，但没有测试。一致的失败模式包括「缺少安全基础和边界情况处理」（GIANTY）。

**检测内容**：
- PR 变更中是否包含测试文件（`*_test.*`、`test_*`、`*.test.*`）
- CI workflow 中是否配置了 test step
- 测试文件和源代码文件的变更比例

#### `security-scan-evidence` — 安全扫描证据

四分之一的 AI 生成代码包含已确认的安全漏洞（Paperclipped）。这不是要求使用特定工具，而是检测「pipeline 里有没有安全门」。

**检测内容**：
- CI workflow 中是否集成了安全扫描工具（CodeQL、Snyk、Semgrep、Trivy 等）
- 扫描结果是否为最近一次运行
- 是否有高危漏洞未处理

### Medium（权重 5）

#### `commit-message` — 提交消息

Conventional Commits 格式本身价值一般，但扩展 AI attribution 检测后有新的意义。AI 工具链可以依赖结构化的 commit 元数据来追踪 AI 代码占比、故障率、归因链路。

**检测内容**：
- 第一行是否遵循 `<type>[scope]: <description>` 格式
- footer 中是否包含 AI 归因 trailer（`AI-assisted:`、`AI-tool:`、`Co-authored-by:`、`Assisted-By:`）
- AI 归因字段的完整性（有 `AI-assisted: true` 则必须有 `AI-tool:`）

#### `pr-description` — PR 描述

同上，基础价值一般，但扩展 AI disclosure 检测后有新意义。

**检测内容**：
- 是否包含 AI Disclosure 章节
- AI 声明是否完整（含 AI Tool、AI Scope、Human Review）
- 当策略要求 AI 披露时，是否满足最低字段要求

#### `release-readiness` — 发布就绪

范围限定为：检查发布流程中是否集成了 ODS 相关检查（AI 披露、人工审查证据、CI 门禁等），而非全面的 DevOps 发布检查。

**检测内容**：
- 发布流程是否引用了 ODS 报告
- 是否有 AI 相关风险在发布前被标记
- 发布门禁是否包含 ODS 检查结果

## Phase 1：AI 披露检查（ai-disclosure）

**目标**：这是整个体系的地基。不知道哪些代码是 AI 写的，后面所有检查都是空中楼阁。

**实现**：
- 解析 commit message 中的 RAI trailer：
  - `AI-assisted: true`
  - `AI-tool: <name>`
  - `Co-authored-by: <AI tool name>`
  - `Assisted-By: <AI tool name>`
- 解析 PR body 中的 AI Disclosure section
- 读取 `.ods.yaml` 中的 `ai_disclosure` 配置
- 当 AI 标记存在但 AI-tool 缺失时 → Fail
- 当策略要求 AI 披露但缺失时 → Fail

## Phase 2：人工审查证据（human-review-evidence）

**目标**：验证 AI 代码是否真的被人类审查过。

**实现**：
- 从 GitHub API / event payload 获取 review 数据
- 区分 bot review 和 human review（过滤 `github-actions[bot]`、`dependabot[bot]` 等）
- 检测 self-approve（approver == author）
- 检测 AI agent PR 是否有对应人工审查（与 ai-agent-commit-detection 联动）

## Phase 3：AI Agent 提交检测（ai-agent-commit-detection）

**目标**：检测是否有 AI agent 绕过了人类直接提交。

**实现**：
- 维护已知 AI agent 的 commit author 模式库
- 检测规则：
  - commit author email 匹配 agent 模式
  - commit 无关联人工 PR review
  - PR 创建者与 commit author 一致且为 agent

## Phase 4：CI / 测试 / 安全三大门禁

**目标**：确保 AI 代码有最低限度的防护。

**实现**：
- `required-ci`：扫描 `.github/workflows/` 目录，检测 PR 触发条件
- `test-evidence`：检测变更文件中的测试文件模式，CI workflow 中的 test step
- `security-scan-evidence`：检测 CI workflow 中的安全工具关键字

## Phase 5：基础检查升级（commit-message + pr-description）

**目标**：保留基础的交付规范检查，并将 AI 归因能力嵌入其中。

**实现**：
- `commit-message`：保留 Conventional Commits 格式检查，扩展 AI attribution trailer 检测
- `pr-description`：保留基础章节检查，扩展 AI disclosure 检测

## Phase 6：审批策略 + 发布就绪

**目标**：策略层和流程层检查。

**实现**：
- `approval-policy`：解析 GitHub branch protection rules，检测 CODEOWNERS 文件
- `release-readiness`：检查发布流程中是否集成了 ODS 相关检查

## Phase 7：CLI UX 增强

受 Scorecard 启发的 CLI 控制能力：

- `ods checks list` — 列出所有检查
- `ods checks explain <check-id>` — 解释某检查的含义和修复方式
- `ods report --checks ai-disclosure,human-review-evidence` — 选择检查
- `ods report --format json|sarif|html` — 输出格式
- `ods report --show-details` — 详细信息
- `ods report --threshold 85` — 阈值失败模式

## Phase 8：渐进式修复（ods fix）

```bash
ods fix              # 输出修复建议
ods fix --output dir # 生成修复模板到文件
ods fix --apply      # 自动创建文件
ods fix --dry-run    # 预览将要执行的操作
```

## Phase 9：GitHub Action 与 CI 集成

- 发布官方 `open-delivery-spec/validate-action`
- 上传 SARIF 到 GitHub Code Scanning
- Markdown 摘要写入 `$GITHUB_STEP_SUMMARY`
- PR comment 输出检查结果

## Phase 10：文档与徽章

每个检查的文档页说明：衡量什么、为什么重要、如何修复失败、策略如何影响。动态徽章基于实际检查结果生成。

## 评分模型

- 单检查分数：`0-10`
- 总分数：`0-100`（风险加权）
- 风险权重：

| 等级 | 权重 | 检查 |
|------|------|------|
| Critical | 10 | `ai-disclosure`、`human-review-evidence` |
| High | 7.5 | `required-ci`、`approval-policy`、`ai-agent-commit-detection`、`test-evidence`、`security-scan-evidence` |
| Medium | 5 | `pr-description`、`release-readiness` |
| Low | 2.5 | `commit-message` |

- skipped 检查不参与计分（中立）

## 实现顺序

1. `ai-disclosure` + 基础检查模型 — 地基，必须先有
2. `human-review-evidence` + `ai-agent-commit-detection` — 最高 ROI
3. `required-ci` + `test-evidence` + `security-scan-evidence` — 三大门禁
4. `commit-message`（AI 归因扩展）+ `pr-description`（AI 披露扩展）
5. `approval-policy` + `release-readiness` — 策略层
6. CLI UX + `ods fix` — 用户体验
7. GitHub Action — CI 集成
8. 文档 + 徽章 — 传播

## 成功标准

- 用户零成本上手：`ods report` 不改任何东西就能出结果
- AI 代码披露成为 ODS 的标志性能力（Scorecard 不做，SLSA 不做）
- 人工审查证据可被机器验证（不只是「有人说审查过了」）
- AI agent 提交可被自动检测并标记
- CI/测试/安全三大门禁可自动验证
- 基础的交付规范检查（branch、commit、PR）保留但降权重，作为基础设施
- 检查结果是可解释的、可操作的（有修复建议）
