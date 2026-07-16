package nodemetric

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type sampleValidator interface {
	Validate(ctx context.Context, sample EdgeMetricSample) error
}

// SystemClock 使用系统 UTC 时间，测试代码应注入可控 Clock。
type SystemClock struct{}

// Now 返回当前 UTC 时间。
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// IngestService 是 HTTP 上报、模拟器和未来真实数据源共享的指标接收入口。
type IngestService struct {
	clock     Clock
	validator sampleValidator
	writer    MetricWriter
}

// NewIngestService 创建统一指标接收服务。
func NewIngestService(clock Clock, validator sampleValidator, writer MetricWriter) (*IngestService, error) {
	if clock == nil {
		return nil, errors.New("node metric: ingest clock is required")
	}
	if validator == nil {
		return nil, errors.New("node metric: validator is required")
	}
	if writer == nil {
		return nil, errors.New("node metric: metric writer is required")
	}
	return &IngestService{
		clock:     clock,
		validator: validator,
		writer:    writer,
	}, nil
}

// Ingest 校验并原子提交一批样本。
func (s *IngestService) Ingest(ctx context.Context, samples []EdgeMetricSample) (int, error) {
	if len(samples) == 0 {
		return 0, fmt.Errorf("%w: samples are required", ErrInvalidMetric)
	}

	receivedAt := s.clock.Now().UTC()
	validated := make([]EdgeMetricSample, len(samples))
	for i, sample := range samples {
		// received_at 是服务端信任时间，必须覆盖客户端提供的值，不能用于伪造活跃度。
		sample.ReceivedAt = receivedAt
		if err := s.validator.Validate(ctx, sample); err != nil {
			return 0, fmt.Errorf("validating sample %d: %w", i, err)
		}
		validated[i] = sample
	}

	// 先校验整批再写入，避免一个非法样本造成批次的前半部分已经持久化。
	count, err := s.writer.Append(ctx, validated)
	if err != nil {
		return 0, fmt.Errorf("storing metric samples: %w", err)
	}
	return count, nil
}
