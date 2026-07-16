package nodemetric

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time { return c.now }

type edgeCheckerStub struct {
	exists bool
	err    error
}

func (s edgeCheckerStub) EdgeExists(context.Context, EdgeKey) (bool, error) {
	return s.exists, s.err
}

func TestValidatorAcceptsValidMetric(t *testing.T) {
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

	if err := validator.Validate(context.Background(), validMetric(now)); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidatorRejectsInvalidMetricFields(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*EdgeMetricSample)
	}{
		{name: "missing from", mutate: func(s *EdgeMetricSample) { s.FromNodeID = "" }},
		{name: "missing to", mutate: func(s *EdgeMetricSample) { s.ToNodeID = "" }},
		{name: "same endpoint", mutate: func(s *EdgeMetricSample) { s.ToNodeID = s.FromNodeID }},
		{name: "unknown protocol", mutate: func(s *EdgeMetricSample) { s.Protocol = "icmp" }},
		{name: "negative latency", mutate: func(s *EdgeMetricSample) { s.LatencyMS = -1 }},
		{name: "nan latency", mutate: func(s *EdgeMetricSample) { s.LatencyMS = math.NaN() }},
		{name: "negative loss", mutate: func(s *EdgeMetricSample) { s.PacketLossRatio = -0.1 }},
		{name: "loss over one", mutate: func(s *EdgeMetricSample) { s.PacketLossRatio = 1.1 }},
		{name: "negative rate", mutate: func(s *EdgeMetricSample) { s.RateBPS = -1 }},
		{name: "missing observed time", mutate: func(s *EdgeMetricSample) { s.ObservedAt = time.Time{} }},
		{name: "future observed time", mutate: func(s *EdgeMetricSample) { s.ObservedAt = now.Add(2 * time.Minute) }},
		{name: "missing received time", mutate: func(s *EdgeMetricSample) { s.ReceivedAt = time.Time{} }},
		{name: "invalid window", mutate: func(s *EdgeMetricSample) { s.SampleWindow = 0 }},
		{name: "missing source", mutate: func(s *EdgeMetricSample) { s.Source = "" }},
		{name: "missing source id", mutate: func(s *EdgeMetricSample) { s.SourceID = "" }},
		{name: "missing sequence", mutate: func(s *EdgeMetricSample) { s.Sequence = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			validator, err := NewValidator(ValidationConfig{
				Clock:         fixedClock{now: now},
				EdgeChecker:   edgeCheckerStub{exists: true},
				MaxFutureSkew: time.Minute,
			})
			if err != nil {
				t.Fatalf("NewValidator() error = %v", err)
			}
			sample := validMetric(now)
			tt.mutate(&sample)
			if err := validator.Validate(context.Background(), sample); !errors.Is(err, ErrInvalidMetric) {
				t.Fatalf("Validate() error = %v, want ErrInvalidMetric", err)
			}
		})
	}
}

func TestValidatorRejectsUnknownEdge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	validator, err := NewValidator(ValidationConfig{
		Clock:         fixedClock{now: now},
		EdgeChecker:   edgeCheckerStub{exists: false},
		MaxFutureSkew: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}

	if err := validator.Validate(context.Background(), validMetric(now)); !errors.Is(err, ErrUnknownEdge) {
		t.Fatalf("Validate() error = %v, want ErrUnknownEdge", err)
	}
}

func validMetric(now time.Time) EdgeMetricSample {
	return EdgeMetricSample{
		EdgeKey:         EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		Protocol:        ProtocolTCP,
		LatencyMS:       20,
		PacketLossRatio: 0.01,
		RateBPS:         1_000_000,
		ObservedAt:      now.Add(-time.Second),
		ReceivedAt:      now,
		SampleWindow:    2 * time.Second,
		Source:          "test",
		SourceID:        "test-source",
		Sequence:        1,
	}
}
