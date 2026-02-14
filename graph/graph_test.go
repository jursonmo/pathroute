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
			{From: "A", To: "B", Weight: 50},
			{From: "B", To: "A", Weight: 80},
			{From: "A", To: "C", Weight: 100},
		},
	}
	g, err := NewFromStruct(gj)
	if err != nil {
		t.Fatal(err)
	}
	if g.NumNodes() != 3 {
		t.Errorf("expected 3 nodes, got %d", g.NumNodes())
	}
	if w := g.Weight(g.NameToIndex["A"], g.NameToIndex["B"]); w != 50 {
		t.Errorf("A->B weight: got %d", w)
	}
	if w := g.Weight(g.NameToIndex["B"], g.NameToIndex["A"]); w != 80 {
		t.Errorf("B->A weight: got %d", w)
	}
}

func TestNewFromStruct_WeightRejected(t *testing.T) {
	gj := &GraphJSON{
		Nodes: []string{"A", "B"},
		Edges: []Edge{
			{From: "A", To: "B", Weight: 0},
		},
	}
	_, err := NewFromStruct(gj)
	if err == nil {
		t.Error("expected error for weight 0")
	}
	gj.Edges[0].Weight = 1001
	_, err = NewFromStruct(gj)
	if err == nil {
		t.Error("expected error for weight 1001")
	}
	gj.Edges[0].Weight = 1
	g, err := NewFromStruct(gj)
	if err != nil {
		t.Fatal(err)
	}
	if g.Weight(0, 1) != 1 {
		t.Errorf("weight 1 should be valid")
	}
}

func TestNewFromStruct_NodesFromEdges(t *testing.T) {
	gj := &GraphJSON{
		Nodes: []string{},
		Edges: []Edge{
			{From: "X", To: "Y", Weight: 10},
			{From: "Y", To: "Z", Weight: 20},
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
	err := os.WriteFile(path, []byte(`{"nodes":["A","B"],"edges":[{"from":"A","to":"B","weight":50}]}`), 0644)
	if err != nil {
		t.Fatal(err)
	}
	g, err := NewFromJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if g.NumNodes() != 2 || g.Weight(0, 1) != 50 {
		t.Errorf("unexpected graph: nodes=%d weight=%d", g.NumNodes(), g.Weight(0, 1))
	}
}

func TestNeighbors(t *testing.T) {
	gj := &GraphJSON{
		Nodes: []string{"A", "B", "C"},
		Edges: []Edge{
			{From: "A", To: "B", Weight: 1},
			{From: "A", To: "C", Weight: 1},
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
			{From: "A", To: "B", Weight: 10},
			{From: "B", To: "C", Weight: 20},
			{From: "A", To: "C", Weight: 5},
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
	if sub.Weight(idxB, idxC) != 20 {
		t.Errorf("B->C in subgraph: got %d", sub.Weight(idxB, idxC))
	}
}

func TestGraphJSON_Roundtrip(t *testing.T) {
	gj := &GraphJSON{
		Nodes: []string{"A", "B"},
		Edges: []Edge{{From: "A", To: "B", Weight: 100}},
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
	if g.Weight(0, 1) != 100 {
		t.Errorf("roundtrip weight: got %d", g.Weight(0, 1))
	}
}
