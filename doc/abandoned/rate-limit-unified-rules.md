---
title: 统一限速规则表
status: abandoned
created: 2026-11-23
updated: 2026-11-23
owner: Rock
completed:
---

# 统一限速规则表

## 废弃原因

2026-11-23 决定还原请求数限速到 v1.0.28 基线，只保留用户和分组两层限速，不做组织上限和渠道预算。本方案以统一规则表、组织上限和渠道容量为前提，这些前提已经不再成立，因此不再执行。文档仅作历史记录保留。

本文档是**提案**，用于评估把用户、组织、渠道三层限速的配置统一为一个规则模型。它会重新审视 [多层限速模型设计](rate-limit-layered-design.md) 与 渠道级请求限速（计划文档未在 dev 上） 中的部分已确定决策，开工前需要 owner 逐项确认（见「待确认」）。

## 目标

1. 三层限速使用同一个规则模型：`主体 + 指标 + 数值 + 窗口`，窗口归属规则本身，不再有的层自带窗口、有的层共用全局窗口。
2. 只有一个生效值解析层。中间件、渠道预算、限速总览读取同一份解析结果，不再各写一套。
3. 给组织、用户规则增加可选窗口，沿用全局窗口为默认，解决"组织不能配窗口"的不对称。
4. 在首次发布前完成统一，使生产环境只经历一次 schema 变更。
5. 不改变 D2（总请求数与成功请求数两个维度）、D6（组织只做总量）、D7（组织占比为静态上限）、TPM 不做的既有范围。

## 背景与前提（2026-11-23 核对）

- 生产版本 v1.0.21 只有用户层限速：`middleware/model-rate-limit.go` 加全局 option（`ModelRequestRateLimitEnabled`、`ModelRequestRateLimitDurationMinutes`、`ModelRequestRateLimitCount`、`ModelRequestRateLimitSuccessCount`）。
- 渠道预算（`service/channel_rate_limit.go`）、组织上限与用户覆盖（`model/rate_limit_override.go`、表 `rate_limit_overrides`）在 `dev`，不在 v1.0.21；`dev` 领先 v1.0.21 共 29 个提交。渠道内组织占比在 `feature/channel-org-share`，尚未合并。
- 因此除全局 option 外，这些配置在生产环境没有数据。现在统一，只需处理开发和 E2E 库中的数据；发布之后再统一，需要迁移生产数据并保持兼容。
- `dev` 相对 v1.0.21 的 `model/` 改动含 `AutoMigrate`，会触发 `new-api-deploy` 的 schema 守卫（该守卫在命中时停止自动部署）。下一次发布本来就需要人工确认；若在发布前完成统一，只需确认一次最终的表结构。

### 现状：三套配置形态

| 层 | 存储 | 窗口 | 指标 | 写入入口 |
| --- | --- | --- | --- | --- |
| 全局用户默认 | option（4 个键） | 全局，单位分钟 | 总数、成功数 | 系统设置 |
| 用户覆盖 | `rate_limit_overrides` | 沿用全局 | `total_count`、`success_count` | 用户编辑抽屉 |
| 组织上限 | `rate_limit_overrides` | 沿用全局 | `total_count` | 组织详情抽屉（root） |
| 渠道预算 | `Channel.Setting.rate_limit` | 自带，单位秒，1 到 86400 | 请求数 | 渠道编辑抽屉 |
| 渠道内组织占比 | `Channel.Setting.rate_limit.org_shares` | 随渠道 | 百分比 | 渠道编辑抽屉 |

- 生效值解析散在三处：`EffectiveUserRateLimits`、`OrganizationTotalRateLimit`、`channelBudget`；`service/rate_limit_overview.go` 又复制了一遍组织和用户的窗口逻辑。
- `rate_limit_overrides.group_name` 列是已取消的分组层（D5）遗留，只用空字符串。

## 与既有决策的关系

