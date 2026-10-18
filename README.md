# Magpie Native Routing

> **状态：架构与可行性研究完成；客户端尚未实现。**
>
> 使用 **MyGo Native UI（纯 Go GPU 绘制，不使用 WebView）**，复刻 [Magpie](https://github.com/yetone/magpie) 的 Routing 界面，并作为**独立桌面应用**连接用户已经运行的 Magpie Desktop。此仓库当前保存经过源码核查的最终结论与实施方案，**不包含可运行的客户端**。

## 目标

- 独立安装、启动、退出；不嵌入或替代 Magpie，不启动第二个 Gateway。
- 现有 Magpie Desktop 继续接收 Codex、Claude Code、OpenCode 等 Agent 的请求；本客户端不转发模型流量。
- 使用 Magpie **已有的 Gateway HTTP API** 和 **本地已落盘的数据文件（只读）**。
- 完整重做 Routing 页面的布局、节点、曲线路径、动画、请求列表、详情、历史回放和路由组视图。
- 不使用 Wails/WebView/HTML/CSS/JavaScript 渲染 UI；Go 后端和 MyGo Native UI 分层。
- 不修改现有 Magpie 程序，也不直接写其配置/数据库。

## 核心结论

**独立客户端可行；但不修改 Magpie 时，无法与其原版 Routing 页面做到 100% 实时功能对等。**

Magpie 公开 Gateway HTTP API 可读取全局活动请求、按 Session 获取实时路由、用量和配额；完整路由 Trace 在**每次请求结束后**保存于本地 `routing/*.jsonl` / `*.jsonl.gz`，因此能够还原已完成请求的真实候选顺序、每次尝试和故障转移，并回放历史。

但是 Magpie 原版 Routing 页面使用的 **全局内存 Trace** (`/api/gateway/trace`) 与 **路由组管理** (`/api/groups`) 是桌面 GUI 内部 API，**并不是现有 Gateway 的对外 API**。Desktop 将 GUI 资源放在 Wails Asset Handler 中，不能假设 `127.0.0.1:3425/api/...` 可访问。另启 `magpie web` 且由原桌面进程承载 Gateway 时，Web 实例的 `mine` 为 false，不能取到原进程的实时 Trace。

因此本项目严格区分：

- **真实实时**：`GET /v1/magpie/concurrency` 中当前在途的 Agent、模型、阶段；对于已知 Session 使用 `GET /v1/magpie/route` 获取尝试详情。
- **完成后完整回放**：通过 Magpie 本地 Routing 历史数据获得全量已记录的最终 Trace。
- **无法无侵入实现的部分**：所有 Session 的全局逐步 Trace、对所有请求实时显示候选账号的每次重试；完整动态路由组管理写入；仅存在于进程内存中的状态。

详细依据见 [最终结论](docs/FINAL_CONCLUSION.md) 和 [功能兼容矩阵](docs/COMPATIBILITY.md)。

## 架构概览

```text
                        Magpie Desktop（原进程，不修改）
                           │                    │
          现有 Gateway HTTP API                  本地数据（只读）
          /v1/magpie/concurrency                 providers.json
          /v1/magpie/route                       routing/*.jsonl[.gz]
          /v1/magpie/usage*                      settings.json（可选）
          /v1/magpie/quotas*                      │
          /v1/models                            │
                      │                         │
                      └──────────┬──────────────┘
                             Go adapters
                                 │
                     Route Store / Event Journal
                                 │
                      MyGo Native GPU UI
             Topology · Animation · Detail · Replay · Groups
```

**重要**：图中的 “Event Journal” 是客户端对外部观测数据进行的归一化处理，**不是重建 Magpie 路由算法**，也不代表任何时间点的数据都能由 Gateway 完整观测。

## 建议的实施优先级

1. **协议与数据真实接入**：Gateway 连通、权限错误处理、历史文件发现、尾随与解压、Route ID 合并、断线重连。
2. **MyGo 原生可视化**：Agent → Gateway → Provider/Account/Model 节点与曲线路径，支持并发动画和真实请求详情。
3. **历史回放**：按天、筛选、速度控制、失败重试、请求时间线。
4. **配置视图与兼容性**：路由组只读展示、自动组与手动组差异、版本探测、状态明确标注。
5. **能力扩展（可选）**：只有 Magpie 官方日后开放全局 Trace 与管理 API，或用户允许修改 Magpie，才补上无损实时回放与组管理。

## 仓库文档

| 文件 | 内容 |
| --- | --- |
| [docs/FINAL_CONCLUSION.md](docs/FINAL_CONCLUSION.md) | 核心结论、不可跨越的边界与决策 |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | 技术架构、数据模型、同步与回放策略 |
| [docs/INTEGRATION.md](docs/INTEGRATION.md) | 已有 API、默认目录、诊断与安全约束 |
| [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md) | 原版 Routing 功能逐项兼容性 |
| [docs/ROADMAP.md](docs/ROADMAP.md) | 开发计划、测试矩阵与验收条件 |
| [docs/SOURCES.md](docs/SOURCES.md) | 源码证据（固定到审核过的 commit） |
| [PUBLISH.md](PUBLISH.md) | 如何把本地仓库发布到 GitHub |

## 技术选型

- Go 1.27+（按当前 MyGo README 的 Native 模板要求；实际开发时应重新核对）。
- [egoist/mygo](https://github.com/egoist/mygo) Native `ui`，使用 Go GPU 绘制；无 WebView。
- `net/http` 用于 Gateway 只读 API；Go 标准库 `encoding/json`、`compress/gzip` 用于历史文件。
- 文件变化监听可以采用平台 watcher 加定时兜底，**不能仅依靠一次 `fsnotify` 事件判断完整 JSONL 行已写入**。

## 当前工程状态

本仓库目前为**决策/设计文档仓库**，不含 `go.mod`、可编译二进制、真实运行截图或已实现的 API Client。任何示意图、功能矩阵都应视为目标与源码推导的可行性，而非完成情况。

## 上游

- Magpie: https://github.com/yetone/magpie
- MyGo: https://github.com/egoist/mygo

这是一项独立集成项目，与上述上游无官方关联。后续如复用上游代码或素材，应分别遵守其许可。
