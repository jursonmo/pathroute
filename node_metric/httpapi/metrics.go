package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

const maxMetricRequestBytes = 1 << 20

// MetricsConfig 限制历史查询和单次上报占用的资源。
type MetricsConfig struct {
	DefaultQueryLimit int
	MaxQueryLimit     int
	MaxReportBatch    int
}

// Metrics 处理指标上报和历史查询。
type Metrics struct {
	ingestor nodemetric.MetricIngestor
	reader   nodemetric.MetricReader
	config   MetricsConfig
}

type metricInput struct {
	FromNodeID      string              `json:"from_node_id"`
	ToNodeID        string              `json:"to_node_id"`
	Protocol        nodemetric.Protocol `json:"proto"`
	LatencyMS       float64             `json:"latency_ms"`
	PacketLossRatio float64             `json:"packet_loss_ratio"`
	RateBPS         float64             `json:"rate_bps"`
	ObservedAt      time.Time           `json:"observed_at"`
	SampleWindowMS  uint64              `json:"sample_window_ms"`
	Source          string              `json:"source"`
	SourceID        string              `json:"source_id"`
	Sequence        uint64              `json:"sequence"`
}

type metricOutput struct {
	FromNodeID      string              `json:"from_node_id"`
	ToNodeID        string              `json:"to_node_id"`
	Protocol        nodemetric.Protocol `json:"proto"`
	LatencyMS       float64             `json:"latency_ms"`
	PacketLossRatio float64             `json:"packet_loss_ratio"`
	RateBPS         float64             `json:"rate_bps"`
	ObservedAt      time.Time           `json:"observed_at"`
	ReceivedAt      time.Time           `json:"received_at"`
	SampleWindowMS  uint64              `json:"sample_window_ms"`
	Source          string              `json:"source"`
	SourceID        string              `json:"source_id"`
	Sequence        uint64              `json:"sequence"`
}

type reportRequest struct {
	Samples []metricInput `json:"samples"`
}

// NewMetrics 创建指标 HTTP 处理器。
func NewMetrics(
	ingestor nodemetric.MetricIngestor,
	reader nodemetric.MetricReader,
	config MetricsConfig,
) (*Metrics, error) {
	if ingestor == nil || reader == nil {
		return nil, errors.New("metric http api: ingestor and reader are required")
	}
	if config.DefaultQueryLimit < 1 || config.MaxQueryLimit < config.DefaultQueryLimit {
		return nil, errors.New("metric http api: invalid query limits")
	}
	if config.MaxReportBatch < 1 {
		return nil, errors.New("metric http api: maximum report batch must be positive")
	}
	return &Metrics{ingestor: ingestor, reader: reader, config: config}, nil
}

// Register 将指标接口注册到指定 ServeMux。
func (h *Metrics) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/metrics/report", h.Report)
	mux.HandleFunc("/api/metrics", h.Query)
}

// Report 接收一批原始指标，业务校验和可信接收时间由 IngestService 统一处理。
func (h *Metrics) Report(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var request reportRequest
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(request.Samples) == 0 || len(request.Samples) > h.config.MaxReportBatch {
		http.Error(w, "samples batch size is invalid", http.StatusBadRequest)
		return
	}

	samples := make([]nodemetric.EdgeMetricSample, 0, len(request.Samples))
	for _, input := range request.Samples {
		samples = append(samples, nodemetric.EdgeMetricSample{
			EdgeKey: nodemetric.EdgeKey{
				FromNodeID: input.FromNodeID,
				ToNodeID:   input.ToNodeID,
			},
			Protocol:        input.Protocol,
			LatencyMS:       input.LatencyMS,
			PacketLossRatio: input.PacketLossRatio,
			RateBPS:         input.RateBPS,
			ObservedAt:      input.ObservedAt,
			SampleWindow:    time.Duration(input.SampleWindowMS) * time.Millisecond,
			Source:          input.Source,
			SourceID:        input.SourceID,
			Sequence:        input.Sequence,
		})
	}

	accepted, err := h.ingestor.Ingest(r.Context(), samples)
	if err != nil {
		if errors.Is(err, nodemetric.ErrInvalidMetric) || errors.Is(err, nodemetric.ErrUnknownEdge) {
			http.Error(w, "invalid metric sample", http.StatusBadRequest)
			return
		}
		// 数据库错误只在服务边界记录，响应不暴露 DSN、SQL 或内部表结构。
		http.Error(w, "metric storage unavailable", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Accepted int `json:"accepted"`
	}{Accepted: accepted})
}

