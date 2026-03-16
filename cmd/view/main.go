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

// types to match data/graph.json (ignore extra fields with ,omitempty)
type simpleNode struct {
	ID string  `json:"id"`
	X  float64 `json:"x,omitempty"`
	Y  float64 `json:"y,omitempty"`
}

type simpleEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Weight int    `json:"weight"`
}

type simpleGraphFile struct {
	Nodes []simpleNode `json:"nodes"`
	Edges []simpleEdge `json:"edges"`
}

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

	// Save node position: POST /save-position {id,x,y}
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
		var gf simpleGraphFile
		if err := json.Unmarshal(data, &gf); err != nil {
			http.Error(w, "cannot parse data/graph.json: "+err.Error(), http.StatusInternalServerError)
			return
		}
		found := false
		for i := range gf.Nodes {
			if gf.Nodes[i].ID == body.ID {
				gf.Nodes[i].X = body.X
				gf.Nodes[i].Y = body.Y
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
		out, err := json.MarshalIndent(gf, "", "  ")
		if err != nil {
			http.Error(w, "cannot marshal data/graph.json: "+err.Error(), http.StatusInternalServerError)
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

