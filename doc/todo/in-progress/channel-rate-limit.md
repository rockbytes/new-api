---
title: 渠道级请求限速
status: in-progress
created: 2026-11-18
updated: 2026-11-20
owner: Rock
completed:
---

# 渠道级请求限速

## 目标

1. 为每个渠道设置请求速率上限，防止单个上游账号被本网关打爆，触发上游 429 或账号风控。
2. 超限时优先换用同优先级的下一个渠道，用户无感；全部候选耗尽才返回 429。
3. 拒绝发生在建连、预扣额度、占用 goroutine 之前，且不产生任何副作用。
4. 配置落在已有的 `Channel.Setting` JSON 字段中，不新增表或列。

## 已确定的设计决策（2026-11-18）

### 超限行为：直接 429，不排队

- 超限直接返回 `429` + 精确 `Retry-After`，与仓库现有全部限流点保持一致（`middleware/rate-limit.go` 的 `writeRateLimited`，Global/Critical/Search/EmailVerification/Model 各限流器均为直接拒绝）。
- **不做请求暂缓/排队。** 理由：
  - 排队会持有 HTTP 连接与 body 存储，与 `middleware/performance.go` 的 `SystemPerformanceCheck` 设计意图冲突。
  - 额度在 `controller/relay.go` 的 `PrepareRequestBilling` 处已预扣（重试循环之前），等待期间同用户其他请求可能因余额不足被拒。
  - Redis 额度是全局共享的，但队列只能在本机内存，N 个节点各排各的，退化为「sleep 后轮询 Redis」，无全局 FIFO 语义。
  - 排队会隐藏过载信号，使超时级联取代快速失败。
- 允许的例外仅为「有界等待」：配置项 `wait_budget_ms` 保留但**默认 0（关闭）**，硬顶 2000ms，仅当由配置推算的单次配额间隔 ≤ 2 秒时生效。v1 不实现，作为可选后续优化。

### 算法：令牌桶，不用固定窗口

- 渠道配额对应上游「每分钟 N 次」，固定窗口在窗口边界允许约两倍突发（`middleware/rate-limit.go` 注释已承认该语义），对上游 RPM 恰好是最不友好的形态。
- 采用令牌桶：速率 = `requests / duration`，容量略大于窗口内请求数（如 `requests`），既能均匀发放，又能吸收突发。
- 复用 `common/limiter/` 的令牌桶实现，但**必须补上 `common/limiter/lua/rate_limit.lua` 中被注释掉的 `EXPIRE`**，否则按渠道建 key 永不过期，导致 Redis 内存泄漏。
- 不阻塞、不排队：桶内有令牌立即放行，无令牌立即拒绝。突变流量全部即时通过，零等待。

### 配置位置：`Channel.Setting`，不新增列

- `ChannelSettings`（`relaykit/dto/channel_settings.go`）新增：

  ```go
  type ChannelRateLimit struct {
      Enabled         bool `json:"enabled,omitempty"`
      Requests        int  `json:"requests,omitempty"`
      DurationSeconds int  `json:"duration_seconds,omitempty"`
  }
  // ChannelSettings 内
  RateLimit *ChannelRateLimit `json:"rate_limit,omitempty"`
  ```

- 选择 `Setting` 而非 `OtherSettings`：该字段已是管理员可编辑、由 `Channel.ValidateSettings()` 统一校验的 JSON，前端表单走同一条通道。
- **零数据库迁移**：不触碰 schema，无需 SQLite/MySQL/PostgreSQL 三库验证矩阵，也避免 `AutoMigrate` 在布尔默认值上的反复 ALTER。
- 校验加入 `Channel.ValidateSettings()`：`requests > 0`、`1 <= duration_seconds <= 86400`，并做 `requests * duration` 的 int64 溢出检查（参照 `setting/rate_limit.go` 的 `maxRateLimitDurationSeconds` 写法）。
- `relaykit/` 为独立 module，此改动只用标准库，需以 `cd relaykit && GOWORK=off go build ./...` 验证。

