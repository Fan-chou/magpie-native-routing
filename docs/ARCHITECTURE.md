# 架构设计：独立 MyGo Native Routing 客户端

> 设计文档，非已实现功能。日期：2026-10-18。

## 约束与非目标

- **必须**：客户端单独启动、独立窗口和生命周期；保持现有 Magpie Desktop 原样运行；原生 UI（纯 Go / MyGo Native），不使用 WebView。
- **只依赖**：Magpie 现有 Gateway HTTP API 及当前用户可读的 Magpie 本地数据。文件系统严格**只读**。
- **不得**：拦截模型请求、替换 Gateway、启动第二个 Gateway、注入 Wails 进程、篡改 Magpie 配置、从日志反推未观测到的实时事件。
- **非首版目标**：依赖未公开 GUI 管理 API 的路由组写操作；对所有 Agent 提供精确的毫秒级流式 Trace。

## 逻辑拓扑

```text
┌───────────────────────────────────────────────────────────┐
│ Magpie Desktop（未修改的独立进程）                          │
│                                                           │
│ Gateway HTTP             本地数据目录                      │
│ /v1/magpie/concurrency   routing/YYYY-MM-DD.jsonl          │
│ /v1/magpie/route         routing/YYYY-MM-DD.jsonl.gz       │
│ /v1/magpie/usage*        providers.json, settings.json      │
│ /v1/magpie/quotas*                                         │
│ /v1/models                                                 │
└──────────┬──────────────────────┬──────────────────────────┘
           │                      │
      Go HTTP Client          Read-only Reader
           │                      │
           └──────────┬───────────┘
                      ▼
              Normalizer / Merger
                      │
                Route State Store
                 / Event Journal
                      │
              MyGo Native UI Thread
                 ├ Topology
                 ├ Animation
                 ├ Request Table
                 ├ Route Details
                 ├ Replay
                 └ Groups (read-only)
```

## 推荐包结构

```text
cmd/magpie-native/main.go
internal/magpie/client.go           # HTTP 请求、超时、状态码和协议版本
internal/magpie/concurrency.go      # 全局活动请求采样
internal/magpie/session_route.go    # 指定 Session 的长轮询
internal/magpie/usage.go            # 已结束请求摘要与统计
internal/magpie/models.go           # 可用模型列表
internal/storage/locator.go         # 数据目录发现与用户覆盖
internal/storage/history.go         # JSONL/GZIP 完整记录解析
internal/storage/tail.go            # 尾随正在增长的 JSONL
internal/storage/groups.go          # 只读 providers.json
internal/routing/types.go           # 统一模型、来源和置信度
internal/routing/store.go           # 请求关联、去重、版本和状态
internal/routing/journal.go         # 客户端自身的事件快照
internal/routing/replay.go          # 回放时钟、节奏与筛选
internal/ui/topology.go             # 节点、贝塞尔路径、几何布局
internal/ui/animation.go            # Native 动画系统
internal/ui/requests.go             # 虚拟列表、筛选
internal/ui/detail.go               # 单请求各次尝试
internal/ui/history.go              # 按天历史与回放
internal/ui/groups.go               # 只读路由组视图
```

目录仅为推荐拆分，不表示仓库已具备这些源码。

## 数据模型与真实来源

对每条展示对象保留 `Source`、`ObservedAt`、`RouteID`、`Session`、`Agent`、`Status`、`Completeness`。至少区分四个来源：

| 来源 | 语义 | 时间范围 |
| --- | --- | --- |
| `live-api` | `/v1/magpie/concurrency` 当前快照 | 当前轮询时刻 |
| `session-route` | `/v1/magpie/route` 已知 Session 最新请求 | 请求进行中及内存保留期间 |
| `completed-history` | `routing/*.jsonl[.gz]` 磁盘历史 | 请求已完成且异步写入之后 |
| `static-config` | `providers.json` 及模型目录 | 读取时刻，不等于运行时路由决策 |

