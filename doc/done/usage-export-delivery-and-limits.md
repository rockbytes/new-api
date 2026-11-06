---
title: 用量导出：直接下载、有效期与下载限制
status: done
created: 2026-11-06
updated: 2026-11-06
owner: Rock
completed: 2026-11-06
---

# 用量导出：直接下载、有效期与下载限制

## 目标

1. 小导出直接流式下载，不在服务器上保存文件，也不生成导出任务记录。
2. 大导出异步生成，文件保留 24 小时，有效期内可以多次下载，到期后删除文件。
3. 限制同时下载和同时生成的数量，并限制每个下载的带宽，避免导出占满带宽、拖慢业务请求。
4. 多实例部署时，名额计数通过 Redis 共享。

## 已确定的设计决策（2026-11-06）

- **交付方式**：导出行数不超过 `USAGE_EXPORT_SYNC_ROW_LIMIT`（默认 5000）的记录表和工作簿，以及所有汇总表，走直接下载。更大的记录表和工作簿走异步任务。汇总表的行数很少，始终直接下载。
- **有效期**：异步任务文件保留 24 小时，有效期内可以多次下载。清理任务每小时运行一次，到期后删除文件并把任务标记为已过期；过期 7 天后删除任务记录。
- **下载限制**（全局，并可通过环境变量配置）：
  - 同时下载总数：`USAGE_EXPORT_DOWNLOAD_CONCURRENCY`，默认 8。
  - 每个用户同时下载数：`USAGE_EXPORT_USER_DOWNLOAD_CONCURRENCY`，默认 2。
  - 每个下载的带宽：`USAGE_EXPORT_DOWNLOAD_BYTES_PER_SECOND`，默认 2 MB/s（2097152）。
- **生成限制**：同时生成的导出总数（直接下载和异步任务共用）：`USAGE_EXPORT_GENERATE_CONCURRENCY`，默认 2。超出时直接下载返回 429，异步任务排队等待。
- **名额存储**：使用租约（有序集合，成员带过期时间）。下载期间每 15 秒续租一次，租约 60 秒。实例崩溃后租约自动过期，不会永久占用名额。时间由应用传入，两个实例在同一台主机上，时钟一致。
  - `RedisEnabled` 为真时使用 Redis，与现有限流中间件的做法一致。
  - `RedisEnabled` 为假时使用进程内存计数，适用于单实例部署。
  - Redis 操作出错时返回 503（`USAGE_EXPORT_UNAVAILABLE`），不退回内存计数。
- **接口**：
  - `GET /api/usage/exports/estimate`：返回 `delivery`（`direct` 或 `queued`）和行数，参数与报表一致。
  - `GET /api/usage/exports/direct`：直接下载，参数同报表。
  - `POST /api/usage/exports`：创建异步任务。
  - 任务列表、详情和下载接口保持不变，下载接口增加下载限制。
- **数据库**：不新增表和列，只删除过期的任务记录。
- **金额**：沿用上一轮的设计，导出金额按显示货币换算。

## 验收条件

1. 小导出在点击后直接下载，服务器上不产生文件，`usage_export_jobs` 中不新增记录。
2. 大导出生成异步任务，任务完成后可以在 24 小时内多次下载；超过 24 小时后下载返回 410，文件被删除。
3. 同时下载数超过总上限或单用户上限时返回 429，名额释放后可以继续下载。
4. 同时生成数超过上限时直接下载返回 429，异步任务保持排队，名额释放后继续生成。
5. 下载速度不超过配置的带宽上限（单元测试验证令牌桶的节流逻辑）。
6. 实例异常退出后，租约在过期时间后自动释放，不需要人工清理。
7. Redis 不可用时，导出接口返回 503。业务接口的行为与现有限流一致（见决定记录）。
8. 任务记录在过期 7 天后从数据库删除。

## 实施计划

### 后端

1. `service/usage_export_limits.go`：名额租约接口，包含进程内实现和 Redis 实现（Lua 脚本，原子地清理过期租约并获取名额）。
2. `service/usage_export_throttle.go`：令牌桶限速的 `io.Writer` 包装。
3. `service/usage_export.go`：
   - 去掉 `inline` 分支，异步任务的生成在获取生成名额后执行。
   - 新增 `EstimateUsageExport` 和 `StreamUsageExport`（直接下载，写入 HTTP 响应）。
   - 有效期从 7 天改为 24 小时。
