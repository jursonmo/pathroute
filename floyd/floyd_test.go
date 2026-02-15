package floyd

import (
	"testing"

	"github.com/jursonmo/pathroute/graph"
)

func TestFloyd_ThreeNodes(t *testing.T) {
	gj := &graph.GraphJSON{
		Nodes: []string{"A", "B", "C"},
		Edges: []graph.Edge{
			{From: "A", To: "B", Weight: 50},
			{From: "B", To: "A", Weight: 80},
			{From: "A", To: "C", Weight: 100},
			{From: "B", To: "C", Weight: 20},
		},
	}
	g, err := graph.NewFromStruct(gj)
	if err != nil {
		t.Fatal(err)
	}
	r := RunFloyd(g)
	// A->B = 50, B->A = 80
	ab := findResult(r, "A", "B")
	if ab == nil || ab.Distance != 50 {
		t.Fatalf("A->B: got %v", ab)
	}
	ba := findResult(r, "B", "A")
	if ba == nil || ba.Distance != 80 {
		t.Fatalf("B->A: got %v", ba)
	}
	// A->C: 1st shortest A->B->C = 70, 2nd A->C = 100
	ac := findResult(r, "A", "C")
	if ac == nil || ac.Distance != 70 {
		t.Fatalf("A->C: expected first shortest 70, got %v", ac)
	}
	if len(ac.Paths) < 1 || ac.Paths[0].Distance != 70 || len(ac.Paths[0].Path) != 3 {
		t.Errorf("A->C first path should be [A,B,C] 70: %v", ac.Paths)
	}
	if len(ac.Paths) < 2 || ac.Paths[1].Distance != 100 {
		t.Errorf("A->C second path should be [A,C] 100: %v", ac.Paths)
	}
}

func TestFloyd_TwoEqualPaths(t *testing.T) {
	// A->C->D->F and A->E->D->F same length
	gj := &graph.GraphJSON{
		Nodes: []string{"A", "C", "D", "E", "F"},
		Edges: []graph.Edge{
			{From: "A", To: "C", Weight: 10},
			{From: "A", To: "E", Weight: 10},
			{From: "C", To: "D", Weight: 10},
			{From: "E", To: "D", Weight: 10},
			{From: "D", To: "F", Weight: 10},
		},
	}
	g, _ := graph.NewFromStruct(gj)
	r := RunFloyd(g)
	af := findResult(r, "A", "F")
	if af == nil || af.Distance != 30 {
		t.Fatalf("A->F distance: expected 30, got %v", af)
	}
	if len(af.Paths) != 2 {
		t.Errorf("A->F expected 2 paths, got %d: %v", len(af.Paths), af.Paths)
	}
}

func TestFloyd_Unreachable(t *testing.T) {
	gj := &graph.GraphJSON{
		Nodes: []string{"A", "B"},
		Edges: []graph.Edge{{From: "A", To: "B", Weight: 1}},
	}
	g, _ := graph.NewFromStruct(gj)
	r := RunFloyd(g)
	ba := findResult(r, "B", "A")
	if ba == nil || ba.Distance != -1 || len(ba.Paths) != 0 {
		t.Errorf("B->A should be unreachable: %v", ba)
	}
}

func TestFloyd_MaxFourPaths(t *testing.T) {
	// Create a graph with many equal-cost paths so we get more than 4
	gj := &graph.GraphJSON{
		Nodes: []string{"A", "B", "C", "D", "E"},
		Edges: []graph.Edge{
			{From: "A", To: "B", Weight: 1},
			{From: "A", To: "C", Weight: 1},
			{From: "A", To: "D", Weight: 1},
			{From: "B", To: "E", Weight: 1},
			{From: "C", To: "E", Weight: 1},
			{From: "D", To: "E", Weight: 1},
		},
	}
	g, _ := graph.NewFromStruct(gj)
	r := RunFloyd(g)
	ae := findResult(r, "A", "E")
	if ae == nil || ae.Distance != 2 {
		t.Fatalf("A->E distance: %v", ae)
	}
	if len(ae.Paths) > MaxShortestPaths {
		t.Errorf("expected at most %d paths, got %d", MaxShortestPaths, len(ae.Paths))
	}
}

func findResult(r *AllPairsResult, from, to string) *PairResult {
	for i := range r.Results {
		if r.Results[i].From == from && r.Results[i].To == to {
			return &r.Results[i]
		}
	}
	return nil
}

func TestFillViaNeighborPaths(t *testing.T) {
	gj := &graph.GraphJSON{
		Nodes: []string{"A", "B", "C", "D"},
		Edges: []graph.Edge{
			{From: "A", To: "B", Weight: 10},
			{From: "A", To: "C", Weight: 10},
			{From: "B", To: "D", Weight: 10},
			{From: "C", To: "D", Weight: 10},
		},
	}
	g, _ := graph.NewFromStruct(gj)
	r := RunFloyd(g)
	r.FillViaNeighborPaths()
	ad := findResult(r, "A", "D")
	if ad == nil {
		t.Fatal("A->D result not found")
	}
	// Via-neighbor: A->B->D and A->C->D (both distance 20)
	if len(ad.ViaNeighborPaths) != 2 {
		t.Errorf("A->D via-neighbor: expected 2 paths, got %d: %v", len(ad.ViaNeighborPaths), ad.ViaNeighborPaths)
	}
	for _, p := range ad.ViaNeighborPaths {
		if p.Distance != 20 {
			t.Errorf("via-neighbor path distance expected 20, got %d", p.Distance)
		}
		// Path must not contain A except at start
		for i := 1; i < len(p.Path); i++ {
			if p.Path[i] == "A" {
				t.Errorf("via-neighbor path should not contain A after start: %v", p.Path)
			}
		}
	}
}

func TestViaNeighbor_StartHasNoOutEdges(t *testing.T) {
	gj := &graph.GraphJSON{
		Nodes: []string{"A", "B"},
		Edges: []graph.Edge{{From: "B", To: "A", Weight: 10}},
	}
	g, _ := graph.NewFromStruct(gj)
	r := RunFloyd(g)
	r.FillViaNeighborPaths()
	// A has no out-edges, so A->B via-neighbor should be empty
	ab := findResult(r, "A", "B")
	if ab == nil {
		t.Fatal("A->B not found")
	}
	if len(ab.ViaNeighborPaths) != 0 {
		t.Errorf("A has no out-neighbors, via-neighbor paths should be empty: %v", ab.ViaNeighborPaths)
	}
}
