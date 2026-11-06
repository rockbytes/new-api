---
title: 用量报表：模型筛选与金额显示
status: in-progress
created: 2026-11-06
updated: 2026-11-06
owner: Rock
completed:
---

# 用量报表：模型筛选与金额显示

## 目标

1. 用量报表支持按模型筛选，与现有的组织、成员、时间段筛选组合使用。
2. 报表中的消费数字从平台额度改为货币金额（显示货币口径），与使用日志页一致。
3. 导出（CSV/XLSX）也支持模型筛选，导出范围与屏幕上的报表一致。

## 已确定的设计决策（2026-11-06）

- **消费口径（方案 X）**：消费 = 消费行（类型 2）的 `logs.quota` 合计，包含钱包、订阅、组织额度三种资金来源的请求。资金来源只决定从哪个余额扣减，不改变单次请求的消费额度，因此报表不按资金来源拆分或过滤。
- **金额显示**：只用显示货币（`quota_display_type` / `usdExchangeRate`），与使用日志页的 `formatLogQuota` 一致。不做充值价（`price`）折算。
- **模型筛选范围**：精确匹配 `logs.model_name`，作用于汇总、趋势、模型表、排行、明细和导出。模型只能收窄范围，不能扩大授权范围。
- **模型选项**：候选项取当前授权范围和时间段内有用量的模型，不随已选模型收窄，也不受 `/usage/models` 的 50 条限制。
- **导出**：导出任务保存模型筛选条件，需要在 `usage_export_jobs` 表新增 `model_name` 列。

## 验收条件

1. 所有视图（平台、组织、个人）都能选择模型筛选；选择后汇总、趋势、模型表、排行、明细的数字都只统计该模型。
2. 模型选项列出授权范围内、当前时间段有用量的全部模型，选中某个模型后选项列表不变。
3. 报表中的消费数字显示为货币金额，格式与使用日志页一致，并随显示货币设置变化。
4. 导出请求携带当前模型筛选，导出文件只包含该模型的记录；未选模型时行为与之前一致。
5. 模型参数超长或包含非法值时返回 400，不影响授权范围。
6. 数据库变更在 SQLite、MySQL、PostgreSQL 上验证：全新数据库和旧版本升级两种场景，启动迁移至少执行两次，已有导出任务记录保留且 `model_name` 为空字符串。

## 实施计划

### 后端

1. `model/usage_report.go`：`UsageScope` 增加 `ModelName` 字段，`usageScoped` 在非空时追加 `logs.model_name = ?`。新增 `GetUsageModelNames`，忽略 `ModelName` 返回范围内的全部模型名。
2. `controller/usage_report.go`：解析 `model_name` 参数（去除首尾空白，长度上限 255），写入已解析的 scope。新增 `GET /api/usage/model-options`。
3. `model/usage_export.go`：`UsageExportJob` 增加 `ModelName` 列（`varchar(255)`，默认空字符串）。
4. `service/usage_export.go`：请求和任务都携带模型名；生成导出时重新应用到 scope。
5. `controller/usage_export.go`：创建导出请求接收 `model_name`，并在导出任务响应中返回。
6. 测试：扩展 `model/usage_report_test.go`、`controller/usage_report_test.go`、`controller/usage_export_test.go` 中已有的用例，不新增文件。

### 前端（`web/src/features/usage-report/`）

1. `types.ts`、`api.ts`：增加 `model_name` 查询参数、模型选项接口。
2. `index.tsx`：增加模型下拉框（所有视图可见），切换时页码重置为 1。模型选项查询的 key 不包含 `model_name`。
3. 金额显示：汇总卡片、趋势、模型表、排行、明细的消费数字改用 `formatLogQuota`。
4. `lib/export.ts`：导出请求携带 `model_name`。
5. i18n：新增文案在 7 种语言的 locale 文件中补齐（使用 `i18n-translate` 技能）。
6. 测试：更新 `__tests__` 中受影响的用例，覆盖模型筛选参数传递、金额显示和导出请求。

### 验证

- 后端：`go build ./...`，以及改动包的 `go test`。
- 前端：`bun run typecheck`、`bun run lint`、`bun run test`、`bun run build`。
- 数据库：SQLite、MySQL、PostgreSQL 各一个版本，全新库与升级库两种场景，记录版本、命令和结果。

## 验证结果

实施提交前已执行（分支 `feature/usage-report-model-filter`）：

- 后端：`go build ./...` 通过；`gofmt -l` 对改动的 Go 文件无输出；`go vet ./model/ ./controller/ ./service/` 无输出；`go test ./model/ ./controller/ ./service/ -run Usage` 通过，包含新增用例 `TestUsageModelFilterNarrowsReportsAndKeepsModelChoicesComplete`、`TestUsageModelFilterFollowsTheCallersScope`、`TestUsageExportCarriesTheModelFilter`。
- 前端（`web/`）：`bun run typecheck` 通过；`bunx vitest run src/features/usage-report` 6 个文件、25 个用例通过；`bunx oxlint -c .oxlintrc.json src/features/usage-report` 退出码 0；`bunx oxfmt --check` 通过；`bun run build` 通过。新增文案在 7 个 locale 文件中均已存在。
- SQLite 迁移：用 `dev`（`cdf4189a1`）构建旧库，写入一条导出任务；再用本分支对该库启动迁移两次，`model_name` 列已添加，旧记录读取为空字符串，其余字段保留；全新库迁移两次后写入和读取 `model_name` 正常。驱动为 `glebarez/sqlite`。
- 导出金额（第二轮）：导出的金额列按显示货币换算（USD、CNY、自定义货币、TOKENS），CSV 与 XLSX 一致；新增断言覆盖三种显示货币和记录表表头列名唯一性，`go test ./service/ -run TestUsageExport` 通过。
- PostgreSQL（`127.0.0.1:5432` 的 `newapi_e2e`，PostgreSQL 18.3）：
  - 升级：启动新版后端，`usage_export_jobs` 添加 `model_name varchar(255) NOT NULL DEFAULT ''`，原有 4 条导出任务保留。
  - 全新库：在 `newapi_e2e` 中新建独立 schema `usage_verify_fresh`，迁移执行两次并写入、读取，随后删除该 schema；`public` 未受影响。
- 端到端（新版后端连接 `newapi_e2e`，前端开发服务器 5173，Playwright 浏览器）：
  - 临时测试账号登录，造 5 条带标记的消费日志（`e2e-model-a` 2 条共 750000 额度，`e2e-model-b` 3 条共 300000 额度）。
  - 报表显示 $2.1、$1.5、$0.6 等金额；模型下拉列出两个模型；选择 `e2e-model-a` 后汇总、模型表、组织排行、明细都只统计该模型。
  - 用量明细 CSV 导出：`consumed_amount` 为 `1.000000`、`0.500000`，币种 `USD`；XLSX 金额单元格的数字格式为 `0.000000`。
  - 测试结束后删除了临时账号、造的日志、导出任务和导出文件，数据库行数恢复到测试前状态。
- 未覆盖：MySQL 的迁移验证；端到端中未切换到 CNY 或 TOKENS 显示，这两种由单元测试覆盖。

## 阻塞原因

验收条件 6 部分满足：PostgreSQL 的全新库、升级、重复启动已验证；MySQL 的迁移验证仍未运行。按本地约定，数据库检查只能使用 `127.0.0.1:5432` 的 `newapi_e2e`，不能使用 5433、5434 容器或 MySQL 容器。在拿到允许使用的 MySQL 实例之前，本变更不能声明与 MySQL 兼容。
