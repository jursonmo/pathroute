package nodemetric_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jursonmo/pathroute/graph"
	nodemetric "github.com/jursonmo/pathroute/node_metric"
	"github.com/jursonmo/pathroute/node_metric/httpapi"
	routeresult "github.com/jursonmo/pathroute/node_metric/route"
)

func TestPageConfigurationToLatestRouteEndToEnd(t *testing.T) {
	clock := &e2eClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	metricStore := &e2eMetricStore{}
	simulationStore := &e2eSimulationStore{}
	costStore := &e2eCostStore{}
	validator, err := nodemetric.NewValidator(nodemetric.ValidationConfig{
		Clock:         clock,
		EdgeChecker:   e2eEdgeChecker{},
		MaxFutureSkew: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	ingestor, err := nodemetric.NewIngestService(clock, validator, metricStore)
	if err != nil {
		t.Fatalf("NewIngestService() error = %v", err)
	}
	source, err := nodemetric.NewRandomMetricSource(clock, e2eRandom(0.5))
	if err != nil {
		t.Fatalf("NewRandomMetricSource() error = %v", err)
	}
	runner, err := nodemetric.NewSimulationRunner(
		simulationStore,
		source,
		ingestor,
		clock,
		nodemetric.SimulationRunnerConfig{ScanInterval: 2 * time.Second},
	)
	if err != nil {
		t.Fatalf("NewSimulationRunner() error = %v", err)
	}

	resultStore := routeresult.NewResultStore()
	coordinator, err := routeresult.NewCoordinator(
		&e2eGraphProvider{costs: costStore},
		routeresult.NewFloydCalculator(),
		resultStore,
		routeresult.Config{Clock: clock, MinInterval: 30 * time.Second},
	)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	udpCalculator, err := nodemetric.NewUDPCostCalculator(1000)
	if err != nil {
		t.Fatalf("NewUDPCostCalculator() error = %v", err)
	}
	expiry, err := nodemetric.NewExpiryPolicy(30 * time.Second)
	if err != nil {
		t.Fatalf("NewExpiryPolicy() error = %v", err)
	}
	stabilizer, err := nodemetric.NewCostStabilizer(nodemetric.StabilizerConfig{
		ChangeThreshold: 0.10,
		Confirmations:   3,
	})
	if err != nil {
		t.Fatalf("NewCostStabilizer() error = %v", err)
	}
	worker, err := nodemetric.NewCostWorker(
		nodemetric.CostWorkerDependencies{
			Reader: metricStore,
			Store:  costStore,
			Clock:  clock,
			Calculators: map[nodemetric.Protocol]nodemetric.ProtocolCostCalculator{
				nodemetric.ProtocolTCP: nodemetric.NewTCPCostCalculator(),
				nodemetric.ProtocolUDP: udpCalculator,
			},
			Combiner:   nodemetric.NewAverageEdgeCostCombiner(),
			Expiry:     expiry,
			Stabilizer: stabilizer,
		},
		nodemetric.CostWorkerConfig{
			Interval:       2 * time.Second,
			Window:         10 * time.Second,
			FormulaVersion: "v1",
			OnPublished: func(publication nodemetric.CostPublication, emergency bool) {
				coordinator.Trigger(routeresult.Trigger{Revision: publication.Revision, Emergency: emergency})
			},
		},
	)
	if err != nil {
		t.Fatalf("NewCostWorker() error = %v", err)
	}

	mux := http.NewServeMux()
	simulationsAPI, _ := httpapi.NewSimulations(simulationStore, 100)
	simulationsAPI.Register(mux)
	metricsAPI, _ := httpapi.NewMetrics(ingestor, metricStore, httpapi.MetricsConfig{
		DefaultQueryLimit: 20,
		MaxQueryLimit:     100,
		MaxReportBatch:    100,
	})
	metricsAPI.Register(mux)
	edgeCostsAPI, _ := httpapi.NewEdgeCosts(costStore)
	edgeCostsAPI.Register(mux)
	routesAPI, _ := httpapi.NewRoutes(resultStore, coordinator)
	routesAPI.Register(mux)

	// 这一步与测试页面的“批量保存”完全相同：为 A->B 启用固定 30ms 的 TCP 指标。
	requestBody := `{"configs":[{"from_node_id":"A","to_node_id":"B","proto":"tcp","enabled":true,"interval_ms":2000,"latency_min_ms":30,"latency_max_ms":30,"packet_loss_min_ratio":0,"packet_loss_max_ratio":0,"rate_min_bps":1000000,"rate_max_bps":1000000}]}`
	request := httptest.NewRequest(http.MethodPut, "/api/metric-simulations", strings.NewReader(requestBody))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("save simulation status = %d body=%s", response.Code, response.Body.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	coordinatorDone := make(chan error, 1)
	go func() { coordinatorDone <- coordinator.Run(ctx) }()

	// 用受控时钟推进默认 2 秒采样周期，测试无需真实休眠但保持相同业务时间语义。
	clock.Advance(2 * time.Second)
	if err := runner.RunOnce(ctx); err != nil {
		t.Fatalf("simulation RunOnce() error = %v", err)
	}
	clock.Advance(time.Millisecond)
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("cost RunOnce() error = %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for {
		latest, available := resultStore.Latest()
		if available && latest.CostRevision == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("latest route was not published: %+v available=%v", latest, available)
		}
		time.Sleep(time.Millisecond)
	}

	latestRequest := httptest.NewRequest(http.MethodGet, "/api/routes/latest", nil)
	latestResponse := httptest.NewRecorder()
	mux.ServeHTTP(latestResponse, latestRequest)
	if latestResponse.Code != http.StatusOK {
		t.Fatalf("latest route status = %d body=%s", latestResponse.Code, latestResponse.Body.String())
	}
	var latestPayload struct {
		Available    bool `json:"available"`
		CostRevision int  `json:"cost_revision"`
		Results      []struct {
			From     string `json:"from"`
			To       string `json:"to"`
			Distance int    `json:"distance"`
		} `json:"results"`
	}
	if err := json.Unmarshal(latestResponse.Body.Bytes(), &latestPayload); err != nil {
		t.Fatalf("decode latest route: %v", err)
	}
	if !latestPayload.Available || latestPayload.CostRevision != 1 {
		t.Fatalf("latest route payload = %+v", latestPayload)
	}
	if !containsE2ERoute(latestPayload.Results, "A", "B", 30) {
		t.Fatalf("latest routes = %+v, want A->B distance 30", latestPayload.Results)
	}

	cancel()
	select {
	case err := <-coordinatorDone:
		if err != nil {
			t.Fatalf("coordinator Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("coordinator did not stop after context cancellation")
	}
}

func containsE2ERoute(results []struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Distance int    `json:"distance"`
}, from, to string, distance int) bool {
	for _, result := range results {
		if result.From == from && result.To == to && result.Distance == distance {
			return true
		}
	}
	return false
}

type e2eClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *e2eClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *e2eClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

type e2eRandom float64

func (r e2eRandom) Float64() float64 { return float64(r) }

type e2eEdgeChecker struct{}

func (e2eEdgeChecker) EdgeExists(_ context.Context, key nodemetric.EdgeKey) (bool, error) {
	return key.FromNodeID == "A" && key.ToNodeID == "B", nil
}

type e2eSimulationStore struct {
	mu      sync.Mutex
	configs []nodemetric.SimulationConfig
}

func (s *e2eSimulationStore) List(context.Context) ([]nodemetric.SimulationConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]nodemetric.SimulationConfig{}, s.configs...), nil
}

func (s *e2eSimulationStore) Replace(_ context.Context, configs []nodemetric.SimulationConfig) error {
	for _, config := range configs {
		if err := nodemetric.ValidateSimulationConfig(config); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configs = append([]nodemetric.SimulationConfig{}, configs...)
	return nil
}

func (s *e2eSimulationStore) UpdateRuntime(_ context.Context, sample nodemetric.EdgeMetricSample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.configs {
		config := &s.configs[i]
		if config.EdgeKey == sample.EdgeKey && config.Protocol == sample.Protocol {
			observedAt := sample.ObservedAt
			config.LastGeneratedAt = &observedAt
			config.LastMetric = &sample
			return nil
		}
	}
	return nodemetric.ErrUnknownEdge
}

type e2eMetricStore struct {
	mu      sync.Mutex
	samples []nodemetric.EdgeMetricSample
}

func (s *e2eMetricStore) Append(_ context.Context, samples []nodemetric.EdgeMetricSample) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, samples...)
	return len(samples), nil
}

