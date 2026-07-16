package nodemetric

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type aggregateReaderStub struct {
	metrics []AggregatedMetric
	err     error
}

type blockingAggregateReader struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (r *blockingAggregateReader) Query(context.Context, MetricQuery) ([]EdgeMetricSample, error) {
	return []EdgeMetricSample{}, nil
}

func (r *blockingAggregateReader) Aggregate(context.Context, time.Time, time.Time) ([]AggregatedMetric, error) {
	if r.calls.Add(1) == 1 {
		close(r.started)
	}
	<-r.release
	return []AggregatedMetric{}, nil
}

func (s *aggregateReaderStub) Query(context.Context, MetricQuery) ([]EdgeMetricSample, error) {
	return []EdgeMetricSample{}, nil
}

func (s *aggregateReaderStub) Aggregate(context.Context, time.Time, time.Time) ([]AggregatedMetric, error) {
	return append([]AggregatedMetric{}, s.metrics...), s.err
}

type costSnapshotStoreStub struct {
	edges        []EdgeCostSnapshot
	publications []CostPublication
	refreshes    int
	publishErr   error
}

func (s *costSnapshotStoreStub) Publish(
	_ context.Context,
	publication CostPublication,
	protocols []ProtocolCostSnapshot,
	edges []EdgeCostSnapshot,
) (CostPublication, error) {
	if s.publishErr != nil {
		return CostPublication{}, s.publishErr
	}
	publication.Revision = uint64(len(s.publications) + 1)
	s.publications = append(s.publications, publication)
	protocolsByEdge := map[string][]ProtocolCostSnapshot{}
	for _, protocol := range protocols {
		protocol.Revision = publication.Revision
		protocolsByEdge[protocol.EdgeKey.String()] = append(protocolsByEdge[protocol.EdgeKey.String()], protocol)
	}
	for i := range edges {
		edges[i].Revision = publication.Revision
		edges[i].PublishedAt = publication.PublishedAt
		edges[i].ProtocolCosts = protocolsByEdge[edges[i].EdgeKey.String()]
	}
	s.edges = append([]EdgeCostSnapshot{}, edges...)
	return publication, nil
}

func (s *costSnapshotStoreStub) ListEdgeCosts(context.Context) ([]EdgeCostSnapshot, error) {
	return append([]EdgeCostSnapshot{}, s.edges...), nil
}

func (s *costSnapshotStoreStub) LatestPublication(context.Context) (CostPublication, error) {
	if len(s.publications) == 0 {
		return CostPublication{}, ErrNoCostSnapshot
	}
	return s.publications[len(s.publications)-1], nil
}

func (s *costSnapshotStoreStub) RefreshMetrics(
	_ context.Context,
	protocols []ProtocolCostSnapshot,
	edges []EdgeCostSnapshot,
) error {
	s.refreshes++
	protocolsByEdge := map[string][]ProtocolCostSnapshot{}
	for _, protocol := range protocols {
		protocolsByEdge[protocol.EdgeKey.String()] = append(protocolsByEdge[protocol.EdgeKey.String()], protocol)
	}
	for i := range edges {
		edges[i].ProtocolCosts = protocolsByEdge[edges[i].EdgeKey.String()]
	}
	s.edges = append([]EdgeCostSnapshot{}, edges...)
	return nil
}

func TestCostWorkerPublishesInitialTCPAndUDPAverage(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{now: now}
	reader := &aggregateReaderStub{metrics: []AggregatedMetric{
		aggregatedMetricForWorker(ProtocolTCP, 30, 0, now),
		aggregatedMetricForWorker(ProtocolUDP, 20, 0.05, now),
	}}
	store := &costSnapshotStoreStub{}
	var callbackPublication CostPublication
	var bypassRouteDelay bool
	worker := newTestCostWorker(t, reader, store, clock, func(publication CostPublication, bypass bool) {
		callbackPublication = publication
		bypassRouteDelay = bypass
	})

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(store.edges) != 1 || store.edges[0].Cost != 50 {
		t.Fatalf("published edges = %+v, want one cost 50", store.edges)
	}
	if len(store.edges[0].ProtocolCosts) != 2 {
		t.Fatalf("protocol costs = %+v, want tcp and udp", store.edges[0].ProtocolCosts)
	}
	if callbackPublication.Revision != 1 {
		t.Fatalf("callback revision = %d, want 1", callbackPublication.Revision)
	}
	if !bypassRouteDelay {
		t.Fatal("first dynamic cost publication must bypass route minimum interval")
	}
}

