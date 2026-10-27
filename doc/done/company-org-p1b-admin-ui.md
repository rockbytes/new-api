---
title: 公司组织 P1b：组织管理界面
status: done
created: 2026-10-27
updated: 2026-10-27
owner: TBD
completed: 2026-10-27
---

# 公司组织 P1b：组织管理界面

本计划是 [公司组织、成员权限、额度计费、用量看板与账单导出方案](../todo/in-progress/company-organization-membership-quota-billing-usage-dashboard-export.md) 的子计划之一，承接总方案实施计划第 5 步中的组织管理界面。产品决策以总方案的「已确定的设计决策」为准。

**依赖**：P1 的后端接口（组织、成员账号、密码重置与首次登录改密码）。页面只调用 P1 已提供的接口，不新增授权逻辑。模式以总方案 D11–D13 为准。

## 目标

1. Root 的「公司组织」管理页：列表；创建组织并直接创建首位管理员账号（用户名与初始密码）；查看详情（含成员）；改名；停用和恢复。
2. 公司侧「组织管理」页面，仅公司管理员可见：成员列表；创建成员账号（用户名与初始密码）；启用和停用成员；删除成员；重置成员密码；修改角色（admin / member）。
3. 首次登录改密码页面：成员账号或被重置密码的账号登录后，必须先修改密码才能进入其他页面。

## 验收条件

- 普通用户和公司管理员在导航中看不到 Root 的「公司组织」页面，直接访问时由后端拒绝。
- 公司管理员在常规导航中看到「组织管理」；普通成员看不到。
- 公司管理员的操作（创建账号、启停、删除、重置密码、改角色）在界面上反映后端结果。后端拒绝时（例如最后一位管理员保护），界面显示可理解的错误，不显示内部英文错误文本。
- 成员账号首次登录后被要求修改密码，修改完成后才能进入其他页面；修改前访问其他页面被拒绝。
- 页面请求不携带组织 ID；组织范围由后端从成员关系解析。
- 复用 `web/src/components/` 中已有组件。新增通用 UI 时，在验证结果中说明已有候选及不能复用的原因。
- 所有新增文案同步到 7 种语言（en、zh、zh-TW、fr、ja、ru、vi），按 i18n-translate 技能执行。
- 日期、数字、金额格式化使用 `@/lib/format` 和 `@/lib/currency`；传给 `Intl` 的语言经过 `toIntlLocale`。
- `bun run typecheck`、`bun run lint`、`bun run test`、`bun run build` 全部通过。
- 使用 playwright MCP 验证每个页面：截图；控制台没有错误；网络请求没有非预期的 4xx 或 5xx（权限拒绝等预期结果除外）。截图路径和检查结果写入验证结果。

## 设计要点

- **错误文案**：界面不直接显示英文内部错误文本。P1 的组织接口已约定返回稳定的 `code` 和 `message_key`（见 P1 文档的「设计要点」），本计划只负责前端映射：在 `web/src/lib/server-error-message.ts` 中为每个组织错误码添加映射，对应的文案写入 7 种语言的 locale 文件。未知错误使用通用文案。
- **菜单可见性**：Root 页面与公司侧页面各自按角色显示入口。菜单只是显示控制，授权始终由后端决定。
- **接口范围**：Root 使用 `/api/organization/`；公司侧使用 `/api/organization/self/`。

## 实施计划

1. 列出 P1 定义的组织错误码，确认每个错误码在 `web/src/lib/server-error-message.ts` 中都有映射。
2. 搜索 `web/src/components/` 中可复用的组件（表格、对话框、确认框、表单、空状态等），在验证结果中记录候选与复用决定。
3. 实现 Root 的「公司组织」页面及路由。
4. 实现公司侧「组织管理」页面及菜单可见性。
5. 实现首次登录改密码页面及登录后的拦截跳转。
6. 补充 7 种语言的文案。
7. 运行前端检查命令：typecheck、lint、test、build。
8. 启动后端和前端（见下方端到端说明），用 playwright MCP 分别以 Root、公司管理员、成员账号（首次登录改密码）走通主流程，记录截图与检查结果。

## 验证结果

验证基线：分支 `feature/org-p1b-admin-ui`，基于 dev `1e61ee13b`，实现提交为 `31cd418cd`。

**门禁约定**（用户于 2026-10-27 确认，替代计划正文中的原门禁）：

- typecheck 以 `bunx -p typescript@5 tsc -p tsconfig.app.json --noEmit` 为准。
- lint 以本次改动的文件无 error 为准。
- 测试接受负载相关的不稳定失败，另行处理。

### 前端检查