### 作用域分层：v1 只做渠道层

四层作用域不是四选一，而是叠加保护不同稀缺资源，实际生效的是最紧的那个。

| 层 | 保护的真实资源 | 谁能配 | v1 决定 |
| --- | --- | --- | --- |
| 渠道 | 上游账号 RPM/TPM 配额（唯一外部强加的真实约束） | root/admin | **实现** |
| 用户 | 单账号公平性 | admin / 分组默认 | **复用已有**，不新增 |
| 组织 | 商业合同额度 | root | 暂不做 |
| Key | 凭据隔离 | 用户自己 | 暂不做 |

- **用户层不新增**：`ModelRequestRateLimit`（`setting/rate_limit.go`、`middleware/model-rate-limit.go`）已按用户 ID 计数。分组覆盖（原 `ModelRequestRateLimitGroup`）已于 2026-11-20 取消，见 [分层限速设计 D5](rate-limit-layered-design.md)。默认关闭（`Enabled = false`），机制完整。再新增一套会导致运维无法判断是哪一层在拦。
- **组织层暂不做**：`Organization` 结构体无 `Setting` JSON 字段，新增需加列并走三库迁移验证；且组织在本系统中是**计费主体**（`OrganizationQuotaAccount` / `OrganizationMemberQuota` 为资金账本），限额属于合同层，应等组织成为对外售卖单位时再评估。将来实现时，计数必须按实际付费方 `Token.BillingOrganizationId` 归属，而非按成员所属组织。
- **Key 层暂不做**：`Token` 全为显式列，同样需迁移；其保护的是自助场景，优先级最低，用户建两个 token 即可低成本隔离流量。
- **统一抽象预留**：闸门接受 `[]ScopeLimit{Scope, RateLimitSpec}`，按作用域顺序检查，任一超限即拒。`RateLimitSpec` 定义在 `service/` 内（不单独建包），待组织层落地后再考虑上移到 `pkg/`。

### 安全边界：配置位置必须与写入权限匹配

- `User.Setting` 是用户可写区（`model/user.go` 的 `SetSetting`，存通知渠道、语言、webhook 等偏好），管理员强制限额**不得**放在此处，否则用户可自行调高上限。
- `Channel.Setting` 仅管理员可写（渠道路由全部挂在 `AdminAuth` 下，见 `router/channel-router.go`），据此放置渠道限额是安全的。
- 规则：限额配置必须放在「能配它的人」与「受它约束的人」不同的位置。

### 拒绝语义必须可归因

- 限流拒绝必须携带 `types.ErrOptionWithSkipRetry()`。`ShouldRetryByStatusCode(429)` 返回 true，若作为普通 `NewAPIError` 进入 `service.DecideRelayRetry` 会被判为 `retry_status_matched`，消耗 `common.RetryTimes` 并重复分流。
- 使用独立 reason（`channel_rate_limited`）写入 policy events（`service/request_policy.go`），并记录作用域名（`rate_limited_scope`），否则线上无法区分「被本网关限流」与「上游返回 429」。
- 响应头带 `x-ratelimit-limit` / `x-ratelimit-remaining` / `x-ratelimit-reset`，兼容 OpenAI 风格客户端的指数退避。
- 不触发渠道自动禁用、不写渠道亲和缓存、不扣额度。

### 故障策略：容量保护面 fail-open

- Redis 不可用时**放行并记录日志**，与现有 `middleware/rate-limit.go` 的 fail-closed 不同。理由：鉴权面（登录、改密）失败关闭是必要的；渠道限流的目的仅是保护上游容量，Redis 抖动时让流量通过比让整站 429 更合理。
- 若 Redis 未启用，退化为进程内限流器，限额需按 `全局值 / 节点数` 折算（在文档中说明，不自动计算）。

## 验收条件

