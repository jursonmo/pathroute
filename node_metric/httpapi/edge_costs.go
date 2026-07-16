package httpapi

import (
	"context"
	"errors"
	"net/http"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

type edgeCostReader interface {
	CurrentEdgeCosts(
		ctx context.Context,
	) (nodemetric.CostPublication, []nodemetric.EdgeCostSnapshot, error)
}

// EdgeCosts 返回当前已原子发布的边 cost 和协议明细。
type EdgeCosts struct {
	reader edgeCostReader
}

// NewEdgeCosts 创建当前动态 cost 查询处理器。
func NewEdgeCosts(reader edgeCostReader) (*EdgeCosts, error) {
	if reader == nil {
		return nil, errors.New("metric http api: edge cost reader is required")
	}
	return &EdgeCosts{reader: reader}, nil
}

// Register 将当前动态 cost 接口注册到指定 ServeMux。
func (h *EdgeCosts) Register(mux *http.ServeMux) {
	mux.HandleFunc("/api/edge-costs", h.Handle)
}

// Handle 返回同一个 publication revision 下的当前边快照。
func (h *EdgeCosts) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	publication, edges, err := h.reader.CurrentEdgeCosts(r.Context())
	if errors.Is(err, nodemetric.ErrNoCostSnapshot) {
		writeJSON(w, http.StatusOK, edgeCostsResponse{
			Edges: []nodemetric.EdgeCostSnapshot{},
		})
		return
	}
	if err != nil {
		http.Error(w, "cost storage unavailable", http.StatusInternalServerError)
		return
	}
	if edges == nil {
		edges = []nodemetric.EdgeCostSnapshot{}
	}
	writeJSON(w, http.StatusOK, edgeCostsResponse{
		Revision:    publication.Revision,
		Publication: &publication,
		Edges:       edges,
	})
}

type edgeCostsResponse struct {
	Revision    uint64                        `json:"revision"`
	Publication *nodemetric.CostPublication   `json:"publication,omitempty"`
	Edges       []nodemetric.EdgeCostSnapshot `json:"edges"`
}
