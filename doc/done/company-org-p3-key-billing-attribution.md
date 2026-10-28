---
title: 公司组织 P3：Key 付款组织与计费快照
status: done
created: 2026-10-26
updated: 2026-10-28
owner: TBD
completed: 2026-10-28
---

# 公司组织 P3：Key 付款组织与计费快照

本计划是 [公司组织、成员权限、额度计费、用量看板与账单导出方案](../todo/in-progress/company-organization-membership-quota-billing-usage-dashboard-export.md) 的子计划之一。产品决策以总方案的「已确定的设计决策」为准。

**依赖**：P1（成员关系）、P2（划拨桶资金来源）。

## 目标

1. Key 保持属于具体用户（`Token.UserId`），增加可空的付款组织字段 `billing_organization_id`，表示由哪个公司承担费用。
2. 用户创建或更新 Key 时只能选择本人承担或自己有权使用的组织承担；服务端校验成员关系与角色，请求中不能临时覆盖付款组织。
3. 同步、流式、图片、音频、任务插件和后台任务的预扣快照、消费日志和结算记录，都携带不可变的付款组织、Key 所属用户和 Key 归属。
4. 成员离开组织时，其公司承担的 Key 同步停用（D4），不静默改为个人承担。
5. 历史记录不随成员关系变化而改写。

## 验收条件

- 创建 Key 时选择公司承担，服务端验证调用者是该组织的有效成员；非成员或无权限者被拒绝。
- 请求正文、Header 或查询参数中的组织 ID 不影响付款归属。
- 公司承担的 Key 只从员工分配额度与组织额度预扣，任一额度不足时直接拒绝，不回退个人钱包或订阅（D2、D3）。
- 个人 Key 的计费偏好（`wallet_first` 等）保持现有行为。
- 消费日志与任务记录保存请求时的付款组织、用户、Key、模型、渠道和计费结果；成员之后离开或转移不改变这些字段。
- 成员离开时其公司承担的 Key 被停用，历史日志仍按消费时的归属查询。
- 组织被停用时，其所有成员的公司承担 Key 同步停用；组织恢复后这些 Key 不会自动启用。
- 所有请求路径（同步、流式、图片、音频、任务插件、后台任务）都有对应的表驱动测试覆盖付款归属的写入。

## 设计要点

- **Key 模型**：`model/token.go` 中 `Token` 已有 `UserId`、`Group`、`AllowUserQuotaQuery` 等字段。新增 `billing_organization_id` 为可空列。`AllowUserQuotaQuery` 允许 Key 查询所属用户的额度，公司承担的 Key 需要单独评估该能力是否开放，避免暴露公司余额结构。
- **日志模型**：`model/log.go` 中 `Log` 已有 `UserId`、`TokenId`、`Group`、`ChannelId`、`Ip`、`Other` 等字段。新增 `billing_organization_id` 快照列并建立索引，索引需要同时覆盖 `(billing_organization_id, created_at)` 等查询形态。`Other` 字段中的 `admin_info` 仅管理员可见的约定保持不变。
- **任务模型**：`model/task.go` 中 `Task` 同样需要携带快照字段，并与 `TaskBillingContext` 的结算路径保持一致。
- **独立日志库**：AGENTS.md 要求覆盖独立配置的日志库（可能是 ClickHouse）。新增日志字段必须同时确认在日志库上的写入和查询行为。
- **计费规则**：本计划改动预扣和消费日志路径，实施前必须完整阅读 `.agents/rules/billing.md`。
- **迁移**：新增列必须可空、有默认值；SQLite 使用 `ALTER TABLE ... ADD COLUMN`；索引创建在三种数据库上都要验证。
- **扩展性**：快照字段只记录事实，不编码「一人一公司」规则。多公司成员以后只需要新增 Key 绑定校验，不需要迁移历史日志。

