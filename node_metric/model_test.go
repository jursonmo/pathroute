package nodemetric

import (
	"testing"
	"time"
)

func TestProtocolIsValid(t *testing.T) {
	tests := []struct {
		name     string
		protocol Protocol
		expected bool
	}{
		{name: "tcp", protocol: ProtocolTCP, expected: true},
		{name: "udp", protocol: ProtocolUDP, expected: true},
		{name: "unknown", protocol: Protocol("icmp"), expected: false},
		{name: "empty", protocol: ProtocolUnknown, expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.protocol.IsValid(); got != tt.expected {
				t.Fatalf("Protocol.IsValid() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestEdgeKeyString(t *testing.T) {
	t.Parallel()

	key := EdgeKey{FromNodeID: "A", ToNodeID: "B"}
	if got := key.String(); got != "A->B" {
		t.Fatalf("EdgeKey.String() = %q, want %q", got, "A->B")
	}
}

func TestEdgeMetricSampleIdentity(t *testing.T) {
	t.Parallel()

	sample := EdgeMetricSample{
		SourceID: "agent-a",
		Sequence: 42,
	}
	if got := sample.Identity(); got != "agent-a:42" {
		t.Fatalf("EdgeMetricSample.Identity() = %q, want %q", got, "agent-a:42")
	}
}

func TestDomainModelsKeepProtocolAndTimeDimensions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.July, 15, 12, 0, 0, 123000000, time.UTC)
	metric := EdgeMetricSample{
		EdgeKey:         EdgeKey{FromNodeID: "A", ToNodeID: "B"},
		Protocol:        ProtocolUDP,
		LatencyMS:       30,
		PacketLossRatio: 0.05,
		RateBPS:         1_000_000,
		ObservedAt:      now,
		ReceivedAt:      now.Add(time.Millisecond),
		SampleWindow:    2 * time.Second,
		Source:          "simulator",
		SourceID:        "sim-A-B-udp",
		Sequence:        1,
	}
	aggregated := AggregatedMetric{
		EdgeKey:               metric.EdgeKey,
		Protocol:              metric.Protocol,
		AverageLatencyMS:      metric.LatencyMS,
		AveragePacketLossRate: metric.PacketLossRatio,
		AverageRateBPS:        metric.RateBPS,
		SampleCount:           1,
		WindowStart:           now.Add(-10 * time.Second),
		WindowEnd:             now,
		LatestObservedAt:      now,
	}

	if aggregated.Protocol != ProtocolUDP || aggregated.EdgeKey != metric.EdgeKey {
		t.Fatalf("aggregated dimensions lost: %+v", aggregated)
	}
}