| 既有决策 | 本方案的处理 |
| --- | --- |
| D2 两个维度 | 保持语义，规则表用 `metric` 区分，不改口径 |
| D6 组织只做总量 | 保持，不为组织增加成功数指标 |
| D7 组织占比静态 | 保持，见待确认 2 |
| D10 编辑归位、只读总览 | 默认保持：总览改为列表，编辑仍在归属对象页面；若要集中编辑页，需另行修订 D10 |
| D11 用户与组织存关系表 | 保持并泛化：`rate_limit_overrides` 演进为通用规则表 |
| 渠道限速"配置落在 `Channel.Setting`，不新增表或列" | **本方案可选地放弃**，由 Phase 3 的决策门控制 |
| 配置位置须与写入权限匹配 | 保持：规则只经管理员接口写入，不放 `User.Setting` |

## 目标模型（草案）

表 `rate_limit_rules`：

```
id
subject_type    user | organization | channel
subject_id
qualifier_type  空 | organization        用于渠道内组织占比
qualifier_id    0 | 组织 ID
metric          requests_admitted | requests_succeeded | share_percent
limit_value     正整数；不存在的行表示不覆盖，不再用 0 表示不覆盖
window_seconds  0 表示沿用全局窗口；渠道规则必须 > 0
updated_by
updated_time
唯一索引 (subject_type, subject_id, qualifier_type, qualifier_id, metric)
```

- 命名避开保留字：不使用 `limit`；`window_seconds` 带后缀，避免 MySQL 8 的 `WINDOW`。
- 每个主体每个指标一条规则；同一主体多个窗口本期不做，索引不含窗口，将来需要时再放宽。
- 全局默认仍留在 option，不迁入规则表：这是生产里唯一已发布的配置，保持其键和行为不变。解析顺序为：规则行，其次全局默认。
- `metric` 预留扩展，但本期不实现 token、并发等指标。

## 实施计划

每个阶段独立可验证，按顺序推进；Phase 3 有决策门。

### Phase 0：确认与前置（不改代码）

- Owner 确认「待确认」1 到 5。
- 先合并 `feature/channel-org-share`，避免与它同时改渠道和组织的限速表单。
- 记录基线：现有 `middleware`、`service`、`controller`、`model` 中限速相关测试的名称与结果，作为 Phase 1 的对照。

### Phase 1：统一解析层（无 schema 变更，无行为变化）

- 新增一个解析入口，输入用户、付费组织、渠道，输出生效规则列表。
- 中间件、渠道预算、限速总览都改为调用它；删除总览里重复的窗口逻辑。
- 不改存储、不改接口、不改响应。
- 价值：一次性消除"各层各写一套解析"，且风险最小。若后续阶段被搁置，这一步仍然独立有效。

### Phase 2：用户与组织规则迁到规则表（schema 变更，三库）

- 新建 `rate_limit_rules`；把 `rate_limit_overrides` 的用户和组织行复制进去（`total_count` 对应 `requests_admitted`，`success_count` 对应 `requests_succeeded`，值为 0 的不生成行）。
- 删除 `group_name` 概念：不带入新表。
- 保留旧表到 Phase 5，期间只读，不再写入。
- 管理接口改为统一的规则接口，权限不变：组织规则只有 root 能写，用户规则由管理员按角色规则写。该功能未发布，直接替换旧接口，不保留兼容层。
- 数据库验证按 `AGENTS.md`：SQLite、MySQL 8 与最低 5.7.8、PostgreSQL 最新与最低 9.6；全新建库；从 v1.0.21 的库升级；从含 `rate_limit_overrides` 的开发库升级；启动迁移至少两次以证明幂等。

### Phase 3：窗口可配与渠道并入（决策门）

