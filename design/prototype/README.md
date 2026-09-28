# 界面原型

这里是「云枢」高保真原型的源文件，导出自设计画布：<https://claude.ai/artifact/BTSS55qUzW1qQfcBnkLuXu>（2026-09-28 版本）。画布里可以直接预览、点击交互。

`*.dc.html` 是画布的页面源文件，使用画布自带的模板运行时（页面里引用的 `support.js`），不能脱离画布单独打开；`canvas.json` 记录各画板的位置和标题。`screenshots/` 里是产品定义、信息架构、设计规范和 10 个页面画板的渲染截图（1440×900），方便不打开画布时查看；实现后的界面截图见 [`docs/images`](../../docs/images)。

## 画板

| 文件 | 画板 | 说明 |
|---|---|---|
| `Main.dc.html` | 01 产品定义 | 背景、目标、角色、MVP 范围、覆盖的云服务 |
| `IA.dc.html` | 02 信息架构与核心流程 | 站点地图、接入云账号的流程、角色权限 |
| `DirA.dc.html` / `DirB.dc.html` / `DirC.dc.html` | 视觉方向 A / B / C | 三个方向的概览页；选定 **B · 明亮专业** |
| `DesignSpec.dc.html` | 03 设计规范 | 颜色、字体、按钮、状态、图表配色 |
| `Sidebar.dc.html` / `Topbar.dc.html` | 公共组件 | 侧边导航、顶栏 |
| `Login.dc.html` | 登录 | |
| `Onboarding.dc.html` | 首次使用 | 还没有云账号时的概览页 |
| `Dashboard.dc.html` | 概览 | |
| `Resources.dc.html` | 资源中心 | |
| `ResourceDetail.dc.html` | 资源详情 · 监控 | 资源中心的详情抽屉 |
| `Monitor.dc.html` | 监控中心 · 跨云对比 | |
| `Accounts.dc.html` | 云账号 · 同步历史 | |
| `AccountForm.dc.html` | 新增云账号 | |
| `Users.dc.html` | 用户管理 | |
| `Audit.dc.html` | 审计日志 | |

## 原型与实现的对应

| 原型 | 前端代码 | 路由 |
|---|---|---|
| 登录 | `web/src/views/LoginView.vue` | `/login` |
| 首次使用 | `web/src/views/OnboardingPanel.vue` | `/dashboard`（没有云账号时） |
| 概览 | `web/src/views/DashboardView.vue` | `/dashboard` |
| 资源中心 | `web/src/views/ResourcesView.vue` | `/resources` |
| 资源详情 | `web/src/views/ResourceDrawer.vue` | `/resources?detail=<id>` |
| 监控中心 | `web/src/views/MonitorView.vue` | `/monitor` |
| 云账号 | `web/src/views/AccountsView.vue` | `/accounts` |
| 新增云账号 | `web/src/views/AccountDialog.vue` | `/accounts?new=1`、`/accounts?edit=<id>` |
| 用户管理 | `web/src/views/UsersView.vue` | `/system/users` |
| 审计日志 | `web/src/views/AuditView.vue` | `/system/audit` |
| 设计规范 | `web/src/styles/theme.css` | — |

## 实现时有意调整的地方

- **图表配色**：原型里 AWS 深蓝 `#22406E` 明度过低，和阿里云橙色放在一起时色弱用户不易区分。图表改用经过校验的 `#2F56B8`（AWS）和 `#E8832A`（阿里云）；云厂商标签仍用原型配色。
- **网格线**：原型的虚线网格改为 1px 实线，虚线容易被误读成阈值线。
- **柱宽**：概览柱状图的柱宽从 30px 收到 24px，相邻柱之间留 2px 间隙，数值直接标在柱顶。
- **CPU 均值曲线**：卡片右上角增加“切换为表格”，可以直接读每小时数值。
- **同步状态胶囊**：有账号部分同步失败时显示琥珀色“部分同步失败”，而不是绿色“同步正常”。
- **阿里云站点**：原型在云账号列表里给阿里云账号标了“中国站”。中国站和国际站的 API 相同，凭证也看不出属于哪个站点，所以实现里阿里云账号不显示站点，只有 AWS 账号显示“全球区 / 中国区”。
- **字体**：Google Fonts 在国内网络无法访问，西文字体 IBM Plex 打包进程序，中文使用系统字体（苹方 / 微软雅黑 / 思源黑体）。