func TestCostWorkerKeepsSnapshotForEmptyWindowBeforeExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{now: now}
	currentProtocol := ProtocolCostSnapshot{
		AggregatedMetric: aggregatedMetricForWorker(ProtocolTCP, 30, 0, now.Add(-10*time.Second)),
		Cost:             30,
		FormulaVersion:   "v1",
	}
	store := &costSnapshotStoreStub{edges: []EdgeCostSnapshot{{
		EdgeKey:        currentProtocol.EdgeKey,
		Cost:           30,
		ProtocolCosts:  []ProtocolCostSnapshot{currentProtocol},
		LatestMetricAt: currentProtocol.LatestObservedAt,
	}}}
	worker := newTestCostWorker(t, &aggregateReaderStub{}, store, clock, nil)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(store.publications) != 0 || store.edges[0].Cost != 30 {
		t.Fatalf("empty non-expired window changed snapshot: publications=%d edge=%+v", len(store.publications), store.edges[0])
	}
}

func TestCostWorkerQueryFailureDegradesOnlyAfterExpiry(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{now: now}
	queryErr := errors.New("clickhouse unavailable")
	reader := &aggregateReaderStub{err: queryErr}
	currentProtocol := ProtocolCostSnapshot{
		AggregatedMetric: aggregatedMetricForWorker(ProtocolTCP, 30, 0, now.Add(-10*time.Second)),
		Cost:             30,
		FormulaVersion:   "v1",
	}
	store := &costSnapshotStoreStub{edges: []EdgeCostSnapshot{{
		EdgeKey:        currentProtocol.EdgeKey,
		Cost:           30,
		ProtocolCosts:  []ProtocolCostSnapshot{currentProtocol},
		LatestMetricAt: currentProtocol.LatestObservedAt,
	}}}
	var emergency bool
	worker := newTestCostWorker(t, reader, store, clock, func(_ CostPublication, isEmergency bool) {
		emergency = isEmergency
	})

	if err := worker.RunOnce(context.Background()); !errors.Is(err, queryErr) {
		t.Fatalf("RunOnce() error = %v, want query error", err)
	}
	if len(store.publications) != 0 {
		t.Fatal("single query failure published a new cost before expiry")
	}

	clock.Advance(21 * time.Second)
	if err := worker.RunOnce(context.Background()); !errors.Is(err, queryErr) {
		t.Fatalf("RunOnce() after expiry error = %v, want query error plus successful degradation", err)
	}
	if len(store.edges) != 1 || store.edges[0].Cost != 1000 || !store.edges[0].IsDegraded {
		t.Fatalf("expired edge = %+v, want degraded cost 1000", store.edges)
	}
	if !emergency {
		t.Fatal("expiry publication was not marked emergency")
	}
}

func TestCostWorkerRefreshesFreshnessWhenCostIsStable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 20, 0, time.UTC)
	clock := &mutableClock{now: now}
	currentProtocol := ProtocolCostSnapshot{
		AggregatedMetric: aggregatedMetricForWorker(ProtocolTCP, 30, 0, now.Add(-20*time.Second)),
		Cost:             30,
		FormulaVersion:   "v1",
	}
	store := &costSnapshotStoreStub{edges: []EdgeCostSnapshot{{
		EdgeKey:        currentProtocol.EdgeKey,
		Cost:           30,
		ProtocolCosts:  []ProtocolCostSnapshot{currentProtocol},
		LatestMetricAt: currentProtocol.LatestObservedAt,
		Revision:       7,
	}}}
	reader := &aggregateReaderStub{metrics: []AggregatedMetric{
		aggregatedMetricForWorker(ProtocolTCP, 30, 0, now),
	}}
	worker := newTestCostWorker(t, reader, store, clock, nil)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("stable RunOnce() error = %v", err)
	}
	if store.refreshes != 1 || len(store.publications) != 0 {
		t.Fatalf("stable cost refresh/publications = %d/%d, want 1/0", store.refreshes, len(store.publications))
	}
	if !store.edges[0].LatestMetricAt.Equal(now) || store.edges[0].Revision != 7 || store.edges[0].Cost != 30 {
		t.Fatalf("refreshed edge = %+v, want fresh metadata with unchanged cost/revision", store.edges[0])
	}

	reader.metrics = nil
	reader.err = errors.New("clickhouse unavailable")
	clock.Advance(20 * time.Second)
	if err := worker.RunOnce(context.Background()); !errors.Is(err, reader.err) {
		t.Fatalf("query failure error = %v", err)
	}
	if len(store.publications) != 0 || store.edges[0].IsDegraded {
		t.Fatalf("recent stable sample degraded on query failure: %+v", store.edges[0])
	}
}

func TestCostWorkerClearsDegradationWhenValidCostRemains1000(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{now: now}
	store := &costSnapshotStoreStub{edges: []EdgeCostSnapshot{{
		EdgeKey:       EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		Cost:          1000,
		IsDegraded:    true,
		DegradeReason: DegradeReasonMetricsExpired,
		Revision:      4,
	}}}
	reader := &aggregateReaderStub{metrics: []AggregatedMetric{
		aggregatedMetricForWorker(ProtocolTCP, 1000, 0, now),
	}}
	worker := newTestCostWorker(t, reader, store, clock, nil)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if store.refreshes != 1 || store.edges[0].IsDegraded || store.edges[0].DegradeReason != "" {
		t.Fatalf("valid cost 1000 did not clear degradation: %+v", store.edges[0])
	}
	if store.edges[0].Cost != 1000 || store.edges[0].Revision != 4 || len(store.publications) != 0 {
		t.Fatalf("recovery changed route weight revision: %+v publications=%d", store.edges[0], len(store.publications))
	}
}

