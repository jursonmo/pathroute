package route

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jursonmo/pathroute/floyd"
	"github.com/jursonmo/pathroute/graph"
)

type routeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *routeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *routeClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

type graphProviderStub struct {
	graph    *graph.Graph
	revision uint64
	err      error
	calls    int
}

func (s *graphProviderStub) BuildGraphWithRevision(context.Context) (*graph.Graph, uint64, error) {
	s.calls++
	return s.graph, s.revision, s.err
}

type routeCalculatorStub struct {
	results []floyd.PairResult
	err     error
}

func (s *routeCalculatorStub) Calculate(*graph.Graph) ([]floyd.PairResult, error) {
	return append([]floyd.PairResult{}, s.results...), s.err
}

func TestCoordinatorPublishesRevisionConsistentResult(t *testing.T) {
	t.Parallel()

	clock := &routeClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	provider := &graphProviderStub{graph: testGraph(t), revision: 7}
	calculator := &routeCalculatorStub{results: []floyd.PairResult{{From: "A", To: "B", Distance: 42}}}
	store := NewResultStore()
	coordinator := newTestCoordinator(t, provider, calculator, store, clock)

	executed, err := coordinator.Recalculate(context.Background(), Trigger{Revision: 7})
	if err != nil {
		t.Fatalf("Recalculate() error = %v", err)
	}
	if !executed {
		t.Fatal("first recalculation was not executed")
	}
	snapshot, available := store.Latest()
	if !available || snapshot.CostRevision != 7 || snapshot.RouteVersion != 1 {
		t.Fatalf("latest snapshot = %+v available=%v", snapshot, available)
	}
	if len(snapshot.Results) != 1 || snapshot.Results[0].Distance != 42 {
		t.Fatalf("latest results = %+v", snapshot.Results)
	}
}

func TestCoordinatorMinimumIntervalAndEmergencyBypass(t *testing.T) {
	t.Parallel()

	clock := &routeClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	provider := &graphProviderStub{graph: testGraph(t), revision: 1}
	coordinator := newTestCoordinator(t, provider, &routeCalculatorStub{}, NewResultStore(), clock)

	if executed, err := coordinator.Recalculate(context.Background(), Trigger{Revision: 1}); err != nil || !executed {
		t.Fatalf("first Recalculate() = %v/%v, want true/nil", executed, err)
	}
	provider.revision = 2
	if executed, err := coordinator.Recalculate(context.Background(), Trigger{Revision: 2}); err != nil || executed {
		t.Fatalf("ordinary Recalculate() inside interval = %v/%v, want false/nil", executed, err)
	}
	if executed, err := coordinator.Recalculate(context.Background(), Trigger{Revision: 2, Emergency: true}); err != nil || !executed {
		t.Fatalf("emergency Recalculate() = %v/%v, want true/nil", executed, err)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
}

func TestCoordinatorFailureKeepsPreviousCompleteResult(t *testing.T) {
	t.Parallel()

	clock := &routeClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	provider := &graphProviderStub{graph: testGraph(t), revision: 1}
	calculator := &routeCalculatorStub{results: []floyd.PairResult{{From: "A", To: "B", Distance: 10}}}
	store := NewResultStore()
	coordinator := newTestCoordinator(t, provider, calculator, store, clock)
	if _, err := coordinator.Recalculate(context.Background(), Trigger{Revision: 1, Emergency: true}); err != nil {
		t.Fatalf("initial Recalculate() error = %v", err)
	}

	provider.revision = 2
	calculator.err = errors.New("calculation failed")
	if _, err := coordinator.Recalculate(context.Background(), Trigger{Revision: 2, Emergency: true}); err == nil {
		t.Fatal("failed recalculation error = nil")
	}
	snapshot, available := store.Latest()
	if !available || snapshot.CostRevision != 1 || snapshot.RouteVersion != 1 || !snapshot.IsStale {
		t.Fatalf("snapshot after failure = %+v available=%v", snapshot, available)
	}
	if len(snapshot.Results) != 1 || snapshot.Results[0].Distance != 10 || snapshot.LastError == "" {
		t.Fatalf("previous result was not retained: %+v", snapshot)
	}
}

func TestCoordinatorTriggerCoalescesHighestRevisionAndEmergency(t *testing.T) {
	t.Parallel()

	clock := &routeClock{now: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)}
	coordinator := newTestCoordinator(
		t,
		&graphProviderStub{graph: testGraph(t), revision: 3},
		&routeCalculatorStub{},
		NewResultStore(),
		clock,
	)
	coordinator.Trigger(Trigger{Revision: 2})
	coordinator.Trigger(Trigger{Revision: 3, Emergency: true})

	pending, ok := coordinator.takePending()
	if !ok || pending.Revision != 3 || !pending.Emergency {
		t.Fatalf("coalesced trigger = %+v ok=%v", pending, ok)
	}
}

func newTestCoordinator(
	t *testing.T,
	provider GraphProvider,
	calculator Calculator,
	store *ResultStore,
	clock Clock,
) *Coordinator {
	t.Helper()
	coordinator, err := NewCoordinator(provider, calculator, store, Config{
		Clock:       clock,
		MinInterval: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	return coordinator
}

func testGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g, err := graph.NewFromStruct(&graph.GraphJSON{
		Nodes: []string{"A", "B"},
		Edges: []graph.Edge{{From: "A", To: "B", Cost: 42, Status: 1}},
	})
	if err != nil {
		t.Fatalf("graph.NewFromStruct() error = %v", err)
	}
	return g
}
