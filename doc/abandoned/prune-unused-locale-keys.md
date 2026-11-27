---
title: 清理前端 locale 中的未使用文案键
status: abandoned
created: 2026-11-20
updated: 2026-11-27
owner:
completed:
---

## 目标

删除前端 locale 文件中不再被任何 `t(...)` 调用引用的文案键，避免死文案留在七种语言的文件里，并保证今后删除界面时有可重复的清理方式。

起因是 [删除分组限速代码](../../done/remove-group-rate-limit.md)：分组限额的编辑区删除后，只属于它的文案键仍留在 `en.json` 及其他语言文件中。

## 现状

- 已确认只属于已删除界面、且无其他引用的键（2026-11-20 检索，排除 `locales/` 目录）：
  - `Group-based rate limits`
  - `Group config overrides global limits, shares the same period`
  - `JSON object:`
  - `Invalid JSON format or values out of allowed range`
- 以下键仍有其他引用，不能删除：`JSON Mode`、`Visual Mode`、`Format:`、`Example:`。清理前必须逐个检查。
- `web/scripts/sync-i18n.mjs` 的 `i18n:sync` 以 `en.json` 为基准拆分多余键到 `_extras/`，不按代码引用判断，因此不会删除这些键。
- 项目规则禁止直接编辑 `web/src/i18n/locales/*.json`，写入必须通过脚本。目前仓库里没有删除键的脚本。

## 验收条件

1. 清理后，`src/` 中每个 `t(...)` 字面量都在 `en.json` 中存在，且 `en.json` 中不存在无引用的键。
2. 七个 locale 文件的键集合一致，并且仍按字母序排列。
3. 所有删除通过仓库内提交的脚本完成，没有手工编辑 locale 文件。
4. `bun run i18n:sync`、`bun run typecheck`、`bun run lint` 与 `vitest run` 通过。

## 实施计划

1. 在 `web/scripts/` 中新增一个只读检查脚本，列出 `en.json` 中未被 `src/` 引用的键，并覆盖动态键（如 `t(\`...${x}\`)`）的误报判断。该脚本提交到仓库，作为检查手段。
2. 对照检查结果，确认第 1 节列出的候选键，以及是否还有其他孤立键。
3. 增加一个经过审查的删除脚本，接收明确的键列表，同时删除七个 locale 文件中的对应键，写入后按字母序排列。删除脚本与检查脚本一起提交。
4. 运行删除脚本，然后运行 `bun run i18n:sync` 规范化文件顺序。
5. 运行验收条件第 4 条中的检查。

## 验证结果

（尚未开始实施。）

## 废弃原因

方案中列出的 4 个候选文案键经重新核对，当前仍被分组限速界面 `web/src/features/system-settings/request-limits/rate-limit-section.tsx` 引用，不符合“无引用死键”的清理前提。为避免误删仍在使用的文案，本方案不再按原范围执行。若后续仍需建立通用的未使用翻译键检查工具，应另行制定方案，并处理动态翻译键带来的误报风险。

## 验证结果

已重新检索前端源码：4 个候选键均在 `rate-limit-section.tsx` 中存在 `t(...)` 调用，因此不应删除。
