package nodemetric

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	// ErrInvalidMetric 表示样本字段违反统一指标模型约束。
	ErrInvalidMetric = errors.New("node metric: invalid metric")
	// ErrUnknownEdge 表示样本引用的有向边不存在。
	ErrUnknownEdge = errors.New("node metric: unknown edge")
)

// ValidationConfig 保存校验器所需的可信时间和拓扑依赖。
type ValidationConfig struct {
	Clock         Clock
	EdgeChecker   EdgeChecker
	MaxFutureSkew time.Duration
}

// Validator 在样本进入时序库前执行统一的信任边界校验。
type Validator struct {
	clock         Clock
	edgeChecker   EdgeChecker
	maxFutureSkew time.Duration
}

// NewValidator 创建指标校验器。
func NewValidator(config ValidationConfig) (*Validator, error) {
	if config.Clock == nil {
		return nil, errors.New("node metric: validation clock is required")
	}
	if config.EdgeChecker == nil {
		return nil, errors.New("node metric: edge checker is required")
	}
	if config.MaxFutureSkew < 0 {
		return nil, errors.New("node metric: maximum future skew must not be negative")
	}
	return &Validator{
		clock:         config.Clock,
		edgeChecker:   config.EdgeChecker,
		maxFutureSkew: config.MaxFutureSkew,
	}, nil
}

// Validate 校验单个样本及其引用的有向边。
func (v *Validator) Validate(ctx context.Context, sample EdgeMetricSample) error {
	if err := validateMetricFields(sample, v.clock.Now(), v.maxFutureSkew); err != nil {
		return err
	}

	exists, err := v.edgeChecker.EdgeExists(ctx, sample.EdgeKey)
	if err != nil {
		return fmt.Errorf("checking metric edge %s: %w", sample.EdgeKey.String(), err)
	}
	if !exists {
		return fmt.Errorf("%w: %s", ErrUnknownEdge, sample.EdgeKey.String())
	}
	return nil
}

func validateMetricFields(sample EdgeMetricSample, now time.Time, maxFutureSkew time.Duration) error {
	fromNodeID := strings.TrimSpace(sample.FromNodeID)
	toNodeID := strings.TrimSpace(sample.ToNodeID)
	switch {
	case fromNodeID == "" || toNodeID == "":
		return fmt.Errorf("%w: from_node_id and to_node_id are required", ErrInvalidMetric)
	case fromNodeID == toNodeID:
		return fmt.Errorf("%w: edge endpoints must differ", ErrInvalidMetric)
	case !sample.Protocol.IsValid():
		return fmt.Errorf("%w: proto must be tcp or udp", ErrInvalidMetric)
	case !isFiniteNonNegative(sample.LatencyMS):
		return fmt.Errorf("%w: latency_ms must be finite and non-negative", ErrInvalidMetric)
	case !isFinite(sample.PacketLossRatio) || sample.PacketLossRatio < 0 || sample.PacketLossRatio > 1:
		return fmt.Errorf("%w: packet_loss_ratio must be between 0 and 1", ErrInvalidMetric)
	case !isFiniteNonNegative(sample.RateBPS):
		return fmt.Errorf("%w: rate_bps must be finite and non-negative", ErrInvalidMetric)
	case sample.ObservedAt.IsZero():
		return fmt.Errorf("%w: observed_at is required", ErrInvalidMetric)
	case sample.ObservedAt.After(now.Add(maxFutureSkew)):
		return fmt.Errorf("%w: observed_at is too far in the future", ErrInvalidMetric)
	case sample.ReceivedAt.IsZero():
		return fmt.Errorf("%w: received_at is required", ErrInvalidMetric)
	case sample.SampleWindow <= 0:
		return fmt.Errorf("%w: sample_window must be positive", ErrInvalidMetric)
	case strings.TrimSpace(sample.Source) == "":
		return fmt.Errorf("%w: source is required", ErrInvalidMetric)
	case strings.TrimSpace(sample.SourceID) == "":
		return fmt.Errorf("%w: source_id is required", ErrInvalidMetric)
	case sample.Sequence == 0:
		return fmt.Errorf("%w: sequence must be positive", ErrInvalidMetric)
	default:
		return nil
	}
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isFiniteNonNegative(value float64) bool {
	return isFinite(value) && value >= 0
}
