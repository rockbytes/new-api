---
title: 个人 Key 用量结算与首次发布
status: done
created: 2026-11-27
updated: 2026-11-27
completed: 2026-11-27
---

## 目标

个人付款的 API Key 用量只在确认资金结算时入账。主库独立于日志保存累计并以结算标识去重，不改变组织账本或定价，不重写历史资金记录。

## 验收条件

- 钱包与订阅结算、用量增量及去重记录同事务（令牌额度仍沿用独立的原更新路径）；失败不写累计，重试不重扣。无 Key、组织付款、违规费与退款不计入。
- 异步差额使用任务快照，补扣原子累计，向下调整不冲减历史累计。关闭或清理日志不影响累计。
- 首次发布排空旧节点并暂停日志清理，回填成功后才切换；旧后续滚动流程不变。

## 实施计划

不再添加一套计费状态机。主库结算事件仅有未提交、已提交两个状态，使用唯一标识和现有事务；失败回滚，不依赖消费日志触发修复。异步任务以已持久化的旧额度作为 CAS 条件，回写新额度与资金、累计同事务。个人 API Key 钱包预扣使用主库余额，其他旧批量路径不在此次修改范围。首次部署用一次显式维护流程隔离新旧写入，见 [发布步骤](personal-key-usage.md)。

## 验证结果

- `go build ./...` 通过。`go test ./model ./service ./controller ./relay ./relay/channel/openai ./router -skip '^(TestAccessTokenRouteRulesCoverEveryDashboardRoute|TestResponsesStreamOutcomesPreserveAccounting)$' -count=1` 通过。两项跳过分别在未改动基线和第一批工作树单独复现；后者只有部分 Responses WebSocket 后续请求 429/500 断言不符。
- 配置隔离的 MySQL、PostgreSQL 和 ClickHouse 后，`go test ./controller -run '^TestUserListPersonalKeyUsage$' -v -count=1` 通过，涵盖 SQLite 3.50.4、MySQL 8.0.46（额外覆盖 `clientFoundRows=true`）、PostgreSQL 18.3、独立 ClickHouse 24.8.14.39。新建及升级、重复迁移、结算失败回滚、同标识重试、删除日志后累计值都已验证。`go test ./model -run '^Test(SubscriptionRefundAcrossDatabases|FailedTaskRefundAcrossDatabases)$' -v -count=1` 同时通过三引擎第一批回归。
- WSL Linux 的 `go test -race ./controller ./router ./model ./service -run 'TestUserListPersonalKeyUsage/sqlite|TestRealtimeMetersUsageOnceForSelectedPayer|TestFailedTaskRefundAcrossDatabases/sqlite|TestAsyncFailureRefundRecoversAfterTransactionRollback' -count=1` 通过；Windows 的检查无法启动 ThreadSanitizer（内存地址分配失败）。
- 本地实际网关升级 PostgreSQL 临时 schema，删除全部四条测试消费记录后，列表和搜索仍显示已回填的 2,000,000 额度。`bash -n deploy/upgrade.sh` 和隔离的 `bash deploy/upgrade_test.sh` 通过；未触碰生产服务。
- 用户功能前端 `bun run typecheck`、`bun run lint`、`bun run test src/features/users` 和 `bun run build` 全部通过。

## 边界

消费统计不会增加未成功确认的结算；失败的同步请求或已终态任务没有新增跨进程的主动重放机制，未确认的结算仍按现有重试/人工核对方式处理。任意时刻重放保护只针对已有结算标识或任务 CAS，不替代上游请求幂等，也不自动修复第一批之前的历史余额。令牌额度的独立更新仍可失败，不改变已提交的付款方资金与累计值。
