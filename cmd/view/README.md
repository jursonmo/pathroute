# 路由查看器与动态边指标

`cmd/view` 是一个模块化单体：同一进程提供拓扑页面、指标 API、服务端随机指标模拟器、动态 cost 聚合发布和 Floyd 路由重算。MySQL 保存拓扑、模拟配置与当前 cost 快照，ClickHouse 保存高频原始指标。

## 数据库准备

先创建 MySQL 数据库；GORM 会在启动时自动迁移拓扑和动态指标表：

```sql
CREATE DATABASE pathroute CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

动态模式还要求 ClickHouse 中目标 database 已存在。程序会自动创建 `edge_metric_samples` 表：

```sql
CREATE DATABASE IF NOT EXISTS pathroute;
```

关闭动态模式时不会连接 ClickHouse，路由继续使用 `graph_edges.cost`。打开动态模式后，`graph_edges.cost` 只作为静态配置保留；可用边使用动态快照，没有快照时以 cost 1000 参与路由。只有 `graph_edges.status == 1` 的边可参与计算。

## 配置

可复制仓库根目录的 `.env.example`，执行 `set -a; source .env.example; set +a` 后启动。程序本身不会自动读取 `.env` 文件。

主要默认值：

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `VIEW_LISTEN_ADDR` | `:8080` | HTTP 监听地址 |
| `MYSQL_DSN` | `root:@tcp(127.0.0.1:3306)/pathroute?...` | MySQL DSN |
| `SEED_FROM_JSON` | `true` | 空库时是否导入图 JSON |
| `GRAPH_JSON_PATH` | `data/graph.json` | 初始化图文件 |
| `DYNAMIC_METRICS_ENABLED` | `false` | 动态指标与动态路由总开关 |
| `METRIC_SAMPLE_INTERVAL` | `2s` | 模拟配置变更的最长发现间隔；实际生成周期由每条配置决定 |
| `METRIC_AGGREGATION_INTERVAL` | `2s` | cost 聚合周期 |
| `METRIC_AGGREGATION_WINDOW` | `10s` | 指标平滑窗口 |
| `METRIC_EXPIRY_THRESHOLD` | `30s` | 协议指标过期阈值 |
| `METRIC_UDP_LOSS_PENALTY` | `1000` | UDP 丢包惩罚系数 |
| `METRIC_COST_CHANGE_THRESHOLD` | `0.10` | 普通 cost 相对变化阈值 |
| `METRIC_COST_CONFIRMATIONS` | `3` | 普通变化连续确认次数 |
| `METRIC_MIN_ROUTE_INTERVAL` | `30s` | 普通自动路由最小间隔 |
| `METRIC_FORMULA_VERSION` | `v1` | 快照记录的公式版本 |
| `METRIC_MAX_FUTURE_SKEW` | `5m` | 客户端观测时间最大超前量 |
| `METRIC_DEFAULT_QUERY_LIMIT` | `200` | 历史查询默认条数 |
| `METRIC_MAX_QUERY_LIMIT` | `1000` | 历史查询和模拟配置批次上限 |
| `METRIC_MAX_REPORT_BATCH` | `1000` | 单次上报最大样本数 |
| `CLICKHOUSE_ADDRS` | `127.0.0.1:9000` | 逗号分隔的原生协议地址 |
| `CLICKHOUSE_DATABASE` | `default` | ClickHouse database |
| `CLICKHOUSE_USERNAME` | `default` | ClickHouse 用户名 |
| `CLICKHOUSE_PASSWORD` | 空 | ClickHouse 密码 |
| `CLICKHOUSE_DIAL_TIMEOUT` | `5s` | 建连超时 |
| `CLICKHOUSE_QUERY_TIMEOUT` | `5s` | 单次查询/写入超时 |
| `CLICKHOUSE_MAX_OPEN_CONNS` | `10` | 最大连接数 |
| `CLICKHOUSE_MAX_IDLE_CONNS` | `5` | 最大空闲连接数 |
| `CLICKHOUSE_CONN_MAX_LIFETIME` | `30m` | 连接最长生命周期 |
| `VIEW_SHUTDOWN_TIMEOUT` | `10s` | HTTP 优雅退出超时 |

## 启动

```bash
export MYSQL_DSN='root:@tcp(127.0.0.1:3306)/pathroute?charset=utf8mb4&parseTime=True&loc=Local'
export DYNAMIC_METRICS_ENABLED=true
export CLICKHOUSE_DATABASE=pathroute
go run ./cmd/view
```

打开 `http://localhost:8080`。页面可以为每条有向边分别保存 TCP/UDP 随机范围、查询指标历史、查看协议 cost、最终 cost、降级原因以及路由使用的 revision。浏览器关闭不会停止服务端模拟器。

## API

- `POST /api/metrics/report`：真实节点或外部采集器批量上报统一指标。
- `GET /api/metrics?from=A&to=B&proto=tcp&start=...&end=...&limit=200`：查询原始指标。
- `GET|PUT /api/metric-simulations`：读取或批量保存随机模拟配置。
- `GET /api/edge-costs`：查询当前边级和协议级 cost 快照。
- `GET /api/routes/latest`：查询最近一次完整路由结果及 cost revision。
- `POST /calculate`：人工强制使用当前一致图重算路由。
- `GET /graph`：查询页面使用的拓扑和有效 cost；同时返回静态 cost、动态 cost 来源和降级状态。

上报示例：

```bash
curl -X POST http://127.0.0.1:8080/api/metrics/report \
  -H 'Content-Type: application/json' \
  -d '{"samples":[{"from_node_id":"A","to_node_id":"B","proto":"tcp","latency_ms":20,"packet_loss_ratio":0,"rate_bps":1000000,"observed_at":"2026-07-15T08:00:00Z","sample_window_ms":2000,"source":"agent","source_id":"agent-A","sequence":1}]}'
```

`received_at` 由服务器覆盖。`source_id + sequence` 是幂等身份；同一条边的 TCP 和 UDP 是两个独立指标维度。

## cost 与扩展点

首版公式为 TCP `cost = 平均延迟`，UDP `cost = 平均延迟 + 平均丢包率 × 1000`；两种协议同时有效时取算术平均，只有一种有效时直接使用该协议。结果四舍五入并限制到 1～1000。所有协议过期时边保持可用状态，但以 cost 1000 降级。

随机生成器仅实现 `node_metric.MetricSource`，并与真实 HTTP 上报共用 `MetricIngestService`。以后接真实探测 API 时，实现新的 `MetricSource` 并在 `assembleDynamicMetrics` 中替换 `RandomMetricSource` 即可，ClickHouse、cost 公式、发布事务和路由模块无需改动。

## 验证

```bash
go test ./...
node --test cmd/view/*.test.js
go test -race ./...
```

设置 `CLICKHOUSE_ADDR` 后可运行 ClickHouse 集成测试：

```bash
go test -tags=integration ./node_metric/clickhouse
```

跨 MySQL/ClickHouse 的持久化端到端用例应使用专用测试库，避免清理测试数据影响开发环境：

```bash
export PATHROUTE_E2E_MYSQL_DSN='root:@tcp(127.0.0.1:3306)/pathroute_e2e?charset=utf8mb4&parseTime=True&loc=Local'
export PATHROUTE_E2E_CLICKHOUSE_ADDR='127.0.0.1:9000'
export PATHROUTE_E2E_CLICKHOUSE_DATABASE='pathroute_e2e'
go test -tags=integration ./node_metric -run TestPersistentPageToRouteEndToEnd -v
```