// Query 查询一条有向边、一个协议在指定时间段内的原始指标。
func (h *Metrics) Query(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query, err := h.parseMetricQuery(r)
	if err != nil {
		http.Error(w, "invalid metric query", http.StatusBadRequest)
		return
	}
	samples, err := h.reader.Query(r.Context(), query)
	if err != nil {
		http.Error(w, "metric storage unavailable", http.StatusInternalServerError)
		return
	}
	items := make([]metricOutput, 0, len(samples))
	for _, sample := range samples {
		items = append(items, metricOutputFromDomain(sample))
	}
	writeJSON(w, http.StatusOK, struct {
		Samples []metricOutput `json:"samples"`
	}{Samples: items})
}

func metricOutputFromDomain(sample nodemetric.EdgeMetricSample) metricOutput {
	return metricOutput{
		FromNodeID:      sample.FromNodeID,
		ToNodeID:        sample.ToNodeID,
		Protocol:        sample.Protocol,
		LatencyMS:       sample.LatencyMS,
		PacketLossRatio: sample.PacketLossRatio,
		RateBPS:         sample.RateBPS,
		ObservedAt:      sample.ObservedAt,
		ReceivedAt:      sample.ReceivedAt,
		SampleWindowMS:  uint64(sample.SampleWindow / time.Millisecond),
		Source:          sample.Source,
		SourceID:        sample.SourceID,
		Sequence:        sample.Sequence,
	}
}

func (h *Metrics) parseMetricQuery(r *http.Request) (nodemetric.MetricQuery, error) {
	values := r.URL.Query()
	fromNodeID := strings.TrimSpace(values.Get("from"))
	toNodeID := strings.TrimSpace(values.Get("to"))
	protocol := nodemetric.Protocol(strings.ToLower(strings.TrimSpace(values.Get("proto"))))
	if fromNodeID == "" || toNodeID == "" || !protocol.IsValid() {
		return nodemetric.MetricQuery{}, errors.New("missing metric dimensions")
	}

	start, err := time.Parse(time.RFC3339Nano, values.Get("start"))
	if err != nil {
		return nodemetric.MetricQuery{}, errors.New("invalid start time")
	}
	end, err := time.Parse(time.RFC3339Nano, values.Get("end"))
	if err != nil || !end.After(start) {
		return nodemetric.MetricQuery{}, errors.New("invalid end time")
	}

	limit, err := parseBoundedInt(values.Get("limit"), h.config.DefaultQueryLimit, 1, h.config.MaxQueryLimit)
	if err != nil {
		return nodemetric.MetricQuery{}, err
	}
	offset, err := parseBoundedInt(values.Get("offset"), 0, 0, int(^uint(0)>>1))
	if err != nil {
		return nodemetric.MetricQuery{}, err
	}
	return nodemetric.MetricQuery{
		EdgeKey:  nodemetric.EdgeKey{FromNodeID: fromNodeID, ToNodeID: toNodeID},
		Protocol: protocol,
		Start:    start.UTC(),
		End:      end.UTC(),
		Offset:   offset,
		Limit:    limit,
	}, nil
}

func parseBoundedInt(raw string, defaultValue, minimum, maximum int) (int, error) {
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, errors.New("integer parameter is out of range")
	}
	return value, nil
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxMetricRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one json value")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}
