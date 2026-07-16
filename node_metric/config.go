package nodemetric

import (
	"errors"
	"time"
)

const defaultFormulaVersion = "v1"

// Config 集中保存动态指标链路中会影响业务语义的参数。
type Config struct {
	Enabled             bool
	SampleInterval      time.Duration
	AggregationInterval time.Duration
	AggregationWindow   time.Duration
	ExpiryThreshold     time.Duration
	UDPLossPenalty      float64
	CostChangeThreshold float64
	CostConfirmations   int
	MinRouteInterval    time.Duration
	FormulaVersion      string
	MaxFutureSkew       time.Duration
	DefaultQueryLimit   int
	MaxQueryLimit       int
}

// DefaultConfig 返回首版经过确认的默认参数。
// 动态功能默认关闭，确保数据库迁移完成但 ClickHouse 尚未接入时仍可回退到静态路由。
func DefaultConfig() Config {
	return Config{
		Enabled:             false,
		SampleInterval:      2 * time.Second,
		AggregationInterval: 2 * time.Second,
		AggregationWindow:   10 * time.Second,
		ExpiryThreshold:     30 * time.Second,
		UDPLossPenalty:      1000,
		CostChangeThreshold: 0.10,
		CostConfirmations:   3,
		MinRouteInterval:    30 * time.Second,
		FormulaVersion:      defaultFormulaVersion,
		MaxFutureSkew:       5 * time.Minute,
		DefaultQueryLimit:   200,
		MaxQueryLimit:       1000,
	}
}

// Validate 在启动时拒绝无法产生确定业务行为的配置。
func (c Config) Validate() error {
	switch {
	case c.SampleInterval <= 0:
		return errors.New("node metric: sample interval must be positive")
	case c.AggregationInterval <= 0:
		return errors.New("node metric: aggregation interval must be positive")
	case c.AggregationWindow <= 0:
		return errors.New("node metric: aggregation window must be positive")
	case c.ExpiryThreshold <= 0:
		return errors.New("node metric: expiry threshold must be positive")
	case !isFiniteNonNegative(c.UDPLossPenalty):
		return errors.New("node metric: udp loss penalty must be finite and non-negative")
	case !isFinite(c.CostChangeThreshold) || c.CostChangeThreshold < 0 || c.CostChangeThreshold > 1:
		return errors.New("node metric: cost change threshold must be between 0 and 1")
	case c.CostConfirmations < 1:
		return errors.New("node metric: cost confirmations must be positive")
	case c.MinRouteInterval < 0:
		return errors.New("node metric: minimum route interval must not be negative")
	case c.FormulaVersion == "":
		return errors.New("node metric: formula version is required")
	case c.MaxFutureSkew < 0:
		return errors.New("node metric: maximum future skew must not be negative")
	case c.DefaultQueryLimit < 1:
		return errors.New("node metric: default query limit must be positive")
	case c.MaxQueryLimit < c.DefaultQueryLimit:
		return errors.New("node metric: maximum query limit must cover the default")
	default:
		return nil
	}
}
