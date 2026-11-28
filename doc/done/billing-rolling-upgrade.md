---
title: 计费用量滚动升级
status: done
created: 2026-11-28
updated: 2026-11-28
completed: 2026-11-28
---

## 目标

首次发布不再因个人 Key 历史回填阻塞网关。允许历史与混用期间旧节点用量统计不完整，不跳过资金结算或退款。

## 验收条件

- 先运行一次增量加表、加列及索引准备命令，再用普通 B→A 流程。准备可重试，不读日志、不启动服务；未准备时新版副节点不能接流量。
- 新旧版本并行：旧节点仍可扣款，新节点保留原子累计和去重；旧 Redis 预扣及订阅的计划重置可被新版识别。
- 用户详情解释统计只由新版记录；补充三数据库兼容、滚动脚本和网关验证。

## 实施计划

不引入发布状态机或运行开关。唯一启动门槛是已提交的兼容表结构；将 DDL 与网关进程分离，保留 B→A 排空流程。旧版批量财务增量和旧版手动重置仍按操作手册限制。历史日志不参与累计初始化。操作步骤见 [首次发布手册](personal-key-usage.md)。

## 验证结果

- `go build ./...` 通过；`go test ./model ./service ./controller ./relay ./relay/channel/openai ./router -skip '^(TestAccessTokenRouteRulesCoverEveryDashboardRoute|TestResponsesStreamOutcomesPreserveAccounting)$' -count=1` 通过，排除两项此前已复现的基线失败。
- 配置隔离数据库后，`go test ./controller -run '^TestUserListPersonalKeyUsage$' -count=1 -v` 与 `go test ./model -run '^Test(SubscriptionRefundAcrossDatabases|RedisBatchReserveNeverFallsBackToStaleDatabaseBalance)$' -count=1 -v` 通过。覆盖 SQLite 3.50.4、MySQL 8.0.46、PostgreSQL 18.3、独立 ClickHouse 24.8.14.39 日志库，以及旧库、重复结构准备、真实扣款不依赖使用统计、旧订阅计划重置、共享 Redis 预扣和日志清理。
- 在本地 PostgreSQL `newapi_e2e` 临时 schema 上，运行旧网关、两次执行新版 `--migrate-billing-schema`、上线新版副节点再替换旧主节点。真实 HTTP 健康检查和用户列表通过：副节点在主节点替换期间仍健康，历史日志未回填。桩测试 `bash deploy/upgrade_test.sh` 对正常 B→A 和排空失败恢复均通过。
- 前端 i18n 同步、类型检查、lint、`bun run test src/features/users`（43 项）和构建通过。未操作生产服务器。