func TestCostWorkerRefreshesExpiredProtocolWhenFinalCostIsUnchanged(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 31, 0, time.UTC)
	clock := &mutableClock{now: now}
	tcp := ProtocolCostSnapshot{AggregatedMetric: aggregatedMetricForWorker(ProtocolTCP, 30, 0, now), Cost: 30, FormulaVersion: "v1"}
	udp := ProtocolCostSnapshot{AggregatedMetric: aggregatedMetricForWorker(ProtocolUDP, 30, 0, now.Add(-31*time.Second)), Cost: 30, FormulaVersion: "v1"}
	store := &costSnapshotStoreStub{edges: []EdgeCostSnapshot{{
		EdgeKey:       tcp.EdgeKey,
		Cost:          30,
		ProtocolCosts: []ProtocolCostSnapshot{tcp, udp},
		Revision:      6,
	}}}
	worker := newTestCostWorker(t, &aggregateReaderStub{metrics: []AggregatedMetric{tcp.AggregatedMetric}}, store, clock, nil)

	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if store.refreshes != 1 || len(store.edges[0].ProtocolCosts) != 2 {
		t.Fatalf("protocol refresh = %+v", store.edges[0])
	}
	for _, protocol := range store.edges[0].ProtocolCosts {
		if protocol.Protocol == ProtocolUDP && !protocol.IsExpired {
			t.Fatalf("expired UDP state was not refreshed: %+v", protocol)
		}
	}
}

func TestCostWorkerCoalescesOverlappingRunOnce(t *testing.T) {
	t.Parallel()

	reader := &blockingAggregateReader{started: make(chan struct{}), release: make(chan struct{})}
	worker := newTestCostWorker(
		t,
		reader,
		&costSnapshotStoreStub{},
		&mutableClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)},
		nil,
	)
	firstDone := make(chan error, 1)
	go func() { firstDone <- worker.RunOnce(context.Background()) }()
	<-reader.started

	// 第二个 tick 在首个聚合仍阻塞时到达，必须直接合并，不能并发读取或产生半批发布。
	if err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("overlapping RunOnce() error = %v", err)
	}
	if calls := reader.calls.Load(); calls != 1 {
		t.Fatalf("Aggregate() calls = %d, want 1", calls)
	}
	close(reader.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first RunOnce() error = %v", err)
	}
}

func TestCostWorkerStopsOnContextCancellation(t *testing.T) {
	t.Parallel()

	worker := newTestCostWorker(
		t,
		&aggregateReaderStub{},
		&costSnapshotStoreStub{},
		&mutableClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)},
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
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

func newTestCostWorker(
	t *testing.T,
	reader MetricReader,
	store CostSnapshotStore,
	clock Clock,
	onPublished func(CostPublication, bool),
) *CostWorker {
	t.Helper()
	udp, err := NewUDPCostCalculator(1000)
	if err != nil {
		t.Fatalf("NewUDPCostCalculator() error = %v", err)
	}
	expiry, err := NewExpiryPolicy(30 * time.Second)
	if err != nil {
		t.Fatalf("NewExpiryPolicy() error = %v", err)
	}
	stabilizer, err := NewCostStabilizer(StabilizerConfig{ChangeThreshold: 0.10, Confirmations: 3})
	if err != nil {
		t.Fatalf("NewCostStabilizer() error = %v", err)
	}
	worker, err := NewCostWorker(CostWorkerDependencies{
		Reader: reader,
		Store:  store,
		Clock:  clock,
		Calculators: map[Protocol]ProtocolCostCalculator{
			ProtocolTCP: NewTCPCostCalculator(),
			ProtocolUDP: udp,
		},
		Combiner:   NewAverageEdgeCostCombiner(),
		Expiry:     expiry,
		Stabilizer: stabilizer,
	}, CostWorkerConfig{
		Interval:       2 * time.Second,
		Window:         10 * time.Second,
		FormulaVersion: "v1",
		OnPublished:    onPublished,
	})
	if err != nil {
		t.Fatalf("NewCostWorker() error = %v", err)
	}
	return worker
}

func aggregatedMetricForWorker(
	protocol Protocol,
	latencyMS float64,
	packetLoss float64,
	latest time.Time,
) AggregatedMetric {
	return AggregatedMetric{
		EdgeKey:               EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		Protocol:              protocol,
		AverageLatencyMS:      latencyMS,
		AveragePacketLossRate: packetLoss,
		AverageRateBPS:        1_000_000,
		SampleCount:           5,
		WindowStart:           latest.Add(-10 * time.Second),
		WindowEnd:             latest,
		LatestObservedAt:      latest,
	}
}
