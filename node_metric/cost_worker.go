package nodemetric

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// CostWorkerDependencies 集中声明聚合发布任务的可替换依赖。
type CostWorkerDependencies struct {
	Reader      MetricReader
	Store       CostSnapshotStore
	Clock       Clock
	Calculators map[Protocol]ProtocolCostCalculator
	Combiner    EdgeCostCombiner
	Expiry      *ExpiryPolicy
	Stabilizer  *CostStabilizer
}

// CostWorkerConfig 保存聚合周期、平滑窗口和发布回调。
type CostWorkerConfig struct {
	Interval       time.Duration
	Window         time.Duration
	FormulaVersion string
	// OnPublished 的 bool 参数表示本次 revision 是否应绕过路由最小重算间隔。
	OnPublished func(publication CostPublication, bypassRouteDelay bool)
	OnError     func(error)
}

// CostWorker 将 ClickHouse 窗口指标稳定发布为 MySQL 当前 cost 快照。
type CostWorker struct {
	dependencies CostWorkerDependencies
	config       CostWorkerConfig
	runMu        sync.Mutex
}

// NewCostWorker 创建单实例 cost 聚合发布任务。
func NewCostWorker(dependencies CostWorkerDependencies, config CostWorkerConfig) (*CostWorker, error) {
	if dependencies.Reader == nil || dependencies.Store == nil || dependencies.Clock == nil {
		return nil, errors.New("node metric: cost worker storage and clock are required")
	}
	if dependencies.Combiner == nil || dependencies.Expiry == nil || dependencies.Stabilizer == nil {
		return nil, errors.New("node metric: cost worker policies are required")
	}
	if dependencies.Calculators[ProtocolTCP] == nil || dependencies.Calculators[ProtocolUDP] == nil {
		return nil, errors.New("node metric: tcp and udp cost calculators are required")
	}
	if config.Interval <= 0 || config.Window <= 0 || config.FormulaVersion == "" {
		return nil, errors.New("node metric: invalid cost worker config")
	}
	if config.OnPublished == nil {
		config.OnPublished = func(CostPublication, bool) {}
	}
	if config.OnError == nil {
		config.OnError = func(error) {}
	}
	return &CostWorker{dependencies: dependencies, config: config}, nil
}

// Run 持续聚合并发布 cost，直到 context 被取消。
func (w *CostWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.config.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.RunOnce(ctx); err != nil {
				w.config.OnError(err)
			}
		}
	}
}

// RunOnce 执行一次聚合发布；若前一次仍在执行，则合并本次 tick。
func (w *CostWorker) RunOnce(ctx context.Context) error {
	if !w.runMu.TryLock() {
		return nil
	}
	defer w.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	now := w.dependencies.Clock.Now().UTC()
	windowStart := now.Add(-w.config.Window)
	aggregates, aggregateErr := w.dependencies.Reader.Aggregate(ctx, windowStart, now)
	if aggregateErr != nil {
		// 不使用可能不完整的查询结果；后续仅依据最后成功快照判断真实过期。
		aggregates = []AggregatedMetric{}
	}
	currentEdges, err := w.dependencies.Store.ListEdgeCosts(ctx)
	if err != nil {
		return errors.Join(aggregateErr, fmt.Errorf("listing current edge costs: %w", err))
	}

	candidates, err := w.buildCandidates(now, aggregates, currentEdges)
	if err != nil {
		return errors.Join(aggregateErr, err)
	}
	currentByKey := make(map[string]*EdgeCostSnapshot, len(currentEdges))
	for i := range currentEdges {
		currentByKey[currentEdges[i].EdgeKey.String()] = &currentEdges[i]
	}

	edgesToPublish := make([]EdgeCostSnapshot, 0, len(candidates))
	protocolsToPublish := []ProtocolCostSnapshot{}
	edgesToRefresh := make([]EdgeCostSnapshot, 0, len(candidates))
	protocolsToRefresh := []ProtocolCostSnapshot{}
	var isExpiryEmergency bool
	var bypassRouteDelay bool
	for _, candidate := range candidates {
		current := currentByKey[candidate.EdgeKey.String()]
		decision := w.dependencies.Stabilizer.Consider(current, candidate)
		if !decision.ShouldPublish {
			if aggregateErr == nil && current != nil && needsMetricRefresh(*current, candidate) {
				refreshed := metricRefreshSnapshot(*current, candidate)
				edgesToRefresh = append(edgesToRefresh, refreshed)
				protocolsToRefresh = append(protocolsToRefresh, refreshed.ProtocolCosts...)
			}
			continue
		}
		edgesToPublish = append(edgesToPublish, candidate)
		protocolsToPublish = append(protocolsToPublish, candidate.ProtocolCosts...)
		isExpiryEmergency = isExpiryEmergency || decision.IsEmergency
		bypassRouteDelay = bypassRouteDelay || decision.IsEmergency || decision.IsInitial
	}
	if len(edgesToRefresh) > 0 {
		// 指标 freshness 与路由权重 revision 分离：稳定 cost 仍刷新最新样本，但绝不绕过防抖改权重。
		if err := w.dependencies.Store.RefreshMetrics(ctx, protocolsToRefresh, edgesToRefresh); err != nil {
			return errors.Join(aggregateErr, fmt.Errorf("refreshing metric snapshots: %w", err))
		}
	}
	if len(edgesToPublish) == 0 {
		return aggregateErr
	}

	reason := "cost_changed"
	if isExpiryEmergency {
		reason = DegradeReasonMetricsExpired
	}
	publication, err := w.dependencies.Store.Publish(
		ctx,
		CostPublication{
			TriggerReason:  reason,
			ChangedEdges:   len(edgesToPublish),
			FormulaVersion: w.config.FormulaVersion,
			PublishedAt:    now,
		},
		protocolsToPublish,
		edgesToPublish,
	)
	if err != nil {
		return errors.Join(aggregateErr, fmt.Errorf("publishing edge costs: %w", err))
	}
	// revision 只有在 MySQL 事务提交成功后才交给路由协调器。
	w.config.OnPublished(publication, bypassRouteDelay)
	return aggregateErr
}