- 3a 窗口：用户、组织规则可带 `window_seconds`，0 沿用全局。Redis 脚本和内存限流器按维度各用自己的窗口；限流提示文案由"X 分钟"改为按规则窗口渲染。
- 3b 渠道并入（可选）：把 `Channel.Setting.rate_limit` 与 `org_shares` 迁入规则表。
  - 代价：放弃"渠道限速零迁移"；渠道保存要同时写渠道行和规则行，需要事务；`relaykit` 的 `ChannelRateLimit` 类型与校验要调整，必须通过 `cd relaykit && GOWORK=off go build ./...`。
  - 若决定不并入：渠道继续留在 `Channel.Setting`，只经 Phase 1 的解析层与其他层统一读取；3a 不受影响。

### Phase 4：前端

- 限速总览由"输入类型和 ID 查一个对象"改为规则列表，可按主体类型筛选，每行链接到归属对象的编辑页。
- 组织、用户编辑表单增加窗口输入；三处表单共用一个规则编辑组件，遵循 `web/AGENTS.md` 的复用顺序，先检索 `@/components/data-table`、`SettingsSection` 等现有组件。
- 七种语言文案走 `i18n-translate` 流程；Playwright 实测列表、筛选、跳转与保存。

### Phase 5：收尾

- 观察期后删除 `rate_limit_overrides` 与相关代码。
- 清理无引用的文案键。
- 同步受影响的文档；本方案与多层限速设计在验收后移到 `doc/done/`。

## 验收条件

1. Phase 1 前后，用户、组织、渠道的生效限额与现有测试结果一致；总览与中间件使用同一解析入口，由测试断言两者对同一主体给出相同的值。
2. Phase 2 的迁移在三种数据库上，全新建库与两种升级场景都通过；连续启动两次无变化；行数与取值与旧表一一对应；唯一索引生效。
3. 不存在的规则行表示沿用全局默认；删除规则后恢复全局默认。
4. Phase 3a：组织规则带窗口 30 秒、上限 2 时，第三个请求被拒，`Retry-After` 不超过 30 秒，提示文案显示规则窗口；未配窗口时行为与全局一致。
5. 多节点：某节点修改规则后，其他节点在一个同步周期内生效。
6. 写入权限不变：非 root 写组织规则返回 403；非管理员写用户规则返回 403；限额不出现在 `User.Setting`。
7. 全局 option 的四个键仍被读取，未配置任何规则时行为与 v1.0.21 一致。
8. 响应头、`rate_limited_scope` 与 Retry-After 行为不变。
9. 如改动 `relaykit/`，独立构建通过；如触及计费文件，先完整阅读 `.agents/rules/billing.md`。
10. 前端：`typecheck`、`lint`、相关测试通过；浏览器内验证列表与表单。

## 风险与注意事项

- **发布门槛**：`dev` 的 schema 改动已会让部署守卫停止自动部署。需要在发布计划中人工确认最终结构；越晚统一，越可能把"先发旧表、再换新表"变成两次生产迁移。
- **文案变化**：窗口按规则渲染后，原来的"分钟"文案会变；后端 i18n（en、zh）与前端都要改。
- **多节点缓存**：规则缓存沿用现有的定期刷新，规模压测沿用验收条件 14 的方法（10 万行量级）。
- **渠道并入的事务边界**：见 Phase 3b；这是本方案中唯一可能引入新一致性问题的位置，因此设为决策门。
- **MySQL 最低版本**：现有 `rate_limit_overrides` 的验证没有覆盖 MySQL 5.7.8 与 PostgreSQL 9.6，本方案的 Phase 2 要补上。

## 不在范围内

TPM 与并发限制；同一主体多个窗口；组织内成员分配；组织占比借用空闲容量；Redis Cluster 的 hash tag；账单与额度规则。

## 待确认

1. 是否把渠道并入规则表（Phase 3b），还是只统一解析层？
2. 渠道内组织占比保持百分比（与渠道预算联动），还是改存绝对值？
3. 是否要求在首次发布前完成（本方案建议如此）？
4. 用户、组织规则的窗口是否允许精确到秒（全局窗口目前以分钟为单位）？
5. `feature/channel-org-share` 是否先合并再开工？

## 验证结果

（实施后填写）

## 阻塞原因

（不适用）
