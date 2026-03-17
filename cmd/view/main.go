package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
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

	// Save node position: POST /save-position {id,x,y} (preserves other node fields)
	http.HandleFunc("/save-position", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			ID string  `json:"id"`
			X  float64 `json:"x"`
			Y  float64 `json:"y"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
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
			if id, _ := m["id"].(string); id == body.ID {
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

	// Update node extra fields: POST /update-node {id, des?, type?, status?}
	http.HandleFunc("/update-node", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			ID     string `json:"id"`
			Des    string `json:"des"`
			Type   *int   `json:"type"`
			Status *int   `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
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
			if id, _ := m["id"].(string); id == body.ID {
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

	// Update edge weight: POST /update-edge {from, to, weight}
	const minWeight, maxWeight = 1, 1000
	http.HandleFunc("/update-edge", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			From   string `json:"from"`
			To     string `json:"to"`
			Weight int    `json:"weight"`
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
		if body.Weight != 0 && (body.Weight < minWeight || body.Weight > maxWeight) {
			http.Error(w, "weight must be 1-1000", http.StatusBadRequest)
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
				if body.Weight >= minWeight && body.Weight <= maxWeight {
					m["weight"] = body.Weight
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

	// Serve static HTML/JS/CSS
	sub, _ := fs.Sub(staticFS, "static")
	http.Handle("/", http.FileServer(http.FS(sub)))

	log.Println("simple viewer listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

