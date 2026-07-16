## Context

当前系统使用 MySQL 中的 `graph_nodes`、`graph_edges` 构造有向图，并把 `graph_edges.cost` 直接交给 Floyd 算法计算路径。边状态已经采用 fail-closed 语义：只有 `status == 1` 的边参与路由，其他状态的边仅用于展示。

本变更需要新增一条跨存储、跨后台任务的数据链路：节点指标进入 ClickHouse，最近指标被聚合为协议 cost，多个协议 cost 再合成为有向边 cost，最终以一致的 revision 发布到 MySQL 并触发路由重算。首版使用测试页面配置的随机指标模拟器，但数据源边界必须允许以后替换为真实节点上报或外部 API。

约束如下：

- 图算法继续只接受 `1～1000` 的整数 cost。
- 同一条有向边可以同时存在 TCP、UDP 指标。
- `status` 是边是否可用的唯一标志，指标系统不得修改它。
- 原始指标需要按边、协议和时间范围查询。
- 首版只计算单条最优路径，不解决容量约束下的多路径流量分配。

## Goals / Non-Goals

**Goals:**

- 提供统一、可校验、可去重的边指标模型和写入链路。
- 使用 ClickHouse 保存原始指标，并支持按时间范围查询和窗口聚合。
- 提供服务端随机指标模拟器，默认每 2 秒生成一次样本。
- 将 cost 公式和多协议合并策略抽象为可替换接口。
- 使用可配置的平滑、过期和防抖策略发布稳定的动态 cost。
- 使用 MySQL 事务和 revision 保证 cost 快照与路由计算的一致性。
- 在不修改静态 `graph_edges.cost` 的前提下，让现有路由计算读取动态 cost。
- 提供最新指标、动态 cost 和最新路由结果的测试页面与 API。

**Non-Goals:**

- 不实现带宽容量约束、流量需求建模或多路径分流。
- 不实现真实节点探测协议、Agent 或第三方 API 适配器。
- 不使用 Kafka、NATS 等消息系统拆分服务。
- 不修改 Floyd 算法的核心实现和 cost 范围。
- 不根据指标自动修改边或节点的 `status`。

## Decisions

### 1. 首版采用模块化单体

指标 API、模拟器、聚合器、cost 引擎和路由协调器运行在现有 `cmd/view` 进程中，但核心逻辑拆分为独立模块并通过接口通信。`cmd/view/main.go` 只负责配置、依赖装配、HTTP 路由和生命周期管理。

选择该方案是因为测试页面可以与现有页面同源，首版只需部署一个 Go 进程，同时模块边界仍允许以后把指标能力迁移到独立 `metricd`。备选方案是立即拆为两个服务或引入事件总线；它们的运维和一致性成本超过当前规模需要。

### 2. 数据源统一输出 `EdgeMetricSample`

统一样本至少包含：`from_node_id`、`to_node_id`、`proto`、`latency_ms`、`packet_loss_ratio`、`rate_bps`、`observed_at`、`received_at`、`sample_window_ms`、`source`、`source_id` 和 `sequence`。

随机模拟器、未来拉取式 API 适配器都实现 `MetricSource` 并向同一个 `MetricIngestService` 输出样本；真实节点也可以通过 `/api/metrics/report` 进入该服务。这样所有来源复用边存在性、协议、数值、时间戳和幂等校验。

服务端模拟器每次进程启动生成独立会话 `source_id`，序列号在会话内按边和协议递增，避免重启后从 1 重新计数与历史样本冲突。调度器按所有启用配置中最近的到期时间动态设置计时器；全局扫描周期只限制发现新增或修改配置的最长延迟，因此单边配置可以使用小于全局扫描周期的生成周期。

协议属于指标而不是 `EdgeModel.Type`。查询和聚合键为 `(from_node_id, to_node_id, proto)`，避免 TCP、UDP 样本混入同一个窗口。

