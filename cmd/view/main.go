package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jursonmo/pathroute/internal/viewdb"
	nodemetric "github.com/jursonmo/pathroute/node_metric"
	metricclickhouse "github.com/jursonmo/pathroute/node_metric/clickhouse"
	"github.com/jursonmo/pathroute/node_metric/httpapi"
	routeresult "github.com/jursonmo/pathroute/node_metric/route"
)

//go:embed static/*
var staticFS embed.FS

func main() {
	config, err := loadAppConfig()
	if err != nil {
		log.Fatal("load config: ", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, config); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, config appConfig) (runErr error) {
	gdb, err := viewdb.OpenMySQL(config.MySQLDSN)
	if err != nil {
		return fmt.Errorf("connect mysql: %w", err)
	}
	mysqlDB, err := gdb.DB()
	if err != nil {
		return fmt.Errorf("get mysql connection pool: %w", err)
	}
	defer func() {
		if err := mysqlDB.Close(); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("close mysql: %w", err))
		}
	}()

	topologyStore := viewdb.NewStore(gdb, viewdb.WithDynamicCosts(config.Metric.Enabled))
	if config.SeedFromJSON {
		if err := topologyStore.SeedFromJSONIfEmpty(ctx, config.GraphJSONPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				log.Printf("seed skipped, file not found: %s", config.GraphJSONPath)
			} else {
				return fmt.Errorf("seed graph from json: %w", err)
			}
		}
	}

	simulationStore, err := viewdb.NewSimulationStore(gdb)
	if err != nil {
		return err
	}
	costStore, err := viewdb.NewCostStore(gdb)
	if err != nil {
		return err
	}
	clock := nodemetric.SystemClock{}
	resultStore := routeresult.NewResultStore()
	coordinator, err := routeresult.NewCoordinator(
		topologyStore,
		routeresult.NewFloydCalculator(),
		resultStore,
		routeresult.Config{
			Clock:       clock,
			MinInterval: config.Metric.MinRouteInterval,
			OnError:     func(err error) { log.Printf("route coordinator: %v", err) },
		},
	)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	if err := registerTopologyHandlers(mux, topologyStore, coordinator); err != nil {
		return fmt.Errorf("register topology handlers: %w", err)
	}
	routesAPI, err := httpapi.NewRoutes(resultStore, coordinator)
	if err != nil {
		return err
	}
	routesAPI.Register(mux)
	simulationAPI, err := httpapi.NewSimulations(simulationStore, config.Metric.MaxQueryLimit)
	if err != nil {
		return err
	}
	simulationAPI.Register(mux)
	edgeCostsAPI, err := httpapi.NewEdgeCosts(costStore)
	if err != nil {
		return err
	}
	edgeCostsAPI.Register(mux)

	backgroundCtx, cancelBackground := context.WithCancel(ctx)
	defer cancelBackground()
	var background sync.WaitGroup
	startBackground := func(name string, runner func(context.Context) error) {
		background.Add(1)
		go func() {
			defer background.Done()
			if err := runner(backgroundCtx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("%s stopped: %v", name, err)
			}
		}()
	}
	startBackground("route coordinator", coordinator.Run)

	var clickHouseDB *sql.DB
	if config.Metric.Enabled {
		clickHouseDB, err = metricclickhouse.Open(ctx, config.ClickHouse)
		if err != nil {
			cancelBackground()
			background.Wait()
			return fmt.Errorf("connect clickhouse: %w", err)
		}
		defer func() {
			if err := clickHouseDB.Close(); err != nil {
				runErr = errors.Join(runErr, fmt.Errorf("close clickhouse: %w", err))
			}
		}()
		if err := metricclickhouse.Migrate(ctx, clickHouseDB); err != nil {
			cancelBackground()
			background.Wait()
			return err
		}
		if err := assembleDynamicMetrics(
			mux,
			config,
			clock,
			topologyStore,
			simulationStore,
			costStore,
			clickHouseDB,
			coordinator,
			startBackground,
		); err != nil {
			cancelBackground()
			background.Wait()
			return err
		}
		log.Printf("dynamic metrics enabled, ClickHouse=%v", config.ClickHouse.Addresses)
	} else {
		log.Print("dynamic metrics disabled; routing uses graph_edges.cost")
	}

	// 首次启动立即生成一份完整路由；动态模式尚无快照时，可用边按 cost 1000 进入该图。
	coordinator.Trigger(routeresult.Trigger{Emergency: true})
	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("pathroute viewer listening on %s", config.ListenAddr)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		shutdownErr := server.Shutdown(shutdownCtx)
		cancelShutdown()
		if shutdownErr != nil {
			runErr = errors.Join(runErr, fmt.Errorf("shutdown HTTP server: %w", shutdownErr))
			// 优雅关闭超时后强制断开遗留连接，确保进程不会继续占用监听端口。
			if closeErr := server.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
				runErr = errors.Join(runErr, fmt.Errorf("force close HTTP server: %w", closeErr))
			}
		}
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr = fmt.Errorf("serve HTTP: %w", err)
		}
	}

	// 先停止并等待模拟、聚合、路由任务，之后 defer 才依次关闭 ClickHouse 和 MySQL 连接池。
	cancelBackground()
	background.Wait()
	return runErr
}

