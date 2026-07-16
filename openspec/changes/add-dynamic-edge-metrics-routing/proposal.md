## Why

当前路由计算只使用人工配置的静态边 cost，无法根据节点之间持续变化的延迟、丢包率和速率调整最优路径。需要建立一条可测试、可替换的数据链路，将边指标持久化并稳定换算为动态 cost，从而自动触发单路径路由重算。

## What Changes

- 增加统一的有向边指标模型和写入、校验、去重、按时间范围查询能力，原始时序指标保存到 ClickHouse。
- 增加可在测试页面配置的随机指标模拟器，按边和协议设置延迟、丢包率、速率范围，默认每 2 秒生成一次指标。
- 增加可替换的 cost 计算接口、协议级计算器和边 cost 合并器，对最近 10 秒指标进行平滑，并将当前动态 cost 快照原子发布到 MySQL。
- 增加动态 cost 过期、降级和防抖规则：指标默认 30 秒过期；全部协议过期时 cost 为 1000；普通 cost 变化达到 10% 且连续确认 3 次后发布。
- 增加自动路由重算和版本化最新结果；边 `status != 1` 时不参与路由，边 `status == 1` 且动态 cost 为 1000 时仍参与计算。
- 保留现有静态 `graph_edges.cost` 作为人工配置和兼容字段，动态路由模式不覆盖它。

## Capabilities

### New Capabilities

- `edge-metric-ingestion`: 定义边指标数据模型、统一写入校验、ClickHouse 存储、去重和时间范围查询。
- `edge-metric-simulation`: 定义测试页面随机范围配置、模拟器生命周期及按周期生成指标的行为。
- `dynamic-edge-cost`: 定义指标聚合、协议 cost 公式、多协议合并、过期降级、防抖和 MySQL 快照发布。
- `automatic-route-recalculation`: 定义动态 cost 与边状态合并构图、自动触发重算、revision 一致性和最新路由查询。

### Modified Capabilities

无。

## Impact

- 影响 `cmd/view` 的启动装配、HTTP API、测试页面、图展示和 `/calculate` 行为。
- 新增独立的指标领域、模拟器、存储、聚合、cost 和路由协调模块，但首版仍运行在现有查看服务进程内。
- MySQL 新增随机模拟配置、最终边 cost 快照、协议级 cost 快照和发布版本相关表。
- 新增 ClickHouse 依赖及边指标时序表。
- `internal/viewdb` 构图时需要组合静态拓扑、边状态和当前动态 cost 快照。
- 现有 Floyd 和图算法无需改变 cost 的基本约束，仍使用 `1～1000` 的整数 cost。