- `bun install --frozen-lockfile`：通过（1234 个依赖，无变更）。
- `bun run typecheck`：失败，与 dev 相同。脚本为 `bun check -b`，仓库中没有 `check` 脚本（`Script not found "check"`）。替代检查 `bunx -p typescript@5 tsc -p tsconfig.app.json --noEmit` 通过（exit 0）。
- `bun run lint`：失败，152 个 error、66 个 warning，全部位于 P1b 未改动的文件（如 `src/assets/logo.tsx`、`scripts/sync-i18n.mjs`、`src/features/home/`）。P1b 涉及的文件 lint 为 0 error，`oxfmt --check` 通过。
- `bun run test`：最近一次全量运行 2443 个用例中 2441 通过、2 失败。两个失败均为 `Test timed out in 20000ms`，分别位于 `channel-configuration.test.tsx` 与 `visual-billing-editor.test.tsx`，二者单独运行共 129 个用例全部通过。此前几次全量运行超时的文件各不相同。P1b 新增与修改的测试全部通过：`organization-admin-page.test.tsx`（3 例）、`password-change-form.test.tsx`（2 例）、`sidebar-config.test.tsx`（更新为包含组织入口）。
- `bun run build`：通过（exit 0）。

### 共享组件复用记录

- 未新增通用 UI 组件。页面均由已有组件组合：`SectionPageLayout`、`StaticDataTable`、`DataTableRowActionMenu`（成员行操作）、`ConfirmDialog`（暂停、删除）、`EmptyState`（非成员、非管理员）、`StatusBadge` + `statusPresets`、`Sheet` 与 `Form` 表单、`PasswordInput`、`accountPasswordSchema`、`sideDrawer*` 样式函数、`formatTimestamp`。
- 改密页使用 `Card`、`Form` 与 `PasswordInput`。
- 所有用户可见错误经 `server-error-message.ts` 映射为本地化文案，未显示英文内部错误文本。

### 端到端（playwright MCP）

环境：后端 `127.0.0.1:3100`，前端 `127.0.0.1:5173`，数据库 `newapi_e2e`。使用账号 `e2e_root`（Root）与 `e2e_admin`（E2E Co 管理员）。本次新建 `P1b Test Co`（首位管理员 `p1b_org_admin`）、成员 `p1b_member`，临时成员 `p1b_temp` 已删除。

- **Root「组织」页**：侧边栏管理员分组可见「组织」；个人分组无组织管理入口。列表、创建组织（`POST /api/organization/` 200）、详情含成员、改名（`PUT /api/organization/2` 200）、暂停（确认框，`PUT .../status` 200）、恢复（200）。截图：`root-organizations-list.png`、`root-org-detail-suspended.png`。
- **公司管理员 `e2e_admin`**：个人分组显示「组织」，管理员分组不显示。`/organization` 列出成员并显示角色与状态。创建成员（200）、设为管理员 / 设为成员 / 禁用 / 启用（均 200 并刷新列表）、重置密码（200，提示需下次登录修改）、删除成员（200，确认框文案正确）。将唯一管理员降为成员时后端返回 409，界面显示「组织必须至少保留一名启用的管理员。」。截图：`admin-org-members.png`。
- **首次登录改密**：`p1b_member` 与 `p1b_org_admin` 登录后，受限接口返回 403 `AUTH_PASSWORD_CHANGE_REQUIRED`，应用跳转 `/change-password`；直接访问 `/dashboard/overview` 被跳回改密页。确认密码不一致时显示「两次输入的密码不一致。」且未发送请求。修改密码（`PUT /api/user/self/password` 200）后跳转登录页，新密码登录后进入 `/dashboard/overview`。截图：`first-login-change-password.png`。
- **普通成员 `p1b_member`**：侧边栏无「组织」；`/organization` 显示「仅组织管理员」；`/organizations` 跳转 `/403`。
- **数据库核对**：`p1b_member` 为 member、`must_change_password=false`；`p1b_org_admin` 为 admin、`must_change_password=true`（尚未修改）；`P1b Test Co Renamed` 状态 active。
- **控制台与网络**：未出现 5xx。预期的错误包括：匿名会话刷新 401；非成员访问 `/api/organization/self` 返回 404（P1 契约，侧边栏每页读取一次，因此每次加载都会在控制台留下一条）；改密门禁下的 403；最后管理员保护的 409。控制台另有一条 Base UI `nativeButton` 警告，来自个人资料页 `LanguagePreferencesCard`，不在本计划改动范围。
- **操作说明**：部分按钮的 Playwright 点击因稳定性检查超时，改用 DOM click 触发同一 React 事件，结果已通过网络请求与界面文本核对。
- **未覆盖**：`p1b_org_admin` 修改密码后的完整登录流程（仅验证了强制跳转）；移动端版面；Root 对非活跃组织的更多操作组合。

## 阻塞原因

无。以下存量问题不在本计划范围内，已在门禁约定中说明处理方式：

- 仓库存量 152 个 lint error（90 个文件），dev 基线即存在，本计划未改动这些文件。
- `bun run typecheck` 指向不存在的 `check` 脚本，dev 基线即存在。
- 全量测试偶发 20 秒超时，失败文件每次不同，单独运行全部通过。
