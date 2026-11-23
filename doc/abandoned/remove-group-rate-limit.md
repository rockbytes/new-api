---
title: 删除分组限速代码
status: abandoned
created: 2026-11-20
updated: 2026-11-23
owner:
completed:
---

## 废弃原因

2026-11-23 决定还原请求数限速到 v1.0.28 基线，分组限速代码因此保留，本删除计划不再执行。文档仅作历史记录保留。

## 目标

删除用户 × 分组的限额覆盖（`ModelRequestRateLimitGroup`）及其代码路径。依据见 [分层限速设计](rate-limit-layered-design.md) 的 D5：分组不作为限速层，组织、用户、渠道三层已覆盖需要的保护。

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

（尚未开始实施。）

## 阻塞原因

无。
