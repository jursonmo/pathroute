package clickhouse

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

const insertMetricSQL = `INSERT INTO edge_metric_samples
    (from_node_id, to_node_id, proto, latency_ms, packet_loss_ratio, rate_bps,
     observed_at, received_at, sample_window_ms, source, source_id, sequence)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const aggregateMetricSQL = `SELECT
    from_node_id,
    to_node_id,
    proto,
    avg(latency_ms) AS avg_latency_ms,
    avg(packet_loss_ratio) AS avg_packet_loss_ratio,
    avg(rate_bps) AS avg_rate_bps,
    count() AS sample_count,
    max(observed_at) AS latest_observed_at
FROM edge_metric_samples FINAL
WHERE observed_at >= ? AND observed_at < ?
GROUP BY from_node_id, to_node_id, proto
ORDER BY from_node_id, to_node_id, proto`

// Store 使用 database/sql 实现原始指标写入、查询和窗口聚合。
type Store struct {
	db           *sql.DB
	queryTimeout time.Duration
}

var (
	_ nodemetric.MetricWriter = (*Store)(nil)
	_ nodemetric.MetricReader = (*Store)(nil)
)

// NewStore 创建 ClickHouse 指标仓储。
func NewStore(db *sql.DB, queryTimeout time.Duration) (*Store, error) {
	if db == nil {
		return nil, errors.New("clickhouse metric: database is required")
	}
	if queryTimeout <= 0 {
		return nil, errors.New("clickhouse metric: query timeout must be positive")
	}
	return &Store{db: db, queryTimeout: queryTimeout}, nil
}

// Append 在一个批次事务中写入全部样本。
func (s *Store) Append(ctx context.Context, samples []nodemetric.EdgeMetricSample) (int, error) {
	if len(samples) == 0 {
		return 0, nil
	}

	queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(queryCtx, nil)
	if err != nil {
		return 0, fmt.Errorf("beginning metric batch: %w", err)
	}
	stmt, err := tx.PrepareContext(queryCtx, insertMetricSQL)
	if err != nil {
		return 0, rollbackWithError(tx, fmt.Errorf("preparing metric batch: %w", err))
	}

	for _, sample := range samples {
		_, err := stmt.ExecContext(
			queryCtx,
			sample.FromNodeID,
			sample.ToNodeID,
			string(sample.Protocol),
			sample.LatencyMS,
			sample.PacketLossRatio,
			sample.RateBPS,
			sample.ObservedAt.UTC(),
			sample.ReceivedAt.UTC(),
			uint64(sample.SampleWindow/time.Millisecond),
			sample.Source,
			sample.SourceID,
			sample.Sequence,
		)
		if err != nil {
			closeErr := stmt.Close()
			writeErr := fmt.Errorf("executing metric batch: %w", err)
			return 0, rollbackWithError(tx, errors.Join(writeErr, closeErr))
		}
	}
	if err := stmt.Close(); err != nil {
		return 0, rollbackWithError(tx, fmt.Errorf("closing metric batch: %w", err))
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing metric batch: %w", err)
	}
	return len(samples), nil
}

// Query 按有向边、协议和半开时间区间 [start, end) 查询原始指标。
func (s *Store) Query(ctx context.Context, query nodemetric.MetricQuery) ([]nodemetric.EdgeMetricSample, error) {
	if query.Start.IsZero() || !query.End.After(query.Start) {
		return nil, errors.New("clickhouse metric: invalid query time range")
	}
	if query.Offset < 0 || query.Limit < 1 {
		return nil, errors.New("clickhouse metric: invalid query pagination")
	}
	if query.Protocol != nodemetric.ProtocolUnknown && !query.Protocol.IsValid() {
		return nil, errors.New("clickhouse metric: invalid query protocol")
	}

	var statement strings.Builder
	statement.WriteString(`SELECT
    from_node_id, to_node_id, proto, latency_ms, packet_loss_ratio, rate_bps,
    observed_at, received_at, sample_window_ms, source, source_id, sequence
FROM edge_metric_samples FINAL
WHERE observed_at >= ? AND observed_at < ?`)
	args := []any{query.Start.UTC(), query.End.UTC()}
	if query.FromNodeID != "" {
		statement.WriteString(" AND from_node_id = ?")
		args = append(args, query.FromNodeID)
	}
	if query.ToNodeID != "" {
		statement.WriteString(" AND to_node_id = ?")
		args = append(args, query.ToNodeID)
	}
	if query.Protocol != nodemetric.ProtocolUnknown {
		statement.WriteString(" AND proto = ?")
		args = append(args, string(query.Protocol))
	}
	statement.WriteString(" ORDER BY observed_at ASC, source_id ASC, sequence ASC LIMIT ? OFFSET ?")
	args = append(args, query.Limit, query.Offset)

	queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(queryCtx, statement.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("querying metric samples: %w", err)
	}
	defer rows.Close()

	samples := []nodemetric.EdgeMetricSample{}
	for rows.Next() {
		var sample nodemetric.EdgeMetricSample
		var protocol string
		var sampleWindowMS uint64
		if err := rows.Scan(
			&sample.FromNodeID,
			&sample.ToNodeID,
			&protocol,
			&sample.LatencyMS,
			&sample.PacketLossRatio,
			&sample.RateBPS,
			&sample.ObservedAt,
			&sample.ReceivedAt,
			&sampleWindowMS,
			&sample.Source,
			&sample.SourceID,
			&sample.Sequence,
		); err != nil {
			return nil, fmt.Errorf("scanning metric sample: %w", err)
		}
		sample.Protocol = nodemetric.Protocol(protocol)
		sample.SampleWindow = time.Duration(sampleWindowMS) * time.Millisecond
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating metric samples: %w", err)
	}
	return samples, nil
}

// Aggregate 按有向边和协议分别计算指定窗口的算术平均指标。
func (s *Store) Aggregate(ctx context.Context, start, end time.Time) ([]nodemetric.AggregatedMetric, error) {
	if start.IsZero() || !end.After(start) {
		return nil, errors.New("clickhouse metric: invalid aggregate time range")
	}

	queryCtx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	rows, err := s.db.QueryContext(queryCtx, aggregateMetricSQL, start.UTC(), end.UTC())
	if err != nil {
		return nil, fmt.Errorf("aggregating metric samples: %w", err)
	}
	defer rows.Close()

	metrics := []nodemetric.AggregatedMetric{}
	for rows.Next() {
		var metric nodemetric.AggregatedMetric
		var protocol string
		if err := rows.Scan(
			&metric.FromNodeID,
			&metric.ToNodeID,
			&protocol,
			&metric.AverageLatencyMS,
			&metric.AveragePacketLossRate,
			&metric.AverageRateBPS,
			&metric.SampleCount,
			&metric.LatestObservedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning aggregated metric: %w", err)
		}
		metric.Protocol = nodemetric.Protocol(protocol)
		metric.WindowStart = start.UTC()
		metric.WindowEnd = end.UTC()
		metrics = append(metrics, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating aggregated metrics: %w", err)
	}
	return metrics, nil
}

func rollbackWithError(tx *sql.Tx, cause error) error {
	if err := tx.Rollback(); err != nil {
		return errors.Join(cause, fmt.Errorf("rolling back metric batch: %w", err))
	}
	return cause
}
