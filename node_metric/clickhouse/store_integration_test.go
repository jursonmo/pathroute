//go:build integration

package clickhouse

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
)

func TestStoreIntegrationDeduplicatesAndAggregates(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("CLICKHOUSE_ADDR"))
	if address == "" {
		t.Skip("CLICKHOUSE_ADDR is not set")
	}

	config := DefaultConfig()
	config.Addresses = []string{address}
	if value := strings.TrimSpace(os.Getenv("CLICKHOUSE_DATABASE")); value != "" {
		config.Database = value
	}
	if value := strings.TrimSpace(os.Getenv("CLICKHOUSE_USERNAME")); value != "" {
		config.Username = value
	}
	config.Password = os.Getenv("CLICKHOUSE_PASSWORD")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := Open(ctx, config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	if err := Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	store, err := NewStore(db, config.QueryTimeout)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	now := time.Now().UTC().Truncate(time.Millisecond)
	testID := fmt.Sprintf("integration-%d", now.UnixNano())
	sample := nodemetric.EdgeMetricSample{
		EdgeKey:         nodemetric.EdgeKey{FromNodeID: testID + "-from", ToNodeID: testID + "-to"},
		Protocol:        nodemetric.ProtocolUDP,
		LatencyMS:       30,
		PacketLossRatio: 0.05,
		RateBPS:         1_000_000,
		ObservedAt:      now,
		ReceivedAt:      now.Add(time.Millisecond),
		SampleWindow:    2 * time.Second,
		Source:          "integration",
		SourceID:        testID,
		Sequence:        1,
	}
	retry := sample
	// 同一身份的重试即使携带跨月观测时间，也必须落入同一哈希分区并只聚合一次。
	retry.ObservedAt = sample.ObservedAt.AddDate(0, 1, 0)
	retry.ReceivedAt = sample.ReceivedAt.Add(time.Millisecond)
	if _, err := store.Append(ctx, []nodemetric.EdgeMetricSample{sample, retry}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	queryStart := now.Add(-time.Second)
	queryEnd := retry.ObservedAt.Add(time.Second)
	samples, err := store.Query(ctx, nodemetric.MetricQuery{
		EdgeKey:  sample.EdgeKey,
		Protocol: sample.Protocol,
		Start:    queryStart,
		End:      queryEnd,
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(samples) != 1 {
		t.Fatalf("Query() returned %d effective samples, want 1", len(samples))
	}

	aggregates, err := store.Aggregate(ctx, queryStart, queryEnd)
	if err != nil {
		t.Fatalf("Aggregate() error = %v", err)
	}
	var found bool
	for _, aggregate := range aggregates {
		if aggregate.EdgeKey == sample.EdgeKey && aggregate.Protocol == sample.Protocol {
			found = true
			if aggregate.SampleCount != 1 {
				t.Fatalf("SampleCount = %d, want 1", aggregate.SampleCount)
			}
		}
	}
	if !found {
		t.Fatal("aggregate for integration edge not found")
	}
}
