---
title: Console and Public UI Redesign
status: in-progress
created: 2026-11-03
updated: 2026-11-03
owner:
completed:
---

# Console and Public UI Redesign

## 目标

整站（公开页、认证页、登录后控制台的全部页面）替换为一套全新的视觉语言与布局外壳，不沿用当前 shadcn 默认观感、蓝色主色、1rem 大圆角和玻璃拟态效果。

设计定位：**精确、克制、数据优先**。面向 AI API 网关的管理者与开发者，界面以信息密度、可读的数字和稳定的表格为核心，装饰只服务于层级与状态。

本方案只定义设计方向、信息架构与实施分期，不包含代码改动。确认后再逐期实施。

## 现状依据

- 技术栈：React 19、TanStack Router / Query / Table、Base UI、Tailwind CSS 4。
- 主题：`web/src/styles/theme.css` 定义 `:root` 令牌；`theme-presets.css` 提供 10 套配色预设（default、anthropic、underground、forest-whisper、lake-view、lavender-dream、ocean-breeze、rose-garden、simple-large、sunset-glow），并通过 `data-theme-font`、`data-theme-radius`、`data-theme-scale` 切换字体、圆角与密度。用户可在配置抽屉（`components/config-drawer.tsx`）自行切换。
- 字体：Public Sans（UI）+ Lora（serif 轴，仅 anthropic 预设使用）。
- 视觉特效：`styles/index.css` 中有 `glass-1` ~ `glass-5`、卡片悬停上浮与阴影、落地页 `landing-*` 入场动画、`auto-group-flow-border` 流光边框、`faded-bottom` 等，均属于本次要淘汰的旧风格。
- 图标：lucide-react（约 303 个文件）、@hugeicons/react（约 34 个文件）、react-icons（1 个文件）三套并存。
- 公开页：`features/home/` 的 Hero、Stats、Features、HowItWorks、CTA；管理员可配置自定义首页（URL、HTML、Markdown），该能力必须保留。
- 登录后：`components/layout/components/` 中的顶栏（app-header）与侧边栏（app-sidebar，采用 drill-in 视图切换）；系统设置侧边栏分为 Site & Branding、Authentication、Billing & Payment、Models、Request policies、Security & Limits、Console Content、Operations 八组。
- 路由约 69 个文件，功能模块约 612 个 TSX 文件（其中包含 `components/ui/` 基础组件）。

## 品牌信息（已移除）

项目声明已在 `chore/remove-project-branding` 中移除，并已合并到 `dev`（提交 `d7e847a2f`）：

- `LICENSE`、`NOTICE`、README 各语言版本、`THIRD-PARTY-LICENSES.md` 已删除。
- 源文件的 QuantumNous 版权头、关于页的版权与 AGPL 段落、页脚的版权行已删除。
- `index.html` 标题改为 `AI Gateway`；默认站点名改为 `AI Gateway`。
- `.github/SECURITY.md` 与 `CODE_OF_CONDUCT.md` 中的支持邮箱已删除，`CODE_OF_CONDUCT.md` 不再提供执行联系方式。
- `AGENTS.md` 中的受保护项目标识章节已删除。

以下功能性标识保留，改动会破坏编译或功能：Go 的 module 与 import 路径、更新检查和插件市场的 GitHub 地址、Dockerfile 的 ldflags、Electron 的 `productName`（它决定用户数据目录）。

设计约束：新界面不再出现 New API / QuantumNous 标识，默认品牌名为 `AI Gateway`，管理员可以在系统设置中改为自己的站点名。

## 视觉语言（候选方案）

以下数值为候选值，进入视觉稿阶段后需要用工具校验对比度，满足 WCAG 2.1 AA（正文至少 4.5:1）。

### 色彩

