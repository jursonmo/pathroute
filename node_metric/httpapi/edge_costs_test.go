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

type costReaderStub struct {
	publication nodemetric.CostPublication
	edges       []nodemetric.EdgeCostSnapshot
	err         error
	readCalls   int
}

func (s *costReaderStub) CurrentEdgeCosts(
	context.Context,
) (nodemetric.CostPublication, []nodemetric.EdgeCostSnapshot, error) {
	s.readCalls++
	return s.publication, s.edges, s.err
}

func TestEdgeCostsReturnsCurrentRevisionAndProtocolDetails(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	reader := &costReaderStub{
		publication: nodemetric.CostPublication{Revision: 7, FormulaVersion: "v1", PublishedAt: now},
		edges: []nodemetric.EdgeCostSnapshot{{
			EdgeKey:        nodemetric.EdgeKey{FromNodeID: "A", ToNodeID: "B"},
			Cost:           50,
			FormulaVersion: "v1",
			Revision:       7,
			ProtocolCosts: []nodemetric.ProtocolCostSnapshot{
				{AggregatedMetric: nodemetric.AggregatedMetric{Protocol: nodemetric.ProtocolTCP}, Cost: 30, Revision: 7},
				{AggregatedMetric: nodemetric.AggregatedMetric{Protocol: nodemetric.ProtocolUDP}, Cost: 70, Revision: 7},
			},
		}},
	}
	handler, err := NewEdgeCosts(reader)
	if err != nil {
		t.Fatalf("NewEdgeCosts() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/edge-costs", nil)
	response := httptest.NewRecorder()

	handler.Handle(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, fragment := range []string{`"revision":7`, `"cost":50`, `"proto":"tcp"`, `"proto":"udp"`} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("response missing %s: %s", fragment, body)
		}
	}
	if reader.readCalls != 1 {
		t.Fatalf("CurrentEdgeCosts() calls = %d, want 1", reader.readCalls)
	}
}

func TestEdgeCostsReturnsEmptyBeforeFirstPublication(t *testing.T) {
	t.Parallel()

	handler, err := NewEdgeCosts(&costReaderStub{err: nodemetric.ErrNoCostSnapshot})
	if err != nil {
		t.Fatalf("NewEdgeCosts() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/edge-costs", nil)
	response := httptest.NewRecorder()

	handler.Handle(response, request)

	if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != `{"revision":0,"edges":[]}` {
		t.Fatalf("unexpected response: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestEdgeCostsDoesNotLeakStorageErrors(t *testing.T) {
	t.Parallel()

	handler, err := NewEdgeCosts(&costReaderStub{err: errors.New("secret mysql error")})
	if err != nil {
		t.Fatalf("NewEdgeCosts() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/edge-costs", nil)
	response := httptest.NewRecorder()

	handler.Handle(response, request)

	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("unexpected error response: status=%d body=%s", response.Code, response.Body.String())
	}
}
