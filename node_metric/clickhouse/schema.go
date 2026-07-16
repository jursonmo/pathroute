package clickhouse

import (
	"context"
	"database/sql"
	"fmt"
)

const edgeMetricSchema = `CREATE TABLE IF NOT EXISTS edge_metric_samples
(
    from_node_id String,
    to_node_id String,
    proto LowCardinality(String),
    latency_ms Float64,
    packet_loss_ratio Float64,
    rate_bps Float64,
    observed_at DateTime64(3, 'UTC'),
    received_at DateTime64(3, 'UTC'),
    sample_window_ms UInt64,
	source LowCardinality(String),
	source_id String,
	sequence UInt64,
	INDEX idx_observed_at observed_at TYPE minmax GRANULARITY 1
)
ENGINE = ReplacingMergeTree(received_at)
PARTITION BY cityHash64(source_id) % 64
ORDER BY (from_node_id, to_node_id, proto, source_id, sequence)`

// SchemaSQL 返回原始指标表的幂等建表语句。
func SchemaSQL() string {
	return edgeMetricSchema
}

// Migrate 创建原始指标表。
// ReplacingMergeTree 使用接收时间保留同一边、协议、来源和序列的最后版本。
// observed_at 不进入去重键，避免同一身份的重试因时间表示差异被重复聚合；minmax 索引用于时间范围裁剪。
// 分区只依赖 source_id，确保同一身份即使跨月重试也会进入同一分区并被 FINAL 去重。
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, SchemaSQL()); err != nil {
		return fmt.Errorf("migrating clickhouse metric schema: %w", err)
	}
	return nil
}
