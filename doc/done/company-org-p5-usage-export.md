---
title: 公司组织 P5：用量与账单导出
status: done
created: 2026-10-26
updated: 2026-10-28
completed: 2026-10-28
owner: TBD
---

# 公司组织 P5：用量与账单导出

本计划是 [公司组织、成员权限、额度计费、用量看板与账单导出方案](./company-organization-membership-quota-billing-usage-dashboard-export.md) 的子计划之一。产品决策以总方案的「已确定的设计决策」为准。

**依赖**：P4（数据范围解析服务与查询接口）。

## 目标

1. 导出看板汇总报表和使用日志明细，导出内容与屏幕使用相同的范围、筛选和口径。
2. 格式顺序（D6）：先实现 CSV（明细与汇总），再在同一查询结果上实现 XLSX 报表。PDF 不在本计划范围内。
3. 小型导出同步完成，大型导出创建异步任务，显示排队、生成中、可下载、失败、过期等状态。
4. 文件通过存储接口读写，v1 提供本地磁盘实现；下载走需登录的鉴权接口，不使用公开链接。
5. 导出任务、创建、完成、失败和下载均有审计记录。

## 验收条件

- 导出请求中的组织 ID、用户 ID 只作为筛选意图，服务端在生成前按当前身份重新授权。
- 导出任务记录发起人、授权范围、筛选条件、格式、时区、生成时间、统计截止时间、行数和过期时间。
- 下载时重新验证发起人当前权限；他人持有的任务 ID 或下载地址返回拒绝。
- 文件到期后自动删除，过期任务的下载返回明确的过期状态。
- CSV 为 UTF-8；可选 UTF-8 BOM；以 `=`、`+`、`-`、`@` 开头的文本单元格被转义，防止公式注入。
- 导出内容不包含 Key 明文、提示词、请求或响应正文，也不包含认证秘密。
- 大范围导出按行数和时间跨度设置上限，并通过流式读取限制内存占用。
- XLSX 的「汇总」「按模型」「按成员」「用量明细」工作表数值与 CSV 一致，不混合不同计量单位。

## 设计要点

- **存储接口**：定义 `ExportStore`（写入、读取、删除），v1 提供本地磁盘实现。本地磁盘只适用于单实例部署；多实例部署必须换为共享存储实现，本计划需在文档中明确这一限制。
- **任务状态**：状态保存在数据库，任何节点都能查询任务状态；文件路径不暴露给客户端。
- **清理**：过期文件由后台任务清理，清理动作记录日志，清理失败不影响下载授权。
- **XLSX 依赖**：实施前检查 `go.mod` 中是否已有 XLSX 库。如需新增依赖，需要说明选择理由、维护状态和许可证。
- **扩展性**：导出格式以「查询结果 → 格式写入器」的接口组织，新增格式只需新增写入器。
- **数据库**：导出任务表需在三种数据库上迁移验证。

## 实施计划

1. 定义导出任务模型与状态机，编写迁移。
2. 实现 `ExportStore` 与本地磁盘实现，并编写测试。
3. 实现 CSV 写入器，包括公式注入转义和 BOM 选项。
4. 实现同步与异步导出：达到阈值时创建任务，后台生成文件，更新状态。
5. 实现鉴权下载接口与过期清理任务。
6. 实现 XLSX 写入器，复用 CSV 的查询结果。
7. 实现审计事件，确认审计内容不包含文件内容和秘密信息。

## 实施决策