func assembleDynamicMetrics(
	mux *http.ServeMux,
	config appConfig,
	clock nodemetric.Clock,
	topologyStore *viewdb.Store,
	simulationStore *viewdb.SimulationStore,
	costStore *viewdb.CostStore,
	clickHouseDB *sql.DB,
	coordinator *routeresult.Coordinator,
	startBackground func(string, func(context.Context) error),
) error {
	metricStore, err := metricclickhouse.NewStore(clickHouseDB, config.ClickHouse.QueryTimeout)
	if err != nil {
		return err
	}
	validator, err := nodemetric.NewValidator(nodemetric.ValidationConfig{
		Clock:         clock,
		EdgeChecker:   topologyStore,
		MaxFutureSkew: config.Metric.MaxFutureSkew,
	})
	if err != nil {
		return err
	}
	ingestor, err := nodemetric.NewIngestService(clock, validator, metricStore)
	if err != nil {
		return err
	}
	metricsAPI, err := httpapi.NewMetrics(ingestor, metricStore, httpapi.MetricsConfig{
		DefaultQueryLimit: config.Metric.DefaultQueryLimit,
		MaxQueryLimit:     config.Metric.MaxQueryLimit,
		MaxReportBatch:    config.MaxReportBatch,
	})
	if err != nil {
		return err
	}
	metricsAPI.Register(mux)

	// 随机源只实现 MetricSource，并通过统一 IngestService 写入；未来真实探测器可以在此处直接替换。
	randomSource, err := nodemetric.NewRandomMetricSource(
		clock,
		rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // 模拟指标不用于安全用途。
	)
	if err != nil {
		return err
	}
	simulationRunner, err := nodemetric.NewSimulationRunner(
		simulationStore,
		randomSource,
		ingestor,
		clock,
		nodemetric.SimulationRunnerConfig{
			ScanInterval: config.Metric.SampleInterval,
			OnError:      func(err error) { log.Printf("metric simulator: %v", err) },
		},
	)
	if err != nil {
		return err
	}

	udpCalculator, err := nodemetric.NewUDPCostCalculator(config.Metric.UDPLossPenalty)
	if err != nil {
		return err
	}
	expiry, err := nodemetric.NewExpiryPolicy(config.Metric.ExpiryThreshold)
	if err != nil {
		return err
	}
	stabilizer, err := nodemetric.NewCostStabilizer(nodemetric.StabilizerConfig{
		ChangeThreshold: config.Metric.CostChangeThreshold,
		Confirmations:   config.Metric.CostConfirmations,
	})
	if err != nil {
		return err
	}
	costWorker, err := nodemetric.NewCostWorker(
		nodemetric.CostWorkerDependencies{
			Reader: metricStore,
			Store:  costStore,
			Clock:  clock,
			Calculators: map[nodemetric.Protocol]nodemetric.ProtocolCostCalculator{
				nodemetric.ProtocolTCP: nodemetric.NewTCPCostCalculator(),
				nodemetric.ProtocolUDP: udpCalculator,
			},
			Combiner:   nodemetric.NewAverageEdgeCostCombiner(),
			Expiry:     expiry,
			Stabilizer: stabilizer,
		},
		nodemetric.CostWorkerConfig{
			Interval:       config.Metric.AggregationInterval,
			Window:         config.Metric.AggregationWindow,
			FormulaVersion: config.Metric.FormulaVersion,
			OnPublished: func(publication nodemetric.CostPublication, bypassRouteDelay bool) {
				// MySQL publication 事务提交后才发送 revision，确保路由不会读取到半批 cost。
				// 首份动态 cost 与指标过期同样需要立即重算，避免继续使用静态 fallback。
				coordinator.Trigger(routeresult.Trigger{Revision: publication.Revision, Emergency: bypassRouteDelay})
			},
			OnError: func(err error) { log.Printf("cost worker: %v", err) },
		},
	)
	if err != nil {
		return err
	}

	startBackground("metric simulator", simulationRunner.Run)
	startBackground("cost worker", costWorker.Run)
	return nil
}
