# Magpie Native Routing

> **状态：可运行的独立原生客户端（阶段 2–4 的主要纵向切片已实现）。**
>
> 使用 **Go + MyGo Native UI**（GPU 原生绘制，无 WebView、HTML/CSS/JavaScript 界面）读取正在运行的 Magpie Desktop Gateway 和只读历史文件。

## 这是什么

Magpie Native Routing 是一个独立桌面应用，用于观察 Magpie 的真实路由行为：

- 左侧 Agent → 中间 Magpie Gateway → 右侧 Provider / Account / Model 拓扑。
- 当前 Gateway 快照中真实在途的请求及其阶段。
- Magpie 已完成历史的请求列表、候选顺序、每次尝试、失败原因、TTFT 和 Token。
- 按日期查看历史并回放真实 Trace。
- 只读展示 `providers.json` 中的路由组、成员顺序和策略。

它**不是** Magpie 的替代品，也不会：

- 启动第二个 Magpie Gateway；
- 代理、转发或修改 Agent 的模型请求；
- 修改 Codex、Claude Code、OpenCode 等配置；
- 写入、覆盖或删除 Magpie 的 `providers.json`、账号、密钥或历史文件；
- 调用 Magpie 的私有 GUI API。

## 数据来源与真实性

界面只展示实际观测到的数据：

| 界面事实 | 来源 |
| --- | --- |
| 当前活动请求、阶段、并发 | `GET /v1/magpie/concurrency` |
| 已知 Session 的最新路由和尝试 | `GET /v1/magpie/route?session=...` |
| 完整候选顺序、尝试链、失败原因 | `routing/YYYY-MM-DD.jsonl[.gz]` 完成后历史 |
| 路由组、成员、策略 | `<magpie-dir>/providers.json` 只读快照 |
| Token / 用量 | 历史 `usage` 字段；Gateway usage API 已封装供后续页面使用 |

两种来源只按真实的 **Route ID** 合并。不会根据模型、Session 或时间猜测同一请求。

需要明确区分：

- **实时观测**：只在当前 Gateway 快照中确实在途时显示为活动；Gateway 断开后旧实时行会清除。
- **完成历史**：来自 Magpie 异步落盘的 JSONL/GZIP；界面标注为“完成后历史”。
- **回放**：回放时钟由 `Route.Time`、`Try.Start` 和记录耗时推导，并明确标注，不伪装成当时的流式采集。

公开 Gateway API 不提供原版 GUI 的全局逐步 Trace 流，因此实时阶段只展示 API 实际返回的部分；完整尝试链在请求完成并落盘后补齐。

## 构建与运行

要求：Go 1.27+、macOS / Linux 原生图形环境（当前开发验证为 macOS arm64）。

```sh
go test ./...
go vet ./...
go build -o bin/magpie-native ./cmd/magpie-native
./bin/magpie-native
```

启动前请先运行 Magpie Desktop。默认 Gateway 为 `http://127.0.0.1:3425`，默认数据目录自动探测：

- `$XDG_CONFIG_HOME/magpie`
- `~/.config/magpie`
- Magpie.app / 可执行文件旁的 `data`

可在应用内“连接与数据”中修改 Gateway 地址、API Key 和 Magpie 数据目录。设置只写入本应用自己的目录：

- macOS: `~/Library/Application Support/magpie-native-routing/`
- Linux: `$XDG_CONFIG_HOME/magpie-native-routing/`

API Key 不会写入日志。也可通过环境变量临时注入：

```sh
MAGPIE_API_KEY='...' ./bin/magpie-native
```

命令行选项：

```text
-gateway URL       覆盖 Gateway
-data-dir PATH     覆盖 Magpie 数据目录
-api-key KEY       本次运行使用 API Key（不建议放在 shell 历史中）
-theme MODE        system | light | dark
-lang LANG         zh | en
-diagnose          只读连接/历史诊断后退出
-json              配合 -diagnose 输出 JSON
```

### 只读诊断

```sh
./bin/magpie-native -diagnose
./bin/magpie-native -diagnose -json
```

诊断只读取 Gateway 和本地历史，不会修改 Magpie。

## 使用

