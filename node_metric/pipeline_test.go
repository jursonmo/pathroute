package nodemetric

import (
	"context"
	"testing"
	"time"
)

type memoryMetricStore struct {
	samples []EdgeMetricSample
}

func (s *memoryMetricStore) Append(_ context.Context, samples []EdgeMetricSample) (int, error) {
	s.samples = append(s.samples, samples...)
	return len(samples), nil
}

func (s *memoryMetricStore) Query(context.Context, MetricQuery) ([]EdgeMetricSample, error) {
	return append([]EdgeMetricSample{}, s.samples...), nil
}

func (s *memoryMetricStore) Aggregate(_ context.Context, start, end time.Time) ([]AggregatedMetric, error) {
	type totals struct {
		metric AggregatedMetric
	}
	grouped := map[string]*totals{}
	for _, sample := range s.samples {
		if sample.ObservedAt.Before(start) || !sample.ObservedAt.Before(end) {
			continue
		}
		key := sample.EdgeKey.String() + ":" + string(sample.Protocol)
		group := grouped[key]
		if group == nil {
			group = &totals{metric: AggregatedMetric{
				EdgeKey:     sample.EdgeKey,
				Protocol:    sample.Protocol,
				WindowStart: start,
				WindowEnd:   end,
			}}
			grouped[key] = group
		}
		group.metric.AverageLatencyMS += sample.LatencyMS
		group.metric.AveragePacketLossRate += sample.PacketLossRatio
		group.metric.AverageRateBPS += sample.RateBPS
		group.metric.SampleCount++
		if sample.ObservedAt.After(group.metric.LatestObservedAt) {
			group.metric.LatestObservedAt = sample.ObservedAt
		}
	}
	metrics := make([]AggregatedMetric, 0, len(grouped))
	for _, group := range grouped {
		count := float64(group.metric.SampleCount)
		group.metric.AverageLatencyMS /= count
		group.metric.AveragePacketLossRate /= count
		group.metric.AverageRateBPS /= count
		metrics = append(metrics, group.metric)
	}
	return metrics, nil
}

func TestMetricCostPipelineIntegration(t *testing.T) {
	t.Parallel()

	clock := &mutableClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	metricStore := &memoryMetricStore{}
	validator, err := NewValidator(ValidationConfig{
		Clock:         clock,
		EdgeChecker:   edgeCheckerStub{exists: true},
		MaxFutureSkew: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	ingestor, err := NewIngestService(clock, validator, metricStore)
	if err != nil {
		t.Fatalf("NewIngestService() error = %v", err)
	}
	source, err := NewRandomMetricSource(clock, constantRandom{value: 0.5})
	if err != nil {
		t.Fatalf("NewRandomMetricSource() error = %v", err)
	}

	tcpConfig := validSimulationConfig(clock.Now())
	tcpConfig.Protocol = ProtocolTCP
	tcpConfig.LatencyMinMS = 30
	tcpConfig.LatencyMaxMS = 30
	tcpConfig.PacketLossMinRatio = 0
	tcpConfig.PacketLossMaxRatio = 0
	udpConfig := validSimulationConfig(clock.Now())
	udpConfig.LatencyMinMS = 20
	udpConfig.LatencyMaxMS = 20
	udpConfig.PacketLossMinRatio = 0.05
	udpConfig.PacketLossMaxRatio = 0.05
	for _, config := range []SimulationConfig{tcpConfig, udpConfig} {
		sample, err := source.Generate(context.Background(), config)
		if err != nil {
			t.Fatalf("Generate(%s) error = %v", config.Protocol, err)
		}
		if _, err := ingestor.Ingest(context.Background(), []EdgeMetricSample{sample}); err != nil {
			t.Fatalf("Ingest(%s) error = %v", config.Protocol, err)
		}
	}

	clock.Advance(time.Millisecond)
	costStore := &costSnapshotStoreStub{}
	worker := newTestCostWorker(t, metricStore, costStore, clock, nil)
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(costStore.edges) != 1 || costStore.edges[0].Cost != 50 || costStore.edges[0].Revision != 1 {
		t.Fatalf("published pipeline edge = %+v, want cost 50 revision 1", costStore.edges)
	}
}
