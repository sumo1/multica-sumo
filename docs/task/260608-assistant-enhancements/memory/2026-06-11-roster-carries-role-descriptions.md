---
name: roster-carries-role-descriptions
description: 修"总控选角色很奇怪"——规划 roster 原来只给角色名+UUID，没有 description，总控只能按名字猜专长。现在每行带上截断的角色描述
metadata:
  type: project
---

实机 bug:总控拆任务时选的执行角色驴唇不对马嘴,尽管每个角色定义里都写了清楚的专长描述。

## 根因

规划角色选择**没有规则/打分匹配**——是总控 LLM 自由判断派给谁。但它判断的依据,只有 roster 里的**角色名字符串** + "(this project's role)" 标记。`formatRosterRow` 只发 `name — kind — role label — @mention(UUID)`,**完全不带 description**。所以总控蒙着眼按名字猜专长,你写的角色描述它一个字看不到。

这是我上一轮做 `buildPlanningRoster`(扩 roster 池)时的低级遗漏:只搬了名字+UUID,漏了描述。

## 修法(纯增量,不改选择逻辑)

- 新 `formatRosterRowWithDesc(name,kind,role,mention,desc)`(`formatRosterRow` 保留为零描述的薄封装,issue-squad briefing 仍用它)。
- `truncateRoleDesc`:取首段(`\n\n` 前)、collapse 空白成单行、截到 ~200 runes 加 `…`。空描述返回 "" → 该行不带描述尾巴。
- squad 成员行(`renderMemberRow` agent 分支)+ 工作区池行,都改用带描述版本,喂 `ag.Description`。leader 自荐行不带(它是自己)。
- 规划 prompt(`buildGoalPlanningPrompt`)那句改成:"每个 roster 条目带角色描述——**读它**,按描述的专长匹配节点,别只看名字"。

## 验证

- `TestBuildPlanningRoster_IncludesRoleDescriptions`(建带描述的 agent → roster 含描述文本)、`TestTruncateRoleDesc`(空/首段/超长截断)、原 `TestBuildPlanningRoster_WidensToWorkspacePool` 仍绿、`buildGoalPlanningPrompt`/`buildSquadLeaderBriefing` 测试仍绿。
- 实机:server 重建(multica 库)+ daemon rebundle 重启(prompt 改了)。4 runtime online、0 token 错误。

## 关联

- [[planning-roster-widened-to-workspace-pool]](同一函数,那轮扩池漏了描述,这轮补)、[[design-member-orchestration]](角色同步:description 从 repo `.claude/agents/` frontmatter 或散文首段来)、[[which-model-ran-it-attribution]]。
