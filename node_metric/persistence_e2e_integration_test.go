//go:build integration

package nodemetric_test

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jursonmo/pathroute/internal/viewdb"
	nodemetric "github.com/jursonmo/pathroute/node_metric"
	metricclickhouse "github.com/jursonmo/pathroute/node_metric/clickhouse"
	"github.com/jursonmo/pathroute/node_metric/httpapi"
	routeresult "github.com/jursonmo/pathroute/node_metric/route"
)

func TestPersistentPageToRouteEndToEnd(t *testing.T) {
	mysqlDSN := strings.TrimSpace(os.Getenv("PATHROUTE_E2E_MYSQL_DSN"))
	clickHouseAddress := strings.TrimSpace(os.Getenv("PATHROUTE_E2E_CLICKHOUSE_ADDR"))
	if mysqlDSN == "" || clickHouseAddress == "" {
		t.Skip("set PATHROUTE_E2E_MYSQL_DSN and PATHROUTE_E2E_CLICKHOUSE_ADDR to run persistent E2E")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	gdb, err := viewdb.OpenMySQL(mysqlDSN)
	if err != nil {
		t.Fatalf("OpenMySQL() error = %v", err)
	}
	mysqlDB, err := gdb.DB()
	if err != nil {
		t.Fatalf("mysql DB() error = %v", err)
	}
	defer mysqlDB.Close()

	clickHouseConfig := metricclickhouse.DefaultConfig()
	clickHouseConfig.Addresses = []string{clickHouseAddress}
	clickHouseConfig.Database = integrationEnv("PATHROUTE_E2E_CLICKHOUSE_DATABASE", clickHouseConfig.Database)
	clickHouseConfig.Username = integrationEnv("PATHROUTE_E2E_CLICKHOUSE_USERNAME", clickHouseConfig.Username)
	clickHouseConfig.Password = os.Getenv("PATHROUTE_E2E_CLICKHOUSE_PASSWORD")
	clickHouseDB, err := metricclickhouse.Open(ctx, clickHouseConfig)
	if err != nil {
		t.Fatalf("clickhouse Open() error = %v", err)
	}
	defer clickHouseDB.Close()
	if err := metricclickhouse.Migrate(ctx, clickHouseDB); err != nil {
		t.Fatalf("clickhouse Migrate() error = %v", err)
	}
	metricStore, err := metricclickhouse.NewStore(clickHouseDB, clickHouseConfig.QueryTimeout)
	if err != nil {
		t.Fatalf("clickhouse NewStore() error = %v", err)
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	fromNodeID := "E2E_A_" + suffix
	toNodeID := "E2E_B_" + suffix
	topologyStore := viewdb.NewStore(gdb, viewdb.WithDynamicCosts(true))
	if err := topologyStore.AddNode(ctx, viewdb.NodeDTO{NodeID: fromNodeID, Status: 1}); err != nil {
		t.Fatalf("AddNode(from) error = %v", err)
	}
	if err := topologyStore.AddNode(ctx, viewdb.NodeDTO{NodeID: toNodeID, Status: 1}); err != nil {
		t.Fatalf("AddNode(to) error = %v", err)
	}
	if err := topologyStore.AddEdge(ctx, viewdb.EdgeDTO{From: fromNodeID, To: toNodeID, Cost: 100, Status: 1}); err != nil {
		t.Fatalf("AddEdge() error = %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		cleanupError := func(resource string, err error) {
			if err != nil {
				t.Errorf("cleanup %s error = %v", resource, err)
			}
		}
		var revisions []uint64
		cleanupError("cost revisions", gdb.WithContext(cleanupCtx).
			Table("edge_cost_snapshots").
			Where("from_node_id = ? AND to_node_id = ?", fromNodeID, toNodeID).
			Pluck("revision", &revisions).Error)
		cleanupError("protocol costs", gdb.WithContext(cleanupCtx).Exec("DELETE FROM edge_protocol_cost_snapshots WHERE from_node_id = ? AND to_node_id = ?", fromNodeID, toNodeID).Error)
		cleanupError("edge costs", gdb.WithContext(cleanupCtx).Exec("DELETE FROM edge_cost_snapshots WHERE from_node_id = ? AND to_node_id = ?", fromNodeID, toNodeID).Error)
		for _, revision := range revisions {
			var remaining int64
			cleanupError("publication references", gdb.WithContext(cleanupCtx).
				Table("edge_cost_snapshots").Where("revision = ?", revision).Count(&remaining).Error)
			if remaining == 0 {
				cleanupError("cost publication", gdb.WithContext(cleanupCtx).
					Exec("DELETE FROM edge_cost_publications WHERE revision = ?", revision).Error)
			}
		}
		cleanupError("simulation", gdb.WithContext(cleanupCtx).Exec("DELETE FROM edge_metric_simulations WHERE from_node_id = ? AND to_node_id = ?", fromNodeID, toNodeID).Error)
		cleanupError("edge", gdb.WithContext(cleanupCtx).Exec("DELETE FROM graph_edges WHERE from_node_id = ? AND to_node_id = ?", fromNodeID, toNodeID).Error)
		cleanupError("nodes", gdb.WithContext(cleanupCtx).Exec("DELETE FROM graph_nodes WHERE node_id IN ?", []string{fromNodeID, toNodeID}).Error)
		_, clickHouseCleanupErr := clickHouseDB.ExecContext(
			cleanupCtx,
			"ALTER TABLE edge_metric_samples DELETE WHERE from_node_id = ? AND to_node_id = ? SETTINGS mutations_sync = 1",
			fromNodeID,
			toNodeID,
		)
		cleanupError("clickhouse metrics", clickHouseCleanupErr)
	}()

	simulationStore, err := viewdb.NewSimulationStore(gdb)
	if err != nil {
		t.Fatalf("NewSimulationStore() error = %v", err)
	}
	costStore, err := viewdb.NewCostStore(gdb)
	if err != nil {
		t.Fatalf("NewCostStore() error = %v", err)
	}
	clock := nodemetric.SystemClock{}
	validator, err := nodemetric.NewValidator(nodemetric.ValidationConfig{
		Clock:         clock,
		EdgeChecker:   topologyStore,
		MaxFutureSkew: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewValidator() error = %v", err)
	}
	ingestor, err := nodemetric.NewIngestService(clock, validator, metricStore)
	if err != nil {
		t.Fatalf("NewIngestService() error = %v", err)
	}
	source, err := nodemetric.NewRandomMetricSource(clock, rand.New(rand.NewSource(1)))
	if err != nil {
		t.Fatalf("NewRandomMetricSource() error = %v", err)
	}
	runner, err := nodemetric.NewSimulationRunner(
		simulationStore,
		source,
		ingestor,
		clock,
		nodemetric.SimulationRunnerConfig{ScanInterval: 2 * time.Second},
	)
	if err != nil {
		t.Fatalf("NewSimulationRunner() error = %v", err)
	}

	resultStore := routeresult.NewResultStore()
	coordinator, err := routeresult.NewCoordinator(
		topologyStore,
		routeresult.NewFloydCalculator(),
		resultStore,
		routeresult.Config{Clock: clock, MinInterval: 30 * time.Second},
	)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	udpCalculator, err := nodemetric.NewUDPCostCalculator(1000)
	if err != nil {
		t.Fatalf("NewUDPCostCalculator() error = %v", err)
	}
	expiry, err := nodemetric.NewExpiryPolicy(30 * time.Second)
	if err != nil {
		t.Fatalf("NewExpiryPolicy() error = %v", err)
	}
	stabilizer, err := nodemetric.NewCostStabilizer(nodemetric.StabilizerConfig{ChangeThreshold: 0.10, Confirmations: 3})
	if err != nil {
		t.Fatalf("NewCostStabilizer() error = %v", err)
	}
	worker, err := nodemetric.NewCostWorker(
		nodemetric.CostWorkerDependencies{
			Reader: metricStore,
			Store:  costStore,
			Clock:  clock,
			Calculators: map[nodemetric.Protocol]nodemetric.ProtocolCostCalculator{
				nodemetric.ProtocolTCP: nodemetric.NewTCPCostCalculator(),
				nodemetric.ProtocolUDP: udpCalculator,
			},
			Combiner: nodemetric.NewAverageEdgeCostCombiner(), Expiry: expiry, Stabilizer: stabilizer,
		},
		nodemetric.CostWorkerConfig{
			Interval: 2 * time.Second, Window: 10 * time.Second, FormulaVersion: "v1",
			OnPublished: func(publication nodemetric.CostPublication, bypassRouteDelay bool) {
				coordinator.Trigger(routeresult.Trigger{Revision: publication.Revision, Emergency: bypassRouteDelay})
			},
		},
	)
	if err != nil {
		t.Fatalf("NewCostWorker() error = %v", err)
	}

	mux := http.NewServeMux()
	simulationsAPI, err := httpapi.NewSimulations(simulationStore, 100)
	if err != nil {
		t.Fatalf("NewSimulations() error = %v", err)
	}
	simulationsAPI.Register(mux)
	configBody := fmt.Sprintf(
		`{"configs":[{"from_node_id":%q,"to_node_id":%q,"proto":"tcp","enabled":true,"interval_ms":2000,"latency_min_ms":30,"latency_max_ms":30,"packet_loss_min_ratio":0,"packet_loss_max_ratio":0,"rate_min_bps":1000000,"rate_max_bps":1000000}]}`,
		fromNodeID,
		toNodeID,
	)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/metric-simulations", strings.NewReader(configBody)))
	if response.Code != http.StatusNoContent {
		t.Fatalf("save page config status = %d body=%s", response.Code, response.Body.String())
	}

	var background sync.WaitGroup
	for _, run := range []func(context.Context) error{runner.Run, worker.Run, coordinator.Run} {
		background.Add(1)
		go func(run func(context.Context) error) {
			defer background.Done()
			_ = run(ctx)
		}(run)
	}

	// 首次模拟 tick 在 2 秒发生；若与聚合 tick 同时，下一次聚合会在 4 秒内发布并自动触发路由。
	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, available := resultStore.Latest()
		if available && persistentRouteExists(snapshot, fromNodeID, toNodeID, 30) {
			cancel()
			background.Wait()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	background.Wait()
	snapshot, available := resultStore.Latest()
	t.Fatalf("persistent E2E route not published: available=%v snapshot=%+v", available, snapshot)
}

func integrationEnv(key, defaultValue string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return defaultValue
}

func persistentRouteExists(snapshot routeresult.Snapshot, from, to string, distance int) bool {
	if snapshot.CostRevision == 0 {
		return false
	}
	for _, result := range snapshot.Results {
		if result.From == from && result.To == to && result.Distance == distance {
			return true
		}
	}
	return false
}
