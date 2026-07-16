package httpapi

import (
	"errors"
	"net/http"
	"time"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

// Simulations 处理随机指标配置的读取和批量保存。
type Simulations struct {
	store    nodemetric.SimulationConfigStore
	maxBatch int
}

type simulationDTO struct {
	FromNodeID      string              `json:"from_node_id"`
	ToNodeID        string              `json:"to_node_id"`
	Protocol        nodemetric.Protocol `json:"proto"`
	Enabled         bool                `json:"enabled"`
	IntervalMS      uint64              `json:"interval_ms"`
	LatencyMinMS    float64             `json:"latency_min_ms"`
	LatencyMaxMS    float64             `json:"latency_max_ms"`
	PacketLossMin   float64             `json:"packet_loss_min_ratio"`
	PacketLossMax   float64             `json:"packet_loss_max_ratio"`
	RateMinBPS      float64             `json:"rate_min_bps"`
	RateMaxBPS      float64             `json:"rate_max_bps"`
	LastGeneratedAt *time.Time          `json:"last_generated_at,omitempty"`
	LastMetric      *metricOutput       `json:"last_metric,omitempty"`
}

type simulationPutRequest struct {
	Configs []simulationDTO `json:"configs"`
}

// NewSimulations 创建随机指标配置 HTTP 处理器。
func NewSimulations(store nodemetric.SimulationConfigStore, maxBatch int) (*Simulations, error) {
	if store == nil {
		return nil, errors.New("metric http api: simulation store is required")
	}
	if maxBatch < 1 {
		return nil, errors.New("metric http api: simulation batch limit must be positive")
	}
	return &Simulations{store: store, maxBatch: maxBatch}, nil
}

// Register 将随机指标配置接口注册到指定 ServeMux。
func (h *Simulations) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/metric-simulations", h.Handle)
}

// Handle 根据 GET/PUT 方法读取或保存模拟配置。
func (h *Simulations) Handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.get(w, r)
	case http.MethodPut:
		h.put(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Simulations) get(w http.ResponseWriter, r *http.Request) {
	configs, err := h.store.List(r.Context())
	if err != nil {
		http.Error(w, "simulation storage unavailable", http.StatusInternalServerError)
		return
	}
	items := make([]simulationDTO, 0, len(configs))
	for _, config := range configs {
		items = append(items, simulationDTOFromDomain(config))
	}
	writeJSON(w, http.StatusOK, struct {
		Configs []simulationDTO `json:"configs"`
	}{Configs: items})
}

func (h *Simulations) put(w http.ResponseWriter, r *http.Request) {
	var request simulationPutRequest
	if err := decodeJSON(w, r, &request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(request.Configs) == 0 || len(request.Configs) > h.maxBatch {
		http.Error(w, "simulation batch size is invalid", http.StatusBadRequest)
		return
	}

	configs := make([]nodemetric.SimulationConfig, 0, len(request.Configs))
	for _, item := range request.Configs {
		configs = append(configs, simulationDTOToDomain(item))
	}
	if err := h.store.Replace(r.Context(), configs); err != nil {
		if errors.Is(err, nodemetric.ErrInvalidSimulationConfig) || errors.Is(err, nodemetric.ErrUnknownEdge) {
			http.Error(w, "invalid simulation config", http.StatusBadRequest)
			return
		}
		http.Error(w, "simulation storage unavailable", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func simulationDTOFromDomain(config nodemetric.SimulationConfig) simulationDTO {
	var lastMetric *metricOutput
	if config.LastMetric != nil {
		item := metricOutputFromDomain(*config.LastMetric)
		lastMetric = &item
	}
	return simulationDTO{
		FromNodeID:      config.FromNodeID,
		ToNodeID:        config.ToNodeID,
		Protocol:        config.Protocol,
		Enabled:         config.IsEnabled,
		IntervalMS:      uint64(config.Interval / time.Millisecond),
		LatencyMinMS:    config.LatencyMinMS,
		LatencyMaxMS:    config.LatencyMaxMS,
		PacketLossMin:   config.PacketLossMinRatio,
		PacketLossMax:   config.PacketLossMaxRatio,
		RateMinBPS:      config.RateMinBPS,
		RateMaxBPS:      config.RateMaxBPS,
		LastGeneratedAt: config.LastGeneratedAt,
		LastMetric:      lastMetric,
	}
}

func simulationDTOToDomain(item simulationDTO) nodemetric.SimulationConfig {
	return nodemetric.SimulationConfig{
		EdgeKey: nodemetric.EdgeKey{
			FromNodeID: item.FromNodeID,
			ToNodeID:   item.ToNodeID,
		},
		Protocol:           item.Protocol,
		IsEnabled:          item.Enabled,
		Interval:           time.Duration(item.IntervalMS) * time.Millisecond,
		LatencyMinMS:       item.LatencyMinMS,
		LatencyMaxMS:       item.LatencyMaxMS,
		PacketLossMinRatio: item.PacketLossMin,
		PacketLossMaxRatio: item.PacketLossMax,
		RateMinBPS:         item.RateMinBPS,
		RateMaxBPS:         item.RateMaxBPS,
	}
}
