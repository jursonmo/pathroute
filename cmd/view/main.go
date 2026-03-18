package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/jursonmo/pathroute/floyd"
	"github.com/jursonmo/pathroute/internal/viewdb"
)

//go:embed static/*
var staticFS embed.FS

func envBool(key string, def bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return def
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func main() {
	dsn := strings.TrimSpace(os.Getenv("MYSQL_DSN"))
	if dsn == "" {
		//create database pathroute;
		dsn = "root:@tcp(127.0.0.1:3306)/pathroute?charset=utf8mb4&parseTime=True&loc=Local"
	}
	if dsn == "" {
		log.Fatal("MYSQL_DSN is required, example: user:pass@tcp(127.0.0.1:3306)/pathroute?charset=utf8mb4&parseTime=True&loc=Local")
	}
	gdb, err := viewdb.OpenMySQL(dsn)
	if err != nil {
		log.Fatal("connect mysql: ", err)
	}
	st := viewdb.NewStore(gdb)

	// Optional bootstrap: import from graph.json only when DB is empty.
	if envBool("SEED_FROM_JSON", true) {
		seedPath := strings.TrimSpace(os.Getenv("GRAPH_JSON_PATH"))
		if seedPath == "" {
			seedPath = "data/graph.json"
		}
		if err := st.SeedFromJSONIfEmpty(context.Background(), seedPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				log.Printf("seed skipped, file not found: %s", seedPath)
			} else {
				log.Fatal("seed from json: ", err)
			}
		}
	}

	http.HandleFunc("/graph", func(w http.ResponseWriter, r *http.Request) {
		data, err := st.GetGraph(r.Context())
		if err != nil {
			http.Error(w, "load graph: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	})

	http.HandleFunc("/calculate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		g, err := st.BuildGraph(r.Context())
		if err != nil {
			http.Error(w, "build graph: "+err.Error(), http.StatusInternalServerError)
			return
		}
		res := floyd.RunFloyd(g)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Results []floyd.PairResult `json:"results"`
		}{Results: res.Results})
	})

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
		n := viewdb.NodeDTO{NodeID: body.NodeID, Des: body.Des}
		if body.X != nil {
			n.X = *body.X
		}
		if body.Y != nil {
			n.Y = *body.Y
		}
		if body.Type != nil {
			n.Type = *body.Type
		}
		if body.Status != nil {
			n.Status = *body.Status
		}
		if err := st.AddNode(r.Context(), n); err != nil {
			switch {
			case errors.Is(err, viewdb.ErrAlreadyExist):
				http.Error(w, "node already exists", http.StatusConflict)
			case errors.Is(err, viewdb.ErrInvalidInput):
				http.Error(w, err.Error(), http.StatusBadRequest)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

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
		if err := st.SavePosition(r.Context(), body.NodeID, body.X, body.Y); err != nil {
			switch {
			case errors.Is(err, viewdb.ErrNotFound):
				http.Error(w, "node not found", http.StatusNotFound)
			case errors.Is(err, viewdb.ErrInvalidInput):
				http.Error(w, err.Error(), http.StatusBadRequest)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

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
		if err := st.UpdateNode(r.Context(), body.NodeID, body.Des, body.Type, body.Status); err != nil {
			switch {
			case errors.Is(err, viewdb.ErrNotFound):
				http.Error(w, "node not found", http.StatusNotFound)
			case errors.Is(err, viewdb.ErrInvalidInput):
				http.Error(w, err.Error(), http.StatusBadRequest)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

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
		e := viewdb.EdgeDTO{From: body.From, To: body.To, Cost: body.Cost, Des: body.Des}
		if body.Type != nil {
			e.Type = *body.Type
		}
		if body.Status != nil {
			e.Status = *body.Status
		}
		if err := st.AddEdge(r.Context(), e); err != nil {
			switch {
			case errors.Is(err, viewdb.ErrAlreadyExist):
				http.Error(w, "edge already exists", http.StatusConflict)
			case errors.Is(err, viewdb.ErrNotFound):
				http.Error(w, "from/to node not found", http.StatusNotFound)
			case errors.Is(err, viewdb.ErrInvalidInput):
				http.Error(w, err.Error(), http.StatusBadRequest)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

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
		if err := st.UpdateEdge(r.Context(), body.From, body.To, body.Cost, body.Des, body.Type, body.Status); err != nil {
			switch {
			case errors.Is(err, viewdb.ErrNotFound):
				http.Error(w, "edge not found", http.StatusNotFound)
			case errors.Is(err, viewdb.ErrInvalidInput):
				http.Error(w, err.Error(), http.StatusBadRequest)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
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
