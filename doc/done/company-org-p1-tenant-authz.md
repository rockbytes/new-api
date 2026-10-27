---
title: 公司组织 P1：组织租户与授权
status: done
created: 2026-10-26
updated: 2026-10-27
owner: TBD
completed: 2026-10-27
---

# 公司组织 P1：组织租户与授权

本计划是 [公司组织、成员权限、额度计费、用量看板与账单导出方案](../todo/in-progress/company-organization-membership-quota-billing-usage-dashboard-export.md) 的子计划之一。产品决策以总方案的「已确定的设计决策」为准（D1–D13）。

**范围说明**：本计划只包含后端：组织与成员账号的模型、授权、接口，首次登录改密，密码重置，删除守卫。组织管理的前端页面由 [P1b 组织管理界面](./company-org-p1b-admin-ui.md) 承接。本计划不包含邀请流程，模式见 D11–D13。

## 目标

1. 建立组织（Organization）与成员关系（OrganizationMembership）两个租户实体，组织状态为 `active`、`suspended`。
2. Root 创建组织时直接创建首位公司管理员账号并设置初始密码，组织立即生效（D11）。
3. 组织管理员创建成员账号（初始密码，首次登录必须改密），停用/启用、删除成员，重置成员密码（D12、D13）。
4. 公司角色 `admin` 与 `member` 的服务端授权；组织范围只来自已验证的成员关系，不信任客户端传入的组织 ID。
5. 组织始终至少保留一位有效管理员。
6. 不改变 `User.Role` 的平台角色语义，公司管理员不会被映射为平台 Admin。

## 验收条件

- Root 可以创建、停用、恢复组织，并在创建时设置首位管理员；普通用户和公司管理员不能执行这些操作。
- 组织管理员只能为本组织创建、停用/启用、删除、重置密码的成员账号；不能操作其他组织的账号。
- 成员账号首次登录后，只能访问查看自己信息、修改密码和退出登录的接口；其他接口返回 `AUTH_PASSWORD_CHANGE_REQUIRED`（403）。
- 改密后旧会话全部失效，用户用新密码重新登录，走完整的登录流程（包括二次验证）。
- 停用成员同时停用账号并撤销会话，成员不能登录或使用 API；启用后恢复。
- 删除成员在同一事务内删除账号和成员关系；账号的历史用量仍按消费时的组织快照归属。
- 不能停用、降级或删除最后一位有效管理员（`ORGANIZATION_LAST_ADMIN`）。
- 平台侧的删除路径（管理员删除用户、用户自助删除）对仍在组织中的用户返回 `USER_IN_ORGANIZATION`（409）。
- 公司管理员只能列出和管理本公司成员；改写路径参数、请求体或查询参数访问其他组织的成员时返回 404。
- 成员关系对 `user_id` 建立唯一约束（v1 一人一公司）。
- 组织状态流转由显式的状态转换表约束，非法转换被拒绝。
- 组织接口的业务错误返回稳定的 `code` 和 `message_key`；未知错误只返回通用文案。

## 设计要点

- **授权模型**：组织角色到操作的映射在服务层以目录形式定义（`service/authz/organization.go`），每次请求通过成员关系实时解析，不写入 Casbin 策略，避免与平台级权限混用。
- **账号创建**：新账号在事务内创建（`createOrganizationAccountWithTx`），密码使用 `common.HashAccountPassword` 存哈希，并设置 `must_change_password`；与成员关系同一事务，失败不留下部分数据。
- **首次登录拦截**：`authHelper` 检查缓存中的 `MustChangePassword`，只放行 `GET /api/user/self`、`PUT /api/user/self/password` 和 `POST /api/user/auth/logout`。用户缓存结构因此增加字段，缓存版本号从 2 升到 3，旧缓存条目会被丢弃。
- **改密后的会话**：改密后递增 `auth_version`，旧会话失效；不在接口里直接签发新会话，而是要求重新登录，这样新会话仍经过二次验证。
- **密码策略**：初始密码和重置密码使用 `User.Password` 的校验规则（8–128 字符）。
- **删除守卫**：平台侧删除（`HardDelete`、`delete`）在事务开头检查组织成员关系；组织内删除只能走组织接口（`DeleteOrganizationMember`），它与 `hardDeleteWithTx` 共用事务，同时删除账号与成员关系。
- **错误响应**：与 new-api 原生做法一致。业务错误同时返回稳定的 `code` 和 `message_key`，写法参考 `controller/access_token.go` 的 `writeAccessTokenError`。前端按 `code` 在 `web/src/lib/server-error-message.ts` 中映射文案（P1b）。
- **并发**：所有会改变管理员数量或成员状态的写操作先对组织行加 `lockForUpdate(tx)`，再计数。
- **扩展性**：一人一公司只由成员关系表的唯一索引保证，不编码进其他表，便于以后支持多公司成员。

