package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jursonmo/pathroute/floyd"
	routeresult "github.com/jursonmo/pathroute/node_metric/route"
)

type routeReaderStub struct {
	snapshot  routeresult.Snapshot
	available bool
}

func (s *routeReaderStub) Latest() (routeresult.Snapshot, bool) {
	return s.snapshot, s.available
}

type manualRouteCalculatorStub struct {
	snapshot routeresult.Snapshot
	err      error
	calls    int
}

func (s *manualRouteCalculatorStub) CalculateNow(context.Context) (routeresult.Snapshot, error) {
	s.calls++
	return s.snapshot, s.err
}

func TestRoutesLatestReturnsRevisionMetadata(t *testing.T) {
	t.Parallel()

	reader := &routeReaderStub{
		available: true,
		snapshot: routeresult.Snapshot{
			Results:      []floyd.PairResult{{From: "A", To: "B", Distance: 42}},
			CostRevision: 7,
			RouteVersion: 3,
			CalculatedAt: time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC),
		},
	}
	handler, err := NewRoutes(reader, &manualRouteCalculatorStub{})
	if err != nil {
		t.Fatalf("NewRoutes() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/routes/latest", nil)
	response := httptest.NewRecorder()

	handler.Latest(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	for _, fragment := range []string{`"available":true`, `"cost_revision":7`, `"route_version":3`, `"distance":42`} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("response missing %s: %s", fragment, body)
		}
	}
}

func TestRoutesLatestReturnsEmptyBeforeFirstCalculation(t *testing.T) {
	t.Parallel()

	handler, err := NewRoutes(&routeReaderStub{}, &manualRouteCalculatorStub{})
	if err != nil {
		t.Fatalf("NewRoutes() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/routes/latest", nil)
	response := httptest.NewRecorder()

	handler.Latest(response, request)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"available":false`) || !strings.Contains(response.Body.String(), `"results":[]`) {
		t.Fatalf("unexpected response: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRoutesLatestDoesNotLeakInternalFailureDetails(t *testing.T) {
	t.Parallel()

	handler, err := NewRoutes(&routeReaderStub{
		available: true,
		snapshot: routeresult.Snapshot{
			Results:   []floyd.PairResult{},
			IsStale:   true,
			LastError: "mysql connection failed with secret dsn",
		},
	}, &manualRouteCalculatorStub{})
	if err != nil {
		t.Fatalf("NewRoutes() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.Latest(response, httptest.NewRequest(http.MethodGet, "/api/routes/latest", nil))

	body := response.Body.String()
	if response.Code != http.StatusOK || strings.Contains(body, "secret") ||
		!strings.Contains(body, `"last_error":"route calculation failed"`) {
		t.Fatalf("unexpected latest failure response: status=%d body=%s", response.Code, body)
	}
}

func TestRoutesCalculateForcesManualCalculation(t *testing.T) {
	t.Parallel()

	calculator := &manualRouteCalculatorStub{snapshot: routeresult.Snapshot{
		Results:      []floyd.PairResult{{From: "A", To: "B", Distance: 50}},
		CostRevision: 8,
		RouteVersion: 4,
	}}
	handler, err := NewRoutes(&routeReaderStub{}, calculator)
	if err != nil {
		t.Fatalf("NewRoutes() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/calculate", nil)
	response := httptest.NewRecorder()

	handler.Calculate(response, request)

	if response.Code != http.StatusOK || calculator.calls != 1 || !strings.Contains(response.Body.String(), `"cost_revision":8`) {
		t.Fatalf("unexpected calculate response: status=%d calls=%d body=%s", response.Code, calculator.calls, response.Body.String())
	}
}

func TestRoutesCalculateDoesNotReplaceResponseOnFailure(t *testing.T) {
	t.Parallel()

	calculator := &manualRouteCalculatorStub{err: errors.New("secret calculation error")}
	handler, err := NewRoutes(&routeReaderStub{}, calculator)
	if err != nil {
		t.Fatalf("NewRoutes() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/calculate", nil)
	response := httptest.NewRecorder()

	handler.Calculate(response, request)

	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("unexpected error response: status=%d body=%s", response.Code, response.Body.String())
	}
}
