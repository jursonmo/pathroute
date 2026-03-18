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

