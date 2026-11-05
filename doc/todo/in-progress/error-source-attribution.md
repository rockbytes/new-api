---
title: 报错来源记录
status: in-progress
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

- 在 `NewAPIError` 上增加 `source` 和 `stage` 属性。未显式设置时，由 `inferErrorAttribution` 根据错误类型、错误码和状态码推断（规则见上表），这样大部分错误不需要改动构造位置；上游连接、流式中断等推断可能不准确的场景，通过 `ErrOptionWithErrorAttribution` 显式指定。已有的 `errorType` 和 `errorCode` 保持不变，内部判断逻辑（如本地错误判断）不受影响。
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

1. 在 `NewAPIError` 上增加 `source` 和 `stage`，实现默认推断规则（`relaykit/types/error_attribution.go`），并提供显式标记的选项。
2. 修改 `RecordErrorLog`，把两个字段写入 `admin_info`。
3. 补充表驱动的单元测试，覆盖判定规则表中的每一行。
4. 在日志详情页增加管理员和 root 视图中的错误来源和阶段展示，并补充前端测试。
5. 使用故障注入人工验证日志分类，并记录结果。
6. 如果后续需要按错误来源筛选，再单独立项，届时再评估新增列和数据库验证范围。

## 验证结果

实施进度（分支 `feature/error-source-attribution`，尚未合并）：

- 已完成：`relaykit/types` 的推断规则和显式标记；`ProcessChannelError` 将 `error_source` 和 `error_stage` 写入 `admin_info`；管理员日志详情显示「错误归因」区块；7 种语言的文案。
- 测试：`relaykit/types` 的表驱动测试（推断规则和显式覆盖）通过；`relaykit` 全部测试通过；根模块和 `relaykit` 均可独立构建；`controller` 中的错误日志集成测试覆盖上游状态错误（来源 upstream、阶段 upstream_response）和额度不足（来源 gateway、阶段 billing），并确认普通用户视图不含 `admin_info`；前端 `usage-logs` 的显示测试覆盖管理员可见、普通用户不可见；前端 typecheck、lint、格式检查通过，`usage-logs` 相关测试 446 个用例通过；流式归因的映射测试（`relay/common`）和日志写入测试（`service`）通过。
- 未完成：
- 流式中断的归因不写在错误日志上，而是写在流式请求的消费日志上：流式异常结束时，`StreamStatus.ErrorAttribution` 根据结束原因给出来源，阶段统一为 `stream`，并写入 `admin_info`（客户端断开和心跳写入失败为 client，超时和上游连接中断为 network，上游错误事件或未完成即结束为 upstream，我方 panic 为 gateway）。
- 故障注入验证（本机独立的 SQLite 库和网关进程，上游为本地模拟服务，不使用任何共享数据库）：

  | 场景 | 客户端结果 | 日志记录 | 来源 / 阶段 | 结论 |
  | --- | --- | --- | --- | --- |
  | 上游返回 500 | 500 | 错误日志 | upstream / upstream_response | 正确 |
  | 上游返回 400 | 400 | 错误日志 | upstream / upstream_response | 正确 |
  | 上游不返回响应头（超时） | 500，`do_request_failed` | 错误日志 | network / upstream_request | 正确 |
  | 上游连接被拒绝 | 500，`do_request_failed` | 错误日志 | network / upstream_request | 正确 |
  | 流式中途超时无数据 | 200（已开始输出） | 消费日志，结束原因 timeout | network / stream | 正确 |
  | 流式中途断开，没有完成信号 | 200（已开始输出） | 消费日志，结束原因 eof，状态 ok | 无归因 | 有缺口，见下 |
  | 模型不存在（无可用渠道） | 503，`model_not_found` | 错误日志（无渠道） | gateway / routing | 正确 |
  | 令牌额度不足 | 403，`pre_consume_token_quota_failed` | 错误日志（无渠道） | gateway / billing | 正确 |
  | 请求缺少必填字段 | 400，`invalid_request` | 错误日志（无渠道） | client / request_build | 正确 |
  | 用户端中途断开流式请求 | 无（客户端已断开） | 消费日志，结束原因 client_gone | client / stream | 正确 |
  | 上游在流式中途连接被重置 | 200（已开始输出） | 消费日志，结束原因 scanner_error | network / stream | 正确 |

  - 第一轮验证发现：模型不存在、额度不足、请求参数错误不写入任何日志。已修复：请求进入渠道之前的错误也写入错误日志，渠道字段为空；额度不足的错误不再排除在日志之外。修复后三类错误都能在日志中找到。
  - 流式中断的区分：用户端断开和上游连接中断已经能区分（分别为 client_gone 和 scanner_error）。上游正常关闭连接但没有完成信号时，传输层上与正常结束无法区分，仍记为成功（结束原因 eof，状态 ok），没有归因。要区分它，需要按协议识别完成标记（例如 OpenAI 的 `[DONE]` 或 `finish_reason`），属于行为变化，需要单独确认。
  - 验证结束后已停止本地网关和上游模拟服务。
- 还未完成：流式「正常关闭但未完成」的归因，等待对完成条件的确认。
- 数据库：本次没有改表结构，字段写入已有的扩展信息，不需要三库验证。

## 阻塞原因

无。
