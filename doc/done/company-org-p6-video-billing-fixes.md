---
title: 公司组织 P6：视频与异步任务计价修正
status: done
created: 2026-10-26
updated: 2026-10-29
completed: 2026-10-29
owner: TBD
---

# 公司组织 P6：视频与异步任务计价修正

本计划是 [公司组织、成员权限、额度计费、用量看板与账单导出方案](./company-organization-membership-quota-billing-usage-dashboard-export.md) 的子计划之一。按 D7 拆分，与组织功能独立验收。

**依赖**：无。可与 P1–P5 并行。

## 目标

1. 逐个核对 Volcengine Ark / Seedance、其他 Seedance 接入、Sora、Alibaba Wan、Kling 的计价事实与价格表达式，确保每个实际执行的插件与接口使用正确的计量单位。
2. 修正缺失的实际用量提取：Ark 的时长与分辨率、Seedance 的 Token 与秒数、Sora 的秒数与规格、Wan 的秒数与分辨率、Kling 的资源扣除量。
3. 明确成功、失败、超时和缺失 usage 时的确定行为，缺失值不得当作零或伪造实际用量。
4. 插件 `usageSchema` 与 `usageProfiles` 的描述、单位和本地化符合项目插件规范。

## 验收条件

- 每个插件的价格表达式都引用插件 schema 中声明的字段；字段缺失时拒绝为未配置价格，不静默按 0 收费。
- 价格表达式按 `<pluginKey>::<model>` 覆盖，不同接入的同名模型不会共用一个全局表达式。
- Ark 的 Token 计费、Seedance 的按秒计价、Sora 的秒数与规格、Wan 的秒数、分辨率与音频、Kling 的 `credit` 单位各自准确结算，不因模型名称相似而串用计费单位。
- 预扣、成功结算、失败退款、缺失或非法用量各有确定结果。
- 每个插件的用量字段描述写明「计费对象 + 单价」，单位放在 `unit` 中，描述不含数字价格且不以标点结尾（按 AGENTS.md）。
- 插件 contract 变更时，`docs/plugin-api/v1.schema.json` 与 `docs/plugin-api/v1.d.ts` 保持一致。

## 设计要点

- **已核对**：`plugins/tasks/kling/plugin.js` 的 `usageSchema` 字段为 `units`，单位为 `credit`，与总方案中的 `u("units")` 一致。
- **待核对**：Doubao、Sora、Alibaba 插件的当前用量声明与完成响应提取尚未逐项核对，总方案中的「当前实现」描述不能直接作为依据。其他 Seedance 接入的插件位置需要在实施开始时确认。
- **官方文档**：Volcengine、OpenAI Sora、Alibaba Model Studio、Kling 的正式计费说明和字段定义以各自当前 API 版本为准，不能用模型能力说明代替计费说明。核对过程需要联网访问这些文档。
- **计费规则**：本计划属于计费修正，实施前必须完整阅读 `.agents/rules/billing.md`，并遵守其中的上限、饱和、日志审计和插件校验要求。
- **插件规范**：实施前阅读 `docs/plugin-api/v1.md`。按 AGENTS.md，不得在 `plugins/` 目录下新增文档文件，也不得在 `docs/` 下新增文件。
- **测试**：遵循 AGENTS.md 的后端测试质量规则，扩展现有测试文件，不为每个插件新建分散的测试文件，不做随机或大循环的伪压力测试。

## 实施计划

1. 逐个核对目标接入的正式计费文档与成功/失败语义，记录每个插件的计费事实来源与版本。
2. 对照现有插件代码，列出差异（字段缺失、单位错误、表达式引用不存在的字段等）。
3. 修正用量声明、完成响应提取和价格表达式，同步更新 schema 描述与本地化。
4. 补充预扣、成功、失败、缺失用量的回归矩阵，扩展现有测试文件。
5. 复核 `docs/plugin-api/v1.schema.json` 与 `v1.d.ts` 的一致性。

## 验证结果

- Sora 移除（2026-10-29）：删除 `plugins/tasks/sora` 与 `plugins/sora_responses_test.go`；删除 `relay/relay_adaptor.go` 中渠道类型 55 与 1 到 `sora` 插件的映射；更新 `plugins/builtin_plugins_test.go`、`relay/relay_adaptor_jsplugin_test.go`；删除只验证 Sora 插件的 `relay/channel/task/jsplugin/adaptor_test.go` 用例。`plugins`、`relay`、`router`、`middleware`、`service`、`pkg/jsplugin`、`setting`、`relay/channel/task/jsplugin` 与 `controller` 中 Plugin、Task、Kling、Channel 相关测试通过。
- 迁移注意：已绑定 `sora` 插件 key 的类型 61 渠道需要改绑；渠道类型 55 与 OpenAI 渠道不再有视频插件映射。
- 未在本次移除的非插件引用：模型名列表（OpenAI 渠道常量、模型倍率默认值）、前端渠道与定价文案、`relay/common/relay_utils.go` 中 `sora-2` 的尺寸校验、`pkg/billingexpr/expr.md` 与 `docs/plugin-api/v1.md` 中的示例。这些需要单独决定是否清理。

2026-10-29 核对结果（官方页面经 WebFetch 或子代理读取，引文以官方页面为准）：

- **Ark / Doubao**：官方按完成 Token 计费，`usage.completion_tokens` 可作为计费对账依据；单价随模型版本、分辨率与参考视频输入变化；生成失败不计费；`duration` 为 -1 时由模型选择时长，实际时长在查询响应的 `duration` 字段返回。当前 schema（token 单位、resolution、video_input）与 completion_tokens 结算一致。结论：无需修改。
- **Alibaba Wan**：官方按秒计费，失败不计费；Wan3.0 按输入加输出时长计费，Wan2.7 的 `usage.duration` 为计费时长；音频只影响 wan2.6-i2v-flash 的价格。当前提取逻辑与之一致。结论：无需修改。未核对：货币单位、Wan3.0 的 `duration: -1` 计费规则、wan3.0-video 列价与限时折扣价的区别。
- **Kling**：官方开发文档在本次核对中无法读取，credit 单价、`final_unit_deduction` 的类型与含义均未经官方确认。缺陷已修复（插件 1.1.2）：解析结果曾把 credit 写入 `completionTokens` / `totalTokens`，无用量表达式时会按 Token 结算。现在 credit 只通过 `units` 事实结算，无表达式时保留预扣估算。
- **Sora**：OpenAI 官方弃用页面列出 Videos API、`sora-2`、`sora-2-pro` 于 2026-09-24 移除，且无替代接口。插件无法对照现行 API 核对计价。

回归测试：

- `plugins`：TestKlingFinishedTaskSettlesCreditsAsUnitsNotTokens。修复前失败（返回 completionTokens 4），修复后通过。
- `controller`：TestKlingNativeRouteSubmitPollSettleAndQuery 更新为预期结果：credit 不按 Token 结算，保留 250,000 预扣额度，用户余额为 750,000。
- `plugins`、`service`、`relay/channel/task/jsplugin`、`setting` 包测试通过；`controller` 中 Plugin、Kling、Task 相关测试通过。

## 阻塞原因

当前无阻塞。

- **Kling**：官方计费文档仍无法读取，`final_unit_deduction` 的字段语义未经官方确认。该风险已由负责人接受：只用旧模型倍率计价的 Kling 任务不再按实际 credit 结算，保留预扣额度；管理员需为 Kling 配置 `u("units")` 表达式。
- **Sora**：已移除，不再有阻塞。