## 认证与 OWASP 控制

- 适用的标准：OWASP ASVS（版本与条目按 AGENTS.md 的要求记录；本次未在线核对官方文档，条目编号需在合并前对照官方版本确认）。涉及的控制领域：
  - 密码存储与策略：初始密码、重置密码只存哈希，长度 8–128；
  - 首次登录与会话：未完成改密前会话只能访问白名单接口；改密后旧会话失效，重新登录走完整流程；
  - 访问控制：组织范围只来自成员关系，跨组织访问返回 404；
  - 日志：审计与错误日志不记录密码、令牌或会话凭据。
- 回归测试覆盖：
  - 失败：错误的当前密码、重复使用旧密码、过短密码、重复用户名、停用后不能登录；
  - 过期/撤销：改密后旧令牌被拒绝（`AUTH_SESSION_REVOKED`），重置密码后成员旧会话失效；
  - 重放与绕过：未改密账号访问白名单之外的接口被拒绝（中间件测试与 HTTP 端到端）。
- 未覆盖的部分：本地没有邮件或短信渠道，本计划不涉及邀请和找回密码，因此相关控制不适用。

## 实施计划

1. 组织与成员关系模型、状态转换表、授权目录（已完成）。
2. Root 创建组织时同时创建首位管理员账号；删除邀请、待接受、拒绝和 `pending_admin` 相关代码与测试（已完成）。
3. 组织管理员创建成员账号、停用/启用、删除、重置密码接口（已完成）。
4. `users.must_change_password` 字段、登录拦截、`PUT /api/user/self/password`（已完成）。
5. 平台侧删除守卫与组织内删除事务（已完成）。
6. 授权矩阵与最后管理员保护测试（已完成）。
7. 三数据库与升级迁移检查（已完成，见下）。
8. 稳定错误码与控制器测试断言（已完成）。
9. 端到端 curl 检查（已完成，见下）。

## 验证结果

以下为本计划完成时的验证记录，按类别列出，命令与结果均在对应小节中。

### 功能测试（`go test ./controller/ -run TestOrganization -count=1 -v`，8 项）

- SQLite 3.50.4：8/8 通过。
- MySQL 5.7.44：8/8 通过。
- MySQL 8.0.46：8/8 通过。
- PostgreSQL 9.6.24：8/8 通过。
- PostgreSQL 17.11：8/8 通过。
- 并发降级测试 `TestOrganizationConcurrentAdminDemotionKeepsOneAdmin` 在 MySQL 8.0 与 PostgreSQL 17 上各以 `-count=10` 运行，全部通过。

### 相关包测试

- `go test ./model/ -count=1`：通过。测试库迁移补上了 `Organization` 和 `OrganizationMembership`。
- `go test ./middleware/ -run 'Auth|Token|PAT|Session|Access'` 与首次改密拦截测试 `TestUserAuthRequiresPasswordChangeBeforeOtherRoutes`：通过。
- `go test ./service/authz/ -count=1`：通过。

### 构建

- `go build ./...`：通过。需要先执行 `cd web && bun install --frozen-lockfile && bun run build` 生成 `web/dist`，否则 `main.go` 的 `go:embed` 报错。
- `cd relaykit && GOWORK=off go build ./...`：通过（relaykit 未改动）。

### 数据库升级与新库初始化（分支代码，探针方法见下）

