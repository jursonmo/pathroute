package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/jursonmo/pathroute/floyd"
	"github.com/jursonmo/pathroute/graph"
)

//go:embed static/*
var staticFS embed.FS

func main() {
	// Serve raw graph.json at /graph
	http.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile("data/graph.json")
		if err != nil {
			http.Error(w, "cannot read data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})

	// Calculate shortest paths: POST /calculate
	// Returns all pairs results so frontend can pick any (from,to) quickly.
	http.HandleFunc("/calculate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		g, err := graph.NewFromJSON("data/graph.json")
		if err != nil {
			http.Error(w, "load graph: "+err.Error(), http.StatusInternalServerError)
			return
		}
		res := floyd.RunFloyd(g)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Results []floyd.PairResult `json:"results"`
		}{Results: res.Results})
	})

	// Add node: POST /add-node {nodeId, x?, y?, des?, type?, status?}
	http.HandleFunc("/add-node", func(w http.ResponseWriter, r *http.Request) {
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
		data, err := os.ReadFile("data/graph.json")
		if err != nil {
			http.Error(w, "cannot read data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			http.Error(w, "cannot parse data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		nodes, _ := raw["nodes"].([]interface{})
		for _, n := range nodes {
			m, _ := n.(map[string]interface{})
			if m == nil {
				continue
			}
			if nodeId, _ := m["nodeId"].(string); nodeId == body.NodeID {
				http.Error(w, "node already exists", http.StatusConflict)
				return
			}
		}
		newNode := map[string]interface{}{"nodeId": body.NodeID}
		if body.X != nil {
			newNode["x"] = *body.X
		}
		if body.Y != nil {
			newNode["y"] = *body.Y
		}
		if body.Des != "" {
			newNode["des"] = body.Des
		}
		if body.Type != nil {
			newNode["type"] = *body.Type
		}
		if body.Status != nil {
			newNode["status"] = *body.Status
		}
		raw["nodes"] = append(nodes, newNode)
		out, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			http.Error(w, "cannot marshal: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile("data/graph.json", out, 0644); err != nil {
			http.Error(w, "cannot write data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Save node position: POST /save-position {nodeId,x,y} (preserves other node fields)
	http.HandleFunc("/save-position", func(w http.ResponseWriter, r *http.Request) {
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
		data, err := os.ReadFile("data/graph.json")
		if err != nil {
			http.Error(w, "cannot read data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			http.Error(w, "cannot parse data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		nodes, _ := raw["nodes"].([]interface{})
		found := false
		for _, n := range nodes {
			m, _ := n.(map[string]interface{})
			if m == nil {
				continue
			}
			if nodeId, _ := m["nodeId"].(string); nodeId == body.NodeID {
				m["x"] = body.X
				m["y"] = body.Y
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
		out, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			http.Error(w, "cannot marshal: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile("data/graph.json", out, 0644); err != nil {
			http.Error(w, "cannot write data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Update node extra fields: POST /update-node {nodeId, des?, type?, status?}
	http.HandleFunc("/update-node", func(w http.ResponseWriter, r *http.Request) {
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
		data, err := os.ReadFile("data/graph.json")
		if err != nil {
			http.Error(w, "cannot read data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			http.Error(w, "cannot parse data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		nodes, _ := raw["nodes"].([]interface{})
		found := false
		for _, n := range nodes {
			m, _ := n.(map[string]interface{})
			if m == nil {
				continue
			}
			if nodeId, _ := m["nodeId"].(string); nodeId == body.NodeID {
				m["des"] = body.Des
				if body.Type != nil {
					m["type"] = *body.Type
				}
				if body.Status != nil {
					m["status"] = *body.Status
				}
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
		out, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			http.Error(w, "cannot marshal: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile("data/graph.json", out, 0644); err != nil {
			http.Error(w, "cannot write data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Update edge: POST /update-edge {from, to, cost, des?, type?, status?}
	const minCost, maxCost = 1, 1000
	http.HandleFunc("/update-edge", func(w http.ResponseWriter, r *http.Request) {
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
		if body.From == "" || body.To == "" {
			http.Error(w, "from and to required", http.StatusBadRequest)
			return
		}
		if body.Cost != 0 && (body.Cost < minCost || body.Cost > maxCost) {
			http.Error(w, "cost must be 1-1000", http.StatusBadRequest)
			return
		}

		data, err := os.ReadFile("data/graph.json")
		if err != nil {
			http.Error(w, "cannot read data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			http.Error(w, "cannot parse data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		edges, _ := raw["edges"].([]interface{})
		found := false
		for _, e := range edges {
			m, _ := e.(map[string]interface{})
			if m == nil {
				continue
			}
			from, _ := m["from"].(string)
			to, _ := m["to"].(string)
			if from == body.From && to == body.To {
				if body.Cost >= minCost && body.Cost <= maxCost {
					m["cost"] = body.Cost
				}
				m["des"] = body.Des
				if body.Type != nil {
					m["type"] = *body.Type
				}
				if body.Status != nil {
					m["status"] = *body.Status
				}
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "edge not found", http.StatusNotFound)
			return
		}
		out, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			http.Error(w, "cannot marshal: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile("data/graph.json", out, 0644); err != nil {
			http.Error(w, "cannot write data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Add edge: POST /add-edge {from, to, cost, des?, type?, status?}
	http.HandleFunc("/add-edge", func(w http.ResponseWriter, r *http.Request) {
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
		if body.From == "" || body.To == "" {
			http.Error(w, "from and to required", http.StatusBadRequest)
			return
		}
		if body.From == body.To {
			http.Error(w, "from and to must differ", http.StatusBadRequest)
			return
		}
		if body.Cost < minCost || body.Cost > maxCost {
			http.Error(w, "cost must be 1-1000", http.StatusBadRequest)
			return
		}

		data, err := os.ReadFile("data/graph.json")
		if err != nil {
			http.Error(w, "cannot read data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(data, &raw); err != nil {
			http.Error(w, "cannot parse data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// validate nodes exist (best effort; if nodes stored as strings, treat as nodeId)
		nodeExists := func(nodeId string) bool {
			nodes, _ := raw["nodes"].([]interface{})
			for _, n := range nodes {
				if s, ok := n.(string); ok && s == nodeId {
					return true
				}
				if m, ok := n.(map[string]interface{}); ok {
					if nid, _ := m["nodeId"].(string); nid == nodeId {
						return true
					}
				}
			}
			return false
		}
		if !nodeExists(body.From) || !nodeExists(body.To) {
			http.Error(w, "from/to node not found", http.StatusNotFound)
			return
		}

		edges, _ := raw["edges"].([]interface{})
		for _, e := range edges {
			m, _ := e.(map[string]interface{})
			if m == nil {
				continue
			}
			from, _ := m["from"].(string)
			to, _ := m["to"].(string)
			if from == body.From && to == body.To {
				http.Error(w, "edge already exists", http.StatusConflict)
				return
			}
		}
		newEdge := map[string]interface{}{
			"from": body.From,
			"to":   body.To,
			"cost": body.Cost,
		}
		if body.Des != "" {
			newEdge["des"] = body.Des
		}
		if body.Type != nil {
			newEdge["type"] = *body.Type
		}
		if body.Status != nil {
			newEdge["status"] = *body.Status
		}
		raw["edges"] = append(edges, newEdge)

		out, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			http.Error(w, "cannot marshal: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := os.WriteFile("data/graph.json", out, 0644); err != nil {
			http.Error(w, "cannot write data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Serve static HTML/JS/CSS
	sub, _ := fs.Sub(staticFS, "static")
	http.Handle("/", http.FileServer(http.FS(sub)))

	log.Println("simple viewer listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