- 浅色：页面底色为暖白纸色（候选 `#F7F7F5`），卡片白色（`#FFFFFF`），正文墨色（候选 `#15171A`），次要文字灰（候选 `#5B616B`），边框（候选 `#E4E4E0`）。
- 深色：底色为石墨色（候选 `#0E1012`），卡片层（候选 `#16191D`），正文（候选 `#ECEEF0`），次要文字（候选 `#9AA3AE`）。
- 强调色：单一"信号色"，建议候选青绿 `#0E7C66`（浅色）/ 提亮版本（深色），替代原蓝色主色。强调色只用于主操作、当前导航、焦点环与关键数据，不做大面积填充。
- 语义色：成功、警告、危险、信息各一组，统一通过令牌暴露（沿用 `--success`、`--warning`、`--destructive`、`--info` 命名，保证现有组件调用不需要改名）。
- 图表色：6 色以内的分类色板，并同时提供形状或文字区分，避免只靠颜色传达信息。

### 字体

- 界面无衬线：候选 Geist（经 `@fontsource-variable` 引入，实施前确认包可用）。
- 数字与标识符：Geist Mono，用于密钥、ID、token 数、金额、时间戳等，并开启 tabular-nums。
- 中文、日文、韩文回退：使用系统字体栈（Noto Sans SC / TC / JP / KR 作为可选回退），不内置大体积 CJK 字体。
- Lora serif 轴与 anthropic 预设随本次淘汰移除。
- 字号阶梯收敛到 6 档（12 / 13 / 14 / 16 / 20 / 28px），页面标题、区块标题与表格文字的对比通过字重与颜色区分，而不是靠大字号。

### 形状、密度与层级

- 圆角收敛：控件 6px，卡片 8px，弹层 12px。取代当前 `--radius: 1rem` 的大圆角。
- 边框优先于阴影：卡片靠 1px 边框与背景层级区分；阴影仅用于浮层（下拉、弹窗、命令面板）。
- 密度：默认舒适密度；表格提供紧凑切换，替代现有 `data-theme-scale` 的全局缩放。
- 去除玻璃拟态（`glass-*`）、卡片悬停上浮、流光边框与落地页入场动画；动效限定在 120–180ms 的状态过渡，并尊重 `prefers-reduced-motion`。

### 主题系统

- 保留浅色 / 深色切换（以 `.dark` 类和 `:root` 令牌实现）。
- 建议淘汰 10 套配色预设、字体轴与圆角轴的用户自定义。这些自定义在 `context/theme-customization-provider.tsx` 与 `config-drawer.tsx` 中，需要一次性迁移：已保存的旧预设值回落到新默认值，不报错。
- 是否保留任何用户可选项，见下文"待确认事项"。

## 信息架构与布局外壳

### 公开区域

- 顶栏：Logo（默认名为 AI Gateway 或站点名）、导航（定价、排行榜、文档等由后端动态下发）、语言切换、主题切换、登录 / 控制台入口。
- 落地页：Hero 直接展示网关能力（接入示例代码、支持的服务商标识）、实时统计、功能分区、接入步骤、CTA。去掉当前的装饰性动画与渐变光晕。
- 页脚：不再显示版权与项目署名，只保留链接分组和用户协议、隐私政策等法律链接，结构收敛为三列（产品、资源、法律）。
- 自定义首页保持原行为（iframe / HTML / Markdown 三种渲染路径不变）。

### 控制台（登录后）

```text
┌──────────────────────────────────────────────────────────────────┐
│ Logo   [组织切换]        [ ⌘K 搜索 ]          语言  主题  账户   │  顶栏 48px
├──────────┬───────────────────────────────────────────────────────┤
│ 概览      │  页面标题                              [主操作]       │
│  仪表盘   │  ─────────────────────────────────────────────────    │
│  用量报告 │  区块 / 筛选 / 工具栏                                  │
│ 接入      │  ─────────────────────────────────────────────────    │
│  密钥     │  内容（表格 / 图表 / 表单）                             │
│  模型     │                                                        │
│  Playground│                                                       │
│ 账务      │                                                        │
│  钱包     │                                                        │
│  订阅     │                                                        │
│  用量日志  │                                                       │
│ 管理（视角）│                                                       │
│  ← 返回    │                                                       │
└──────────┴───────────────────────────────────────────────────────┘
```

- 侧边栏：保留 drill-in 视图机制（`sidebar-view-registry.ts`），只重做视觉与分组文案。分组建议为概览、接入、账务、组织、管理。管理员的系统设置视图沿用现有八组结构，但改用同一套列表样式。
- 顶栏：只放全局操作。组织切换与搜索（⌘K 命令面板）保留，语言与主题切换收进账户菜单，减少顶栏图标数量。
- 内容区：统一页面头（标题、说明、主操作）+ 工具栏 + 内容区的三段结构。

