# Magpie 集成契约与诊断

> 基于 2026-10-18 所核查的 Magpie 源码。以下命令用于**测试接口**，不等于客户端已经实现。必须按实际 Magpie 版本复核。

## 连接模式

预期运行方式：Magpie Desktop 在本机运行并继续承载 Gateway；MyGo Native 作为独立进程连接同一 Gateway，仅进行只读调用。默认 URL **示例**：`http://127.0.0.1:3425`；端口以实际安装设置为准。

本地示例：

```sh
BASE_URL=http://127.0.0.1:3425
KEY=magpie
curl -fsS -H "Authorization: Bearer ${KEY}" "${BASE_URL}/v1/models"
curl -fsS -H "Authorization: Bearer ${KEY}" "${BASE_URL}/v1/magpie/concurrency"
curl -fsS -H "Authorization: Bearer ${KEY}" "${BASE_URL}/v1/magpie/usage/requests?limit=20"
```

使用已有 Session ID（必须使用 Agent 实际发送给 Magpie 的 Session ID）：

```sh
SESSION_ID='your-existing-session-id'
curl -fsS -G -H "Authorization: Bearer ${KEY}" \
  --data-urlencode "session=${SESSION_ID}" \
  "${BASE_URL}/v1/magpie/route"
```

可以通过 `after`、`wait` 使用指定 Session 的长轮询；`wait` 为秒，单次最多 60 秒。仅授权范围内的数据可读。对于没有 Session 的请求不能自行猜测 ID。

## 当前可用接口及边界

| 接口 | 内容 | 关键限制 |
| --- | --- | --- |
| `GET /v1/models` | Gateway 列出的模型 | 不是全部路由组内部成员和规则求值快照 |
| `GET /v1/magpie/concurrency` | 全局活动请求、lane、活动阶段 | 快照，非全量事件流；受 key 权限限制 |
| `GET /v1/magpie/route?session=...` | 指定 Session 最新 Route、尝试和状态 | 按 Session，不提供全局所有请求 |
| `GET /v1/magpie/usage` | 用量汇总 | 不是完整 Route / Try 链路 |
| `GET /v1/magpie/usage/requests` | 已有请求用量分页 | 不是全局内存 Trace |
| `GET /v1/magpie/quotas` | 配额等 | 授权 Key、账户隐私与作用域限制 |

远端访问须满足 Magpie 自身的 LAN 分享和 API Key 规则；受限 Key 可能无法调用 `/concurrency` 或完整汇总。不能绕过这些拒绝。

## 不得误用的 GUI 私有 API

Magpie 桌面 GUI 资源由 Wails Asset Handler 提供：

```text
/api/gateway/trace
/api/gateway/history
/api/gateway/route
/api/groups
/api/groups/{action}
```

这些路径属于 Magpie GUI 内部服务，**不是** Desktop Gateway `:3425` 必然存在的公开 API。原版窗口通过 Wails 资源处理器调用它们。把 UI 请求重定向到 `/v1/...` 或手工启动第二个 `magpie web` 都不能保证拿到原桌面进程的全局内存 Trace。

## 本地路径发现

源码规定默认配置目录：

- **普通安装**：`~/.config/magpie`；如存在有效 `XDG_CONFIG_HOME`，使用其下 `magpie` 子目录。
- **Portable**：在 Magpie 可执行文件旁的 `data/` 目录，或 `.portable` 标记机制（以运行实例实际路径为准）。
- **手动覆盖**：GUI 允许用户在连接设置中指定实际目录（只读）。

主要关注：

```text
<config-dir>/providers.json
<config-dir>/settings.json
<config-dir>/routing/YYYY-MM-DD.jsonl
<config-dir>/routing/YYYY-MM-DD.jsonl.gz
```

`providers.json` 含 `providers`、`groups`、`groupOrder` 等静态结构；只读解析不能完整替代 Magpie GUI 的 `groupsState()`，因为部分自动组、账号状态和可用性来自运行时逻辑。不得写入或覆盖该文件。

## 历史记录契约

已结束请求异步写入每日 JSONL；旧日文件可能 gzip 压缩。Magpie 源码中的保留规则为 30 天、总 200 MiB；实际可用历史还受文件是否仍存在等因素影响。GUI 的历史 API 对单次查看另有限制，但**直接只读扫描本地文件**可以在合理资源上限内按文件读取，不应把单次 GUI 分页上限等同于磁盘保留总量。

Route 可含 `id`、`time`、`agent`、`session`、`model`、`group`、`rule`、`order`、`tries`、`done`、`status`、`ms`、`ttft`、`usage` 等。字段可能 `omitempty`，必须做好版本兼容和空值处理。

## 验证流程

1. Magpie Desktop 已启动并确认 Agent 模型请求可以正常通过 Gateway。
2. `GET /v1/models`、`GET /v1/magpie/concurrency` 至少有可解析 JSON；失败时保存脱敏的状态码/错误，不记录 token。
3. 用真实 Agent 发起长请求，在其执行期间抽样 `/concurrency`，观察 `active` 数组有无对应行。
4. 若 Agent 会发送 Session ID，再查询 `/v1/magpie/route?session=...`，核对路由模型和 Try。
5. 请求完成后，确认正确的 Magpie 数据目录下出现对应 JSONL 完整行（写入异步，可能稍迟）。
6. 将 API 的 `active[].route` 与历史 `Route.id` 关联；未匹配时不强行合并。
7. 停启 Magpie 桌面版，验证客户端重连及历史记录不重复。
8. 使用便携配置目录、日志轮转、跨日和损坏/不完整 JSONL 行测试只读行为。

## 权限与隐私

默认不进行路由组管理；不要读取与展示 `providers.json` 中的密钥字段。即使是本地运行，也只需要最小访问范围。不要尝试将内部 GUI API 或 Wails 资源处理器暴露到外网。

相关源码固定版本见 [SOURCES.md](SOURCES.md)。