1. 未配置 `rate_limit` 或 `enabled=false` 的渠道，行为与改动前完全一致。
2. 渠道未超限时请求正常转发；桶内有突发容量时，瞬间多个请求全部即时通过，无任何延迟或挂起。
3. 渠道超限时，若同优先级存在其他可用渠道，自动换渠道，用户收到正常响应。
4. 换渠道不消耗 `common.RetryTimes`，也不改变重试的优先级档位索引。
5. 全部候选渠道超限时返回 429，带精确 `Retry-After` 与 `x-ratelimit-*` 头。
6. 被限流拒绝的请求不预扣额度、不写亲和缓存、不触发渠道自动禁用。
7. `policy events` 中出现 `channel_rate_limited` 决策与作用域名，可与上游 429 区分。
8. auto 分组场景下，因限流换渠道后跨分组索引不跳档。
9. pin 命中的渠道（`ChannelPin`）超限时直接返回 429，不重路由。
10. Redis 不可用时限流放行，服务不因此返回 500。
11. `cd relaykit && GOWORK=off go build ./...` 通过。

## 实施计划

### 后端

1. `relaykit/dto/channel_settings.go`：新增 `ChannelRateLimit` 类型与 `ChannelSettings.RateLimit` 字段，新增 `ValidateChannelRateLimit()` 并接入 `ValidateHTTPTransport` 同级校验路径。
2. `model/channel.go`：在 `ValidateSettings()` 中调用限流配置校验。
3. `service/channel_rate_limit.go`（新增）：
   - `RateLimitSpec` 与 `ScopeLimit` 类型。
   - `AcquireChannelSlot(c, channel) bool`：按渠道解析配置并占用令牌；Redis 路径复用令牌桶脚本，内存路径使用独立限流器实例（不与 `middleware` 共享，`Init` 只认首次配置）。
   - 无配置时零开销快速返回。
4. `common/limiter/lua/rate_limit.lua`：补上 `EXPIRE`，避免 key 泄漏。
5. `dto/channel_constraints.go` + `model/channel_constraint.go`：新增 `FilterChannelRateLimited` 与 `ExcludeChannelIDs`，`channelMatchesFilter` 增加纯成员判断分支，`filterEvalOrder` 置于最后以保证错误归因优先报硬约束。
   - **注意**：`filterCandidateIDs` 在 `channelSyncLock` 读锁内执行，该分支必须是纯函数，禁止任何网络 I/O。
6. `service/channel_select.go`：抽出 `SelectChannelWithBudget(c, modelName, retryParam)`，实现「选路 → 占预算 → 拒绝则加入排除集重选」循环，上限取 `min(候选数, 8)`。
   - 重选必须使用**相同的 retry 值**，不得调用 `retryParam.IncreaseRetry()`——`model/channel_cache.go` 中 retry 直接映射优先级档位，自增会静默降档。
   - auto 分组分支会改写 `ContextKeyAutoGroupIndex` 并调用 `SetRetry(0)`/`ResetRetryNextTry()`，循环内需使用 `RetryParam` 副本或在拒绝后恢复原始状态。
   - pin 命中时无候选可换，直接返回 429。
7. `middleware/distributor.go` 与 `controller/relay.go`：两处选路改为调用 `SelectChannelWithBudget`。
8. `service/request_policy.go`：新增限流拒绝事件与作用域名记录；`AppendUsedChannel` 轨迹中标注跳过原因。
9. 测试：集中在单个 `service/channel_rate_limit_test.go`，覆盖验收条件 1-11 的关键分支，使用 `testify/require` 与 `assert`。

### 前端

1. `web/src/features/channels/lib/channel-form.ts`：`parseChannelSetting` / `buildSettingJson` 增加 `rate_limit` 字段的读写。
2. 渠道编辑抽屉的高级设置区增加启用开关、请求数、窗口秒数三个输入框。
3. i18n：为 en、zh、zh-TW、fr、ja、ru、vi 七个 locale 补齐文案。
4. 若 v1 先只做后端，允许通过 `Setting` JSON 手填配置，前端留后续迭代；此时需在文档中注明。

## 未覆盖项与风险