### 3. ClickHouse 保存原始时序指标

`edge_metric_samples` 使用 `DateTime64(3, 'UTC')` 保存 `observed_at` 和 `received_at`。`ReplacingMergeTree` 以 `(from_node_id, to_node_id, proto, source_id, sequence)` 作为样本身份排序键，并以 `received_at` 选择同一身份的最后版本；表按 `source_id` 的固定哈希桶分区，确保同一身份即使观测时间跨月也能合并；`observed_at` 使用 minmax 跳数索引支持时间范围裁剪。这样同一身份的重试不会重复进入聚合，同时仍可按观测时间高效过滤。

选择 ClickHouse 是因为指标以追加写和时间窗口查询为主。MySQL 只保存配置和当前快照，避免把高频历史数据与拓扑事务数据混在一起。首版不强制历史数据 TTL，后续可按实际容量增加保留策略。

### 4. MySQL 保存模拟配置和三层 cost 快照

`edge_metric_simulations` 以 `(from_node_id, to_node_id, proto)` 唯一，保存三项指标的最小值、最大值、生成周期和启用状态。

cost 发布使用以下三张表：

- `edge_cost_publications`：追加保存全局 revision、触发原因、变化边数、公式版本和发布时间。
- `edge_cost_snapshots`：以 `(from_node_id, to_node_id)` 唯一，保存最终 cost、降级原因、窗口时间、最后指标时间、公式版本和最近发布 revision。
- `edge_protocol_cost_snapshots`：以 `(from_node_id, to_node_id, proto)` 唯一，保存各协议 cost、过期状态、聚合指标、样本数、窗口时间和 revision。

一次发布在同一个 MySQL 事务中插入 publication 并更新协议、边快照。事务提交后才使用该 revision 触发路由计算。候选 cost 和连续确认次数只保存在内存；重启后重新累计即可。

revision 只表示“路由可见权重版本”。当新聚合指标使最终 cost 未达到发布阈值时，系统在不创建 publication 的独立事务中刷新协议聚合、过期状态、降级状态和最后指标时间，同时保留当前 cost、revision 与发布时间。这样持续稳定的 cost 仍能提供正确 freshness，并且不会绕过防抖触发路由重算。

未选择把协议明细放入 JSON，是为了保留约束、索引和可测试性。未选择每个 revision 复制整张全量快照，是为了避免不必要的写放大；路由读取在同一个一致性事务中读取最新 publication 和当前边快照。

### 5. 指标聚合、协议公式和多协议合并相互独立

聚合器默认每 2 秒运行一次，按 `(from, to, proto)` 查询最近 10 秒样本并对延迟、丢包率和速率取算术平均。平滑窗口可配置。

`CostCalculator` 只负责把一个协议的聚合指标换算为 cost：

- TCP：`cost = avg_latency_ms`
- UDP：`cost = avg_latency_ms + avg_packet_loss_ratio × 1000`

结果四舍五入并限制到 `1～1000`。丢包惩罚系数和公式版本可配置、可追踪。速率首版只存储和展示，不参与公式。

`EdgeCostCombiner` 只合并当前有效协议：TCP、UDP 都有效时取算术平均；只有一个有效时直接使用它；没有有效协议时最终 cost 为 1000，并标记为降级。将计算器和合并器分开，允许以后独立替换协议公式或合并策略。

### 6. 过期与边状态具有不同语义

协议最后有效样本距当前时间达到默认 30 秒后视为过期，阈值可配置。单个协议过期时将其排除出合并；所有协议过期或从未出现有效指标时，边 cost 立即发布为 1000。

指标过期不会修改 `graph_edges.status`。构图规则为：

- `status != 1`：边不进入路由图。
- `status == 1` 且存在动态快照：使用快照 cost，包括 1000。
- `status == 1` 且不存在动态快照：使用 cost 1000。

