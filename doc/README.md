# 文档管理规范

`doc/` 是项目主 Git 仓库中的目录，用于存放项目方案、实施计划和相关验证记录。文档按照执行状态分目录管理，分支和提交在项目主仓库中管理。

## 目录状态

| 目录 | 状态 | 说明 |
| --- | --- | --- |
| `todo/planned/` | planned | 方案已经拟定，但尚未开始实施 |
| `todo/in-progress/` | in-progress | 方案正在实施 |
| `todo/blocked/` | blocked | 方案因外部依赖或未解决问题暂时无法继续 |
| `done/` | done | 方案已经完成，并通过必要验证 |

## 文档流转

1. 新方案创建在 `todo/planned/`。
2. 开始实施时，移动到 `todo/in-progress/`。
3. 遇到阻塞时，移动到 `todo/blocked/`，并记录阻塞原因。
4. 阻塞解除后，移动回 `todo/in-progress/`。
5. 满足验收条件并完成验证后，移动到 `done/`。

移动文档时不要复制出重复文件；如果文档包含相对路径链接，需要同步更新链接。

## 文档格式

方案文档建议使用以下 frontmatter，并确保 `status` 与所在目录一致：

```yaml
---
title: Example Plan
status: planned
created: 2026-08-12
updated: 2026-08-12
owner:
completed:
---
```

方案正文建议包含以下内容：

```md
## 目标

## 验收条件

## 实施计划

## 验证结果

## 阻塞原因
```

`done/` 中的文档必须保留完成日期和验证结果。`doc/README.md` 是规则说明文件，不属于方案状态流转范围。

## 自动校验

在项目主仓库根目录执行以下命令，检查目录结构、文档状态和必填内容：

```bash
npm --prefix doc run docs:check
```

修改校验脚本后运行以下命令验证脚本自身：

```bash
npm --prefix doc run docs:test
```

若当前目录已切换到 `doc/`，也可以执行：

```bash
npm run docs:check
```
