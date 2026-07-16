package viewdb

import "time"

// EdgeMetricSimulationModel 持久化一条有向边、一个协议的模拟范围。
type EdgeMetricSimulationModel struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	FromNodeID      string     `gorm:"column:from_node_id;size:128;not null;uniqueIndex:uidx_edge_metric_simulation"`
	ToNodeID        string     `gorm:"column:to_node_id;size:128;not null;uniqueIndex:uidx_edge_metric_simulation"`
	Protocol        string     `gorm:"column:proto;size:8;not null;uniqueIndex:uidx_edge_metric_simulation"`
	Enabled         bool       `gorm:"column:enabled;not null;default:false"`
	IntervalMS      uint64     `gorm:"column:interval_ms;not null"`
	LatencyMinMS    float64    `gorm:"column:latency_min_ms;not null"`
	LatencyMaxMS    float64    `gorm:"column:latency_max_ms;not null"`
	PacketLossMin   float64    `gorm:"column:packet_loss_min_ratio;not null"`
	PacketLossMax   float64    `gorm:"column:packet_loss_max_ratio;not null"`
	RateMinBPS      float64    `gorm:"column:rate_min_bps;not null"`
	RateMaxBPS      float64    `gorm:"column:rate_max_bps;not null"`
	LastGeneratedAt *time.Time `gorm:"column:last_generated_at"`
	LastMetricJSON  []byte     `gorm:"column:last_metric_json;type:json"`
}

// TableName 返回模拟配置表名。
func (EdgeMetricSimulationModel) TableName() string { return "edge_metric_simulations" }

// EdgeCostPublicationModel 记录一次完整 cost 发布的全局 revision。
type EdgeCostPublicationModel struct {
	Revision       uint64    `gorm:"column:revision;primaryKey;autoIncrement"`
	TriggerReason  string    `gorm:"column:trigger_reason;size:64;not null"`
	ChangedEdges   int       `gorm:"column:changed_edges;not null"`
	FormulaVersion string    `gorm:"column:formula_version;size:64;not null"`
	PublishedAt    time.Time `gorm:"column:published_at;not null;index"`
}

// TableName 返回 cost publication 表名。
func (EdgeCostPublicationModel) TableName() string { return "edge_cost_publications" }

// EdgeCostSnapshotModel 保存每条有向边当前对路由可见的最终 cost。
type EdgeCostSnapshotModel struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	FromNodeID     string    `gorm:"column:from_node_id;size:128;not null;uniqueIndex:uidx_edge_cost_snapshot"`
	ToNodeID       string    `gorm:"column:to_node_id;size:128;not null;uniqueIndex:uidx_edge_cost_snapshot"`
	Cost           int       `gorm:"column:cost;not null"`
	Degraded       bool      `gorm:"column:degraded;not null;default:false"`
	DegradeReason  string    `gorm:"column:degrade_reason;size:128;not null;default:''"`
	WindowStart    time.Time `gorm:"column:window_start;not null"`
	WindowEnd      time.Time `gorm:"column:window_end;not null"`
	LatestMetricAt time.Time `gorm:"column:latest_metric_at;not null"`
	CalculatedAt   time.Time `gorm:"column:calculated_at;not null"`
	PublishedAt    time.Time `gorm:"column:published_at;not null"`
	FormulaVersion string    `gorm:"column:formula_version;size:64;not null"`
	Revision       uint64    `gorm:"column:revision;not null;index"`
}

// TableName 返回最终边 cost 快照表名。
func (EdgeCostSnapshotModel) TableName() string { return "edge_cost_snapshots" }

// EdgeProtocolCostSnapshotModel 保存协议 cost 和对应聚合指标。
type EdgeProtocolCostSnapshotModel struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time

	FromNodeID        string    `gorm:"column:from_node_id;size:128;not null;uniqueIndex:uidx_edge_protocol_cost_snapshot"`
	ToNodeID          string    `gorm:"column:to_node_id;size:128;not null;uniqueIndex:uidx_edge_protocol_cost_snapshot"`
	Protocol          string    `gorm:"column:proto;size:8;not null;uniqueIndex:uidx_edge_protocol_cost_snapshot"`
	Cost              int       `gorm:"column:cost;not null"`
	Expired           bool      `gorm:"column:expired;not null;default:false"`
	AverageLatencyMS  float64   `gorm:"column:avg_latency_ms;not null"`
	AveragePacketLoss float64   `gorm:"column:avg_packet_loss_ratio;not null"`
	AverageRateBPS    float64   `gorm:"column:avg_rate_bps;not null"`
	SampleCount       uint64    `gorm:"column:sample_count;not null"`
	WindowStart       time.Time `gorm:"column:window_start;not null"`
	WindowEnd         time.Time `gorm:"column:window_end;not null"`
	LatestObservedAt  time.Time `gorm:"column:latest_observed_at;not null"`
	CalculatedAt      time.Time `gorm:"column:calculated_at;not null"`
	FormulaVersion    string    `gorm:"column:formula_version;size:64;not null"`
	Revision          uint64    `gorm:"column:revision;not null;index"`
}

// TableName 返回协议 cost 快照表名。
func (EdgeProtocolCostSnapshotModel) TableName() string {
	return "edge_protocol_cost_snapshots"
}

// MetricModels 返回动态指标模块需要由 MySQL 迁移的模型。
func MetricModels() []any {
	return []any{
		&EdgeMetricSimulationModel{},
		&EdgeCostPublicationModel{},
		&EdgeCostSnapshotModel{},
		&EdgeProtocolCostSnapshotModel{},
	}
}