1. **Responses WebSocket 路径**：`controller/responses_websocket.go` 未调用 `SetupContextForSelectedChannel`，走独立选路。实施前必须先核实该路径的接点，否则 WebSocket 请求会绕过渠道限流。
2. **公平性缺口**：仅有渠道限流时，单个用户可打满全部渠道，使其他用户收到 429。渠道层保护上游，但不保护用户间公平。缓解方式为同时开启已有的 `ModelRequestRateLimit`（系统设置，全局生效，影响所有用户的模型请求）。开启的参数需要在部署前单独确认，不属于本功能的代码改动。
3. **多 key 渠道**：预算按 `channel.Id` 计数，而非按单个 key。同一上游账号的多个 key 通常共享配额，按 key 记会超额。若后续需要按 key 限额，key 改为 `channel:<id>:<multi_key_index>` 即可。
4. **TPM 暂不做（2026-11-20 确认）**：v1 与当前需求只限请求数。上游按 token 计量时，长 prompt 仍可能打穿配额，待有明确需求时再引入 TPM，复用 `PrepareRequestBilling` + `RefundFailedRequestBilling` 的预占-结算骨架。
5. **固定窗口兼容性**：现有 `middleware/rate-limit.go` 的固定窗口脚本继续服务其他限流点，本次不改其语义，仅新增令牌桶路径。若后续统一，需单独评估边界双倍突发的行为变更。

## 验证结果

以下为 2026-11-20 在分支 `feature/channel-rate-limit` 上实际执行的结果。验收条件尚未全部满足，文档保持在 `in-progress`。

**已通过：**

- `go build ./...` 通过；`cd relaykit && GOWORK=off go build ./...` 通过；改动文件 `gofmt -l` 无输出。
- `go test -count=1` 通过：`./service/`、`./model/`、`./middleware/`、`./controller/`、`./common/...`、`./i18n/...`、`./dto/...`、`./setting/...`、`./relay/`、`./relay/common/...`；`relaykit` 的 `GOWORK=off go test ./...` 通过。
- 新增 `service/channel_rate_limit_test.go`：同档换渠道且不消耗重试索引；低档位不被降级替换；pin 渠道超限直接 429 不换渠道；内存模式下每窗口准入数等于配置值，并给出 Retry-After；被排除的渠道只对当次选择生效；配置校验（零值、溢出、超过一天的窗口）。
- 前端 `cd web && bun run typecheck` 通过；`bun run lint` 退出码 0，改动文件无告警；`bun run test` 运行 `channel-rate-limit-setting.test.ts` 与 `vertex-channel.test.ts`，6/6 通过。新增的测试锁住“保存渠道表单不丢失 `rate_limit`”。

**未验证（不能声称完成）：**

