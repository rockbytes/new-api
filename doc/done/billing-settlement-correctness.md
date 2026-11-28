---
title: 计费结算第一批修复
status: done
created: 2026-11-27
updated: 2026-11-27
completed: 2026-11-27
---

## 目标

修复旧 Realtime 重复扣费、旧 Midjourney 组织 Key 错扣、失败任务漏退及订阅退款。个人 Key 用量发布和历史修账不在本批。

## 验收条件

- Realtime 只使用一个计费会话；最终扣一次、按正确付款方；旧 Midjourney 在向上游提交前拒绝不能正确扣费的组织 Key。
- 轮询阶段失败的任务与退款状态独立，正常路径、渠道缺失及异常中断后均可收尾；同一任务不会因重试多退，老终态不自动退款。
- 订阅预扣退款和退款标记同事务，订阅重置后旧退款及向下结算不侵占新周期；正常同步和异步路径保持。
- 运行相关包及数据库三种真实方言的回归测试，不涉及生产账户或部署。

## 实施计划

任务使用持久化标记区分无需退款、待退款和已完成：轮询阶段任务进入失败时写入退款意图，主库事务锁定待退款任务，并提交原付款方变动及完成标记，失败保留待退款供轮询恢复。订阅沿用原预扣记录，增加原周期标识；同一事务锁定预扣记录和订阅余额，重试收到已退款时无动作。旧记录仅用时间推断已知的跨周期情况，不重算历史账。Realtime 复用已有会话状态，不引入新工作流。

## 验证结果

- `go build ./...` 通过；Realtime 钱包/组织两种付款方、组织 Key 令牌额度不足回退、旧 Midjourney 提交限制、原有主路径的 HTTP/WebSocket 集成回归通过。Realtime、任务退款与并发去重的 `-race` 专项测试通过。
- 配置隔离测试实例后，`go test ./model -run '^Test(SubscriptionRefundAcrossDatabases|FailedTaskRefundAcrossDatabases)$' -count=1 -v` 在 SQLite 3.50.4、MySQL 8.0.46、PostgreSQL 18.3 全部通过。覆盖全新结构、发布前等价结构升级、重复迁移、事务失败后重试、并发防重复、跨订阅周期和原余额保留。
- `go test ./service -run '^TestFixedPriceBillingDatabaseMatrix$' -count=1 -v` 在上述三种 SQL 引擎及各自独立日志库上通过。
- 全量 router 测试仍有已知基线失败 `TestAccessTokenRouteRulesCoverEveryDashboardRoute`，不涉及本批计费。`go test ./model ./service ./controller ./relay ./relay/channel/openai ./router -skip '^TestAccessTokenRouteRulesCoverEveryDashboardRoute$' -count=1` 通过。没有改生产数据、历史终态任务或部署。

## 未覆盖边界

任务提交后立刻失败时，原路径把零额度终态任务入库后由请求会话释放预扣；进程正好中断于这两步之间，仍不属于本批轮询退款恢复的保证。钱包批量预扣持久化、旧 Realtime 已转发的输出片段，以及分离日志和 Redis 的提交后更新不是本批主库退款事务的保证。
