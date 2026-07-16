package viewdb

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

func TestSimulationStoreReplaceRollsBackUnknownEdge(t *testing.T) {
	t.Parallel()

	gdb, mock, closeDB := newMockGORM(t)
	defer closeDB()
	store, err := NewSimulationStore(gdb)
	if err != nil {
		t.Fatalf("NewSimulationStore() error = %v", err)
	}
	config := simulationConfigForStore()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM `graph_edges` WHERE from_node_id = ? AND to_node_id = ?")).
		WithArgs("A", "B").
		WillReturnRows(sqlmock.NewRows([]string{"count(*)"}).AddRow(0))
	mock.ExpectRollback()

	err = store.Replace(context.Background(), []nodemetric.SimulationConfig{config})
	if !errors.Is(err, nodemetric.ErrUnknownEdge) {
		t.Fatalf("Replace() error = %v, want ErrUnknownEdge", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestSimulationStoreReplaceUpsertsInOneTransaction(t *testing.T) {
	t.Parallel()

	gdb, mock, closeDB := newMockGORM(t)
	defer closeDB()
	store, err := NewSimulationStore(gdb)
	if err != nil {
		t.Fatalf("NewSimulationStore() error = %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT count(*) FROM `graph_edges` WHERE from_node_id = ? AND to_node_id = ?")).
		WithArgs("A", "B").
		WillReturnRows(sqlmock.NewRows([]string{"count(*)"}).AddRow(1))
	mock.ExpectExec("INSERT INTO `edge_metric_simulations`").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	if err := store.Replace(context.Background(), []nodemetric.SimulationConfig{simulationConfigForStore()}); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestSimulationStoreListMapsRuntimeState(t *testing.T) {
	t.Parallel()

	gdb, mock, closeDB := newMockGORM(t)
	defer closeDB()
	store, err := NewSimulationStore(gdb)
	if err != nil {
		t.Fatalf("NewSimulationStore() error = %v", err)
	}
	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{
		"id",
		"created_at",
		"updated_at",
		"from_node_id",
		"to_node_id",
		"proto",
		"enabled",
		"interval_ms",
		"latency_min_ms",
		"latency_max_ms",
		"packet_loss_min_ratio",
		"packet_loss_max_ratio",
		"rate_min_bps",
		"rate_max_bps",
		"last_generated_at",
		"last_metric_json",
	}).AddRow(1, now, now, "A", "B", "tcp", true, 2000, 10, 20, 0, 0.1, 100, 200, now, nil)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `edge_metric_simulations`")).WillReturnRows(rows)

	configs, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(configs) != 1 || configs[0].Interval != 2*time.Second || configs[0].Protocol != nodemetric.ProtocolTCP {
		t.Fatalf("List() = %+v", configs)
	}
	if configs[0].LastGeneratedAt == nil || !configs[0].LastGeneratedAt.Equal(now) {
		t.Fatalf("LastGeneratedAt = %v, want %s", configs[0].LastGeneratedAt, now)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func newMockGORM(t *testing.T) (*gorm.DB, sqlmock.Sqlmock, func()) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	gdb, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return gdb, mock, func() {
		mock.ExpectClose()
		if err := sqlDB.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	}
}

func simulationConfigForStore() nodemetric.SimulationConfig {
	return nodemetric.SimulationConfig{
		EdgeKey:            nodemetric.EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		Protocol:           nodemetric.ProtocolTCP,
		IsEnabled:          true,
		Interval:           2 * time.Second,
		LatencyMinMS:       10,
		LatencyMaxMS:       20,
		PacketLossMinRatio: 0,
		PacketLossMaxRatio: 0.1,
		RateMinBPS:         100,
		RateMaxBPS:         200,
	}
}
