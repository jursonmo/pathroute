package viewdb

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

func TestCostStorePublishUsesOneRevisionAndTransaction(t *testing.T) {
	t.Parallel()

	gdb, mock, closeDB := newMockGORM(t)
	defer closeDB()
	store, err := NewCostStore(gdb)
	if err != nil {
		t.Fatalf("NewCostStore() error = %v", err)
	}
	publication, protocolCosts, edgeCosts := costPublicationFixture()

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `edge_cost_publications`").WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectExec("INSERT INTO `edge_protocol_cost_snapshots`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `edge_cost_snapshots`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	got, err := store.Publish(context.Background(), publication, protocolCosts, edgeCosts)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if got.Revision != 7 {
		t.Fatalf("Publish() revision = %d, want 7", got.Revision)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCostStorePublishRollsBackSnapshotFailure(t *testing.T) {
	t.Parallel()

	gdb, mock, closeDB := newMockGORM(t)
	defer closeDB()
	store, err := NewCostStore(gdb)
	if err != nil {
		t.Fatalf("NewCostStore() error = %v", err)
	}
	publication, protocolCosts, edgeCosts := costPublicationFixture()
	writeErr := errors.New("protocol write failed")

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `edge_cost_publications`").WillReturnResult(sqlmock.NewResult(8, 1))
	mock.ExpectExec("INSERT INTO `edge_protocol_cost_snapshots`").WillReturnError(writeErr)
	mock.ExpectRollback()

	if _, err := store.Publish(context.Background(), publication, protocolCosts, edgeCosts); !errors.Is(err, writeErr) {
		t.Fatalf("Publish() error = %v, want wrapped write error", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCostStoreRefreshMetricsKeepsPublishedRevisionWithoutNewPublication(t *testing.T) {
	t.Parallel()

	gdb, mock, closeDB := newMockGORM(t)
	defer closeDB()
	store, err := NewCostStore(gdb)
	if err != nil {
		t.Fatalf("NewCostStore() error = %v", err)
	}
	_, protocolCosts, edgeCosts := costPublicationFixture()
	protocolCosts[0].Revision = 7
	edgeCosts[0].Revision = 7

	// 指标元数据刷新只能更新两层快照，不能插入 publication 或生成新 revision。
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `edge_protocol_cost_snapshots`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO `edge_cost_snapshots`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := store.RefreshMetrics(context.Background(), protocolCosts, edgeCosts); err != nil {
		t.Fatalf("RefreshMetrics() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestCostStoreCurrentEdgeCostsReadsOneConsistentTransaction(t *testing.T) {
	t.Parallel()

	gdb, mock, closeDB := newMockGORM(t)
	defer closeDB()
	store, err := NewCostStore(gdb)
	if err != nil {
		t.Fatalf("NewCostStore() error = %v", err)
	}
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT \\* FROM `edge_cost_publications`").WillReturnRows(sqlmock.NewRows([]string{
		"revision", "trigger_reason", "changed_edges", "formula_version", "published_at",
	}).AddRow(7, "cost_changed", 1, "v1", now))
	mock.ExpectQuery("SELECT \\* FROM `edge_cost_snapshots`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery("SELECT \\* FROM `edge_protocol_cost_snapshots`").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectCommit()

	publication, edges, err := store.CurrentEdgeCosts(context.Background())
	if err != nil {
		t.Fatalf("CurrentEdgeCosts() error = %v", err)
	}
	if publication.Revision != 7 || len(edges) != 0 {
		t.Fatalf("CurrentEdgeCosts() = publication %+v edges %+v", publication, edges)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func costPublicationFixture() (
	nodemetric.CostPublication,
	[]nodemetric.ProtocolCostSnapshot,
	[]nodemetric.EdgeCostSnapshot,
) {
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	aggregated := nodemetric.AggregatedMetric{
		EdgeKey:               nodemetric.EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		Protocol:              nodemetric.ProtocolTCP,
		AverageLatencyMS:      30,
		AveragePacketLossRate: 0,
		AverageRateBPS:        1_000_000,
		SampleCount:           5,
		WindowStart:           now.Add(-10 * time.Second),
		WindowEnd:             now,
		LatestObservedAt:      now.Add(-time.Second),
	}
	protocol := nodemetric.ProtocolCostSnapshot{
		AggregatedMetric: aggregated,
		Cost:             30,
		FormulaVersion:   "v1",
		CalculatedAt:     now,
	}
	edge := nodemetric.EdgeCostSnapshot{
		EdgeKey:        aggregated.EdgeKey,
		Cost:           30,
		ProtocolCosts:  []nodemetric.ProtocolCostSnapshot{protocol},
		WindowStart:    aggregated.WindowStart,
		WindowEnd:      aggregated.WindowEnd,
		LatestMetricAt: aggregated.LatestObservedAt,
		FormulaVersion: "v1",
		CalculatedAt:   now,
		PublishedAt:    now,
	}
	publication := nodemetric.CostPublication{
		TriggerReason:  "cost_changed",
		ChangedEdges:   1,
		FormulaVersion: "v1",
		PublishedAt:    now,
	}
	return publication, []nodemetric.ProtocolCostSnapshot{protocol}, []nodemetric.EdgeCostSnapshot{edge}
}
