package nodemetric

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const (
	// DegradeReasonMetricsExpired 表示一条边没有任何当前有效的协议指标。
	DegradeReasonMetricsExpired = "metrics_expired"
)

var (
	// ErrNoCostSnapshot 表示动态 cost 尚未完成首次发布。
	ErrNoCostSnapshot = errors.New("node metric: no cost snapshot")
)

// ProtocolCostCalculator 将一个协议维度的聚合指标换算为整数 cost。
type ProtocolCostCalculator interface {
	Calculate(metric AggregatedMetric) (int, error)
}

// TCPCostCalculator 使用平均延迟计算 TCP cost。
type TCPCostCalculator struct{}

// NewTCPCostCalculator 创建 TCP cost 计算器。
func NewTCPCostCalculator() *TCPCostCalculator { return &TCPCostCalculator{} }

// Calculate 返回 TCP 平均延迟四舍五入并限制到 1～1000 后的 cost。
func (c *TCPCostCalculator) Calculate(metric AggregatedMetric) (int, error) {
	if metric.Protocol != ProtocolTCP {
		return 0, fmt.Errorf("node metric: tcp calculator received protocol %q", metric.Protocol)
	}
	if !isFiniteNonNegative(metric.AverageLatencyMS) {
		return 0, errors.New("node metric: invalid average tcp latency")
	}
	return roundAndClampCost(metric.AverageLatencyMS), nil
}

// UDPCostCalculator 使用平均延迟和丢包惩罚计算 UDP cost。
type UDPCostCalculator struct {
	lossPenalty float64
}

// NewUDPCostCalculator 创建 UDP cost 计算器。
func NewUDPCostCalculator(lossPenalty float64) (*UDPCostCalculator, error) {
	if !isFiniteNonNegative(lossPenalty) {
		return nil, errors.New("node metric: udp loss penalty must be finite and non-negative")
	}
	return &UDPCostCalculator{lossPenalty: lossPenalty}, nil
}

// Calculate 按“平均延迟 + 平均丢包率 × 惩罚系数”计算 UDP cost。
func (c *UDPCostCalculator) Calculate(metric AggregatedMetric) (int, error) {
	if metric.Protocol != ProtocolUDP {
		return 0, fmt.Errorf("node metric: udp calculator received protocol %q", metric.Protocol)
	}
	validLatency := isFiniteNonNegative(metric.AverageLatencyMS)
	validPacketLoss := isFinite(metric.AveragePacketLossRate) &&
		metric.AveragePacketLossRate >= 0 &&
		metric.AveragePacketLossRate <= 1
	if !validLatency || !validPacketLoss {
		return 0, errors.New("node metric: invalid average udp metric")
	}
	// AverageRateBPS 首版只用于存储和展示，不能隐式改变已批准的协议公式。
	value := metric.AverageLatencyMS + metric.AveragePacketLossRate*c.lossPenalty
	return roundAndClampCost(value), nil
}

func roundAndClampCost(value float64) int {
	rounded := int(math.Round(value))
	switch {
	case rounded < 1:
		return 1
	case rounded > 1000:
		return 1000
	default:
		return rounded
	}
}

// EdgeCombineInput 集中传递协议合并所需上下文。
type EdgeCombineInput struct {
	EdgeKey        EdgeKey
	ProtocolCosts  []ProtocolCostSnapshot
	CalculatedAt   time.Time
	FormulaVersion string
}

// EdgeCostCombiner 将同一条有向边的有效协议 cost 合成为最终 cost。
type EdgeCostCombiner interface {
	Combine(input EdgeCombineInput) EdgeCostSnapshot
}

// AverageEdgeCostCombiner 对当前有效协议 cost 取算术平均。
type AverageEdgeCostCombiner struct{}

// NewAverageEdgeCostCombiner 创建首版协议合并器。
func NewAverageEdgeCostCombiner() *AverageEdgeCostCombiner {
	return &AverageEdgeCostCombiner{}
}

// Combine 忽略过期协议；没有有效协议时返回仍可参与路由的降级 cost 1000。
func (c *AverageEdgeCostCombiner) Combine(input EdgeCombineInput) EdgeCostSnapshot {
	protocolCosts := append([]ProtocolCostSnapshot{}, input.ProtocolCosts...)
	var total int
	var validCount int
	var windowStart time.Time
	var windowEnd time.Time
	var latestMetricAt time.Time
	for _, protocolCost := range protocolCosts {
		if windowStart.IsZero() || protocolCost.WindowStart.Before(windowStart) {
			windowStart = protocolCost.WindowStart
		}
		if protocolCost.WindowEnd.After(windowEnd) {
			windowEnd = protocolCost.WindowEnd
		}
		if protocolCost.LatestObservedAt.After(latestMetricAt) {
			latestMetricAt = protocolCost.LatestObservedAt
		}
		if protocolCost.IsExpired {
			continue
		}
		total += protocolCost.Cost
		validCount++
	}

	result := EdgeCostSnapshot{
		EdgeKey:        input.EdgeKey,
		ProtocolCosts:  protocolCosts,
		WindowStart:    windowStart,
		WindowEnd:      windowEnd,
		LatestMetricAt: latestMetricAt,
		FormulaVersion: input.FormulaVersion,
		CalculatedAt:   input.CalculatedAt,
	}
	if validCount == 0 {
		result.Cost = 1000
		result.IsDegraded = true
		result.DegradeReason = DegradeReasonMetricsExpired
		return result
	}
	result.Cost = roundAndClampCost(float64(total) / float64(validCount))
	return result
}

// ExpiryPolicy 判断协议最后有效样本是否已经过期。
type ExpiryPolicy struct {
	threshold time.Duration
}

// NewExpiryPolicy 创建可配置的指标过期策略。
func NewExpiryPolicy(threshold time.Duration) (*ExpiryPolicy, error) {
	if threshold <= 0 {
		return nil, errors.New("node metric: expiry threshold must be positive")
	}
	return &ExpiryPolicy{threshold: threshold}, nil
}

// IsExpired 在最后样本缺失或年龄达到阈值时返回 true。
func (p *ExpiryPolicy) IsExpired(now, latestObservedAt time.Time) bool {
	if latestObservedAt.IsZero() {
		return true
	}
	return !now.Before(latestObservedAt.Add(p.threshold))
}
