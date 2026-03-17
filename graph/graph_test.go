package graph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewFromStruct_Valid(t *testing.T) {
	gj := &GraphJSON{
		Nodes: []string{"A", "B", "C"},
		Edges: []Edge{
			{From: "A", To: "B", Cost: 50},
			{From: "B", To: "A", Cost: 80},
			{From: "A", To: "C", Cost: 100},
		},
	}
	g, err := NewFromStruct(gj)
	if err != nil {
		t.Fatal(err)
	}
	if g.NumNodes() != 3 {
		t.Errorf("expected 3 nodes, got %d", g.NumNodes())
	}
	if w := g.Cost(g.NameToIndex["A"], g.NameToIndex["B"]); w != 50 {
		t.Errorf("A->B cost: got %d", w)
	}
	if w := g.Cost(g.NameToIndex["B"], g.NameToIndex["A"]); w != 80 {
		t.Errorf("B->A cost: got %d", w)
	}
}

func TestNewFromStruct_CostRejected(t *testing.T) {
	gj := &GraphJSON{
		Nodes: []string{"A", "B"},
		Edges: []Edge{
			{From: "A", To: "B", Cost: 0},
		},
	}
	_, err := NewFromStruct(gj)
	if err == nil {
		t.Error("expected error for cost 0")
	}
	gj.Edges[0].Cost = 1001
	_, err = NewFromStruct(gj)
	if err == nil {
		t.Error("expected error for cost 1001")
	}
	gj.Edges[0].Cost = 1
	g, err := NewFromStruct(gj)
	if err != nil {
		t.Fatal(err)
	}
	if g.Cost(0, 1) != 1 {
		t.Errorf("cost 1 should be valid")
	}
}

func TestNewFromStruct_NodesFromEdges(t *testing.T) {
	gj := &GraphJSON{
		Nodes: []string{},
		Edges: []Edge{
			{From: "X", To: "Y", Cost: 10},
			{From: "Y", To: "Z", Cost: 20},
		},
	}
	g, err := NewFromStruct(gj)
	if err != nil {
		t.Fatal(err)
	}
	if g.NumNodes() != 3 {
		t.Errorf("expected 3 nodes from edges, got %d", g.NumNodes())
	}
	_, ok := g.Index("X")
	if !ok {
		t.Error("node X not found")
	}
	_, ok = g.Index("Z")
	if !ok {
		t.Error("node Z not found")
	}
}

func TestNewFromJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "graph.json")
	err := os.WriteFile(path, []byte(`{"nodes":["A","B"],"edges":[{"from":"A","to":"B","cost":50}]}`), 0644)
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewFromJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if g.NumNodes() != 2 || g.Cost(0, 1) != 50 {
		t.Errorf("unexpected graph: nodes=%d cost=%d", g.NumNodes(), g.Cost(0, 1))
	}
}

func TestNeighbors(t *testing.T) {
	gj := &GraphJSON{
		Nodes: []string{"A", "B", "C"},
		Edges: []Edge{
			{From: "A", To: "B", Cost: 1},
			{From: "A", To: "C", Cost: 1},
		},
	}
	g, _ := NewFromStruct(gj)
	idxA, _ := g.Index("A")
	neigh := g.Neighbors(idxA)
	if len(neigh) != 2 {
		t.Fatalf("expected 2 neighbors of A, got %d", len(neigh))
	}
	names := make(map[string]bool)
	for _, i := range neigh {
		names[g.Name(i)] = true
	}
	if !names["B"] || !names["C"] {
		t.Error("expected neighbors B and C", names)
	}
}

func TestCopyWithoutNode(t *testing.T) {
	gj := &GraphJSON{
		Nodes: []string{"A", "B", "C"},
		Edges: []Edge{
			{From: "A", To: "B", Cost: 10},
			{From: "B", To: "C", Cost: 20},
			{From: "A", To: "C", Cost: 5},
		},
	}
	g, _ := NewFromStruct(gj)
	idxA, _ := g.Index("A")
	sub, oldToNew := g.CopyWithoutNode(idxA)
	if sub.NumNodes() != 2 {
		t.Fatalf("expected 2 nodes after remove A, got %d", sub.NumNodes())
	}
	if oldToNew[idxA] != -1 {
		t.Error("excluded node should map to -1")
	}
	// B and C remain; B->C should exist
	idxB, idxC := oldToNew[g.NameToIndex["B"]], oldToNew[g.NameToIndex["C"]]
	if sub.Cost(idxB, idxC) != 20 {
		t.Errorf("B->C in subgraph: got %d", sub.Cost(idxB, idxC))
	}
}

func TestGraphJSON_Roundtrip(t *testing.T) {
	gj := &GraphJSON{
		Nodes: []string{"A", "B"},
		Edges: []Edge{{From: "A", To: "B", Cost: 100}},
	}
	data, _ := json.Marshal(gj)
	var decoded GraphJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	g, err := NewFromStruct(&decoded)
	if err != nil {
		t.Fatal(err)
	}
	if g.Cost(0, 1) != 100 {
		t.Errorf("roundtrip cost: got %d", g.Cost(0, 1))
	}
}