1. 启动 Magpie Desktop，并保持 Gateway 开启。
2. 启动本应用；窗口顶部显示 Gateway 在线状态。
3. “实时”页查看真实在途拓扑和最新请求。
4. “历史”页选择日期，查看完成请求的完整候选顺序和尝试链。
5. 在历史页使用播放、暂停、重播、拖动时间轴和倍速控制回放真实 Trace。
6. “路由组”页只读查看手动组、自动生成组、成员顺序、规则与策略信息。

界面结构按原版 Routing 页对齐：顶部是同款 Magpie 工具栏与
`实时 / 历史 / 路由组` 页签；实时区依次展示 Agent → Gateway →
Provider 舞台、路由故事、上下文窗口、按日期分组的请求列表和账号列。
窗口变窄时，舞台切换为上下布局，请求指标移到第二行。

“实时”列表与原版一样取自 Gateway 保留的最近 60 个请求；切换日期后
显示该日完整历史，统计数字也随当前列表范围变化。

## 已验证

```sh
go test ./...
go test -race ./...
go vet ./...
go build -o bin/magpie-native ./cmd/magpie-native
./bin/magpie-native -diagnose
```

本地验证结果（2026-10-19）：

- Go `1.27.2 darwin/arm64`，CGO 可用。
- 单元测试、`go vet`、Race Detector 全部通过。
- 真实 Gateway `http://127.0.0.1:3425` 连接成功。
- 读取到真实 `~/.config/magpie/routing` 的 14 天历史，最新一天有真实记录。
- `providers.json` 可读，路由组和自动组可展示。
- 已实际启动原生窗口并确认拓扑、历史和详情界面渲染。
- Agent、Provider 与 Gateway 图标复用上游 Magpie MIT 授权的真实 SVG/PNG 资产，来源与许可证记录在 `internal/ui/assets/icons/NOTICE.md`。

## 已实现

- 只读 Gateway HTTP Client：并发、Session Route、usage、quotas、models。
- 可配置 Base URL、API Key、超时、取消、状态码解析与有界重连。
- JSONL / GZIP 历史读取；兼容追加、部分行、截断、替换、轮转和拼接 gzip。
- Route ID 去重；API 与历史统一 Store；UI 只读不可变快照。
- 按天历史、持续增量读取、真实时间线回放。
- 原生拓扑、分段贝塞尔连线、按弧长采样的喜鹊飞行动画、并发请求路径。
- 喜鹊由 MyGo `Path`/`Painter`/`Transform` 原生绘制：远翅、尾羽、身体、腹部、头、喙、近翅分层，约 420ms 扇翅周期、飞行切线朝向、平滑转向、出现/离场缩放与透明度。
- 实际业务阶段驱动：Agent → Gateway → Provider 去程，Provider 等待，失败经 Gateway 转到下一 Provider，成功后返回 Agent；不在画布上生成未经观测的请求。
- 历史回放的飞行仍由回放时钟驱动；播放、暂停、重播、倍速与时间轴共用同一真实 Try 时间线。
- 静态节点与连线直接由 MyGo `Painter` 绘制，飞鸟、请求圆点和流动虚线在动态层；空闲或暂停时停止动画帧。
- 请求列表对齐原版 `renderHist`：模型、目的地、尝试次数，以及耗时、
  首 Token、Token、缓存和输出速度指标；双栏宽度的列表按原版
  `metaFit` 将这些指标放到第二行。
- 账号列对齐 `renderActs`：以账号/密钥为主标题，显示 plan、模型、
  `used` 百分比（0–100，与 Magpie 历史字段一致）、尝试/回答次数和
  最近回答时间；Provider 作为分组标题。
- 请求详情、完整候选顺序、尝试链、固定查看。
- 手动/自动组只读视图，包括成员、匹配规则、pick、停用成员、Fast 和等级。
- 中英文界面、浅色/深色/跟随系统、响应式布局、设置弹窗和原生目录选择。

## 已知限制

