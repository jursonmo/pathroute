package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"

	"github.com/jursonmo/pathroute/internal/viewdb"
	routeresult "github.com/jursonmo/pathroute/node_metric/route"
)

type routeTrigger interface {
	Trigger(trigger routeresult.Trigger)
}

// registerTopologyHandlers 注册原有拓扑编辑接口，并把会影响路由的修改通知协调器。
func registerTopologyHandlers(mux *http.ServeMux, store *viewdb.Store, trigger routeTrigger) error {
	mux.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
		data, err := store.GetGraph(r.Context())
		if err != nil {
			http.Error(w, "load graph: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	})

	mux.HandleFunc("/add-node", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			NodeID string   `json:"nodeId"`
			X      *float64 `json:"x"`
			Y      *float64 `json:"y"`
			Des    string   `json:"des"`
			Type   *int     `json:"type"`
			Status *int     `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NodeID == "" {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		node := viewdb.NodeDTO{NodeID: body.NodeID, Des: body.Des}
		if body.X != nil {
			node.X = *body.X
		}
		if body.Y != nil {
			node.Y = *body.Y
		}
		if body.Type != nil {
			node.Type = *body.Type
		}
		if body.Status != nil {
			node.Status = *body.Status
		}
		if err := store.AddNode(r.Context(), node); err != nil {
			writeTopologyError(w, err, "node already exists", "node not found")
			return
		}
		trigger.Trigger(routeresult.Trigger{Emergency: true})
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/save-position", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			NodeID string  `json:"nodeId"`
			X      float64 `json:"x"`
			Y      float64 `json:"y"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NodeID == "" {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if err := store.SavePosition(r.Context(), body.NodeID, body.X, body.Y); err != nil {
			writeTopologyError(w, err, "already exists", "node not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/update-node", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			NodeID string `json:"nodeId"`
			Des    string `json:"des"`
			Type   *int   `json:"type"`
			Status *int   `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.NodeID == "" {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if err := store.UpdateNode(r.Context(), body.NodeID, body.Des, body.Type, body.Status); err != nil {
			writeTopologyError(w, err, "already exists", "node not found")
			return
		}
		if body.Status != nil {
			// 节点可用性变化与边 status 一样会改变路由图，必须绕过普通最小重算间隔。
			trigger.Trigger(routeresult.Trigger{Emergency: true})
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/add-edge", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			From   string `json:"from"`
			To     string `json:"to"`
			Cost   int    `json:"cost"`
			Des    string `json:"des"`
			Type   *int   `json:"type"`
			Status *int   `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		edge := viewdb.EdgeDTO{From: body.From, To: body.To, Cost: body.Cost, Des: body.Des}
		if body.Type != nil {
			edge.Type = *body.Type
		}
		if body.Status != nil {
			edge.Status = *body.Status
		}
		if err := store.AddEdge(r.Context(), edge); err != nil {
			writeTopologyError(w, err, "edge already exists", "from/to node not found")
			return
		}
		trigger.Trigger(routeresult.Trigger{Emergency: true})
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/update-edge", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			From   string `json:"from"`
			To     string `json:"to"`
			Cost   int    `json:"cost"`
			Des    string `json:"des"`
			Type   *int   `json:"type"`
			Status *int   `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if err := store.UpdateEdge(r.Context(), body.From, body.To, body.Cost, body.Des, body.Type, body.Status); err != nil {
			writeTopologyError(w, err, "edge already exists", "edge not found")
			return
		}
		// status 改变必须立即生效；静态模式下人工 cost 修改同样需要立即刷新最新路由。
		trigger.Trigger(routeresult.Trigger{Emergency: true})
		w.WriteHeader(http.StatusNoContent)
	})

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	return nil
}

func writeTopologyError(w http.ResponseWriter, err error, conflictMessage, notFoundMessage string) {
	switch {
	case errors.Is(err, viewdb.ErrAlreadyExist):
		http.Error(w, conflictMessage, http.StatusConflict)
	case errors.Is(err, viewdb.ErrNotFound):
		http.Error(w, notFoundMessage, http.StatusNotFound)
	case errors.Is(err, viewdb.ErrInvalidInput):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
