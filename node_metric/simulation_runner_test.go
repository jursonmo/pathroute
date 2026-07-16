package nodemetric

import (
	"context"
	"sync"
	"testing"
	"time"
)

type mutableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *mutableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *mutableClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

type simulationStoreStub struct {
	configs []SimulationConfig
	updates []EdgeMetricSample
}

func (s *simulationStoreStub) List(context.Context) ([]SimulationConfig, error) {
	return append([]SimulationConfig{}, s.configs...), nil
}

func (s *simulationStoreStub) Replace(context.Context, []SimulationConfig) error { return nil }

func (s *simulationStoreStub) UpdateRuntime(_ context.Context, sample EdgeMetricSample) error {
	s.updates = append(s.updates, sample)
	return nil
}

type metricSourceStub struct {
	clock Clock
	calls int
}

func (s *metricSourceStub) Generate(_ context.Context, config SimulationConfig) (EdgeMetricSample, error) {
	s.calls++
	return EdgeMetricSample{
		EdgeKey:      config.EdgeKey,
		Protocol:     config.Protocol,
		ObservedAt:   s.clock.Now(),
		SampleWindow: config.Interval,
		Source:       "stub",
		SourceID:     "stub-source",
		Sequence:     uint64(s.calls),
	}, nil
}

type metricIngestorStub struct {
	samples []EdgeMetricSample
}

func (s *metricIngestorStub) Ingest(_ context.Context, samples []EdgeMetricSample) (int, error) {
	s.samples = append(s.samples, samples...)
	return len(samples), nil
}

func TestSimulationRunnerRunOnceRespectsPerConfigInterval(t *testing.T) {
	t.Parallel()

	clock := &mutableClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	config := validSimulationConfig(clock.Now())
	store := &simulationStoreStub{configs: []SimulationConfig{config}}
	source := &metricSourceStub{clock: clock}
	ingestor := &metricIngestorStub{}
	runner, err := NewSimulationRunner(store, source, ingestor, clock, SimulationRunnerConfig{
		ScanInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSimulationRunner() error = %v", err)
	}

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() first error = %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() same interval error = %v", err)
	}
	if len(ingestor.samples) != 1 {
		t.Fatalf("same interval generated %d samples, want 1", len(ingestor.samples))
	}

	clock.Advance(config.Interval)
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() next interval error = %v", err)
	}
	if len(ingestor.samples) != 2 || len(store.updates) != 2 {
		t.Fatalf("generated/runtime updates = %d/%d, want 2/2", len(ingestor.samples), len(store.updates))
	}
}

func TestSimulationRunnerSkipsDisabledConfig(t *testing.T) {
	t.Parallel()

	clock := &mutableClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	config := validSimulationConfig(clock.Now())
	config.IsEnabled = false
	store := &simulationStoreStub{configs: []SimulationConfig{config}}
	source := &metricSourceStub{clock: clock}
	ingestor := &metricIngestorStub{}
	runner, err := NewSimulationRunner(store, source, ingestor, clock, SimulationRunnerConfig{
		ScanInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewSimulationRunner() error = %v", err)
	}

	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if source.calls != 0 || len(ingestor.samples) != 0 {
		t.Fatalf("disabled config generated source/ingest calls = %d/%d", source.calls, len(ingestor.samples))
	}
}

func TestSimulationRunnerAppliesUpdatedConfigOnNextScan(t *testing.T) {
	t.Parallel()

	clock := &mutableClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	config := validSimulationConfig(clock.Now())
	store := &simulationStoreStub{configs: []SimulationConfig{config}}
	source := &metricSourceStub{clock: clock}
	runner, err := NewSimulationRunner(
		store,
		source,
		&metricIngestorStub{},
		clock,
		SimulationRunnerConfig{ScanInterval: 10 * time.Millisecond},
	)
	if err != nil {
		t.Fatalf("NewSimulationRunner() error = %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}

	clock.Advance(time.Second)
	config.Interval = time.Second
	store.configs = []SimulationConfig{config}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() after config update error = %v", err)
	}
	if source.calls != 2 {
		t.Fatalf("Generate() calls = %d, want updated 1s interval to take effect", source.calls)
	}

	config.IsEnabled = false
	store.configs = []SimulationConfig{config}
	clock.Advance(time.Second)
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() after disabling config error = %v", err)
	}
	if source.calls != 2 {
		t.Fatalf("disabled updated config generated another sample; calls=%d", source.calls)
	}
}

func TestSimulationRunnerNextDelayUsesPerConfigIntervalBelowScanInterval(t *testing.T) {
	t.Parallel()

	clock := &mutableClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	config := validSimulationConfig(clock.Now())
	config.Interval = 100 * time.Millisecond
	store := &simulationStoreStub{configs: []SimulationConfig{config}}
	runner, err := NewSimulationRunner(
		store,
		&metricSourceStub{clock: clock},
		&metricIngestorStub{},
		clock,
		SimulationRunnerConfig{ScanInterval: 2 * time.Second},
	)
	if err != nil {
		t.Fatalf("NewSimulationRunner() error = %v", err)
	}
	if err := runner.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}

	delay, err := runner.nextDelay(context.Background())
	if err != nil {
		t.Fatalf("nextDelay() error = %v", err)
	}
	if delay != config.Interval {
		t.Fatalf("nextDelay() = %s, want per-config interval %s", delay, config.Interval)
	}

	clock.Advance(30 * time.Millisecond)
	delay, err = runner.nextDelay(context.Background())
	if err != nil {
		t.Fatalf("nextDelay() after advance error = %v", err)
	}
	if delay != 70*time.Millisecond {
		t.Fatalf("nextDelay() after advance = %s, want 70ms", delay)
	}
}

func TestSimulationRunnerStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	clock := &mutableClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	store := &simulationStoreStub{}
	runner, err := NewSimulationRunner(
		store,
		&metricSourceStub{clock: clock},
		&metricIngestorStub{},
		clock,
		SimulationRunnerConfig{ScanInterval: time.Millisecond},
	)
	if err != nil {
		t.Fatalf("NewSimulationRunner() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runner.Run(ctx)
	}()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}
