package nodemetric

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// SimulationRunnerConfig 保存配置扫描周期和后台错误回调。
type SimulationRunnerConfig struct {
	ScanInterval time.Duration
	OnError      func(error)
}

// SimulationRunner 在服务端调度全部启用的随机指标配置。
type SimulationRunner struct {
	store       SimulationStore
	source      MetricSource
	ingestor    MetricIngestor
	clock       Clock
	config      SimulationRunnerConfig
	runMu       sync.Mutex
	lastAttempt map[string]time.Time
}

// NewSimulationRunner 创建一个单实例模拟任务调度器。
func NewSimulationRunner(
	store SimulationStore,
	source MetricSource,
	ingestor MetricIngestor,
	clock Clock,
	config SimulationRunnerConfig,
) (*SimulationRunner, error) {
	if store == nil || source == nil || ingestor == nil || clock == nil {
		return nil, errors.New("node metric: simulation runner dependencies are required")
	}
	if config.ScanInterval <= 0 {
		return nil, errors.New("node metric: simulation scan interval must be positive")
	}
	if config.OnError == nil {
		config.OnError = func(error) {}
	}
	return &SimulationRunner{
		store:       store,
		source:      source,
		ingestor:    ingestor,
		clock:       clock,
		config:      config,
		lastAttempt: map[string]time.Time{},
	}, nil
}

// Run 持续扫描配置，直到 context 被取消。
func (r *SimulationRunner) Run(ctx context.Context) error {
	timer := time.NewTimer(r.config.ScanInterval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			if err := r.RunOnce(ctx); err != nil {
				r.config.OnError(err)
			}
			if err := ctx.Err(); err != nil {
				return nil
			}
			delay, err := r.nextDelay(ctx)
			if err != nil {
				r.config.OnError(err)
				delay = r.config.ScanInterval
			}
			timer.Reset(delay)
		}
	}
}

// RunOnce 执行一次到期配置扫描；若上一次扫描仍在执行则直接合并本次触发。
func (r *SimulationRunner) RunOnce(ctx context.Context) error {
	if !r.runMu.TryLock() {
		return nil
	}
	defer r.runMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	configs, err := r.store.List(ctx)
	if err != nil {
		return fmt.Errorf("listing simulation configs: %w", err)
	}
	now := r.clock.Now().UTC()
	var runErrors []error
	for _, config := range configs {
		if !config.IsEnabled || !r.isDue(config, now) {
			continue
		}
		key := simulationScheduleKey(config)
		// 无论本周期生成或落库是否成功，都等到下一个配置周期再重试，避免存储故障造成忙循环。
		r.lastAttempt[key] = now
		sample, err := r.source.Generate(ctx, config)
		if err != nil {
			runErrors = append(runErrors, fmt.Errorf("generating simulation %s:%s: %w", config.EdgeKey.String(), config.Protocol, err))
			continue
		}
		accepted, err := r.ingestor.Ingest(ctx, []EdgeMetricSample{sample})
		if err != nil {
			// 写入失败时不排队缓存样本，下一个周期重新生成，避免 ClickHouse 故障导致内存无限增长。
			runErrors = append(runErrors, fmt.Errorf("ingesting simulation %s:%s: %w", config.EdgeKey.String(), config.Protocol, err))
			continue
		}
		if accepted != 1 {
			runErrors = append(runErrors, fmt.Errorf("ingesting simulation %s:%s: accepted %d samples", config.EdgeKey.String(), config.Protocol, accepted))
			continue
		}

		if err := r.store.UpdateRuntime(ctx, sample); err != nil {
			runErrors = append(runErrors, fmt.Errorf("updating simulation runtime %s:%s: %w", config.EdgeKey.String(), config.Protocol, err))
		}
	}
	return errors.Join(runErrors...)
}

func (r *SimulationRunner) isDue(config SimulationConfig, now time.Time) bool {
	key := simulationScheduleKey(config)
	lastGenerated, exists := r.lastAttempt[key]
	if !exists && config.LastGeneratedAt != nil {
		lastGenerated = config.LastGeneratedAt.UTC()
		exists = true
	}
	return !exists || !now.Before(lastGenerated.Add(config.Interval))
}

// nextDelay 计算最近一条启用配置的到期时间，同时用 ScanInterval 限制配置变更发现延迟。
func (r *SimulationRunner) nextDelay(ctx context.Context) (time.Duration, error) {
	r.runMu.Lock()
	defer r.runMu.Unlock()

	configs, err := r.store.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("listing simulation configs for next schedule: %w", err)
	}
	now := r.clock.Now().UTC()
	delay := r.config.ScanInterval
	for _, config := range configs {
		if !config.IsEnabled || config.Interval <= 0 {
			continue
		}
		key := simulationScheduleKey(config)
		lastGenerated, exists := r.lastAttempt[key]
		if !exists && config.LastGeneratedAt != nil {
			lastGenerated = config.LastGeneratedAt.UTC()
			exists = true
		}
		candidateDelay := time.Duration(0)
		if exists {
			candidateDelay = lastGenerated.Add(config.Interval).Sub(now)
		}
		if candidateDelay < time.Millisecond {
			candidateDelay = time.Millisecond
		}
		if candidateDelay < delay {
			delay = candidateDelay
		}
	}
	return delay, nil
}

func simulationScheduleKey(config SimulationConfig) string {
	return config.EdgeKey.String() + ":" + string(config.Protocol)
}