func metricRefreshSnapshot(current, candidate EdgeCostSnapshot) EdgeCostSnapshot {
	refreshed := candidate
	refreshed.Cost = current.Cost
	refreshed.Revision = current.Revision
	refreshed.PublishedAt = current.PublishedAt
	for i := range refreshed.ProtocolCosts {
		refreshed.ProtocolCosts[i].Revision = current.Revision
	}
	return refreshed
}

func needsMetricRefresh(current, candidate EdgeCostSnapshot) bool {
	if current.IsDegraded != candidate.IsDegraded || current.DegradeReason != candidate.DegradeReason ||
		!current.LatestMetricAt.Equal(candidate.LatestMetricAt) || len(current.ProtocolCosts) != len(candidate.ProtocolCosts) {
		return true
	}
	currentProtocols := make(map[Protocol]ProtocolCostSnapshot, len(current.ProtocolCosts))
	for _, protocol := range current.ProtocolCosts {
		currentProtocols[protocol.Protocol] = protocol
	}
	for _, candidateProtocol := range candidate.ProtocolCosts {
		currentProtocol, exists := currentProtocols[candidateProtocol.Protocol]
		if !exists || currentProtocol.Cost != candidateProtocol.Cost ||
			currentProtocol.IsExpired != candidateProtocol.IsExpired ||
			!currentProtocol.LatestObservedAt.Equal(candidateProtocol.LatestObservedAt) ||
			currentProtocol.AverageLatencyMS != candidateProtocol.AverageLatencyMS ||
			currentProtocol.AveragePacketLossRate != candidateProtocol.AveragePacketLossRate ||
			currentProtocol.AverageRateBPS != candidateProtocol.AverageRateBPS ||
			currentProtocol.SampleCount != candidateProtocol.SampleCount {
			return true
		}
	}
	return false
}

func (w *CostWorker) buildCandidates(
	now time.Time,
	aggregates []AggregatedMetric,
	currentEdges []EdgeCostSnapshot,
) ([]EdgeCostSnapshot, error) {
	keys := map[string]EdgeKey{}
	protocolsByEdge := map[string]map[Protocol]ProtocolCostSnapshot{}
	for _, edge := range currentEdges {
		key := edge.EdgeKey.String()
		keys[key] = edge.EdgeKey
		protocolsByEdge[key] = map[Protocol]ProtocolCostSnapshot{}
		for _, protocol := range edge.ProtocolCosts {
			protocol.IsExpired = w.dependencies.Expiry.IsExpired(now, protocol.LatestObservedAt)
			protocol.CalculatedAt = now
			protocolsByEdge[key][protocol.Protocol] = protocol
		}
	}

	for _, aggregate := range aggregates {
		calculator := w.dependencies.Calculators[aggregate.Protocol]
		if calculator == nil {
			return nil, fmt.Errorf("calculating unsupported protocol %q", aggregate.Protocol)
		}
		cost, err := calculator.Calculate(aggregate)
		if err != nil {
			return nil, fmt.Errorf("calculating %s cost for %s: %w", aggregate.Protocol, aggregate.EdgeKey.String(), err)
		}
		key := aggregate.EdgeKey.String()
		keys[key] = aggregate.EdgeKey
		if protocolsByEdge[key] == nil {
			protocolsByEdge[key] = map[Protocol]ProtocolCostSnapshot{}
		}
		protocolsByEdge[key][aggregate.Protocol] = ProtocolCostSnapshot{
			AggregatedMetric: aggregate,
			Cost:             cost,
			IsExpired:        w.dependencies.Expiry.IsExpired(now, aggregate.LatestObservedAt),
			FormulaVersion:   w.config.FormulaVersion,
			CalculatedAt:     now,
		}
	}

	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	sort.Strings(orderedKeys)
	candidates := make([]EdgeCostSnapshot, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		protocolMap := protocolsByEdge[key]
		protocols := make([]ProtocolCostSnapshot, 0, len(protocolMap))
		for _, protocol := range protocolMap {
			protocols = append(protocols, protocol)
		}
		sort.Slice(protocols, func(i, j int) bool {
			return protocols[i].Protocol < protocols[j].Protocol
		})
		candidate := w.dependencies.Combiner.Combine(EdgeCombineInput{
			EdgeKey:        keys[key],
			ProtocolCosts:  protocols,
			CalculatedAt:   now,
			FormulaVersion: w.config.FormulaVersion,
		})
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}
