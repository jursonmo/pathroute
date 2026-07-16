package nodemetric

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	// ErrInvalidSimulationConfig 表示随机范围或生成周期不符合指标约束。
	ErrInvalidSimulationConfig = errors.New("node metric: invalid simulation config")
)

// ValidateSimulationConfig 校验一条随机模拟配置。
func ValidateSimulationConfig(config SimulationConfig) error {
	fromNodeID := strings.TrimSpace(config.FromNodeID)
	toNodeID := strings.TrimSpace(config.ToNodeID)
	switch {
	case fromNodeID == "" || toNodeID == "":
		return fmt.Errorf("%w: from_node_id and to_node_id are required", ErrInvalidSimulationConfig)
	case fromNodeID == toNodeID:
		return fmt.Errorf("%w: edge endpoints must differ", ErrInvalidSimulationConfig)
	case !config.Protocol.IsValid():
		return fmt.Errorf("%w: proto must be tcp or udp", ErrInvalidSimulationConfig)
	case config.Interval <= 0:
		return fmt.Errorf("%w: interval must be positive", ErrInvalidSimulationConfig)
	case !validRange(config.LatencyMinMS, config.LatencyMaxMS, 0, false):
		return fmt.Errorf("%w: invalid latency range", ErrInvalidSimulationConfig)
	case !validRange(config.PacketLossMinRatio, config.PacketLossMaxRatio, 0, true):
		return fmt.Errorf("%w: invalid packet loss range", ErrInvalidSimulationConfig)
	case !validRange(config.RateMinBPS, config.RateMaxBPS, 0, false):
		return fmt.Errorf("%w: invalid rate range", ErrInvalidSimulationConfig)
	default:
		return nil
	}
}

func validRange(minimum, maximum, lowerBound float64, hasUpperBound bool) bool {
	if !isFinite(minimum) || !isFinite(maximum) || minimum < lowerBound || minimum > maximum {
		return false
	}
	return !hasUpperBound || maximum <= 1
}

// RandomMetricSource 根据配置范围生成指标，内部序列号按边和协议独立递增。
type RandomMetricSource struct {
	clock          Clock
	random         Float64Source
	sourceIDPrefix string
	mu             sync.Mutex
	sequences      map[string]uint64
}

var _ MetricSource = (*RandomMetricSource)(nil)

// NewRandomMetricSource 创建可注入时钟和随机数的模拟数据源。
func NewRandomMetricSource(clock Clock, random Float64Source) (*RandomMetricSource, error) {
	if clock == nil || random == nil {
		return nil, errors.New("node metric: simulation clock and random source are required")
	}
	return &RandomMetricSource{
		clock:          clock,
		random:         random,
		sourceIDPrefix: fmt.Sprintf("simulator:%d", clock.Now().UTC().UnixNano()),
		sequences:      map[string]uint64{},
	}, nil
}

// Generate 生成一个样本；ReceivedAt 留空，由统一 IngestService 填写可信服务端时间。
func (s *RandomMetricSource) Generate(ctx context.Context, config SimulationConfig) (EdgeMetricSample, error) {
	if err := ctx.Err(); err != nil {
		return EdgeMetricSample{}, err
	}
	if err := ValidateSimulationConfig(config); err != nil {
		return EdgeMetricSample{}, err
	}

	key := config.EdgeKey.String() + ":" + string(config.Protocol)
	s.mu.Lock()
	defer s.mu.Unlock()

	latency, err := s.randomBetween(config.LatencyMinMS, config.LatencyMaxMS)
	if err != nil {
		return EdgeMetricSample{}, err
	}
	packetLoss, err := s.randomBetween(config.PacketLossMinRatio, config.PacketLossMaxRatio)
	if err != nil {
		return EdgeMetricSample{}, err
	}
	rate, err := s.randomBetween(config.RateMinBPS, config.RateMaxBPS)
	if err != nil {
		return EdgeMetricSample{}, err
	}
	s.sequences[key]++

	return EdgeMetricSample{
		EdgeKey:         config.EdgeKey,
		Protocol:        config.Protocol,
		LatencyMS:       latency,
		PacketLossRatio: packetLoss,
		RateBPS:         rate,
		ObservedAt:      s.clock.Now().UTC(),
		SampleWindow:    config.Interval,
		Source:          "simulator",
		// 每个模拟器进程使用独立来源前缀，重启后序列从 1 开始也不会与旧会话身份冲突。
		SourceID: s.sourceIDPrefix + ":" + key,
		Sequence: s.sequences[key],
	}, nil
}

func (s *RandomMetricSource) randomBetween(minimum, maximum float64) (float64, error) {
	if minimum == maximum {
		return minimum, nil
	}
	ratio := s.random.Float64()
	if !isFinite(ratio) || ratio < 0 || ratio >= 1 {
		return 0, errors.New("node metric: random source must return a value in [0,1)")
	}
	return minimum + (maximum-minimum)*ratio, nil
}
