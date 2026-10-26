---
title: 公司组织 P6：视频与异步任务计价修正
status: planned
created: 2026-10-26
updated: 2026-10-26
owner: TBD
completed:
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

实施后填写。计划包括：

- 每个插件的回归矩阵结果（预扣、成功结算、失败退款、缺失或非法用量），记录命令与输出；
- 精确额度结果的表达式测试；
- 插件 schema 与 `v1.d.ts`、`v1.schema.json` 的一致性检查；
- 若涉及数据库结构变化，则按 AGENTS.md 完成三种数据库验证；否则说明本计划未改动数据库。

## 阻塞原因

当前无内部阻塞。若无法访问供应商正式计费文档，无法确认计费字段和费率，则将本计划移入 `todo/blocked/` 并记录具体缺失的文档。
