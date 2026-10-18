# 最终结论与架构决策

研究日期：2026-10-18  
研究对象：Magpie Desktop + MyGo Native UI  
前提：**独立应用；不修改 Magpie；仅依赖 Magpie 现有 HTTP API 和已有本地数据；本地文件只读。**

## 1. 结论摘要

**做得成一个独立、真实接入、界面可高度复刻的 Native Routing 客户端，但做不到在完全不改 Magpie 的同时 100% 复刻原版所有实时能力与管理功能。**

不能把“已完成路由的历史回放”“全局活动请求概况”和“全局路由 Trace 的实时推送”混为一谈。这三者来自不同来源，其数据粒度和时效性不同。

### 可以做的

- 独立连接已运行的 Magpie Desktop Gateway，实时显示 Agent 请求活动概况、阶段与持续时间；无须改 Agent 的请求路径。
- 用 Session ID 查询对应请求当前/最近路由、尝试、故障转移信息；适用于当前 `/v1/magpie/route` 能识别的 Session。
- 直接**只读**加载 Magpie 已完成请求的真实 `routing/YYYY-MM-DD.jsonl[.gz]`：包含请求模型、规则、候选顺序、尝试及错误等，能够重放对应历史。
- 读取 `providers.json` 中的静态配置，联合 `/v1/models`、配额等 API 构建路由组只读视图。
- 使用 MyGo 的 Native UI 实现自己的拓扑、交互、动画与筛选，完全不使用 WebView。

### 当前做不到或无法保证的

- 通过公开 Gateway API 订阅**所有请求**的逐步 Trace；不存在等价于 GUI `/api/gateway/trace` 的对外全局接口。
- 对于未知或未传 Session 的请求，在其运行过程中获取完整的候选账号重试详情。
- 在不修改 Magpie、不直接改其数据文件的前提下，完整创建/编辑/删除路由组并保持与 Magpie GUI 的一致性。
- 仅从 `providers.json` 还原运行时推导的所有自动路由组、模型可用性、规则计算状态、登录账号信息。
- 把完成后读取到的 Trace 说成“执行中逐毫秒观测到了每次决策”。回放是真实的历史数据，播放时间不代表收到事件的原始时间。

## 2. 为什么桌面应用不能像独立 HTTP 服务那样开放内部 `/api`

Magpie Desktop 使用 Wails v3 作为桌面壳。其 `internal/gui/app.go` 把 `gui.Handler` 传给 Wails `application.AssetOptions{Handler: ...}`；路由页通过这个 Handler 调用 `/api/gateway/trace`、`/api/groups` 等 GUI 内部接口。

这个 Handler **不是**用户已在 `127.0.0.1:3425` 上运行的公共 Gateway HTTP Router。假设 `GET http://127.0.0.1:3425/api/gateway/trace` 一定可用，是错误的集成策略。

`magpie web` 是另一种运行形态。如果原桌面进程已经占有 Gateway 端口，新起的 Web 进程可呈现共享的配置，但不会自动取得该桌面进程的内存 Trace。其 `/api/gateway/trace` 在不持有 Gateway 的情况下 `mine=false`，不会提供原桌面进程的实时流。

## 3. 数据来源的真实性与保真度

| 类型 | 来源 | 保真度 | 何时可见 |
| --- | --- | --- | --- |
| 当前请求、Agent、模型、执行阶段 | `/v1/magpie/concurrency` | 真实活动状态，但无完整候选顺序 | 请求执行中，按采样间隔 |
| 特定 Session 的路由 | `/v1/magpie/route?session=...` | 真实，但限定在该 Session 最新可见请求 | 执行中/结束后，Trace 缓存保留期间 |
| 已完成路由 | `routing/*.jsonl`, `*.jsonl.gz` | 已写入请求的完整持久化 Route 结构 | 请求完成且异步写盘之后 |
| 静态路由组 | `providers.json` | 手工静态配置；不能直接等同于运行期最终状态 | 文件读取时 |
| 用量/配额/模型目录 | Gateway 对应 API | 各端点声明范围内真实 | 读取时 |
| GUI 全局实时 Trace | 桌面进程中的内存 Trace | 真实、粒度最高 | **外部不开放** |

**关键取舍：UI 可以 1:1 复刻，但功能对等不能伪造。** 对于不可观测的阶段，界面必须明确标为 `not available`、`awaiting completed trace` 或 `replayed from history`。

## 4. 最终设计决策（ADR）

**ADR-001 · 独立进程。** MyGo 与 Magpie 分别运行；MyGo 退出不影响模型代理；Magpie 重启可自动重连。

**ADR-002 · 无侵入。** 不 patch Magpie、不反向注入其 Wails 进程、不拦截 Agent 请求、不把 Gateway 端口改成 MyGo 代理。

**ADR-003 · HTTP + 文件只读双通道。** HTTP 用来呈现活动与指标；本地历史用于完成后完整细节。可配置数据路径，禁止文件写入。

**ADR-004 · 来源分级。** `live-api`、`session-route`、`completed-history`、`static-config` 四类数据要有明确来源标记和时间戳，不将轮询快照捏造成中间事件。

**ADR-005 · 同一请求合并。** 用 Magpie Trace 的 Route ID 作为主关联键；`concurrency.active[].route` 对应它；注意 `active[].id` 是活动计数 ID，不是 Route ID。对于没有 Route ID 的活动行使用临时 UI 标识，待完成后谨慎关联。

**ADR-006 · 路由组管理暂缓。** 只读展示作为第一个版本；不存在官方可调用的公开写 API 时，不绕过 Magpie 的验证和锁直接写 `providers.json`。后续如上游开放安全接口，再实现管理操作。

**ADR-007 · 源码优先，版本显式。** 每个特性建立带 Magpie commit / 版本的契约测试，升级后先诊断字段和数据结构，再启用展示。

## 5. 何时才能称为“完整复刻”

需同时具备两项**上游公开支持**或获得放宽约束：

1. 提供鉴权后的全局只读增量 Trace 端点（可分页/长轮询/SSE），字段足以承载原版 `Route`、`Try`、`Order`、`Left`、`Totals`。
2. 提供经验证的路由组管理 API，覆盖 GUI `/api/groups/{action}` 所允许的管理语义，支持并发修改冲突处理与权限控制。

当前不以实现这两项作为第一版验收要求，也**不建议**为了宣称完整而篡改静态配置或编造实时事件。

## 6. 项目当前状态

这里记录的是经源码核对的工程研究结论，**并不是已经完成的软件**。是否能在特定版本的 Magpie Desktop 上正常工作，仍需完成 API 响应样本、真实文件、异常重启以及 MyGo 原生窗口的端到端测试。
