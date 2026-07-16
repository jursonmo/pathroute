package main

import (
	"strings"
	"testing"
	"time"
)

func TestLoadAppConfigUsesApprovedMetricDefaults(t *testing.T) {
	t.Parallel()

	config, err := loadAppConfigFrom(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadAppConfigFrom() error = %v", err)
	}
	if config.Metric.Enabled {
		t.Fatal("dynamic metrics must be disabled by default")
	}
	if config.Metric.SampleInterval != 2*time.Second || config.Metric.AggregationWindow != 10*time.Second {
		t.Fatalf("metric intervals = %s/%s", config.Metric.SampleInterval, config.Metric.AggregationWindow)
	}
	if config.Metric.ExpiryThreshold != 30*time.Second || config.Metric.MinRouteInterval != 30*time.Second {
		t.Fatalf("expiry/min route = %s/%s", config.Metric.ExpiryThreshold, config.Metric.MinRouteInterval)
	}
	if config.ClickHouse.Addresses[0] != "127.0.0.1:9000" || config.ListenAddr != ":8080" {
		t.Fatalf("connection defaults = %+v / %q", config.ClickHouse.Addresses, config.ListenAddr)
	}
}

func TestLoadAppConfigReadsDynamicOverrides(t *testing.T) {
	t.Parallel()

	values := map[string]string{
		"DYNAMIC_METRICS_ENABLED":    "true",
		"METRIC_SAMPLE_INTERVAL":     "1500ms",
		"METRIC_EXPIRY_THRESHOLD":    "45s",
		"METRIC_UDP_LOSS_PENALTY":    "800",
		"METRIC_COST_CONFIRMATIONS":  "4",
		"METRIC_MIN_ROUTE_INTERVAL":  "20s",
		"CLICKHOUSE_ADDRS":           "ch-1:9000, ch-2:9000",
		"CLICKHOUSE_DATABASE":        "pathroute",
		"METRIC_DEFAULT_QUERY_LIMIT": "50",
		"METRIC_MAX_QUERY_LIMIT":     "500",
		"METRIC_MAX_REPORT_BATCH":    "250",
		"VIEW_SHUTDOWN_TIMEOUT":      "8s",
	}
	config, err := loadAppConfigFrom(func(key string) string { return values[key] })
	if err != nil {
		t.Fatalf("loadAppConfigFrom() error = %v", err)
	}
	if !config.Metric.Enabled || config.Metric.SampleInterval != 1500*time.Millisecond {
		t.Fatalf("dynamic config = %+v", config.Metric)
	}
	if config.Metric.ExpiryThreshold != 45*time.Second || config.Metric.CostConfirmations != 4 {
		t.Fatalf("metric overrides = %+v", config.Metric)
	}
	if len(config.ClickHouse.Addresses) != 2 || config.ClickHouse.Addresses[1] != "ch-2:9000" {
		t.Fatalf("clickhouse addresses = %v", config.ClickHouse.Addresses)
	}
	if config.MaxReportBatch != 250 || config.ShutdownTimeout != 8*time.Second {
		t.Fatalf("app limits = %+v", config)
	}
}

func TestLoadAppConfigRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := loadAppConfigFrom(func(key string) string {
		if key == "METRIC_AGGREGATION_WINDOW" {
			return "not-a-duration"
		}
		return ""
	})
	if err == nil || !strings.Contains(err.Error(), "METRIC_AGGREGATION_WINDOW") {
		t.Fatalf("loadAppConfigFrom() error = %v, want named invalid environment value", err)
	}
}
