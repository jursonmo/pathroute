package nodemetric

import (
	"math"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if cfg.Enabled {
		t.Fatal("DefaultConfig().Enabled = true, want false for safe migration")
	}
	if cfg.SampleInterval != 2*time.Second {
		t.Fatalf("SampleInterval = %s, want 2s", cfg.SampleInterval)
	}
	if cfg.AggregationWindow != 10*time.Second {
		t.Fatalf("AggregationWindow = %s, want 10s", cfg.AggregationWindow)
	}
	if cfg.ExpiryThreshold != 30*time.Second {
		t.Fatalf("ExpiryThreshold = %s, want 30s", cfg.ExpiryThreshold)
	}
	if cfg.UDPLossPenalty != 1000 {
		t.Fatalf("UDPLossPenalty = %v, want 1000", cfg.UDPLossPenalty)
	}
	if cfg.CostChangeThreshold != 0.10 {
		t.Fatalf("CostChangeThreshold = %v, want 0.10", cfg.CostChangeThreshold)
	}
	if cfg.CostConfirmations != 3 {
		t.Fatalf("CostConfirmations = %d, want 3", cfg.CostConfirmations)
	}
	if cfg.MinRouteInterval != 30*time.Second {
		t.Fatalf("MinRouteInterval = %s, want 30s", cfg.MinRouteInterval)
	}
	if cfg.FormulaVersion == "" {
		t.Fatal("FormulaVersion must not be empty")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("DefaultConfig().Validate() error = %v", err)
	}
}

func TestConfigValidateRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "sample interval", mutate: func(c *Config) { c.SampleInterval = 0 }},
		{name: "aggregation interval", mutate: func(c *Config) { c.AggregationInterval = 0 }},
		{name: "aggregation window", mutate: func(c *Config) { c.AggregationWindow = 0 }},
		{name: "expiry threshold", mutate: func(c *Config) { c.ExpiryThreshold = 0 }},
		{name: "udp loss penalty", mutate: func(c *Config) { c.UDPLossPenalty = -1 }},
		{name: "udp loss penalty nan", mutate: func(c *Config) { c.UDPLossPenalty = math.NaN() }},
		{name: "udp loss penalty infinite", mutate: func(c *Config) { c.UDPLossPenalty = math.Inf(1) }},
		{name: "cost threshold low", mutate: func(c *Config) { c.CostChangeThreshold = -0.1 }},
		{name: "cost threshold high", mutate: func(c *Config) { c.CostChangeThreshold = 1.1 }},
		{name: "cost threshold nan", mutate: func(c *Config) { c.CostChangeThreshold = math.NaN() }},
		{name: "cost threshold infinite", mutate: func(c *Config) { c.CostChangeThreshold = math.Inf(1) }},
		{name: "cost confirmations", mutate: func(c *Config) { c.CostConfirmations = 0 }},
		{name: "route interval", mutate: func(c *Config) { c.MinRouteInterval = -time.Second }},
		{name: "formula version", mutate: func(c *Config) { c.FormulaVersion = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := DefaultConfig()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Config.Validate() error = nil, want error")
			}
		})
	}
}
