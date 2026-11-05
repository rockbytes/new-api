---
title: 报错来源记录
status: planned
created: 2026-11-05
updated: 2026-11-05
owner:
completed:
---

# 报错来源记录

## 目标

前端用户调用网关出错时，管理员和 root 能在错误日志里快速判断错误来源（我方网关、用户请求、上游供应商、连接环节），以及出错的处理阶段，而不需要阅读错误文本逐条猜测。普通用户看不到这些信息。

判定在错误产生的位置完成，而不是事后解析错误文本。

## 非目标

- 不向上游传递网关请求 ID，也不新增任何发往上游的请求头。
- 不改变对外返回的 `type` 和 `code`。错误类型标准化是另一项方案，两者互不依赖。
- 不新增数据库列，不做数据库迁移。第一版字段写入错误日志已有的管理员扩展信息。

## 字段设计

每条错误日志在管理员扩展信息（`admin_info`）中增加两个字段。`admin_info` 在用户视图中会被删除，只对管理员和 root 可见。

| 字段 | 取值 | 含义 |
| --- | --- | --- |
| `source` | `gateway`、`client`、`upstream`、`network` | 错误来源 |
| `stage` | `auth`、`routing`、`billing`、`request_build`、`upstream_request`、`upstream_response`、`stream`、`response_build` | 出错的处理阶段 |

错误来源含义：

- `gateway`：我方网关的问题，包括鉴权、余额与额度、分组和模型权限、渠道配置、无可用渠道、我方的请求构造或响应构造失败。
- `client`：用户请求本身的问题，包括参数不合法、模型名不存在、请求格式错误。
- `upstream`：上游供应商或上游网关返回的错误，包括上游 4xx、5xx、返回内容无法解析、流式输出中途被上游断开。
- `network`：连接上游失败或超时。这类错误可能是上游宕机，也可能是我方到上游的网络问题，日志中统一标记为 `network`，不强行归责。

## 判定规则

| 场景 | source | stage |
| --- | --- | --- |
| 令牌无效、用户被禁用 | gateway | auth |
| 用户余额、组织额度、成员额度不足 | gateway | billing |
| 分组或模型权限不允许 | gateway | auth |
| 无可用渠道、渠道配置错误、参数覆盖失败 | gateway | routing |
| 请求参数不合法、模型名不存在 | client | request_build |
| 我方序列化或转换请求失败 | gateway | request_build |
| 连接上游失败、连接被拒绝、TLS 失败 | network | upstream_request |
| 上游请求超时 | network | upstream_request |
| 上游返回 4xx 或 5xx | upstream | upstream_response |
| 上游返回内容无法解析 | upstream | upstream_response |
| 流式输出中途被上游断开 | upstream | stream |
| 我方构造对外响应失败 | gateway | response_build |

## 实现要点

- 在 `NewAPIError` 上增加 `source` 和 `stage` 属性，由构造该错误的位置直接设置。已有的 `errorType` 和 `errorCode` 保持不变，内部判断逻辑（如本地错误判断）不受影响。
- `RecordErrorLog` 把这两个属性写入 `admin_info`。
- 日志详情页在管理员和 root 视图中显示错误来源和阶段。
- 错误来源和阶段不出现在对外的错误响应中，也不出现在普通用户的日志视图中。
- `relaykit/` 模块保持独立构建，不依赖根模块的日志结构。

## 验收条件

- 上表中的每一种场景都有对应的测试，断言 `source` 和 `stage` 的取值。
- 管理员和 root 的日志详情中能看到错误来源和阶段；普通用户的日志中看不到这两个字段。
- 对外错误响应的 `type`、`code` 和 `message` 与改动前一致。
- 未新增数据库列，未修改任何迁移。
- 注入以下故障并人工确认日志分类：上游返回 500、上游超时、上游连接被拒绝、余额不足、无可用渠道、请求参数非法、流式中途断开。

## 实施计划

1. 在 `NewAPIError` 上增加 `source` 和 `stage`，并为判定规则表中的每一种场景补充构造位置的设置。
2. 修改 `RecordErrorLog`，把两个字段写入 `admin_info`。
3. 补充表驱动的单元测试，覆盖判定规则表中的每一行。
4. 在日志详情页增加管理员和 root 视图中的错误来源和阶段展示，并补充前端测试。
5. 使用故障注入人工验证日志分类，并记录结果。
6. 如果后续需要按错误来源筛选，再单独立项，届时再评估新增列和数据库验证范围。

## 验证结果

尚未实施。

## 阻塞原因

无。
