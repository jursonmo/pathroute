package nodemetric

import (
	"context"
	"errors"
	"testing"
	"time"
)

type metricWriterStub struct {
	samples []EdgeMetricSample
	count   int
	err     error
}

func (s *metricWriterStub) Append(_ context.Context, samples []EdgeMetricSample) (int, error) {
	s.samples = append([]EdgeMetricSample{}, samples...)
	return s.count, s.err
}

func TestIngestServiceSetsTrustedReceivedTime(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	validator, err := NewValidator(ValidationConfig{
		Clock:         fixedClock{now: now},
		EdgeChecker:   edgeCheckerStub{exists: true},
		MaxFutureSkew: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	writer := &metricWriterStub{count: 1}
	service, err := NewIngestService(fixedClock{now: now}, validator, writer)
	if err != nil {
		t.Fatalf("NewIngestService() error = %v", err)
	}

	sample := validMetric(now)
	originalObservedAt := sample.ObservedAt
	sample.ReceivedAt = now.Add(-time.Hour)
	count, err := service.Ingest(context.Background(), []EdgeMetricSample{sample})
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if count != 1 || len(writer.samples) != 1 {
		t.Fatalf("Ingest() count/samples = %d/%d, want 1/1", count, len(writer.samples))
	}
	if !writer.samples[0].ReceivedAt.Equal(now) {
		t.Fatalf("ReceivedAt = %s, want %s", writer.samples[0].ReceivedAt, now)
	}
	if !writer.samples[0].ObservedAt.Equal(originalObservedAt) {
		t.Fatalf("ObservedAt = %s, want preserved %s", writer.samples[0].ObservedAt, originalObservedAt)
	}
}

func TestIngestServiceValidatesWholeBatchBeforeWriting(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	validator, err := NewValidator(ValidationConfig{
		Clock:         fixedClock{now: now},
		EdgeChecker:   edgeCheckerStub{exists: true},
		MaxFutureSkew: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	writer := &metricWriterStub{count: 2}
	service, err := NewIngestService(fixedClock{now: now}, validator, writer)
	if err != nil {
		t.Fatalf("NewIngestService() error = %v", err)
	}
	valid := validMetric(now)
	invalid := validMetric(now)
	invalid.Sequence = 2
	invalid.PacketLossRatio = 2

	if _, err := service.Ingest(context.Background(), []EdgeMetricSample{valid, invalid}); !errors.Is(err, ErrInvalidMetric) {
		t.Fatalf("Ingest() error = %v, want ErrInvalidMetric", err)
	}
	if len(writer.samples) != 0 {
		t.Fatalf("writer received %d samples after validation failure", len(writer.samples))
	}
}

func TestIngestServicePropagatesStorageFailure(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	validator, err := NewValidator(ValidationConfig{
		Clock:         fixedClock{now: now},
		EdgeChecker:   edgeCheckerStub{exists: true},
		MaxFutureSkew: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	storageErr := errors.New("storage unavailable")
	writer := &metricWriterStub{err: storageErr}
	service, err := NewIngestService(fixedClock{now: now}, validator, writer)
	if err != nil {
		t.Fatalf("NewIngestService() error = %v", err)
	}

	if _, err := service.Ingest(context.Background(), []EdgeMetricSample{validMetric(now)}); !errors.Is(err, storageErr) {
		t.Fatalf("Ingest() error = %v, want wrapped storage error", err)
	}
}
