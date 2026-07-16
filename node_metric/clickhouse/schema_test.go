package clickhouse

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if len(cfg.Addresses) != 1 || cfg.Addresses[0] != "127.0.0.1:9000" {
		t.Fatalf("Addresses = %v, want [127.0.0.1:9000]", cfg.Addresses)
	}
	if cfg.Database != "default" {
		t.Fatalf("Database = %q, want default", cfg.Database)
	}
	if cfg.DialTimeout != 5*time.Second || cfg.QueryTimeout != 5*time.Second {
		t.Fatalf("timeouts = %s/%s, want 5s/5s", cfg.DialTimeout, cfg.QueryTimeout)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate() error = %v", err)
	}
}

func TestSchemaSQLSupportsMetricAccessPattern(t *testing.T) {
	t.Parallel()

	sql := SchemaSQL()
	required := []string{
		"CREATE TABLE IF NOT EXISTS edge_metric_samples",
		"from_node_id String",
		"to_node_id String",
		"proto LowCardinality(String)",
		"observed_at DateTime64(3, 'UTC')",
		"received_at DateTime64(3, 'UTC')",
		"sample_window_ms UInt64",
		"source_id String",
		"sequence UInt64",
		"ReplacingMergeTree(received_at)",
		"PARTITION BY cityHash64(source_id) % 64",
		"INDEX idx_observed_at observed_at TYPE minmax GRANULARITY 1",
		"ORDER BY (from_node_id, to_node_id, proto, source_id, sequence)",
	}

	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Errorf("SchemaSQL() missing %q", fragment)
		}
	}
	if strings.Contains(sql, "PARTITION BY toYYYYMM(observed_at)") {
		t.Fatal("identity retries must not be split into different monthly partitions")
	}
}
