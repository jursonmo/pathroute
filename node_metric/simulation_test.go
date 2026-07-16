package nodemetric

import (
	"context"
	"errors"
	"testing"
	"time"
)

type constantRandom struct {
	value float64
}

func (r constantRandom) Float64() float64 { return r.value }

func TestValidateSimulationConfig(t *testing.T) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	valid := validSimulationConfig(now)
	if err := ValidateSimulationConfig(valid); err != nil {
		t.Fatalf("ValidateSimulationConfig(valid) error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*SimulationConfig)
	}{
		{name: "same endpoint", mutate: func(c *SimulationConfig) { c.ToNodeID = c.FromNodeID }},
		{name: "invalid protocol", mutate: func(c *SimulationConfig) { c.Protocol = "icmp" }},
		{name: "invalid interval", mutate: func(c *SimulationConfig) { c.Interval = 0 }},
		{name: "negative latency", mutate: func(c *SimulationConfig) { c.LatencyMinMS = -1 }},
		{name: "latency reversed", mutate: func(c *SimulationConfig) { c.LatencyMinMS = 30 }},
		{name: "negative loss", mutate: func(c *SimulationConfig) { c.PacketLossMinRatio = -0.1 }},
		{name: "loss over one", mutate: func(c *SimulationConfig) { c.PacketLossMaxRatio = 1.1 }},
		{name: "loss reversed", mutate: func(c *SimulationConfig) { c.PacketLossMinRatio = 0.2 }},
		{name: "negative rate", mutate: func(c *SimulationConfig) { c.RateMinBPS = -1 }},
		{name: "rate reversed", mutate: func(c *SimulationConfig) { c.RateMinBPS = 2_000_000 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := valid
			tt.mutate(&config)
			if err := ValidateSimulationConfig(config); !errors.Is(err, ErrInvalidSimulationConfig) {
				t.Fatalf("ValidateSimulationConfig() error = %v, want ErrInvalidSimulationConfig", err)
			}
		})
	}
}

func TestRandomMetricSourceGeneratesDeterministicMetric(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	source, err := NewRandomMetricSource(fixedClock{now: now}, constantRandom{value: 0.5})
	if err != nil {
		t.Fatalf("NewRandomMetricSource() error = %v", err)
	}
	config := validSimulationConfig(now)

	first, err := source.Generate(context.Background(), config)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	second, err := source.Generate(context.Background(), config)
	if err != nil {
		t.Fatalf("Generate() second error = %v", err)
	}

	if first.LatencyMS != 15 || first.PacketLossRatio != 0.05 || first.RateBPS != 1_000_000 {
		t.Fatalf("generated metric outside deterministic midpoint: %+v", first)
	}
	if first.Protocol != ProtocolUDP || first.EdgeKey != config.EdgeKey {
		t.Fatalf("generated dimensions = %+v", first)
	}
	if !first.ObservedAt.Equal(now) || !first.ReceivedAt.IsZero() || first.SampleWindow != 2*time.Second {
		t.Fatalf("generated times = observed %s received %s window %s", first.ObservedAt, first.ReceivedAt, first.SampleWindow)
	}
	if first.Sequence != 1 || second.Sequence != 2 || first.SourceID == "" {
		t.Fatalf("generated identities = %s/%d then %d", first.SourceID, first.Sequence, second.Sequence)
	}
	if first.SourceID != second.SourceID {
		t.Fatalf("one simulator instance changed source ID: %q != %q", first.SourceID, second.SourceID)
	}
	otherSource, err := NewRandomMetricSource(fixedClock{now: now.Add(time.Nanosecond)}, constantRandom{value: 0.5})
	if err != nil {
		t.Fatalf("NewRandomMetricSource(other instance) error = %v", err)
	}
	other, err := otherSource.Generate(context.Background(), config)
	if err != nil {
		t.Fatalf("Generate(other instance) error = %v", err)
	}
	if other.SourceID == first.SourceID {
		t.Fatalf("simulator restart reused source ID %q while sequence reset", other.SourceID)
	}
}

func validSimulationConfig(_ time.Time) SimulationConfig {
	return SimulationConfig{
		EdgeKey:            EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		Protocol:           ProtocolUDP,
		IsEnabled:          true,
		Interval:           2 * time.Second,
		LatencyMinMS:       10,
		LatencyMaxMS:       20,
		PacketLossMinRatio: 0,
		PacketLossMaxRatio: 0.1,
		RateMinBPS:         500_000,
		RateMaxBPS:         1_500_000,
	}
}
