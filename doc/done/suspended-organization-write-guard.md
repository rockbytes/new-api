---
title: 暂停组织后的写操作冻结
status: done
created: 2026-11-16
updated: 2026-11-17
owner: TBD
completed: 2026-11-17
---

# 暂停组织后的写操作冻结

## 问题

暂停组织（Suspended）原本只做两件事：把 `organizations.status` 改为 `suspended`，并停用该组织承担的所有 Key。管理操作入口几乎没有组织状态检查，因此暂停之后仍然可以：

1. 新增组织成员（Root 侧入口）
2. 重置成员密码
3. 删除成员
4. 设置成员分配额度
5. 发放、调整组织额度
7. 通过用户编辑页把用户加入该组织

其中只有「组织管理员新增成员」「修改成员角色/状态」「绑定 Key 到组织」三处原本已有检查。结果是暂停形同虚设：组织被冻结之后，额度和成员仍可被改动。

## 目标

暂停组织表示整体冻结：成员、分配额度和组织额度都保持暂停那一刻的状态，直到恢复为止。

## 验收条件

- 暂停后拒绝：新增成员、修改成员角色/状态、重置成员密码、设置分配额度、发放额度、调整额度、把用户加入该组织、用该组织额度消费。
- 暂停后仍允许：对已扣费请求的结算与自动退款、移除成员、恢复组织、删除组织。
- 恢复组织后，上述被拒绝的操作全部立即恢复，且无需逐个改动成员行。
- 被拒绝的调用不改变任何额度数字。
- 拒绝统一返回 `ORGANIZATION_NOT_ACTIVE`。

## 设计要点

### 检查放在事务内，而不是控制层

检查写在 `lockActiveOrganization` 中（`model/organization.go`），它在 `lockOrganization` 取到行锁之后判断状态：

```go
func lockActiveOrganization(tx *gorm.DB, organizationId int) (*Organization, error) {
	organization, err := lockOrganization(tx, organizationId)
	if err != nil {
		return nil, err
	}
	if organization.Status != OrganizationStatusActive {
		return nil, ErrOrganizationNotActive
	}
	return organization, nil
}
```

放在模型层而不是控制层的原因：

- **无竞态**：控制层「先查状态、再调用模型」之间存在 TOCTOU 窗口，组织可能在两步之间被暂停。检查与写入同处一个事务、同一把行锁之下，窗口不存在。
- **覆盖所有入口**：Root 路径、组织自助路径、用户编辑页三个入口都经过同一批模型函数，一处检查即可覆盖，不会漏掉将来新增的调用方。
- **复用既有错误映射**：`ErrOrganizationNotActive` 已经被 `respondOrganizationError` 映射为 409 + `ORGANIZATION_NOT_ACTIVE`，前端 `server-error-message.ts` 与 7 种语言文案均已存在，无需新增。

### 两类写操作刻意不冻结

**1. 已扣费请求的结算与自动退款**

`SettleOrganizationQuota`、`SettleOrganizationRequestTo`、`RefundOrganizationPreConsume` 继续使用 `lockOrganization`。请求在暂停之前已经预扣的额度必须能够结算并退回，否则暂停会把在途请求的钱永久卡住。

同一轮工作里人工退款（`RefundOrganizationRequest`）已被整体移除，见下节「移除人工退款」，因此暂停冻结清单里不再有它。

**2. 移除成员**

`DeleteOrganizationMember` 继续使用 `lockOrganization`。删除组织要求「组织已暂停」且「没有剩余普通成员」（见 D14），而移除成员只有组织自助这一条路径，Root 侧没有对应接口。若把移除成员一并冻结，暂停的组织将永远无法清空成员，也就永远无法删除。

### 状态码

拒绝返回 **409 Conflict**，而不是方案初稿写的 403 Forbidden。原因是 `ErrOrganizationNotActive` 在仓库中既有的映射就是 409，且语义上「组织处于暂停状态」属于资源状态冲突而非权限不足。沿用既有映射可以避免同一个错误码对应两种状态码。

## 改动清单

改为 `lockActiveOrganization`：

| 函数 | 文件 |
| --- | --- |
| `CreateOrganizationMemberAccount` | `model/organization.go` |
| `UpdateOrganizationMember` | `model/organization.go` |
| `ResetOrganizationMemberPassword` | `model/organization.go` |
| `GrantOrganizationQuota` | `model/organization_quota.go` |
| `setOrganizationMemberAllocation` | `model/organization_quota.go` |
| `AdjustOrganizationUnallocatedQuota` | `model/organization_quota.go` |
| `ConsumeOrganizationQuota` | `model/organization_quota_billing.go` |

`SetUserOrganizationWithTx`（`model/organization.go`）单独处理：在成员关系查询之后才拒绝，使「重新保存一个成员关系未变的用户」仍是 no-op，用户编辑页在不改组织字段时不会因此失败。

前端无改动：错误码、英文文案和 7 种语言翻译均已存在。

数据库无改动：复用既有的 `organizations.status` 字段，无新增字段与迁移。

## 验证结果

构建、静态检查与三层测试全部通过，在真实 SQLite 与 PostgreSQL 实例上验证，并用真实服务跑了一遍端到端流程；MySQL 未覆盖，原因见下。

### 构建与静态检查

- `go build ./...` 通过。
- `go vet ./model/ ./service/ ./controller/` 无输出。
- `gofmt` 对改动文件无输出。

### 测试

