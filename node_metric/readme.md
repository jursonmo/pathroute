# `node_metric`

这个目录实现有向边指标到动态路由 cost 的核心链路：

1. 节点上报或服务端模拟器产生 TCP/UDP 的延迟、丢包率和当前速率。
2. `MetricIngestService` 统一校验边、协议、数值、时间和样本身份，再写入 ClickHouse。
3. `CostWorker` 聚合最近窗口，通过可替换的协议公式和合并器计算边 cost，并以 MySQL revision 原子发布。
4. 路由协调器在 revision 提交后读取一致图，自动运行 Floyd 并原子替换最新完整结果。

关键接口包括 `MetricSource`、`MetricIngestor`、`ProtocolCostCalculator`、`EdgeCostCombiner` 和 `CostSnapshotStore`。首版页面模拟器只是 `MetricSource` 的一个实现；以后接入真实探测 API 不需要改变存储、cost 或路由模块。

默认业务语义：每条模拟配置默认 2 秒采样、2 秒聚合、10 秒平滑窗口、30 秒过期；TCP cost 为平均延迟，UDP cost 为平均延迟加丢包率乘 1000；两个协议都有效时取平均。指标全部过期时边降级为 cost 1000，但只有拓扑边 `status != 1` 才表示不可用。

数据库、环境变量、HTTP API 和运行方式见 [`cmd/view/README.md`](../cmd/view/README.md)。
