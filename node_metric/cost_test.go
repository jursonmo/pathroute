package nodemetric

import (
	"math"
	"testing"
	"time"
)

func TestProtocolCostCalculators(t *testing.T) {
	tests := []struct {
		name       string
		calculator ProtocolCostCalculator
		metric     AggregatedMetric
		expected   int
	}{
		{
			name:       "tcp uses latency",
			calculator: NewTCPCostCalculator(),
			metric: AggregatedMetric{
				Protocol:         ProtocolTCP,
				AverageLatencyMS: 35.4,
			},
			expected: 35,
		},
		{
			name:       "udp adds packet loss penalty",
			calculator: mustUDPCalculator(t, 1000),
			metric: AggregatedMetric{
				Protocol:              ProtocolUDP,
				AverageLatencyMS:      30,
				AveragePacketLossRate: 0.05,
			},
			expected: 80,
		},
		{
			name:       "rounds half up",
			calculator: NewTCPCostCalculator(),
			metric: AggregatedMetric{
				Protocol:         ProtocolTCP,
				AverageLatencyMS: 35.5,
			},
			expected: 36,
		},
		{
			name:       "clamps low",
			calculator: NewTCPCostCalculator(),
			metric: AggregatedMetric{
				Protocol:         ProtocolTCP,
				AverageLatencyMS: 0,
			},
			expected: 1,
		},
		{
			name:       "clamps high",
			calculator: mustUDPCalculator(t, 1000),
			metric: AggregatedMetric{
				Protocol:              ProtocolUDP,
				AverageLatencyMS:      900,
				AveragePacketLossRate: 0.5,
			},
			expected: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.calculator.Calculate(tt.metric)
			if err != nil {
				t.Fatalf("Calculate() error = %v", err)
			}
			if got != tt.expected {
				t.Fatalf("Calculate() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestAverageEdgeCostCombiner(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 0, time.UTC)
	combiner := NewAverageEdgeCostCombiner()
	tcp := ProtocolCostSnapshot{AggregatedMetric: AggregatedMetric{Protocol: ProtocolTCP, LatestObservedAt: now}, Cost: 30}
	udp := ProtocolCostSnapshot{AggregatedMetric: AggregatedMetric{Protocol: ProtocolUDP, LatestObservedAt: now}, Cost: 70}

	both := combiner.Combine(EdgeCombineInput{
		EdgeKey:        EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		ProtocolCosts:  []ProtocolCostSnapshot{tcp, udp},
		CalculatedAt:   now,
		FormulaVersion: "v1",
	})
	if both.Cost != 50 || both.IsDegraded {
		t.Fatalf("both protocols = %+v, want cost 50 not degraded", both)
	}

	udp.IsExpired = true
	one := combiner.Combine(EdgeCombineInput{
		EdgeKey:        EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		ProtocolCosts:  []ProtocolCostSnapshot{tcp, udp},
		CalculatedAt:   now,
		FormulaVersion: "v1",
	})
	if one.Cost != 30 || one.IsDegraded {
		t.Fatalf("one valid protocol = %+v, want tcp cost 30", one)
	}

	tcp.IsExpired = true
	none := combiner.Combine(EdgeCombineInput{
		EdgeKey:        EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		ProtocolCosts:  []ProtocolCostSnapshot{tcp, udp},
		CalculatedAt:   now,
		FormulaVersion: "v1",
	})
	if none.Cost != 1000 || !none.IsDegraded || none.DegradeReason != DegradeReasonMetricsExpired {
		t.Fatalf("no valid protocols = %+v, want degraded cost 1000", none)
	}
}

func TestExpiryPolicyExpiresAtThreshold(t *testing.T) {
	t.Parallel()

	policy, err := NewExpiryPolicy(30 * time.Second)
	if err != nil {
		t.Fatalf("NewExpiryPolicy() error = %v", err)
	}
	now := time.Date(2026, time.July, 15, 12, 0, 30, 0, time.UTC)
	if policy.IsExpired(now, now.Add(-29*time.Second)) {
		t.Fatal("metric expired before threshold")
	}
	if !policy.IsExpired(now, now.Add(-30*time.Second)) {
		t.Fatal("metric must expire exactly at threshold")
	}
	if !policy.IsExpired(now, time.Time{}) {
		t.Fatal("missing metric time must be expired")
	}
}

func TestCostStabilizerRequiresThreeConfirmations(t *testing.T) {
	t.Parallel()

	stabilizer, err := NewCostStabilizer(StabilizerConfig{ChangeThreshold: 0.10, Confirmations: 3})
	if err != nil {
		t.Fatalf("NewCostStabilizer() error = %v", err)
	}
	current := &EdgeCostSnapshot{EdgeKey: EdgeKey{FromNodeID: "A", ToNodeID: "B"}, Cost: 100}

	if decision := stabilizer.Consider(current, EdgeCostSnapshot{EdgeKey: current.EdgeKey, Cost: 109}); decision.ShouldPublish {
		t.Fatal("change below threshold must not publish")
	}
	for confirmation := 1; confirmation <= 2; confirmation++ {
		decision := stabilizer.Consider(current, EdgeCostSnapshot{EdgeKey: current.EdgeKey, Cost: 115 + confirmation})
		if decision.ShouldPublish {
			t.Fatalf("confirmation %d published early", confirmation)
		}
	}
	decision := stabilizer.Consider(current, EdgeCostSnapshot{EdgeKey: current.EdgeKey, Cost: 118})
	if !decision.ShouldPublish || decision.IsEmergency {
		t.Fatalf("third confirmation decision = %+v, want ordinary publish", decision)
	}
}

func TestCostStabilizerPublishesInitialAndExpiryImmediately(t *testing.T) {
	t.Parallel()

	stabilizer, err := NewCostStabilizer(StabilizerConfig{ChangeThreshold: 0.10, Confirmations: 3})
	if err != nil {
		t.Fatalf("NewCostStabilizer() error = %v", err)
	}
	key := EdgeKey{FromNodeID: "A", ToNodeID: "B"}
	initial := stabilizer.Consider(nil, EdgeCostSnapshot{EdgeKey: key, Cost: 50})
	if !initial.ShouldPublish || initial.IsEmergency || !initial.IsInitial {
		t.Fatalf("initial decision = %+v, want initial immediate publish", initial)
	}
	emergency := stabilizer.Consider(
		&EdgeCostSnapshot{EdgeKey: key, Cost: 50},
		EdgeCostSnapshot{EdgeKey: key, Cost: 1000, IsDegraded: true, DegradeReason: DegradeReasonMetricsExpired},
	)
	if !emergency.ShouldPublish || !emergency.IsEmergency {
		t.Fatalf("expiry decision = %+v, want emergency publish", emergency)
	}
	initialExpiry := stabilizer.Consider(
		nil,
		EdgeCostSnapshot{EdgeKey: EdgeKey{FromNodeID: "C", ToNodeID: "D"}, Cost: 1000, IsDegraded: true, DegradeReason: DegradeReasonMetricsExpired},
	)
	if !initialExpiry.ShouldPublish || !initialExpiry.IsEmergency || !initialExpiry.IsInitial {
		t.Fatalf("initial expiry decision = %+v, want emergency publish", initialExpiry)
	}
}

func TestCostStabilizerRejectsNonFiniteThreshold(t *testing.T) {
	t.Parallel()

	for _, threshold := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := NewCostStabilizer(StabilizerConfig{ChangeThreshold: threshold, Confirmations: 3}); err == nil {
			t.Fatalf("NewCostStabilizer(%v) error = nil, want error", threshold)
		}
	}
}

func mustUDPCalculator(t *testing.T, penalty float64) ProtocolCostCalculator {
	t.Helper()
	calculator, err := NewUDPCostCalculator(penalty)
	if err != nil {
		t.Fatalf("NewUDPCostCalculator() error = %v", err)
	}
	return calculator
}