| 命令 | 结果 |
| --- | --- |
| `go test ./model/ -count=1` | ok（22.4s） |
| `go test ./service/ -count=1` | ok（10.6s） |
| `go test ./controller/ -count=1` | ok（139.2s） |

新增 `TestSuspendedOrganizationRefusesChangesButStillSettlesAndWindsDown`（`model/organization_quota_test.go`），覆盖：8 类写操作在暂停后被拒、被拒调用不改变额度数字、已扣费请求仍可结算与退款、恢复后立即可写、暂停状态下仍可移除成员并完成删除。

调整 `TestOrganizationDeleteEndpointBlocksUntilClearedThenSoftDeletes`（`controller/organization_test.go`）：把发放额度提到暂停之前（该用例保护的是删除时的余额，不是发放的时机），并补充一条「暂停后发放额度返回 409 `ORGANIZATION_NOT_ACTIVE`」的断言。

### 数据库

改动涉及事务与行锁，按 `AGENTS.md` 的要求执行真实数据库验证。

| 引擎 | 版本 | 命令 | 结果 |
| --- | --- | --- | --- |
| SQLite | 3（`glebarez/sqlite`） | `go test ./model/ -count=1` | 18 个组织用例通过 |
| PostgreSQL | 18.3 | `TEST_ORGANIZATION_QUOTA_DIALECT=postgres TEST_POSTGRES_DSN=... go test ./model/ -run 'TestOrganization\|TestSuspendedOrganization\|TestUserListFilters' -count=1 -v` | 18 个用例全部 PASS |
| MySQL | — | — | **未验证**，本机无可用实例，经负责人确认本次不覆盖 |

本次无 schema 变更，不涉及迁移与升级路径验证。

### 端到端

在任务分支上以 `PORT=3100 go run main.go` 启动服务，连 `.env` 指向的 PostgreSQL `newapi_e2e`，用真实 HTTP 接口走完整流程。临时建了一个 root 账号和组织 `E2E Suspend Co`，验证结束后全部删除，库中只剩运行前已存在的数据。

| 阶段 | 操作 | 结果 |
| --- | --- | --- |
| active | 发放额度、Root 新增成员 | 200 |
| suspended · Root | 发放额度、调整额度、设置分配额度、新增成员 | 4 项全部 409 `ORGANIZATION_NOT_ACTIVE` |
| suspended · 组织管理员 | 查看成员、查看额度 | 200（读操作放行） |
| suspended · 组织管理员 | 新增成员、修改成员、重置密码、设置分配额度 | 4 项全部 409 `ORGANIZATION_NOT_ACTIVE` |
| suspended · 例外 | 移除成员 | 200，随后 `deletion-impact` 的 `blockers` 为空 |
| resumed | 发放额度、新增成员 | 200，恢复后立即可写 |
| 删除链路 | 清退成员 → 暂停 → 删除组织 | 200，组织状态变为 `deleted` |

两项顺带确认的行为：组织管理员账号在组织暂停期间**仍可登录**（印证「暂停只改组织状态、不动成员账号」的设计）；组织创建的账号首次登录仍强制改密码，与本次改动无关。

移除人工退款之后又跑了一轮，确认退款路由确实不在，且冻结逻辑未受影响。临时账号与组织同样在结束后删除。

| 阶段 | 操作 | 结果 |
| --- | --- | --- |
| 退款接口 | `POST /api/organization/:id/requests/:request_id/refund` | 404，带 token 与不带 token 都是 404（路由已不存在，而非鉴权拒绝） |
| active | 发放额度、Root 新增成员 | 200 |
| suspended | 发放额度、调整额度、新增成员 | 3 项全部 409 `ORGANIZATION_NOT_ACTIVE` |
| resumed | 发放额度 | 200 |

## 移除人工退款

同一轮工作里，人工退款被整体移除：`RefundOrganizationRequest`、`RootRefundOrganizationRequest`、路由 `POST /api/organization/:id/requests/:request_id/refund`、专属错误 `ErrOrganizationRequestUnknown` 及其错误码映射 `ORGANIZATION_REQUEST_NOT_FOUND`。

理由是它从来没有前端入口（D14 已决定组织额度页不提供退款表单），只能手工拼 `request_id` 调接口，实际不可用；而失败请求本身不扣费，已扣费的请求由结算差额自动返还，人工纠正的场景不存在。

移除后仍有保障的退款路径，全部保留：

| 路径 | 触发条件 |
| --- | --- |
| 结算差额退款（`SettleOrganizationQuota` 的负差额） | 最终用量低于预扣 |
| 任务结算退款（`SettleOrganizationRequestTo`） | 异步任务的实际用量低于预扣 |
| 请求失败退款（`RefundOrganizationPreConsume`） | 请求失败或中断，退回未结算部分 |

`ErrOrganizationRefundExceeded` 保留：它同时被 `refundOrganizationQuotaTx` 使用，而后者是上述三条路径共用的底层实现。

### 已知影响

已成功结算且被多扣的请求，从此没有纠正手段。若要修正，只能调整成员分配额度或组织额度来补偿，账本上会留下一次与请求不对应的调整记录。

混合负载测试原本用人工退款制造退款事件，现改为把前三个请求通过 `SettleOrganizationRequestTo` 结算回 0，账本对账覆盖不减弱。

## 遗留事项

- MySQL 未验证（见上表）。本次没有 schema 变更，改动仅为事务内的一次状态判断，不含方言相关语法。
- 重命名组织（`UpdateOrganizationName`）未冻结。它不改变成员与额度，且不在本次需求范围内。
