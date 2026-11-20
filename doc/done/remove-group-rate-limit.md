---
title: 删除分组限速代码
status: done
created: 2026-11-20
updated: 2026-11-20
owner:
completed: 2026-11-20
---

## 目标

删除用户 × 分组的限额覆盖（`ModelRequestRateLimitGroup`）及其代码路径。依据见 [分层限速设计](../in-progress/rate-limit-layered-design.md) 的 D5：分组不作为限速层，组织、用户、渠道三层已覆盖需要的保护。

删除之后，用户层的限额只按用户计数，不再按令牌的分组区分。

## 现状

生产中 `options` 表的 `ModelRequestRateLimitGroup` 值为 `{}`，即没有任何分组覆盖。删除它不改变生产行为。

涉及的位置（2026-11-20 代码检索）：

- 后端：
  - `setting/rate_limit.go`：分组映射与 `GetGroupRateLimit`。
  - `model/option.go`：读写该选项的分支。
  - `controller/option.go`：选项接口中的处理。
  - `middleware/model-rate-limit.go`：`ModelRequestRateLimit` 按令牌分组查找覆盖，并把分组拼进用户计数 key（`用户ID:分组`）。
  - 测试：`middleware/model_rate_limit_test.go` 中的 `TestModelRateLimitCountsConfiguredGroupsSeparately`，以及 `controller/responses_websocket_test.go` 中的相关引用。
- 前端：
  - `web/src/features/system-settings/request-limits/rate-limit-section.tsx`：分组限额编辑区。
  - `web/src/features/system-settings/types.ts`：字段类型。
  - `web/src/features/system-settings/security/section-registry.tsx` 与 `security/index.tsx`：入口注册。
- 文档：`doc/todo/in-progress/channel-rate-limit.md` 中提到分组覆盖的部分；`docs/authentication.md` 中关于分组 key 的描述（若有）。

## 验收条件

1. 代码中不再有 `ModelRequestRateLimitGroup`、`GetGroupRateLimit` 及分组拼接的计数 key 引用。
2. 全局用户限额与组织总量的行为不变，相关测试通过。
3. 旧数据库中残留 `ModelRequestRateLimitGroup` 选项时，启动与选项读取正常，不报错。
4. 系统设置中不再显示分组限额编辑入口。
5. 不改变数据库表结构，因此不需要三库矩阵验证；若改动涉及选项的写入行为，需在 SQLite、MySQL、PostgreSQL 上各验证一次读写。

## 实施计划

1. 确认 `model/option.go` 加载选项时对未知键的处理，确保残留键不会导致启动失败。
2. 删除后端的分组映射、选项读写分支和 `GetGroupRateLimit`。
3. 修改 `ModelRequestRateLimit`：用户计数只用用户 ID，去掉分组查找和分组拼接。
4. 删除分组覆盖的测试，保留全局用户限额与组织总量的测试；修正 `responses_websocket_test.go` 中的引用。
5. 前端：删除分组限额编辑区与相关类型和入口。阅读 `web/AGENTS.md`。文案的清理只通过 `add-missing-keys.mjs` 与 `bun run i18n:sync`，不手改 locale 文件。
6. 更新文档中提到分组覆盖的部分。
7. 生产选项行的处理：保留数据库中的 `ModelRequestRateLimitGroup` 行（值为 `{}`），不做删除。在 `model/option.go` 的已退役选项列表中加入该键，使它加载时从内存中移除，不再出现在选项接口里。数据库行本身不改动。

## 决定记录

- 生产的 `ModelRequestRateLimitGroup` 行：保留，不删除（2026-11-20 确认）。该行由管理员通过后台保存设置时写入，项目本身的功能是上游主线的一部分。
- 保留的理由：代码删除后，该行没有任何作用，对限速、计费和请求策略都没有影响；删除生产数据没有收益。
- 处理方式：加入已退役选项列表，与 `retiredThemeOptionKey` 等现有做法一致。

## 验证结果

（2026-11-20）

- 代码检索：`ModelRequestRateLimitGroup`、`GetGroupRateLimit`、`CheckModelRequestRateLimitGroup`、`UpdateModelRequestRateLimitGroupByJSONString`、`ModelRequestRateLimitMutex`、`RateLimitVisualEditor`、`rate-limit-dialog` 在代码与文档中无残留引用（已排除计划与历史文档）。
- 后端：`go build ./...` 通过；`gofmt` 对修改文件无输出；`go vet` 对 `setting`、`model`、`controller`、`middleware` 通过。
- 后端测试：
  - `go test ./middleware -run 'ModelRedis|ModelMemory|ModelRate|ModelRequest|OrganizationTotal|OrganizationRefusal|TokenModel|RequestRefused|Distribute'` 通过，全局用户限额与组织总量的用例保留并通过。
  - `go test ./setting/...` 通过。
  - `go test ./model -run 'Option|RateLimit|Retired|Frontend'` 通过，包括新增的 `TestRetiredModelRequestRateLimitGroupOptionIsNotLoaded`（退役键加载时不进入内存选项）。
  - `go test ./controller -run 'Option|ResponsesWebSocket|RateLimit'` 通过。
- 前端：`bun run typecheck` 通过；`system-settings` 目录 oxlint 无 error（剩余为原有警告，不在改动文件中）；`vitest run` 覆盖 `system-settings` 与 `organizations` 共 26 个文件、238 个用例，全部通过。
- 限速键：用户层的计数 key 不再拼接分组，`用户ID` 即为唯一主体。生产没有分组覆盖配置，计数行为与现状一致，历史计数无需迁移。
- 数据库：不改表结构，不需要三库矩阵验证；选项加载为内存逻辑，由单元测试覆盖。
- 未验证：
  - 本地 E2E 与生产旧库启动演练未执行，旧库残留键的加载路径仅由单元测试覆盖。
  - 系统设置页面不再显示分组入口，没有断言"不存在"的界面测试。
  - locale 文件中仍保留只属于分组限额的文案键（如 `Group-based rate limits`）。i18n 规则禁止手改 locale，而 `i18n:sync` 按 `en.json` 而不是代码引用清理键，需要单独处理。

## 阻塞原因

无。