- Redis 路径：已在 WSL Ubuntu 24.04 的 Redis 7.0.15（`127.0.0.1:6379`，无持久化）上验证：`TestChannelBudgetRedisPathAgainstRealRedis` 通过，确认令牌桶准入数、Retry-After，以及 Lua 设置的 key 过期时间（TTL 大于 0 且不超过 窗口 + 60s）。通过环境变量 `NEW_API_TEST_REDIS_ADDR` 启用，使用 DB 15。Redis 错误时的 fail-open 仍未验证：limiter 是单例，测试中无法替换为故障客户端。
- 数据库矩阵：本次没有 schema 变更，未在 SQLite / MySQL / PostgreSQL 上验证。`ValidateSettings` 的新校验只在 Go 层执行，仅有单元测试覆盖。
- 端到端（2026-11-20，本机后端连 `newapi_e2e` 与 WSL Redis DB 14，模拟上游 OpenAI 服务）：渠道 A、B 同为优先级 10，各 2 次/60 秒；渠道 C 优先级 5，无预算。连续 5 个请求，结果为：前 4 个 200，第 5 个 429，`Retry-After: 30`，错误码 `channel_rate_limited`。模拟上游收到 A 2 次、B 2 次，C 0 次，被拒绝的请求没有到达上游。Redis 中 `rateLimit:channel:<A>`、`rateLimit:channel:<B>` 存在，TTL 为 93 秒（`60 + 60` 的过期窗口减去空闲时间），C 没有 key。测试渠道与令牌已删除，测试 DB 已清空。
- 测试中出现的两个非代码问题：`e2e-budget-model` 没有价格，返回 400（发请求前的配置问题，改用内置定价的 `gpt-4o-mini` 后解决）；渠道缓存每 5 秒同步一次，建渠道后需要等待。
- 已修复：缓存端点预选（`PrepareTaskPluginEndpoint`）现在也经过预算闸门（提交 `05fa8c3bf`）。该修复只有代码阅读和现有 middleware/service 测试覆盖，端到端测试没有走任务插件端点。
- Responses WebSocket：通过阅读代码确认它与 HTTP 共用 `service.SelectChannelForRequest`，并已接入 `SelectChannelWithBudget`，但没有专门的测试。
- 已修复：任务插件端点的缓存渠道分支（`PrepareTaskPluginEndpoint`）现在也经过预算闸门。
- 前端预算输入（2026-11-20，分支 `feature/channel-budget-ui`）：渠道编辑抽屉的「请求处理」区新增「请求预算」开关，开启后显示「每个窗口的请求数」和「窗口（秒）」。校验与后端一致：启用时要求请求数为 1 到 106751991167300 之间的整数，窗口为 1 到 86400 秒之间的整数；禁用时不校验。输入框不使用原生 `min`/`max`，避免浏览器在提交前拦截、绕过翻译后的错误文案。
  - `bun run typecheck` 通过；改动文件 oxlint 无错误；`vitest run src/features/channels` 为 31 个文件、372 个用例，全部通过。
  - 新增的 6 个文案键已写入七个 locale，`i18n:sync` 报告中缺失与多余键均为 0。
  - 未做浏览器内的视觉核对。
- Redis 故障放行（2026-11-20，分支 `feature/channel-budget-fail-open`）：新增 `TestChannelBudgetFailsOpenWhenRedisIsUnreachable`。Redis 地址不可达时，三次准入全部放行、没有 `Retry-After`，释放不 panic，并记录错误日志。真实 Redis 回归 `TestChannelBudgetRedisPathAgainstRealRedis`（DB 15）通过。`relaykit` 独立构建通过（验收 11）。
- 验收条件逐项状态（2026-11-20）：
  - 1、2、3、4、9：已有单元测试覆盖（未配置或禁用时不限流、突发即时放行、同档换渠道、不消耗重试索引、pin 不换渠道）。
  - 6：只覆盖"释放预算"路径；不写亲和缓存、不触发自动禁用未单独验证。
  - 10：已验证（本次）。
  - 7：已实现（Phase 6，分支 `feature/rate-limit-scope-attribution`）。拒绝写入 policy event，带 `rate_limited_scope: channel`、错误码 `channel_rate_limited` 与 `decision.reason: rate_limited`；在开启错误日志的本地后端上，从日志接口读回事件验证。
  - 5：部分实现（Phase 6）。渠道预算拒绝带 `x-ratelimit-limit`、`x-ratelimit-remaining: 0` 与 `x-ratelimit-reset`，reset 是补充一个请求所需的准确时间，与 `Retry-After` 相同。用户层与组织层拒绝的 `Retry-After` 是计算出的等待时间：令牌桶（Redis）按补足所需令牌的时间算，内存限流器按最早一条记录离开窗口的时间算（有在途预占时退回窗口长度）；成功量限制仍为窗口长度，是上限。成功响应也带 `x-ratelimit-limit` 与 `x-ratelimit-remaining`（取剩余请求数最少的一层），已实现，并在本地后端上验证。
  - 8：未验证。auto 分组下因限流换渠道后的跨档索引没有测试。
  - 因 5、8 未完成，本文档保持 `in-progress`。

## 阻塞原因

（不适用）
