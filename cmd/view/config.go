package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	nodemetric "github.com/jursonmo/pathroute/node_metric"
	metricclickhouse "github.com/jursonmo/pathroute/node_metric/clickhouse"
)

const defaultMySQLDSN = "root:@tcp(127.0.0.1:3306)/pathroute?charset=utf8mb4&parseTime=True&loc=Local"

// appConfig 集中保存进程装配、数据库和动态指标配置。
type appConfig struct {
	ListenAddr      string
	MySQLDSN        string
	SeedFromJSON    bool
	GraphJSONPath   string
	Metric          nodemetric.Config
	ClickHouse      metricclickhouse.Config
	MaxReportBatch  int
	ShutdownTimeout time.Duration
}

func loadAppConfig() (appConfig, error) {
	return loadAppConfigFrom(os.Getenv)
}

func loadAppConfigFrom(get func(string) string) (appConfig, error) {
	metricConfig := nodemetric.DefaultConfig()
	clickHouseConfig := metricclickhouse.DefaultConfig()

	var err error
	if metricConfig.Enabled, err = parseBoolEnv(get, "DYNAMIC_METRICS_ENABLED", metricConfig.Enabled); err != nil {
		return appConfig{}, err
	}
	if metricConfig.SampleInterval, err = parseDurationEnv(get, "METRIC_SAMPLE_INTERVAL", metricConfig.SampleInterval); err != nil {
		return appConfig{}, err
	}
	if metricConfig.AggregationInterval, err = parseDurationEnv(get, "METRIC_AGGREGATION_INTERVAL", metricConfig.AggregationInterval); err != nil {
		return appConfig{}, err
	}
	if metricConfig.AggregationWindow, err = parseDurationEnv(get, "METRIC_AGGREGATION_WINDOW", metricConfig.AggregationWindow); err != nil {
		return appConfig{}, err
	}
	if metricConfig.ExpiryThreshold, err = parseDurationEnv(get, "METRIC_EXPIRY_THRESHOLD", metricConfig.ExpiryThreshold); err != nil {
		return appConfig{}, err
	}
	if metricConfig.UDPLossPenalty, err = parseFloatEnv(get, "METRIC_UDP_LOSS_PENALTY", metricConfig.UDPLossPenalty); err != nil {
		return appConfig{}, err
	}
	if metricConfig.CostChangeThreshold, err = parseFloatEnv(get, "METRIC_COST_CHANGE_THRESHOLD", metricConfig.CostChangeThreshold); err != nil {
		return appConfig{}, err
	}
	if metricConfig.CostConfirmations, err = parseIntEnv(get, "METRIC_COST_CONFIRMATIONS", metricConfig.CostConfirmations); err != nil {
		return appConfig{}, err
	}
	if metricConfig.MinRouteInterval, err = parseDurationEnv(get, "METRIC_MIN_ROUTE_INTERVAL", metricConfig.MinRouteInterval); err != nil {
		return appConfig{}, err
	}
	metricConfig.FormulaVersion = stringEnv(get, "METRIC_FORMULA_VERSION", metricConfig.FormulaVersion)
	if metricConfig.MaxFutureSkew, err = parseDurationEnv(get, "METRIC_MAX_FUTURE_SKEW", metricConfig.MaxFutureSkew); err != nil {
		return appConfig{}, err
	}
	if metricConfig.DefaultQueryLimit, err = parseIntEnv(get, "METRIC_DEFAULT_QUERY_LIMIT", metricConfig.DefaultQueryLimit); err != nil {
		return appConfig{}, err
	}
	if metricConfig.MaxQueryLimit, err = parseIntEnv(get, "METRIC_MAX_QUERY_LIMIT", metricConfig.MaxQueryLimit); err != nil {
		return appConfig{}, err
	}
	if err := metricConfig.Validate(); err != nil {
		return appConfig{}, fmt.Errorf("invalid dynamic metric configuration: %w", err)
	}

	if raw := strings.TrimSpace(get("CLICKHOUSE_ADDRS")); raw != "" {
		clickHouseConfig.Addresses = splitNonEmpty(raw)
	}
	clickHouseConfig.Database = stringEnv(get, "CLICKHOUSE_DATABASE", clickHouseConfig.Database)
	clickHouseConfig.Username = stringEnv(get, "CLICKHOUSE_USERNAME", clickHouseConfig.Username)
	clickHouseConfig.Password = strings.TrimSpace(get("CLICKHOUSE_PASSWORD"))
	if clickHouseConfig.DialTimeout, err = parseDurationEnv(get, "CLICKHOUSE_DIAL_TIMEOUT", clickHouseConfig.DialTimeout); err != nil {
		return appConfig{}, err
	}
	if clickHouseConfig.QueryTimeout, err = parseDurationEnv(get, "CLICKHOUSE_QUERY_TIMEOUT", clickHouseConfig.QueryTimeout); err != nil {
		return appConfig{}, err
	}
	if clickHouseConfig.MaxOpenConns, err = parseIntEnv(get, "CLICKHOUSE_MAX_OPEN_CONNS", clickHouseConfig.MaxOpenConns); err != nil {
		return appConfig{}, err
	}
	if clickHouseConfig.MaxIdleConns, err = parseIntEnv(get, "CLICKHOUSE_MAX_IDLE_CONNS", clickHouseConfig.MaxIdleConns); err != nil {
		return appConfig{}, err
	}
	if clickHouseConfig.ConnMaxLifetime, err = parseDurationEnv(get, "CLICKHOUSE_CONN_MAX_LIFETIME", clickHouseConfig.ConnMaxLifetime); err != nil {
		return appConfig{}, err
	}
	if err := clickHouseConfig.Validate(); err != nil {
		return appConfig{}, fmt.Errorf("invalid ClickHouse configuration: %w", err)
	}

	seedFromJSON, err := parseBoolEnv(get, "SEED_FROM_JSON", true)
	if err != nil {
		return appConfig{}, err
	}
	maxReportBatch, err := parseIntEnv(get, "METRIC_MAX_REPORT_BATCH", 1000)
	if err != nil {
		return appConfig{}, err
	}
	shutdownTimeout, err := parseDurationEnv(get, "VIEW_SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return appConfig{}, err
	}
	if maxReportBatch < 1 {
		return appConfig{}, fmt.Errorf("METRIC_MAX_REPORT_BATCH must be positive")
	}
	if shutdownTimeout <= 0 {
		return appConfig{}, fmt.Errorf("VIEW_SHUTDOWN_TIMEOUT must be positive")
	}

	return appConfig{
		ListenAddr:      stringEnv(get, "VIEW_LISTEN_ADDR", ":8080"),
		MySQLDSN:        stringEnv(get, "MYSQL_DSN", defaultMySQLDSN),
		SeedFromJSON:    seedFromJSON,
		GraphJSONPath:   stringEnv(get, "GRAPH_JSON_PATH", "data/graph.json"),
		Metric:          metricConfig,
		ClickHouse:      clickHouseConfig,
		MaxReportBatch:  maxReportBatch,
		ShutdownTimeout: shutdownTimeout,
	}, nil
}

func stringEnv(get func(string) string, key, defaultValue string) string {
	if value := strings.TrimSpace(get(key)); value != "" {
		return value
	}
	return defaultValue
}

func parseBoolEnv(get func(string) string, key string, defaultValue bool) (bool, error) {
	raw := strings.ToLower(strings.TrimSpace(get(key)))
	if raw == "" {
		return defaultValue, nil
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be a boolean", key)
	}
}

func parseDurationEnv(get func(string) string, key string, defaultValue time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(get(key))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a Go duration: %w", key, err)
	}
	return value, nil
}

func parseFloatEnv(get func(string) string, key string, defaultValue float64) (float64, error) {
	raw := strings.TrimSpace(get(key))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number: %w", key, err)
	}
	return value, nil
}

func parseIntEnv(get func(string) string, key string, defaultValue int) (int, error) {
	raw := strings.TrimSpace(get(key))
	if raw == "" {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return value, nil
}

func splitNonEmpty(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}