- 起点：最近发布版本 `v1.0.0-rc.47`，它的 `model/` 中没有组织相关的表。
- 探针：临时 worktree 中的程序调用 `common.InitEnv()`、`model.InitDB()`、`model.InitLogDB()`，并确认输出 `database migration started`；没有调用 `InitEnv` 时迁移会被跳过，这一点已经修正。
- 每个数据库：发布结构建立（运行两次）→ 插入标记用户 → 当前分支升级（运行两次）→ 读取标记用户的新列。
- 结果（五个目标全部 `migration ok`，无致命错误）：
  - MySQL 8.0.46：标记用户 `must_change_password=false`；`users.must_change_password` 为可空 `tinyint(1)`；组织表和唯一索引存在。
  - MySQL 5.7.44：同上。
  - PostgreSQL 9.6.24：标记用户 `must_change_password=false`。
  - PostgreSQL 17.11：标记用户 `must_change_password=false`。
  - SQLite 3.50.4：标记用户 `must_change_password=false`。
- 新库初始化（每个目标运行两次）：全部成功，组织表和唯一索引存在。
- NULL 处理：升级前插入的行在新列中为 NULL，读取时视为 `false`，五个目标均已验证。
- 日志库：未设置 `LOG_SQL_DSN` 时与主库共用同一数据库，本计划不改动日志表。ClickHouse 不可用，未验证，因为本计划不涉及日志库路径。

### 端到端（HTTP，`newapi_e2e`，后端端口 3100）

- 测试数据：在 `newapi_e2e` 中创建专用测试 root 账号 `e2e_root`，没有使用或修改已有账号。
- 首位管理员：`e2e_admin` 登录后访问 `/api/organization/self` 返回 403 `AUTH_PASSWORD_CHANGE_REQUIRED`；`/api/user/self` 返回 200；错误的当前密码返回 400 `AUTH_CURRENT_PASSWORD_INVALID`；改密返回 200 与 `relogin_required`；旧令牌返回 401 `AUTH_SESSION_REVOKED`；新密码登录后访问组织接口返回 200，角色为 `admin`。
- 成员账号：短初始密码返回 400 `ORGANIZATION_PASSWORD_INVALID`；创建成功；重复用户名返回 409 `ORGANIZATION_USERNAME_TAKEN`；成员登录后访问组织接口被拦截，改密后可访问，角色为 `member`，权限只有 `organization.view`；成员访问成员列表返回 403 `ORGANIZATION_PERMISSION_DENIED`。
- 管理员重置成员密码：成员旧会话返回 401 `AUTH_SESSION_REVOKED`；用重置后的密码登录，组织接口再次被拦截。
- 停用/启用（`PUT`）：停用后登录失败；启用后登录恢复。
- 删除：最后一位管理员删除自己返回 409 `ORGANIZATION_LAST_ADMIN`；管理员删除成员返回 200，成员列表只剩管理员，已删除成员无法登录。
- 未验证：平台侧删除守卫（`USER_IN_ORGANIZATION`）未通过 HTTP 验证。`DELETE /api/user/:id` 先要求安全验证，走不到模型守卫；该守卫由单元测试 `TestOrganizationMemberCannotBeDeletedThroughRootOrSelfPaths` 覆盖。

### 控制器包回归

- `go test ./controller/ -count=1` 对比 dev 基线（`ff7290f04`）：两边都是 109 个失败测试，失败集合逐项相同（无新增、无修复）；两边各有 267 处 `TempDir RemoveAll cleanup` 与 107 处 `no such table` 日志，数量相同，且没有非清理类的断言失败。
- 测试库迁移补充：`model/task_cas_test.go`、`controller/user_manage_test.go`、`controller/security_enrollment_test.go` 的测试库已迁移组织表，删除路径相关测试不再出现 `no such table: organization_memberships`。

## 未完成

- 审计：写操作由通用中间件记录，未为组织路由添加 action 文案映射，前端显示为通用的 `METHOD route`。该项不阻塞后端验收，随 P1b 一并处理。
- 平台侧删除守卫的 HTTP 层验证需要安全验证的操作流程，目前只有单元测试覆盖。

## 阻塞原因

当前不存在外部阻塞。
