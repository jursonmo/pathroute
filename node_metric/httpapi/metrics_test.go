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

type ingestorStub struct {
	samples []nodemetric.EdgeMetricSample
	count   int
	err     error
}

func (s *ingestorStub) Ingest(_ context.Context, samples []nodemetric.EdgeMetricSample) (int, error) {
	s.samples = append([]nodemetric.EdgeMetricSample{}, samples...)
	return s.count, s.err
}

type metricReaderStub struct {
	query   nodemetric.MetricQuery
	samples []nodemetric.EdgeMetricSample
	err     error
}

func (s *metricReaderStub) Query(_ context.Context, query nodemetric.MetricQuery) ([]nodemetric.EdgeMetricSample, error) {
	s.query = query
	return s.samples, s.err
}

func (s *metricReaderStub) Aggregate(context.Context, time.Time, time.Time) ([]nodemetric.AggregatedMetric, error) {
	return nil, errors.New("not implemented in handler stub")
}

func TestMetricsReportAcceptsBatch(t *testing.T) {
	t.Parallel()

	ingestor := &ingestorStub{count: 1}
	handler, err := NewMetrics(ingestor, &metricReaderStub{}, MetricsConfig{
		DefaultQueryLimit: 20,
		MaxQueryLimit:     100,
		MaxReportBatch:    10,
	})
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	body := `{"samples":[{"from_node_id":"A","to_node_id":"B","proto":"udp","latency_ms":30,"packet_loss_ratio":0.05,"rate_bps":1000000,"observed_at":"2026-07-15T12:00:00Z","sample_window_ms":2000,"source":"agent","source_id":"agent-a","sequence":7}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/metrics/report", strings.NewReader(body))
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if strings.TrimSpace(response.Body.String()) != `{"accepted":1}` {
		t.Fatalf("body = %q, want accepted count", response.Body.String())
	}
	if len(ingestor.samples) != 1 {
		t.Fatalf("ingestor samples = %d, want 1", len(ingestor.samples))
	}
	sample := ingestor.samples[0]
	if sample.Protocol != nodemetric.ProtocolUDP || sample.SampleWindow != 2*time.Second || sample.Sequence != 7 {
		t.Fatalf("mapped sample = %+v", sample)
	}
	if !sample.ReceivedAt.IsZero() {
		t.Fatalf("handler must leave trusted ReceivedAt to ingest service, got %s", sample.ReceivedAt)
	}
}

func TestMetricsReportMapsInputAndStorageErrors(t *testing.T) {
	tests := []struct {
		name      string
		ingestErr error
		expected  int
	}{
		{name: "invalid metric", ingestErr: nodemetric.ErrInvalidMetric, expected: http.StatusBadRequest},
		{name: "unknown edge", ingestErr: nodemetric.ErrUnknownEdge, expected: http.StatusBadRequest},
		{name: "storage failure", ingestErr: errors.New("secret database error"), expected: http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler, err := NewMetrics(&ingestorStub{err: tt.ingestErr}, &metricReaderStub{}, MetricsConfig{
				DefaultQueryLimit: 20,
				MaxQueryLimit:     100,
				MaxReportBatch:    10,
			})
			if err != nil {
				t.Fatalf("NewMetrics() error = %v", err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/metrics/report", strings.NewReader(validReportBody()))
			response := httptest.NewRecorder()

			handler.Report(response, request)

			if response.Code != tt.expected {
				t.Fatalf("status = %d, want %d", response.Code, tt.expected)
			}
			if tt.expected == http.StatusInternalServerError && strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("internal error leaked to response: %s", response.Body.String())
			}
		})
	}
}

func TestMetricsQueryBuildsBoundedTimeRange(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	reader := &metricReaderStub{samples: []nodemetric.EdgeMetricSample{}}
	handler, err := NewMetrics(&ingestorStub{}, reader, MetricsConfig{
		DefaultQueryLimit: 20,
		MaxQueryLimit:     100,
		MaxReportBatch:    10,
	})
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}
	url := "/api/metrics?from=A&to=B&proto=tcp&start=" + start.Format(time.RFC3339) +
		"&end=" + start.Add(time.Minute).Format(time.RFC3339) + "&limit=50&offset=5"
	request := httptest.NewRequest(http.MethodGet, url, nil)
	response := httptest.NewRecorder()

	handler.Query(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if reader.query.FromNodeID != "A" || reader.query.ToNodeID != "B" || reader.query.Protocol != nodemetric.ProtocolTCP {
		t.Fatalf("query dimensions = %+v", reader.query)
	}
	if reader.query.Limit != 50 || reader.query.Offset != 5 {
		t.Fatalf("query pagination = %d/%d, want 50/5", reader.query.Limit, reader.query.Offset)
	}
	if strings.TrimSpace(response.Body.String()) != `{"samples":[]}` {
		t.Fatalf("body = %q, want initialized empty samples", response.Body.String())
	}
}

func TestMetricsQuerySerializesSampleWindowInMilliseconds(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	reader := &metricReaderStub{samples: []nodemetric.EdgeMetricSample{{
		EdgeKey:      nodemetric.EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		Protocol:     nodemetric.ProtocolTCP,
		ObservedAt:   start.Add(time.Second),
		ReceivedAt:   start.Add(time.Second),
		SampleWindow: 2 * time.Second,
		Source:       "test",
		SourceID:     "test-a",
		Sequence:     1,
	}}}
	handler, err := NewMetrics(&ingestorStub{}, reader, MetricsConfig{
		DefaultQueryLimit: 20,
		MaxQueryLimit:     100,
		MaxReportBatch:    10,
	})
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}
	url := "/api/metrics?from=A&to=B&proto=tcp&start=" + start.Format(time.RFC3339) +
		"&end=" + start.Add(time.Minute).Format(time.RFC3339)
	response := httptest.NewRecorder()
	handler.Query(response, httptest.NewRequest(http.MethodGet, url, nil))

	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"sample_window_ms":2000`) {
		t.Fatalf("unexpected metric response: status=%d body=%s", response.Code, body)
	}
	if strings.Contains(body, `"sample_window":`) {
		t.Fatalf("response leaked Go duration nanoseconds: %s", body)
	}
}

func TestMetricsQueryRejectsInvalidParameters(t *testing.T) {
	t.Parallel()

	handler, err := NewMetrics(&ingestorStub{}, &metricReaderStub{}, MetricsConfig{
		DefaultQueryLimit: 20,
		MaxQueryLimit:     100,
		MaxReportBatch:    10,
	})
	if err != nil {
		t.Fatalf("NewMetrics() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/metrics?from=A&to=B&proto=icmp&start=bad&end=bad&limit=1000",
		nil,
	)
	response := httptest.NewRecorder()

	handler.Query(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
}

func validReportBody() string {
	return `{"samples":[{"from_node_id":"A","to_node_id":"B","proto":"tcp","latency_ms":20,"packet_loss_ratio":0,"rate_bps":1000,"observed_at":"2026-07-15T12:00:00Z","sample_window_ms":2000,"source":"test","source_id":"test-a","sequence":1}]}`
}
