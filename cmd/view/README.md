# `cmd/view` with MySQL storage

`cmd/view` now persists graph data in MySQL via GORM.

## Environment

- `MYSQL_DSN` (required)
  - Example:
    - `user:pass@tcp(127.0.0.1:3306)/pathroute?charset=utf8mb4&parseTime=True&loc=Local`
- `SEED_FROM_JSON` (optional, default `true`)
  - If true, when DB has no nodes, it imports from `GRAPH_JSON_PATH`
- `GRAPH_JSON_PATH` (optional, default `data/graph.json`)

## Run

```bash
export MYSQL_DSN='user:pass@tcp(127.0.0.1:3306)/pathroute?charset=utf8mb4&parseTime=True&loc=Local'
go run ./cmd/view
```

Open: `http://localhost:8080`

## Shortest path with waypoints

In the viewer, click `计算路径`, then choose a start node, up to three ordered
waypoints, and an end node.

- When all segments are reachable, the viewer shows the top 4 shortest combined
  paths that pass through the selected waypoints in order.
- Returned paths never repeat a node. The start, waypoint, and end selections
  also cannot contain duplicate node IDs.
- When any segment is unreachable, the viewer stops at that segment and shows
  only the reachable prefix path under the no-repeated-node constraint.