静态 `graph_edges.cost` 保留用于人工配置、兼容和关闭动态模式后的回退；动态模式不得覆盖该字段。

### 7. cost 发布使用阈值、连续确认和最小重算间隔

普通候选 cost 与当前已发布 cost 的变化率为 `abs(candidate-published)/published`。默认变化率达到 10%，并连续确认 3 次后才发布。普通 cost 触发的路由重算默认至少间隔 30 秒。这些参数均可配置。

以下事件绕过普通防抖或最小间隔：边状态变为不可用、边恢复可用、所有协议过期并降级为 1000，以及一条边首次获得动态 cost。这样普通抖动不会频繁切路，拓扑可用性变化和首份动态权重又可以立即生效。

### 8. 路由结果按 revision 原子替换

路由协调器在 cost 事务提交后读取一个一致 revision 的静态拓扑、状态和当前动态 cost，构造图并运行现有 Floyd。只有完整计算成功后才原子替换内存中的最新路由结果。结果包含 cost revision、路由版本和计算时间，并通过 `/api/routes/latest` 查询。

现有 `/calculate` 保留为手动强制计算入口。图 API 和测试页面显示有效动态 cost，同时明确区分静态配置 cost、协议 cost 和最终 cost。

### 9. 故障采用保留最后成功状态的策略

ClickHouse 单次写入或查询失败不会立即将边降级为 1000；系统保留最后成功发布的 cost。若连续超过过期阈值仍无法取得有效指标，再按过期规则降级。MySQL 发布失败时事务回滚且不触发路由计算。路由计算失败时保留上一份完整结果。

后台聚合任务同一时刻最多运行一个实例，重叠 tick 被合并或跳过。服务关闭时通过 `context.Context` 停止模拟器和后台任务，并等待在途写入结束。

## Risks / Trade-offs

- [风险] 随机指标彼此独立，不能真实模拟流量、延迟和丢包之间的因果关系 → [缓解] 首版只用于验证数据链路和路由行为，真实数据源复用相同接口后再校准公式。
- [风险] 单路径根据延迟切换仍可能在负载变化时振荡 → [缓解] 使用平滑窗口、10% 阈值、连续确认和最小重算间隔；多路径流量工程明确留到后续变更。
- [风险] ClickHouse 不可用会使指标写入和聚合停滞 → [缓解] 保留最后成功快照，显式暴露错误，并在超过过期阈值后降级为 1000。
- [风险] 模块化单体中的后台任务可能影响查看 API → [缓解] 使用有界并发、批量查询、超时和独立接口，保留以后拆分 `metricd` 的边界。
- [风险] 每条边每个协议每天约产生 43,200 条记录，长期存储会增长 → [缓解] 使用 ClickHouse 分区和排序键，后续按容量配置 TTL 或聚合保留策略。
- [取舍] 当前快照表只保存最新值，不能单独还原每个历史 revision 的全量图 → [缓解] 原始指标和 publication 审计记录仍可用于重新计算；首版避免全量快照写放大。

## Migration Plan

1. 增加 ClickHouse 连接配置和 `edge_metric_samples` 表迁移。
2. 增加 MySQL 模拟配置、publication、边快照和协议快照表迁移，不修改现有 `graph_edges.cost` 数据。
3. 部署指标模块、API 和页面，先保持动态路由后台任务关闭。
4. 配置随机模拟器并验证指标写入、查询、聚合和快照发布。
5. 启用动态路由模式，首次没有快照的可用边按 cost 1000 参与计算。
6. 观察 cost revision、过期降级和路由结果后再扩大模拟边范围。

回滚时关闭动态路由和指标后台任务，构图恢复使用 `graph_edges.cost`；新增 ClickHouse/MySQL 表可以保留，不影响旧版本运行。

## Open Questions

无。首版需要的协议公式、聚合窗口、过期阈值、防抖参数、状态语义、存储边界和部署形态均已确认。
