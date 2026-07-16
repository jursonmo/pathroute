package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

type simulationConfigStoreStub struct {
	configs  []nodemetric.SimulationConfig
	replaced []nodemetric.SimulationConfig
	err      error
}

func (s *simulationConfigStoreStub) List(context.Context) ([]nodemetric.SimulationConfig, error) {
	return append([]nodemetric.SimulationConfig{}, s.configs...), s.err
}

func (s *simulationConfigStoreStub) Replace(_ context.Context, configs []nodemetric.SimulationConfig) error {
	s.replaced = append([]nodemetric.SimulationConfig{}, configs...)
	return s.err
}

func TestSimulationsGetReturnsSavedRuntimeState(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	store := &simulationConfigStoreStub{configs: []nodemetric.SimulationConfig{{
		EdgeKey:         nodemetric.EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		Protocol:        nodemetric.ProtocolTCP,
		IsEnabled:       true,
		Interval:        2 * time.Second,
		LatencyMinMS:    10,
		LatencyMaxMS:    20,
		LastGeneratedAt: &now,
	}}}
	handler, err := NewSimulations(store, 100)
	if err != nil {
		t.Fatalf("NewSimulations() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/metric-simulations", nil)
	response := httptest.NewRecorder()

	handler.Handle(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, `"interval_ms":2000`) || !strings.Contains(body, `"last_generated_at":"2026-07-15T12:00:00Z"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestSimulationsPutMapsAndSavesConfigs(t *testing.T) {
	t.Parallel()

	store := &simulationConfigStoreStub{}
	handler, err := NewSimulations(store, 100)
	if err != nil {
		t.Fatalf("NewSimulations() error = %v", err)
	}
	body := `{"configs":[{"from_node_id":"A","to_node_id":"B","proto":"udp","enabled":true,"interval_ms":2000,"latency_min_ms":10,"latency_max_ms":20,"packet_loss_min_ratio":0,"packet_loss_max_ratio":0.1,"rate_min_bps":100,"rate_max_bps":200}]}`
	request := httptest.NewRequest(http.MethodPut, "/api/metric-simulations", strings.NewReader(body))
	response := httptest.NewRecorder()

	handler.Handle(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", response.Code, response.Body.String())
	}
	if len(store.replaced) != 1 {
		t.Fatalf("replaced configs = %d, want 1", len(store.replaced))
	}
	config := store.replaced[0]
	if config.Protocol != nodemetric.ProtocolUDP || config.Interval != 2*time.Second || config.PacketLossMaxRatio != 0.1 {
		t.Fatalf("mapped config = %+v", config)
	}
}

func TestSimulationsPutMapsValidationError(t *testing.T) {
	t.Parallel()

	store := &simulationConfigStoreStub{err: nodemetric.ErrInvalidSimulationConfig}
	handler, err := NewSimulations(store, 100)
	if err != nil {
		t.Fatalf("NewSimulations() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/metric-simulations", strings.NewReader(`{"configs":[{}]}`))
	response := httptest.NewRecorder()

	handler.Handle(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func TestSimulationsGetDoesNotLeakStorageError(t *testing.T) {
	t.Parallel()

	store := &simulationConfigStoreStub{err: errors.New("secret mysql error")}
	handler, err := NewSimulations(store, 100)
	if err != nil {
		t.Fatalf("NewSimulations() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/metric-simulations", nil)
	response := httptest.NewRecorder()

	handler.Handle(response, request)

	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("unexpected error response: status=%d body=%s", response.Code, response.Body.String())
	}
}
