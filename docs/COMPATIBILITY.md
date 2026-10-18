# 与 Magpie 原版 Routing 页面兼容性矩阵

约束：现有 Magpie Desktop，不修改二进制；公开 Gateway API + 磁盘文件只读。`可做`表示实现路径明确，**不表示已开发或已测试**。

| 原版特性 | 计划支持 | 来源/条件 | 需要承认的差异 |
| --- | --- | --- | --- |
| Agent → Gateway → 模型拓扑 | 可做 | API + 文件 | 某些 Provider/Account 运行时信息可能缺失 |
| 节点、曲线、鸟飞行动画、并发效果 | 可做 | MyGo 原生绘制 | 视觉可复刻；动画时序受观测粒度影响 |
| 当前活动请求总览 | 可做 | `/v1/magpie/concurrency` | 采样，而非事件流 |
| 当前请求的排队/RPM/上游阶段 | 可做 | `active[].phase` | 只有 `slot`、`rpm`、`upstream` 等已声明阶段 |
| 单 Session 的尝试详情 | 可做 | `/v1/magpie/route` | 仅最新可见路由；需 Session ID |
| 全局同时请求的候选/失败细节实时更新 | 受限 | 无公开全局 Trace API | 不能忠实复制原版实时过程 |
| 完整请求最终路由顺序与尝试 | 可做 | 完成后的 JSONL/GZIP | 异步落盘后才可读 |
| Routing 历史按天检索 | 可做 | 本地文件 | 仅保留期内存在的数据 |
| 历史动画回放、变速/暂停 | 可做 | 已完成 Trace | 必须注明回放，而非实时采集 |
| 请求耗时、失败原因、错误状态 | 可做 | Route / API | 部分数据视字段、版本而定 |
| 请求 Token/配额/用量 | 可做 | Usage / Quotas | 价格推算需权威费率数据，不应假定缺失为 0 |
| 手动路由组基础展示 | 可做 | `providers.json` | 不是运行时完整 `groupsState()` |
| 自动生成组、订阅池、动态可用性 | 部分可做 | 配置 + 模型与配额 | 不能无损复制内存中求值结果 |
| 新建、编辑、删除、调序路由组 | **不支持**（本约束下） | GUI 内部 `/api/groups` 不对外开放 | 禁止直接写用户 Magpie 配置 |
| 远程 Magpie 监控 | 部分可做 | Gateway API + 授权 | 无远端本地文件访问，历史完整性受限 |
| 在 Magpie 本机运行的跨进程客户端 | 最优方案 | localhost API + 用户可读数据目录 | 无需在 Magpie 进程中插桩 |

## 判定规则

- **Live**：来自活动请求 API 或单 Session 端点，并有采集时刻。只能断言被观测时刻的已知状态。
- **Completed**：来自磁盘已结束请求的记录（`done=true` 或最终记录），允许展示所有持久化字段。
- **Replayed**：在 UI 上重放 Completed，不把模拟路径时间显示为原始实时传输的时间。
- **Unavailable**：接口无字段或被权限拒绝时，显示不可用，不产生推测性候选/账号/故障转移。

## 来源

详见 [SOURCES.md](SOURCES.md)，特别是 `internal/gui/trace.go`、`internal/gui/app.go`、`internal/gateway/active.go`、`internal/gateway/session_route.go`、`internal/gateway/history.go`、`internal/gui/groups.go`。
