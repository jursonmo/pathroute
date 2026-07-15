package viewdb

import "testing"

func TestIsNodeAvailable(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		expected bool
	}{
		{name: "unavailable status", status: nodeStatusUnavailable, expected: false},
		{name: "available status", status: nodeStatusAvailable, expected: true},
		{name: "unknown non available status", status: 2, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNodeAvailable(tt.status); got != tt.expected {
				t.Fatalf("isNodeAvailable(%d) = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestIsEdgeAvailable(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		expected bool
	}{
		{name: "unavailable status", status: edgeStatusUnavailable, expected: false},
		{name: "available status", status: edgeStatusAvailable, expected: true},
		{name: "unknown status", status: 2, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEdgeAvailable(tt.status); got != tt.expected {
				t.Fatalf("isEdgeAvailable(%d) = %v, want %v", tt.status, got, tt.expected)
			}
		})
	}
}

func TestBuildAvailableGraphJSONExcludesUnavailableNodes(t *testing.T) {
	gdto := &GraphDTO{
		Nodes: []NodeDTO{
			{NodeID: "A", Status: nodeStatusUnavailable},
			{NodeID: "B", Status: nodeStatusAvailable},
			{NodeID: "C", Status: nodeStatusAvailable},
			{NodeID: "D", Status: nodeStatusUnavailable},
		},
		Edges: []EdgeDTO{
			{From: "A", To: "B", Cost: 1, Status: edgeStatusAvailable},
			{From: "B", To: "C", Cost: 1, Status: edgeStatusAvailable},
			{From: "C", To: "D", Cost: 5, Status: edgeStatusAvailable},
		},
	}

	gj := buildAvailableGraphJSON(gdto)

	if len(gj.Nodes) != 2 {
		t.Fatalf("expected available nodes B and C, got %v", gj.Nodes)
	}
	if gj.Nodes[0] != "B" || gj.Nodes[1] != "C" {
		t.Fatalf("unexpected available nodes: %v", gj.Nodes)
	}
	if len(gj.Edges) != 1 {
		t.Fatalf("expected only edge B->C, got %v", gj.Edges)
	}
	if gj.Edges[0].From != "B" || gj.Edges[0].To != "C" {
		t.Fatalf("unexpected available edge: %+v", gj.Edges[0])
	}
}

func TestBuildAvailableGraphJSONExcludesUnavailableEdges(t *testing.T) {
	gdto := &GraphDTO{
		Nodes: []NodeDTO{
			{NodeID: "A", Status: nodeStatusAvailable},
			{NodeID: "B", Status: nodeStatusAvailable},
			{NodeID: "C", Status: nodeStatusAvailable},
		},
		Edges: []EdgeDTO{
			{From: "A", To: "B", Cost: 1, Status: edgeStatusAvailable},
			{From: "A", To: "C", Cost: 2, Status: edgeStatusUnavailable},
			{From: "B", To: "C", Cost: 3, Status: 2},
		},
	}

	gj := buildAvailableGraphJSON(gdto)

	if len(gj.Edges) != 1 {
		t.Fatalf("expected only available edge A->B, got %v", gj.Edges)
	}
	if gj.Edges[0].From != "A" || gj.Edges[0].To != "B" {
		t.Fatalf("unexpected available edge: %+v", gj.Edges[0])
	}
}