- **已确认的实施决策**（2026-10-28）：
  - **成员停用后的 Key**：成员被停用时，其公司承担的 Key 同步停用，不改为个人承担。成员重新启用后，Key 不会自动恢复，需要 Key 所有者自己重新启用。
  - **公司承担的 Key 不开放额度查询**：Key 的 `AllowUserQuotaQuery` 强制关闭，个人 Key 保持现有行为。
  - **绑定权限**：只有 Key 所有者本人，且在该组织为有效成员（不限角色）时才能绑定，组织管理员不能替成员绑定。绑定只影响之后的请求，历史记录不变。
  - **额度可见性**：组织管理员只看成员级别的分配量和已用量，不查看成员每把 Key 的额度上限或明细。Key 的额度上限只由 Key 所有者查看和修改。
  - **日志库与 ClickHouse**：快照列需要在主库、独立日志库、ClickHouse 三处完成迁移和查询验证，在 P3 内完成。
  - **组织停用**（2026-10-28）：Root 停用组织时，该组织下所有成员的公司承担 Key 同步停用，Key 仍绑定该组织。恢复组织不会自动启用这些 Key，需要 Key 所有者自己重新启用。
  - **违规费用**（2026-10-28）：违规费用仍从个人钱包扣，不归属公司付款组织，保持现有行为。

## 实施计划

1. 为 `Token` 增加 `billing_organization_id`，实现绑定、解绑、停用接口，并接入 P1 的权限校验。
2. 在预扣快照、`Log`、`Task` 上增加付款组织快照字段，编写迁移与索引。
3. 梳理所有 relay 路径（同步、流式、图片、音频、任务插件、后台任务），确认每条路径从快照取付款归属，而不是从请求正文取。
4. 实现成员离开时的 Key 停用逻辑，与 P1 的成员移除流程事务化。
5. 编写表驱动测试：按路径类型覆盖付款归属写入、成员变更后的历史不变、请求中组织 ID 被忽略。
6. 在三种主数据库和日志库上执行迁移与查询验证。

## 验证结果

方法：以 `main`（34003e9c2）构建的基线二进制先启动并写入种子数据（用户、Key），再由当前版本升级启动、重复启动；全新库由当前版本启动两次。通过条件：无 `[error]` / `FATAL`，`tokens.billing_organization_id`、`tasks.billing_organization_id`、`logs.billing_organization_id` 与 `idx_logs_billing_org_created_at`、`idx_tokens_billing_organization_id`、`idx_tasks_billing_organization_id` 存在，种子数据保留。重复启动时，通过 MySQL 通用日志或 PostgreSQL DDL 日志确认重启不再发出 DDL（SQLite 未采集 DDL 日志，只记录无错误与库完整性）。

| 数据库 | 版本 | 结果 |
|---|---|---|
| SQLite | glebarez/sqlite v1.11.0 | 升级库启动多次（含种子数据后），全新库启动两次，无错误；列与索引存在；种子数据保留；`PRAGMA integrity_check` 为 ok |
| PostgreSQL | 17.11 | 升级库启动三次、全新库两次，无错误；列与索引存在；重启 DDL 为 0（同时验证了 DDL 日志会记录探针语句） |
| PostgreSQL | 9.6.24 | 同上，无错误；重启 DDL 为 0 |
| MySQL | 8.0.46 | 同上，无错误；重启 DDL 为 0（通用日志探针 1 条被记录） |
| MySQL | 5.7.44 | 同上，无错误；重启 DDL 为 0（通用日志探针 1 条被记录） |

独立日志库（`LOG_SQL_DSN`，主库为 SQLite）：

| 日志库 | 结果 |
|---|---|
| PostgreSQL 17.11（基线升级后当前版本启动两次） | `logs.billing_organization_id` 与 `idx_logs_billing_org_created_at` 存在，无错误 |
| MySQL 8.0.46（全新，启动两次） | 同上，无错误 |

ClickHouse（`clickhouse/clickhouse-server:latest`，26.5.1）：

- 发现并修复一处回归：修复前，`logs` 表 DDL 不含 `billing_organization_id`，而 `Log` 写入会带上该列，全新库与升级库上所有消费日志写入都失败（`No such column billing_organization_id`），个人 Key 与公司承担的 Key 都会丢日志。修复：`CREATE TABLE` 增加 `billing_organization_id Int32 DEFAULT 0`，迁移时执行 `ALTER TABLE logs ADD COLUMN IF NOT EXISTS billing_organization_id Int32 DEFAULT 0`，覆盖由旧版本创建的表。
- 修复后验证：全新库与升级库（旧表中已有一行历史数据）上，个人与公司承担两类写入均成功；历史行保留且快照为 0，公司承担行快照为 5；两次迁移均无错误。
- 与 P3 无关的已知问题：ClickHouse 24.8 在 `migrateLOGDB` 创建 `audit_logs` 时因 JSON 列失败（需要 `allow_experimental_json_type`），该 DDL 已在 `main` 中存在，未在本计划内修改。
- 未覆盖：ClickHouse 表没有对应的二级索引（ORDER BY 未改动），按组织过滤为分区内扫描；当前没有按组织查询日志的接口，因此没有性能验证。