func (s *e2eMetricStore) Query(_ context.Context, query nodemetric.MetricQuery) ([]nodemetric.EdgeMetricSample, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []nodemetric.EdgeMetricSample{}
	for _, sample := range s.samples {
		if sample.EdgeKey == query.EdgeKey && sample.Protocol == query.Protocol &&
			!sample.ObservedAt.Before(query.Start) && sample.ObservedAt.Before(query.End) {
			result = append(result, sample)
		}
	}
	return result, nil
}

func (s *e2eMetricStore) Aggregate(_ context.Context, start, end time.Time) ([]nodemetric.AggregatedMetric, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var aggregate nodemetric.AggregatedMetric
	for _, sample := range s.samples {
		if sample.ObservedAt.Before(start) || !sample.ObservedAt.Before(end) {
			continue
		}
		aggregate.EdgeKey = sample.EdgeKey
		aggregate.Protocol = sample.Protocol
		aggregate.AverageLatencyMS += sample.LatencyMS
		aggregate.AveragePacketLossRate += sample.PacketLossRatio
		aggregate.AverageRateBPS += sample.RateBPS
		aggregate.SampleCount++
		aggregate.LatestObservedAt = sample.ObservedAt
	}
	if aggregate.SampleCount == 0 {
		return []nodemetric.AggregatedMetric{}, nil
	}
	count := float64(aggregate.SampleCount)
	aggregate.AverageLatencyMS /= count
	aggregate.AveragePacketLossRate /= count
	aggregate.AverageRateBPS /= count
	aggregate.WindowStart = start
	aggregate.WindowEnd = end
	return []nodemetric.AggregatedMetric{aggregate}, nil
}