**合并关系**：`active[].route` 对应 Magpie `Route.ID`，不应与 `active[].id`（活动计数器 ID）混淆；后者只在活动生命周期内使用。`Route.ID` 是客户端优先关联键。同一 Session 可以包含多个 Route，不能仅以 Session 作为唯一主键。

- 对于已存在的 Route，以 `Route.ID` 更新最新状态；历史到达时把完整 `Route` 合并到相同 ID。
- 对无 `route` 的活动请求创建临时实体，不根据模型名称和时间接近程度强行“认定”为某条历史请求。
- 断线后先重取活动快照，再扫描尾部 JSONL 补齐结束请求；已收到的 Request ID 去重。
- 持久历史优先于采样快照提供完成后尝试细节；但不能据此填补执行时未知的事件时间戳。

## 同步机制

### HTTP 采样

`/v1/magpie/concurrency` 使用可配置轮询间隔，推荐初始 500–1000ms，按错误率或应用后台状态退避。`/v1/magpie/route` 只对确切已知且需要详细跟踪的 Session 进行长轮询；并发受限、取消旧请求，不能为所有活动请求任意凭空生成 Session ID。

- 所有 HTTP 请求使用超时、重试退避和 Context Cancellation。
- 区分 401/403 权限错误、404 版本缺少该端点、5xx 暂时不可用及网络断开。
- 处理服务重启导致状态归零：重取活动请求，重建临时实体，不篡改历史。

### 文件尾随

- 只读打开当天 `routing/YYYY-MM-DD.jsonl`，按字节偏移增量解析**完整的换行结尾 JSON**；不把半行当作坏记录。
- 处理文件轮转、重命名、截断、进程重启、跨零点保存、时区变化和历史清理。
- `*.jsonl.gz` 按 gzip 流读取；旧日文件和补充写入可能共存，须根据记录 ID 去重。
- 通过文件监听加定时扫描兜底，避免事件丢失；历史记录涉及隐私，默认不复制全文到客户端持久缓存。
- `providers.json` 按快照读取并解析；解析失败保留最后一次成功快照，显示故障，不自动修复/回写。

### 交互线程

HTTP/文件解码和回放调度在 Go Worker 中进行，通过线程安全的 Store 发布不可变 UI 快照；Native UI 主线程只负责输入、布局、绘制和动画。动画帧率与 API 采样速率独立；不可因收到多个更新而多次无意义全布局重建。

## 动画真实性规范

- **Live**：只标记确实由 API 观察到的活跃阶段；可以显示请求在 Agent、Gateway 和已知模型节点之间的活动效果。
- **Completed**：历史文件确认请求结束，详情中展示真实的候选顺序、尝试、失败和返回状态。
- **Replay**：使用 `Try.Start`、`Millis` 等实际可用字段组织时间线；字段缺失的段落使用明确的示意过渡，绝不声称是原始传输时序。
- **Unavailable**：不可获得的路由账号、规则或失败步骤，用“未观测到 / 暂不可用”显示，不进行推断补齐。

## 安全策略

- HTTP 限制到用户指定地址，默认 `127.0.0.1`；远端连接需要用户显式配置和 Magpie 授权。
- 只读数据路径必须经用户确认，尤其是 portable、共享目录或符号链接。不要在日志中记录 Gateway Key、账号 token、完整 Prompt 和响应正文。
- 路由历史本身可能包含账号标签、文件夹或 Session 信息，截图导出需要脱敏模式。
- 不写 Magpie `providers.json`、`settings.json`，不调用 GUI 的内部管理 API，不尝试提权。

## 关于“100%”功能对等

原版 Routing 页面读的是同进程全局内存 Trace（`/api/gateway/trace`）和 GUI 私有配置管理 API（`/api/groups`）。从外部 HTTP 和已完成文件不能重建所有尚未完成请求的每一次决策；这个限制是接口设计边界，而非 UI 框架限制。

参考源码：[sources](SOURCES.md)、[compatibility](COMPATIBILITY.md)。