请求路径与缓存：

- 服务层表驱动测试 `TestOrganizationPayerIsUsedByEveryRequestPath` 覆盖同步、流式、图片、音频、异步任务五类路径，并验证个人 Key 不计入组织额度。
- 新增 `TestBillingOrganizationRoundTripsThroughRedisHashCache`：缓存中的公司承担 Key 保留付款组织，个人 Key 的字段为空。默认使用 miniredis；设置 `TEST_REDIS_ADDR` 时连接真实 Redis，并清空该实例的当前数据库（只用于一次性测试实例）。在 Redis 7.4.9 上通过，测试后无残留键。
- 新增 `TestOrganizationPayerPerRequestPathThroughHTTP`（`router/organization_payer_http_test.go`）：经真实中继路由（`SetRelayRouter`、`SetTaskPluginProtocolRouter`）和本地模拟上游，逐路径检查消费日志、组织账本和员工钱包。覆盖同步文本、流式文本、图片、音频和个人 Key；请求体中的 `organization_id` 被忽略。临时禁用中继中的付款组织后，四条组织路径全部失败，个人路径保持通过。
- 端到端（真实后端二进制，PostgreSQL 17.11 `newapi_e2e`，Redis 7.4.9）：组织 Key 的同步、流式、图片、音频请求均返回 200；日志的 `billing_organization_id` 为组织 ID；组织已用额度与日志合计一致（20052）；员工钱包不变；个人 Key 的请求只扣个人钱包。Redis 缓存中组织 Key 的条目为 `BillingOrganizationId = 1`，`AllowUserQuotaQuery = false`。
- 未覆盖：异步任务路径（提交、轮询结算、失败退款）的 HTTP 级端到端验证。服务层测试 `TestOrganizationPaidTaskSettlesAndRefundsThroughLedger` 覆盖了任务结算与退款的账本逻辑。

代码检查：`gofmt`、`go vet ./model` 通过。组织停用测试 `TestSuspendingOrganizationDisablesItsPaidKeysOnly` 验证：停用后公司承担 Key 被停用、仍绑定组织、个人 Key 不受影响，恢复后不自动启用。P3 相关测试（绑定、快照、成员停用与移除、组织停用、Redis 往返）在 SQLite、PostgreSQL 17.11、PostgreSQL 9.6.24、MySQL 8.0.46、MySQL 5.7.44 上各 8 项全部通过。

组织付款任务的结算与退款：复核时发现，付款组织的异步任务在轮询结算和失败退款时仍走钱包或订阅路径，失败退款会返还到员工钱包，而组织账本没有回退。已修复：提交时记录付款请求 ID（`PrivateData.BillingRequestId`），结算与退款改为 `SettleOrganizationRequestTo`，把账本中该请求持有的用量移动到目标额度（失败任务的目标为 0）；账本键包含目标额度，重复执行为空操作。测试：`TestOrganizationRequestSettlesToTargetThroughLedger`（SQLite、PostgreSQL 17.11、PostgreSQL 9.6.24、MySQL 8.0.46、MySQL 5.7.44 通过）和 `TestOrganizationPaidTaskSettlesAndRefundsThroughLedger`（SQLite，结算与失败退款后组织账本正确，员工钱包不变）。`go test ./service` 全包通过。

控制器测试：`TestUpdateOptionAliasBillingExprUsesPluginSchema` 与 `TestSecurityEnrollmentAccessToken*` 的断言全部通过，失败只出现在 Windows 清理临时目录时（`audit.db` 被占用），属于环境问题，与本次改动无关。

环境注意：测试期间 WSL 中的 Docker 服务多次被整体重启（WSL 发行版在无会话时自动关闭）。失败的运行（连接被拒绝）已重跑，本节只记录重跑后的结果。

## 阻塞原因

当前无阻塞。依赖 P1 和 P2 完成。