### 页面模板

| 模板 | 适用页面 | 结构 |
| --- | --- | --- |
| 报表页 | 仪表盘、用量报告、Pricing 统计 | KPI 指标条 → 主图表 → 明细表 |
| 列表页 | 密钥、渠道、用户、用量日志、订阅、兑换码 | 工具栏（搜索 / 筛选 / 批量）→ 表格 → 分页；行操作收进菜单 |
| 设置页 | 系统设置八组、个人设置 | 左侧分区导航 + 右侧分组表单，保存栏固定在底部 |
| 详情 / 编辑 | 渠道编辑、密钥创建、模型配置 | 右侧抽屉（宽屏）或整页（复杂表单） |
| 认证页 | 登录、注册、找回密码、OAuth 回调、初始化向导 | 居中单栏卡片，不带导航 |
| 错误与空状态 | 404、403、500、空列表 | 统一的插画位 + 文案 + 主操作，复用 `empty-state` / `error-state` |

## 组件策略

依据 `web/AGENTS.md` 的复用优先规则，改造顺序为"令牌 → 基础组件 → 共享业务组件 → 布局 → 页面"，避免逐页重写：

1. **令牌层**：重写 `theme.css` 与 `index.css` 中的 `:root` 与 `.dark` 令牌，删除 `theme-presets.css` 中的预设。大部分页面在这一步即获得新视觉。
2. **基础组件层**：`components/ui/` 下的 button、input、select、tabs、card、badge、dialog、dropdown-menu、tooltip、table 等按新令牌与圆角规范统一调整，不改 API 与 props。
3. **共享业务组件层**：`confirm-dialog`、`dialog`、`empty-state`、`loading-state`、`error-state`、`status-badge`、`copy-button`、`data-table` 及其工具栏，统一外观。这些组件被大量页面复用，一次修改即全站生效。
4. **布局层**：重写 `layout/components/` 中的 app-header、app-sidebar、nav-group、nav-link-item、footer、public-header、mobile-drawer。
5. **页面层**：按模板逐个模块套用，不引入新的一次性组件。若某页面需要新组件，先按 AGENTS.md 检索现有实现，并在变更说明中记录能力缺口。

### 图标统一

建议统一为 lucide-react。Hugeicons 与 react-icons 的使用点分别迁移到 lucide 的对应图标，迁移后移除两个依赖。该项独立排期，不阻塞视觉改造。

## 国际化与可访问性

- 7 种语言（en、zh、zh-TW、fr、ja、ru、vi）的文案长度差异较大，尤其是 fr、ru、vi 的按钮与表头。组件需要支持文本换行或截断，不能依赖固定宽度。
- 所有新增文案通过 `t('English key')` 接入，并通过 `bun run i18n:sync` 同步 7 个语言文件，不新增硬编码文字。
- 数字与金额展示沿用 `@/lib/format` 与 `@/lib/currency`，并通过 `toIntlLocale` 转换语言码，符合项目强制规则。
- 对比度、焦点环、键盘导航、`aria-*` 状态与 reduced motion 为硬性验收项。

## 实施计划

### 阶段 0：设计确认（当前阶段）

- 评审本文档，确认视觉方向、信息架构与待确认事项。
- 产出静态视觉稿（颜色、字体、按钮与表单状态、四类页面模板、浅色与深色各一版），用于确认风格。视觉稿可以是独立的 HTML 页面，不进入应用代码。
- 确认后将本文档移动到 `doc/todo/in-progress/`，并从 `dev` 切出 `feat/console-ui-redesign` 分支（按 code-branch-management 流程）。

### 阶段 1：令牌、基础组件与登录后外壳

- 重写 `theme.css`、`index.css` 的令牌与基础样式，删除 glass、landing 动画、卡片悬停上浮等旧样式。
- 调整 `components/ui/` 基础组件的外观。
- 重写 layout 外壳（顶栏、侧边栏、移动端抽屉），保持 drill-in 视图与 ⌘K 行为不变。
- 重写认证页与错误页。
- 补充布局回归测试，覆盖侧边栏折叠、移动端抽屉、焦点顺序与深浅主题。