- **XLSX 依赖**：新增 `github.com/xuri/excelize/v2 v2.11.0`（BSD-3-Clause，2026-07 发布，仍在维护）。选择理由：Go 生态中最常用的 XLSX 库，提供流式写入（`StreamWriter`）。`go get` 同时把 `golang.org/x/crypto`、`net`、`sync`、`sys`、`text` 升到 excelize 要求的最低版本，合并前需确认这些升级。
- **XLSX 行数上限**：excelize 在关闭工作簿时才组装文件，内存占用与工作簿体积相关，因此 XLSX 上限为 100,000 行（CSV 为 500,000 行）。超出时提示改用 CSV。
- **工作表**：汇总、按模型、按组织（平台视图）或按成员（组织视图）、用量明细。个人视图没有排行，不生成排行工作表。
- **同一查询结果**：CSV 与 XLSX 读取同一组查询，XLSX 中的数值写为数字单元格，文本与 CSV 相同，文本同样做公式转义。
- **同步与异步**：明细行数不超过 5,000 行时同步完成，否则创建任务后台生成。每个用户最多同时 3 个排队或运行中的任务，超出返回 429 `USAGE_EXPORT_BUSY`。
- **限制**：时间跨度与看板相同（最长 400 天），时区偏移 ±14 小时，文件保留 7 天。
- **存储限制**：本地磁盘存储只适用于单实例部署。多实例部署必须先实现共享的 `ExportStore`，在此之前不得多实例运行导出。
- **审计限制**：审计事件由后台生成与导出请求共用，写入时不带请求上下文，因此不记录请求 IP 与 User-Agent。事件内容只含任务 ID、类型、格式、行数或错误码，不含文件内容、存储路径或密钥。
- **前端未纳入**：本计划的实施步骤只覆盖后端。接口已返回状态（queued、running、succeeded、failed、expired）与下载地址，导出入口与任务列表界面需另行确定。

## 验证结果

- 授权与越权下载：`controller/usage_export_test.go` 的 TestUsageExportForeignJobIsRefused（同组织成员与 root 查询或下载他人任务返回 403，未知任务返回 404）、TestUsageExportCreateRefusesAnotherOrganization（越权创建返回 403）、TestUsageExportDownloadRechecksTheCallersCurrentScope（发起人降为成员后下载返回 403）；`service/usage_export_test.go` 的 TestUsageExportAuthorizesTheCallerWhenCreated。
- 过期清理与过期状态：`service/usage_export_test.go` 的 TestUsageExportCleanupExpiresFilesAndFailsStaleJobs（过期文件被删除并标记 expired，未到期文件保留，陈旧运行任务转为 failed）；`controller/usage_export_test.go` 的 TestUsageExportExpiredFileIsGone（下载返回 410 与 USAGE_EXPORT_EXPIRED，到期任务即读作 expired）。
- CSV 公式注入转义：`service/usage_export_test.go` 的 TestUsageExportTextEscapesFormulaCharacters（表驱动，9 例），以及 TestUsageExportCSVAndWorkbookReportTheSameNumbers 中用户名 `=1+1` 在 CSV 与 XLSX 中都写为 `'=1+1`。
- 响应不泄露存储键：TestUsageExportResponsesNeverRevealStorageKeys。
- 迁移：`model/usage_export_test.go` 的 TestUsageExportJobMigratesAndTransitions，每个引擎迁移两次并跑完整状态机。通过的引擎：SQLite（测试内置）、MySQL 5.7.44、MySQL 8.0.46、PostgreSQL 9.6.24、PostgreSQL 17.11。
- 同步与异步一致：TestUsageExportAsyncRunMatchesInlineBytes（同一请求的同步路径与后台路径输出字节一致，含 BOM）；TestUsageExportCSVAndWorkbookReportTheSameNumbers（汇总、按模型、按组织、用量明细四个工作表与 CSV 逐格一致）。
- 并发上限：TestUsageExportLimitsActiveJobsPerUser。
- 访问令牌规则：`middleware`、`router` 中的 AccessToken、Route、Scope 测试通过。
- 构建与静态检查：`go build ./...`、`go vet`（service、model、controller）、变更文件 gofmt 通过。
- 全量 Go 测试（model、service、controller、middleware、router、common）：全部通过。并发上限改动在全量测试之后加入，之后重跑了导出与用量相关测试，通过。

## 阻塞原因

当前无阻塞。依赖 P4 已完成。