type e2eCostStore struct {
	mu          sync.Mutex
	publication nodemetric.CostPublication
	edges       []nodemetric.EdgeCostSnapshot
}

func (s *e2eCostStore) Publish(
	_ context.Context,
	publication nodemetric.CostPublication,
	_ []nodemetric.ProtocolCostSnapshot,
	edges []nodemetric.EdgeCostSnapshot,
) (nodemetric.CostPublication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	publication.Revision = s.publication.Revision + 1
	s.publication = publication
	s.edges = append([]nodemetric.EdgeCostSnapshot{}, edges...)
	for i := range s.edges {
		s.edges[i].Revision = publication.Revision
	}
	return publication, nil
}

func (s *e2eCostStore) ListEdgeCosts(context.Context) ([]nodemetric.EdgeCostSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]nodemetric.EdgeCostSnapshot{}, s.edges...), nil
}

func (s *e2eCostStore) RefreshMetrics(
	_ context.Context,
	_ []nodemetric.ProtocolCostSnapshot,
	edges []nodemetric.EdgeCostSnapshot,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.edges = append([]nodemetric.EdgeCostSnapshot{}, edges...)
	return nil
}

func (s *e2eCostStore) LatestPublication(context.Context) (nodemetric.CostPublication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publication.Revision == 0 {
		return nodemetric.CostPublication{}, nodemetric.ErrNoCostSnapshot
	}
	return s.publication, nil
}

func (s *e2eCostStore) CurrentEdgeCosts(
	context.Context,
) (nodemetric.CostPublication, []nodemetric.EdgeCostSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.publication.Revision == 0 {
		return nodemetric.CostPublication{}, nil, nodemetric.ErrNoCostSnapshot
	}
	return s.publication, append([]nodemetric.EdgeCostSnapshot{}, s.edges...), nil
}

type e2eGraphProvider struct{ costs *e2eCostStore }

func (p *e2eGraphProvider) BuildGraphWithRevision(context.Context) (*graph.Graph, uint64, error) {
	publication, err := p.costs.LatestPublication(context.Background())
	if err != nil {
		return nil, 0, err
	}
	edges, err := p.costs.ListEdgeCosts(context.Background())
	if err != nil || len(edges) != 1 {
		return nil, 0, fmt.Errorf("loading e2e cost: %w", err)
	}
	input, err := graph.NewFromStruct(&graph.GraphJSON{
		Nodes: []string{"A", "B"},
		Edges: []graph.Edge{{From: "A", To: "B", Cost: edges[0].Cost, Status: 1}},
	})
	return input, publication.Revision, err
}
