package viewdb

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

func TestStoreEdgeExistsChecksDirectedEdge(t *testing.T) {
	t.Parallel()

	gdb, mock, closeDB := newMockGORM(t)
	defer closeDB()
	store := NewStore(gdb)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM `graph_edges` WHERE from_node_id = ? AND to_node_id = ?")).
		WithArgs("A", "B").
		WillReturnRows(sqlmock.NewRows([]string{"count(*)"}).AddRow(1))

	exists, err := store.EdgeExists(context.Background(), nodemetric.EdgeKey{FromNodeID: "A", ToNodeID: "B"})
	if err != nil {
		t.Fatalf("EdgeExists() error = %v", err)
	}
	if !exists {
		t.Fatal("EdgeExists() = false, want true")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

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

func TestBuildGraphDTOUsesDynamicCostSnapshot(t *testing.T) {
	t.Parallel()

	nodes := []NodeModel{
		{NodeID: "A", Status: nodeStatusAvailable},
		{NodeID: "B", Status: nodeStatusAvailable},
	}
	edges := []EdgeModel{{
		FromNodeID: "A",
		ToNodeID:   "B",
		Cost:       10,
		Status:     edgeStatusAvailable,
	}}
	snapshots := []EdgeCostSnapshotModel{{
		FromNodeID: "A",
		ToNodeID:   "B",
		Cost:       42,
		Revision:   7,
	}}

	dto := buildGraphDTO(nodes, edges, snapshots, true, 7)
	if dto.CostRevision != 7 || !dto.DynamicMetricsEnabled || len(dto.Edges) != 1 {
		t.Fatalf("dynamic graph dto = %+v", dto)
	}
	edge := dto.Edges[0]
	if edge.Cost != 42 || edge.StaticCost != 10 || edge.DynamicCost == nil || *edge.DynamicCost != 42 {
		t.Fatalf("dynamic edge = %+v, want effective 42 and static 10", edge)
	}
}

func TestBuildGraphDTOUsesDegradedCostWithoutSnapshot(t *testing.T) {
	t.Parallel()

	nodes := []NodeModel{
		{NodeID: "A", Status: nodeStatusAvailable},
		{NodeID: "B", Status: nodeStatusAvailable},
	}
	edges := []EdgeModel{{
		FromNodeID: "A",
		ToNodeID:   "B",
		Cost:       10,
		Status:     edgeStatusAvailable,
	}}

	dto := buildGraphDTO(nodes, edges, nil, true, 0)
	edge := dto.Edges[0]
	if edge.Cost != 1000 || edge.StaticCost != 10 || edge.DynamicCost != nil || !edge.CostDegraded {
		t.Fatalf("edge without dynamic snapshot = %+v, want degraded effective cost 1000", edge)
	}
	g, err := graphFromDTO(dto)
	if err != nil {
		t.Fatalf("graphFromDTO() error = %v", err)
	}
	from, _ := g.Index("A")
	to, _ := g.Index("B")
	if g.Cost(from, to) != 1000 {
		t.Fatalf("route graph cost = %d, want 1000", g.Cost(from, to))
	}
}

func TestBuildGraphDTOUsesStaticCostWhenDynamicDisabled(t *testing.T) {
	t.Parallel()

	dto := buildGraphDTO(
		[]NodeModel{{NodeID: "A", Status: 1}, {NodeID: "B", Status: 1}},
		[]EdgeModel{{FromNodeID: "A", ToNodeID: "B", Cost: 10, Status: 1}},
		[]EdgeCostSnapshotModel{{FromNodeID: "A", ToNodeID: "B", Cost: 42, Revision: 7}},
		false,
		7,
	)
	edge := dto.Edges[0]
	if dto.DynamicMetricsEnabled {
		t.Fatal("static graph unexpectedly reports dynamic metrics enabled")
	}
	if edge.Cost != 10 || edge.StaticCost != 10 || edge.DynamicCost != nil {
		t.Fatalf("static edge = %+v, want cost 10", edge)
	}
}

func TestRoutingExcludesUnavailableEdgeButKeepsCost1000Edge(t *testing.T) {
	t.Parallel()

	dto := &GraphDTO{
		Nodes: []NodeDTO{
			{NodeID: "A", Status: 1},
			{NodeID: "B", Status: 1},
			{NodeID: "C", Status: 1},
		},
		Edges: []EdgeDTO{
			{From: "A", To: "B", Cost: 1, Status: 0},
			{From: "A", To: "C", Cost: 1000, Status: 1},
			{From: "C", To: "B", Cost: 1, Status: 1},
		},
	}
	g, err := graphFromDTO(dto)
	if err != nil {
		t.Fatalf("graphFromDTO() error = %v", err)
	}
	a, _ := g.Index("A")
	b, _ := g.Index("B")
	c, _ := g.Index("C")
	if g.Cost(a, b) != 0 {
		t.Fatalf("unavailable A->B cost = %d, want no edge", g.Cost(a, b))
	}
	if g.Cost(a, c) != 1000 || g.Cost(c, b) != 1 {
		t.Fatalf("degraded route edges = %d/%d, want 1000/1", g.Cost(a, c), g.Cost(c, b))
	}
}
