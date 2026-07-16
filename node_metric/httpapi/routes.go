package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/jursonmo/pathroute/floyd"
	routeresult "github.com/jursonmo/pathroute/node_metric/route"
)

type routeReader interface {
	Latest() (routeresult.Snapshot, bool)
}

type manualRouteCalculator interface {
	CalculateNow(ctx context.Context) (routeresult.Snapshot, error)
}

// Routes 提供最新自动路由查询和手动强制计算入口。
type Routes struct {
	reader     routeReader
	calculator manualRouteCalculator
}

// NewRoutes 创建路由结果 HTTP 处理器。
func NewRoutes(reader routeReader, calculator manualRouteCalculator) (*Routes, error) {
	if reader == nil || calculator == nil {
		return nil, errors.New("metric http api: route reader and calculator are required")
	}
	return &Routes{reader: reader, calculator: calculator}, nil
}

// Register 注册最新结果和兼容的手动计算接口。
func (h *Routes) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/routes/latest", h.Latest)
	mux.HandleFunc("/calculate", h.Calculate)
}

// Latest 返回最近一次完整成功的自动路由结果。
func (h *Routes) Latest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snapshot, available := h.reader.Latest()
	if snapshot.Results == nil {
		snapshot.Results = []floyd.PairResult{}
	}
	if snapshot.LastError != "" {
		// 完整错误只保留在服务端日志和内存状态中，HTTP 不泄露 SQL、地址等内部细节。
		snapshot.LastError = "route calculation failed"
	}
	writeJSON(w, http.StatusOK, latestRoutesResponse{
		Available: available,
		Snapshot:  snapshot,
	})
}

// Calculate 绕过自动最小间隔执行一次手动强制计算。
func (h *Routes) Calculate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snapshot, err := h.calculator.CalculateNow(r.Context())
	if err != nil {
		http.Error(w, "route calculation failed", http.StatusInternalServerError)
		return
	}
	if snapshot.Results == nil {
		snapshot.Results = []floyd.PairResult{}
	}
	writeJSON(w, http.StatusOK, snapshot)
}

type latestRoutesResponse struct {
	Available bool `json:"available"`
	routeresult.Snapshot
}
