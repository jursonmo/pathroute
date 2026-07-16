package nodemetric

import (
	"context"
	"fmt"
	"time"
)

// Protocol 表示指标所属的传输协议。
// 协议必须挂在指标上，不能从拓扑边类型推断，否则同一条边无法同时保存 TCP 和 UDP 指标。
type Protocol string

const (
	ProtocolUnknown Protocol = ""
	ProtocolTCP     Protocol = "tcp"
	ProtocolUDP     Protocol = "udp"
)

// IsValid 判断协议是否可参与首版指标计算。
func (p Protocol) IsValid() bool {
	return p == ProtocolTCP || p == ProtocolUDP
}

// EdgeKey 唯一标识一条有向边，A->B 与 B->A 是两个不同的键。
type EdgeKey struct {
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`
}

// String 返回适合日志和内存映射使用的稳定有向边标识。
func (k EdgeKey) String() string {
	return k.FromNodeID + "->" + k.ToNodeID
}

// EdgeMetricSample 是所有指标来源进入系统时使用的统一样本。
type EdgeMetricSample struct {
	EdgeKey
	Protocol        Protocol      `json:"proto"`
	LatencyMS       float64       `json:"latency_ms"`
	PacketLossRatio float64       `json:"packet_loss_ratio"`
	RateBPS         float64       `json:"rate_bps"`
	ObservedAt      time.Time     `json:"observed_at"`
	ReceivedAt      time.Time     `json:"received_at"`
	SampleWindow    time.Duration `json:"sample_window"`
	Source          string        `json:"source"`
	SourceID        string        `json:"source_id"`
	Sequence        uint64        `json:"sequence"`
}

// Identity 返回样本的幂等键。
// SourceID 标识一个稳定数据源，Sequence 标识该数据源内的一次采样，两者共同避免重试被重复聚合。
func (s EdgeMetricSample) Identity() string {
	return fmt.Sprintf("%s:%d", s.SourceID, s.Sequence)
}

// AggregatedMetric 保存同一有向边、同一协议在一个平滑窗口内的聚合结果。
type AggregatedMetric struct {
	EdgeKey
	Protocol              Protocol  `json:"proto"`
	AverageLatencyMS      float64   `json:"avg_latency_ms"`
	AveragePacketLossRate float64   `json:"avg_packet_loss_ratio"`
	AverageRateBPS        float64   `json:"avg_rate_bps"`
	SampleCount           uint64    `json:"sample_count"`
	WindowStart           time.Time `json:"window_start"`
	WindowEnd             time.Time `json:"window_end"`
	LatestObservedAt      time.Time `json:"latest_observed_at"`
}

// ProtocolCostSnapshot 保存单个协议的当前 cost 及其聚合依据。
type ProtocolCostSnapshot struct {
	AggregatedMetric
	Cost           int       `json:"cost"`
	IsExpired      bool      `json:"is_expired"`
	FormulaVersion string    `json:"formula_version"`
	CalculatedAt   time.Time `json:"calculated_at"`
	Revision       uint64    `json:"revision"`
}

// EdgeCostSnapshot 保存路由算法实际使用的最终有向边 cost。
type EdgeCostSnapshot struct {
	EdgeKey
	Cost           int                    `json:"cost"`
	IsDegraded     bool                   `json:"is_degraded"`
	DegradeReason  string                 `json:"degrade_reason,omitempty"`
	ProtocolCosts  []ProtocolCostSnapshot `json:"protocol_costs"`
	WindowStart    time.Time              `json:"window_start"`
	WindowEnd      time.Time              `json:"window_end"`
	LatestMetricAt time.Time              `json:"latest_metric_at"`
	FormulaVersion string                 `json:"formula_version"`
	CalculatedAt   time.Time              `json:"calculated_at"`
	PublishedAt    time.Time              `json:"published_at"`
	Revision       uint64                 `json:"revision"`
}

// CostPublication 描述一次原子发布。
// 路由协调器只能在 publication 与全部快照提交成功后使用该 Revision，避免读取半批新 cost。
type CostPublication struct {
	Revision       uint64    `json:"revision"`
	TriggerReason  string    `json:"trigger_reason"`
	ChangedEdges   int       `json:"changed_edges"`
	FormulaVersion string    `json:"formula_version"`
	PublishedAt    time.Time `json:"published_at"`
}

// SimulationConfig 保存一条有向边、一个协议的随机指标生成范围。
type SimulationConfig struct {
	EdgeKey
	Protocol           Protocol          `json:"proto"`
	IsEnabled          bool              `json:"enabled"`
	Interval           time.Duration     `json:"interval"`
	LatencyMinMS       float64           `json:"latency_min_ms"`
	LatencyMaxMS       float64           `json:"latency_max_ms"`
	PacketLossMinRatio float64           `json:"packet_loss_min_ratio"`
	PacketLossMaxRatio float64           `json:"packet_loss_max_ratio"`
	RateMinBPS         float64           `json:"rate_min_bps"`
	RateMaxBPS         float64           `json:"rate_max_bps"`
	LastGeneratedAt    *time.Time        `json:"last_generated_at,omitempty"`
	LastMetric         *EdgeMetricSample `json:"last_metric,omitempty"`
}

// MetricQuery 描述原始指标的有界时间范围查询。
type MetricQuery struct {
	EdgeKey
	Protocol Protocol
	Start    time.Time
	End      time.Time
	Offset   int
	Limit    int
}

// Clock 为过期判断和后台任务提供可控时间。
type Clock interface {
	Now() time.Time
}

// Float64Source 为模拟器提供可固定 seed 的随机数来源。
type Float64Source interface {
	Float64() float64
}

// EdgeChecker 在信任边界校验有向边是否存在。
type EdgeChecker interface {
	EdgeExists(ctx context.Context, key EdgeKey) (bool, error)
}

// MetricWriter 持久化已经通过领域校验的指标样本。
type MetricWriter interface {
	Append(ctx context.Context, samples []EdgeMetricSample) (int, error)
}

// MetricReader 提供历史查询和协议窗口聚合。
type MetricReader interface {
	Query(ctx context.Context, query MetricQuery) ([]EdgeMetricSample, error)
	Aggregate(ctx context.Context, start, end time.Time) ([]AggregatedMetric, error)
}

// MetricIngestor 是模拟器与真实上报共同依赖的写入边界。
type MetricIngestor interface {
	Ingest(ctx context.Context, samples []EdgeMetricSample) (int, error)
}

// MetricSource 根据一条配置生成一个指标样本。
type MetricSource interface {
	Generate(ctx context.Context, config SimulationConfig) (EdgeMetricSample, error)
}

// SimulationConfigStore 持久化模拟配置，替换操作必须保持原子性。
type SimulationConfigStore interface {
	List(ctx context.Context) ([]SimulationConfig, error)
	Replace(ctx context.Context, configs []SimulationConfig) error
}

// SimulationRuntimeUpdater 保存最近一次成功生成的指标状态。
type SimulationRuntimeUpdater interface {
	UpdateRuntime(ctx context.Context, sample EdgeMetricSample) error
}

// SimulationStore 组合模拟配置和运行状态持久化能力。
type SimulationStore interface {
	SimulationConfigStore
	SimulationRuntimeUpdater
}

// CostSnapshotStore 原子发布和读取动态 cost 快照。
type CostSnapshotStore interface {
	Publish(
		ctx context.Context,
		publication CostPublication,
		protocols []ProtocolCostSnapshot,
		edges []EdgeCostSnapshot,
	) (CostPublication, error)
	// RefreshMetrics 更新最新聚合和过期状态，但不得改变路由使用的 cost 或 revision。
	RefreshMetrics(ctx context.Context, protocols []ProtocolCostSnapshot, edges []EdgeCostSnapshot) error
	ListEdgeCosts(ctx context.Context) ([]EdgeCostSnapshot, error)
	LatestPublication(ctx context.Context) (CostPublication, error)
}
