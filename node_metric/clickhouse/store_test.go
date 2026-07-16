package clickhouse

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

func TestStoreAppendWritesBatchInOneTransaction(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewStore(db, time.Second)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	sample := nodemetric.EdgeMetricSample{
		EdgeKey:         nodemetric.EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		Protocol:        nodemetric.ProtocolTCP,
		LatencyMS:       20,
		PacketLossRatio: 0.01,
		RateBPS:         1_000_000,
		ObservedAt:      now.Add(-time.Second),
		ReceivedAt:      now,
		SampleWindow:    2 * time.Second,
		Source:          "test",
		SourceID:        "source-a",
		Sequence:        1,
	}

	mock.ExpectBegin()
	prepared := mock.ExpectPrepare(regexp.QuoteMeta("INSERT INTO edge_metric_samples"))
	prepared.ExpectExec().WithArgs(
		"A",
		"B",
		"tcp",
		float64(20),
		float64(0.01),
		float64(1_000_000),
		sample.ObservedAt,
		now,
		uint64(2000),
		"test",
		"source-a",
		uint64(1),
	).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	count, err := store.Append(context.Background(), []nodemetric.EdgeMetricSample{sample})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("Append() count = %d, want 1", count)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestStoreQueryFiltersTimeRangeAndDimensions(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewStore(db, time.Second)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	start := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	end := start.Add(time.Minute)
	observedAt := start.Add(10 * time.Second)
	receivedAt := observedAt.Add(time.Millisecond)
	rows := sqlmock.NewRows([]string{
		"from_node_id",
		"to_node_id",
		"proto",
		"latency_ms",
		"packet_loss_ratio",
		"rate_bps",
		"observed_at",
		"received_at",
		"sample_window_ms",
		"source",
		"source_id",
		"sequence",
	}).AddRow("A", "B", "udp", 30.0, 0.05, 2_000_000.0, observedAt, receivedAt, uint64(2000), "agent", "agent-a", uint64(9))
	mock.ExpectQuery("FROM edge_metric_samples FINAL").
		WithArgs(start, end, "A", "B", "udp", 20, 5).
		WillReturnRows(rows)

	got, err := store.Query(context.Background(), nodemetric.MetricQuery{
		EdgeKey:  nodemetric.EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		Protocol: nodemetric.ProtocolUDP,
		Start:    start,
		End:      end,
		Offset:   5,
		Limit:    20,
	})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(got) != 1 || got[0].SampleWindow != 2*time.Second {
		t.Fatalf("Query() = %+v, want one 2s sample", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestStoreAggregateSeparatesProtocolDimensions(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := NewStore(db, time.Second)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	start := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Second)
	latest := end.Add(-time.Second)
	rows := sqlmock.NewRows([]string{
		"from_node_id",
		"to_node_id",
		"proto",
		"avg_latency_ms",
		"avg_packet_loss_ratio",
		"avg_rate_bps",
		"sample_count",
		"latest_observed_at",
	}).
		AddRow("A", "B", "tcp", 20.0, 0.0, 1_000_000.0, uint64(4), latest).
		AddRow("A", "B", "udp", 30.0, 0.05, 900_000.0, uint64(4), latest)
	mock.ExpectQuery("GROUP BY from_node_id, to_node_id, proto").
		WithArgs(start, end).
		WillReturnRows(rows)

	got, err := store.Aggregate(context.Background(), start, end)
	if err != nil {
		t.Fatalf("Aggregate() error = %v", err)
	}
	if len(got) != 2 || got[0].Protocol != nodemetric.ProtocolTCP || got[1].Protocol != nodemetric.ProtocolUDP {
		t.Fatalf("Aggregate() = %+v, want separate tcp and udp rows", got)
	}
	if got[0].WindowStart != start || got[0].WindowEnd != end {
		t.Fatalf("Aggregate() window = %s-%s, want %s-%s", got[0].WindowStart, got[0].WindowEnd, start, end)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}
