package viewdb

import (
	"reflect"
	"strings"
	"testing"
)

func TestMetricModelsExposeExpectedTables(t *testing.T) {
	t.Parallel()

	models := MetricModels()
	if len(models) != 4 {
		t.Fatalf("MetricModels() length = %d, want 4", len(models))
	}

	tables := []string{
		models[0].(*EdgeMetricSimulationModel).TableName(),
		models[1].(*EdgeCostPublicationModel).TableName(),
		models[2].(*EdgeCostSnapshotModel).TableName(),
		models[3].(*EdgeProtocolCostSnapshotModel).TableName(),
	}
	expected := []string{
		"edge_metric_simulations",
		"edge_cost_publications",
		"edge_cost_snapshots",
		"edge_protocol_cost_snapshots",
	}
	for i := range tables {
		if tables[i] != expected[i] {
			t.Errorf("table[%d] = %q, want %q", i, tables[i], expected[i])
		}
	}
}

func TestMetricModelsDeclareBusinessUniqueKeys(t *testing.T) {
	t.Parallel()

	assertFieldsShareIndex(
		t,
		reflect.TypeOf(EdgeMetricSimulationModel{}),
		"uidx_edge_metric_simulation",
		"FromNodeID",
		"ToNodeID",
		"Protocol",
	)
	assertFieldsShareIndex(
		t,
		reflect.TypeOf(EdgeCostSnapshotModel{}),
		"uidx_edge_cost_snapshot",
		"FromNodeID",
		"ToNodeID",
	)
	assertFieldsShareIndex(
		t,
		reflect.TypeOf(EdgeProtocolCostSnapshotModel{}),
		"uidx_edge_protocol_cost_snapshot",
		"FromNodeID",
		"ToNodeID",
		"Protocol",
	)
}

func assertFieldsShareIndex(t *testing.T, typ reflect.Type, index string, fieldNames ...string) {
	t.Helper()

	for _, fieldName := range fieldNames {
		field, ok := typ.FieldByName(fieldName)
		if !ok {
			t.Fatalf("%s missing field %s", typ.Name(), fieldName)
		}
		if !strings.Contains(field.Tag.Get("gorm"), "uniqueIndex:"+index) {
			t.Errorf("%s.%s gorm tag = %q, want uniqueIndex:%s", typ.Name(), fieldName, field.Tag.Get("gorm"), index)
		}
	}
}