4. `service/usage_export_cleanup.go`：增加删除过期 7 天任务记录的步骤。
5. `model/usage_export.go`：新增按过期时间删除任务记录的函数，在三种数据库上都能工作（使用 GORM 的 `Where` 和 `Delete`）。
6. `controller/usage_export.go`、`router/api-router.go`：新增 `estimate` 和 `direct` 接口，下载接口加入下载名额和限速。
7. 测试：扩展已有的 `service/usage_export_test.go` 和 `controller/usage_export_test.go`，新增一个限制测试文件（租约、限速、Redis 不可用）。

### 前端（`web/src/features/usage-report/`）

1. `api.ts`：新增 `getUsageExportEstimate` 和 `downloadUsageExportDirect`。
2. `components/usage-export-panel.tsx`：点击导出时先调用 estimate；`direct` 直接下载，`queued` 创建任务并提示。任务列表增加“到期时间”列。
3. 错误处理：429 显示“下载或导出数量已达上限，请稍后再试”；503 显示“导出服务暂不可用”。
4. i18n：新增文案在 7 种语言中补齐。

### 验证

- 后端：`go build ./...`，`go test` 改动包（`model`、`controller`、`service`），`gofmt`。
- 前端：`bun run typecheck`、`bun run lint`、`bunx vitest run src/features/usage-report`、`bun run build`。
- 数据库：本次没有新增表和列，不需要迁移验证；删除过期任务的查询在 PostgreSQL（`newapi_e2e`）上执行验证。MySQL 不做，按用户的决定。
- 端到端：本地后端连接 `newapi_e2e`，使用 Redis 实例（或兼容的测试实例）验证直接下载、异步任务、多次下载、429 限制和带宽限制。测试数据和账号在结束后清理。

## 验证结果

- 后端：`go build ./...` 通过；`gofmt` 和 `go vet` 对改动包无输出；`go test ./model/ ./controller/ ./service/ -run Usage` 通过；`go test ./common/limiter/` 通过（未改动，作为 Redis 限流回归）。新增测试覆盖：
  - 名额租约：进程内和 Redis（miniredis + Lua）两种实现的上限、释放、过期、续租；
  - 下载名额：全局上限与单用户上限，超出时拒绝；
  - 生成名额：直接下载在名额满时拒绝，排队任务在超时后失败；
  - 限速：令牌桶在固定时钟下的休眠时长；
  - Redis 不可用：下载名额和生成名额返回 `ErrUsageExportUnavailable`，控制器映射为 503；
  - 交付方式：超过同步行数阈值时估算为排队，直接下载被拒绝；
  - 直接下载与排队任务的输出逐字节一致，且直接下载不新增任务记录；
  - 清理：遗留排队任务失败，过期 7 天的失败和已过期记录删除，成功记录保留。
- 前端：`bun run typecheck` 通过；`bunx oxlint` 对改动文件无错误；`bunx oxfmt --check` 通过；`bunx vitest run src/features/usage-report src/lib` 17 个文件、112 个用例通过；`bun run build` 通过。
- 数据库（PostgreSQL 18.3，`127.0.0.1:5432` 的 `newapi_e2e`）：本次没有新增表和列。在临时 schema 中执行清理语句（遗留排队任务失败、过期记录删除）的验证通过，临时 schema 已删除。MySQL 按决定不做。
- 端到端（本地后端连接 `newapi_e2e`，Redis 使用兼容协议的测试服务，同步行数阈值设为 3，浏览器操作）：
  - 小导出（2 行）直接下载：未新建任务记录，未在服务器保存文件，审计记录为 `usage_export.direct_downloaded`；
  - 大导出（5 行）排队：提示“导出已加入队列”，任务状态为就绪，有效期 86400 秒；
  - 同一任务两次下载均成功（审计记录 2 条）；
  - 用 Redis 占满测试用户的 2 个下载名额后，直接下载返回 429，页面提示“正在进行的下载过多，请稍后再试。”；释放后恢复正常；
  - 过期任务在列表中显示为“已过期”且没有下载按钮。
- 测试数据与测试账号已在结束后清理，数据库行数恢复到测试前状态。

## 决定记录

- 2026-11-06：验收条件 7 的范围按现状确认。Redis 不可用时，导出接口返回 503，但项目已有的全局限流中间件 `GlobalAPIRateLimit`（以及启用时的模型限流）在 Redis 出错时返回 500，整个 `/api` 与转发接口随之失败。这是现有的失败关闭设计，本次不修改。Redis 的可用性由部署层面保障（持久化、主从或哨兵）。如需在 Redis 故障时放行全局 API 限流，可通过 `GLOBAL_API_RATE_LIMIT_ENABLE` 关闭，这是运维决定，不作为默认做法。
- 已知限制：Redis 名额的计数依赖 Redis 的时钟（由应用传入），两个实例在同一主机上，时钟一致。