### 阶段 2：共享业务组件

- 统一 dialog、confirm-dialog、empty-state、loading-state、error-state、status-badge、copy-button。
- 改造 `data-table` 的工具栏与分页外观，支持紧凑密度切换。

### 阶段 3：公开页面

- 重做落地页（Hero、统计、功能、接入步骤、CTA）与页脚。
- 重做定价页、排行榜页、关于 / 条款 / 隐私页的版式。页面中的品牌名统一为默认站点名或系统设置中的站点名。
- 验证自定义首页三种渲染路径不变。

### 阶段 4：控制台页面（按模块分批）

按使用频率排期：仪表盘与用量 → 密钥与模型 → 渠道 → 钱包、订阅与兑换码 → 用户、组织与权限 → 系统设置（八组）→ 其余模块（Playground、任务插件、系统信息、运维等）。每批完成后单独验证。

### 阶段 5：清理

- 删除未使用的主题预设、Lora 字体、serif 轴与旧动画样式。
- 移除 Hugeicons 与 react-icons 依赖（若选定统一 lucide）。
- 运行 `bun run knip` 检查未使用的导出与依赖。

## 验收条件

1. 视觉：浅色与深色主题下，主要页面与组件的文字对比度满足 WCAG 2.1 AA。
2. 布局：7 种语言在 375px（移动端）与 1280px（桌面）宽度下无水平溢出，无文字被截断到不可读。
3. 功能：所有页面的数据请求、表单提交、权限判断与路由行为与改造前一致；已有测试全部通过。
4. 国际化：`web/src/i18n/locales/*.json` 与代码中的 `t()` 键一致，`bun run i18n:sync` 无遗漏。
5. 品牌：`index.html` 标题与默认站点名为 `AI Gateway`；页面不出现 New API / QuantumNous 标识、版权头或 AGPL 声明（功能性地址除外）。
6. 可访问性：键盘可完成主要流程；表单错误与状态通过 `aria-invalid`、`aria-expanded`、`aria-selected`、`aria-disabled` 等与视觉状态一致。
7. 构建：`bun check -b`、`bun run lint`、`bun run build` 通过，并在 `web/` 下运行受影响的 Vitest 测试。
8. 视觉回归：对每个模板与关键页面，在两种主题、7 种语言、桌面与移动宽度下截图留存。

## 待确认事项

1. **主题自定义**：是否完全移除用户可选的配色预设、字体与圆角，只保留浅色 / 深色与密度？建议移除。
2. **强调色**：采用本文档的青绿候选色，还是保留原蓝色体系、只改圆角与字体？
3. **图标库**：统一到 lucide-react（建议），还是保留 Hugeicons？
4. **交付节奏**：整站一次性合并，还是按阶段分多个 PR 合入 `dev`？建议按阶段分批。
5. **自定义首页**：管理员自定义首页的能力保持不变，本次只改内置落地页。是否同意？

## 验证结果

尚未执行。本文档为设计方案，不包含代码改动。

## 阻塞原因

无。等待待确认事项的回复后进入阶段 0 的视觉稿制作。

## 进度

### 阶段 1：令牌、基础组件与登录后外壳（PC 端完成）

- 已完成：浅色与深色令牌、0.5rem 基础圆角、Geist 与 Geist Mono 字体、移除卡片悬停上浮；顶栏卡片底色与底边线；认证页卡片与可访问的品牌链接；未找到页与错误页的状态码样式。
- 已验证：生产构建、类型检查、改动文件的 lint、完整前端测试（218 个文件、2501 个用例）；登录页、未找到页、控制台顶栏在浅色与深色主题下检查通过。
- 未完成：`components/ui/` 基础组件未逐个调整，目前通过令牌统一；手机宽度不在本阶段范围内。

### 待办阶段

- 阶段 2：共享业务组件与数据表格工具栏、分页。
- 阶段 3：公开页面（落地页、页脚三列结构、定价页、排行榜页、关于/条款/隐私页）。
- 阶段 4：控制台页面，按模块分批。
- 阶段 5：清理配色预设、Lora 字体、旧动画样式，迁移图标到 lucide，运行 knip。
