# 源码依据与核查范围

核查日期：2026-10-18。为了避免未来 `main` 分支更新导致结论失去上下文，以下链接固定到核查时的提交。

- **Magpie** `d783df46cbac9021e26a6bd71ca0b55544552eb2`（提交时间：2026-10-17T23:13:12Z）
- **MyGo** `4dc371f94507573ecf0e3a228857a2fd5d6ca436`（提交时间：2026-10-18T12:18:43Z）

| 依据 | 源码 | 验证要点 |
| --- | --- | --- |
| Desktop GUI 实现 | [Magpie app.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gui/app.go) | Wails `AssetOptions{Handler: ...}`；不是 Gateway 的公网 HTTP GUI |
| GUI 内部 API | [Magpie api.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gui/api.go) | 内部路由、来源与访问边界 |
| GUI Trace/历史 | [Magpie gui/trace.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gui/trace.go) | `/api/gateway/trace`、`history`、`route` |
| 原版 Routing 页面 | [Routing client.js](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gui/assets/plugins/routing/client.js) | 原页面实际读 `/api/gateway/trace`、`/api/groups` |
| 原版 Routing 样式 | [routing.css](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gui/assets/plugins/routing/routing.css) | 视觉实现参考，不能直接搬作 Native 代码 |
| 内存 Trace 模型 | [gateway/trace.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gateway/trace.go) | Route/Weighed/Try/TraceState、保留 60 条等 |
| 单 Session 路由 API | [gateway/session_route.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gateway/session_route.go) | `/v1/magpie/route`、查询范围、鉴权 |
| 活动请求定义 | [gateway/active.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gateway/active.go) | Agent/Session/Phase/Route ID |
| 并发 API | [gateway/concurrency.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gateway/concurrency.go) | 返回 `active`、`requests`、`waiting`、`tools` |
| HTTP 用量 API | [gateway/usage_api.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gateway/usage_api.go) | `/v1/magpie/usage*` 与权限规则 |
| 完成后路由历史 | [gateway/history.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gateway/history.go) | JSONL/GZIP、30 天、200 MiB、异步写入 |
| 组 GUI 管理 | [gui/groups.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gui/groups.go) | `GET /api/groups` 与 `POST /api/groups/{action}` 仅 GUI |
| 配置文件结构 | [provider/provider.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/provider/provider.go) | `providers.json` 的静态 Groups/Providers/GroupOrder |
| 安装/便携路径 | [appdir/appdir.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/appdir/appdir.go) | 默认 `~/.config/magpie`、portable 数据目录 |
| Web 与 Gateway 进程关系 | [gui/web.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gui/web.go) & [gui/backend.go](https://github.com/yetone/magpie/blob/d783df46cbac9021e26a6bd71ca0b55544552eb2/internal/gui/backend.go) | 独立 Web 进程不能直接读取其他实例的内存 Trace |
| MyGo Native | [MyGo README](https://github.com/egoist/mygo/blob/4dc371f94507573ecf0e3a228857a2fd5d6ca436/README.md) | 纯 Go 原生 UI、无需 WebView、Go 版本要求 |
| MyGo Native 文档 | [MyGo docs/native.md](https://github.com/egoist/mygo/blob/4dc371f94507573ecf0e3a228857a2fd5d6ca436/docs/native.md) | 原生视图与事件处理 |
| MyGo 原生绘制 | [MyGo docs/ui/drawing.md](https://github.com/egoist/mygo/blob/4dc371f94507573ecf0e3a228857a2fd5d6ca436/docs/ui/drawing.md) | 路径、绘制能力，实施前需以当前 API 实测 |

> 源码核查 ≠ 已有可运行客户端。后续发布第一个可执行版本前，需要跑真实性/权限/断线/文件尾随测试，再更新这里的兼容性判断。