- 公开 Gateway API 没有原版 GUI 的 `/api/gateway/trace` 全局逐步 Trace；实时候选顺序只能在完成后从历史补齐。
- 原版 GUI 的组管理写接口是私有的；本应用只读展示，编辑/删除按钮明确禁用。
- 历史保留由 Magpie 的约 30 天 / 200 MiB 策略决定；应用不假设历史永久存在。
- 实时已完成行的历史落盘存在异步延迟；在落盘前会保持“实时观测”来源，不会伪造完整 Trace。
- 回放时间精度受 Magpie 记录的毫秒字段和落盘时间限制。
- 当前实际启动验证在 macOS arm64 上完成；其他平台需自行执行构建与启动测试。
- 喜鹊是原生矢量移植，几何形状、图层顺序和 420ms 扇翅关键帧来自上游 `FLIER`/`rtflap`，但不是浏览器 SVG/CSS 的逐像素渲染；字体、系统抗锯齿与亚像素布局会有差异。
- 实时 API 只给出当前 provider 和采样时刻的 `ms`；若完整 Try 链尚未落盘，应用只显示实际观测到的等待/在途状态，不补造候选顺序或重试。
- 实时请求完成到历史落盘之间，返回鸟根据当前快照的在线观测和保持时间播放；不会把历史回放伪装成实时观测。

## 动画架构与性能

静态层与动态层分离：

- Agent、Gateway、Provider 卡片、静态曲线和卡片底由第一条绘制路径完成，文字、真实品牌图标和状态胶囊随后绘制。
- 动态层只绘制飞鸟、请求圆点、流动虚线与选中路径高亮。
- 位置按路径累计弧长采样，而不是按贝塞尔 `t` 采样，避免转弯处速度突跳。
- 每只鸟、每个飞行段独立使用阶段时钟；支持多请求并发和回放暂停。
- 飞行曲线按“布局 + Agent/Provider 身份”缓存弧长表，飞鸟和动态连线共享同一组路径，不在每帧重复采样贝塞尔曲线。
- 节点列表不再截断到 4 个 Agent / 5 个 Provider；并发升高时压缩节点高度并增高舞台，所有活动飞行都有可见端点。
- 历史回放的飞行窗口直接由真实 Trace 阶段构建；暂停、重播或直接 Seek 到返回段都从时间轴重建，不依赖之前是否播放过。
- `Painter.AnimationFrame()` 只在有真实在途请求，或历史回放确实处于播放状态时调用；暂停回放、空闲和窗口隐藏时不再请求帧。
- 渲染使用当前帧 `Painter.Now()`，不按固定帧计数推进动画。

> MyGo `v0.5.6-0.20261018121843-4dc371f94507` 在页面 `Clip` 内合成 `ui.Canvas` 时会以错误源原点采样，造成附近卡片文字在舞台上出现浅色重影。本项目因此不使用整页 `Canvas` 合成；已解析的 SVG 图标、文字布局和按布局缓存的飞行弧长表仍然复用。这是对上游渲染缺陷的隔离，不改变业务数据。

2026-10-19 在真实 Gateway（`127.0.0.1:3425`）和真实并发请求上的
`MYGO_FRAME_STATS=1` 观测：

- 飞行重绘帧：`build 0.0`、`layout 0.0`、`paint` 约 0.2–1.4 ms；
  `present` 通常 0.3–0.7 ms，偶发 GPU 调度达到 9–14 ms。
- 约 1 秒一次的 Gateway 轮询会重建视图：`build` 约 6–17 ms，
  这是当前主要 CPU 成本，而不是喜鹊绘制。
- 空闲且无动画时不再请求帧；顶部实时圆点也改为静态，避免空闲页
  每秒重建 30 次整棵 Native 视图。

该数字是单次本机采样，不是跨平台保证。

## 工程结构

```text
cmd/magpie-native/   启动、窗口、worker 生命周期
internal/config/     应用配置与 Magpie 路径探测
internal/magpie/     只读 Gateway HTTP Client
internal/routing/    Route 模型、历史、Store、监听、回放、组视图
internal/ui/         MyGo Native UI、拓扑、动画、主题、本地化
internal/testutil/   仅用于测试的明确合成样本
```

历史文件和 `providers.json` 始终只读。真实历史不会提交到仓库；`.gitignore` 已排除 JSONL/GZIP、构建产物和本地运行数据。

## 上游

- Magpie: https://github.com/yetone/magpie
- MyGo: https://github.com/egoist/mygo

独立集成项目，与上游无官方关联。`internal/ui/assets/icons/` 中使用上游 MIT 授权的品牌图标，归属和许可证见该目录的 `NOTICE.md`；未复制上游其他专有源码。
