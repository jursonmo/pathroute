package route

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jursonmo/pathroute/graph"
)

// Clock 为最小重算间隔提供可控时间。
type Clock interface {
	Now() time.Time
}

// GraphProvider 在同一数据库一致性视图中返回图和 cost revision。
type GraphProvider interface {
	BuildGraphWithRevision(ctx context.Context) (*graph.Graph, uint64, error)
}

// Trigger 描述一次待合并的路由重算事件。
type Trigger struct {
	Revision  uint64
	Emergency bool
}

// Config 保存最小重算间隔、时钟和后台错误回调。
type Config struct {
	Clock       Clock
	MinInterval time.Duration
	OnError     func(error)
}

// Coordinator 合并 cost/status 触发并原子发布完整路由结果。
type Coordinator struct {
	provider   GraphProvider
	calculator Calculator
	store      *ResultStore
	config     Config

	processMu      sync.Mutex
	lastCalculated time.Time
	pendingMu      sync.Mutex
	pending        Trigger
	hasPending     bool
	signal         chan struct{}
}

// NewCoordinator 创建动态路由重算协调器。
func NewCoordinator(
	provider GraphProvider,
	calculator Calculator,
	store *ResultStore,
	config Config,
) (*Coordinator, error) {
	if provider == nil || calculator == nil || store == nil || config.Clock == nil {
		return nil, errors.New("route: coordinator dependencies are required")
	}
	if config.MinInterval < 0 {
		return nil, errors.New("route: minimum interval must not be negative")
	}
	if config.OnError == nil {
		config.OnError = func(error) {}
	}
	return &Coordinator{
		provider:   provider,
		calculator: calculator,
		store:      store,
		config:     config,
		signal:     make(chan struct{}, 1),
	}, nil
}

// Trigger 合并待处理 revision；紧急标志在合并过程中不会丢失。
func (c *Coordinator) Trigger(trigger Trigger) {
	c.mergePending(trigger, true)
}

// Recalculate 尝试执行一次重算，普通事件在最小间隔内返回 executed=false。
func (c *Coordinator) Recalculate(ctx context.Context, trigger Trigger) (bool, error) {
	return c.calculate(ctx, trigger, false)
}

// CalculateNow 绕过最小间隔执行手动强制计算。
func (c *Coordinator) CalculateNow(ctx context.Context) (Snapshot, error) {
	if _, err := c.calculate(ctx, Trigger{Emergency: true}, true); err != nil {
		return Snapshot{}, err
	}
	snapshot, _ := c.store.Latest()
	return snapshot, nil
}

// Run 处理合并触发，并在普通最小间隔到期后执行最新 revision。
func (c *Coordinator) Run(ctx context.Context) error {
	var timer *time.Timer
	var timerChannel <-chan time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-c.signal:
			trigger, ok := c.takePending()
			if !ok {
				continue
			}
			executed, err := c.Recalculate(ctx, trigger)
			if err != nil {
				c.config.OnError(err)
			}
			if !executed && err == nil {
				c.mergePending(trigger, false)
				timer, timerChannel = resetTimer(timer, c.timeUntilAllowed())
			}
		case <-timerChannel:
			timerChannel = nil
			trigger, ok := c.takePending()
			if !ok {
				continue
			}
			executed, err := c.Recalculate(ctx, trigger)
			if err != nil {
				c.config.OnError(err)
			}
			if !executed && err == nil {
				c.mergePending(trigger, false)
				timer, timerChannel = resetTimer(timer, c.timeUntilAllowed())
			}
		}
	}
}

func (c *Coordinator) calculate(ctx context.Context, trigger Trigger, force bool) (bool, error) {
	c.processMu.Lock()
	defer c.processMu.Unlock()
	now := c.config.Clock.Now().UTC()
	insideMinimumInterval := !c.lastCalculated.IsZero() && now.Before(c.lastCalculated.Add(c.config.MinInterval))
	if !force && !trigger.Emergency && insideMinimumInterval {
		return false, nil
	}

	input, revision, err := c.provider.BuildGraphWithRevision(ctx)
	if err != nil {
		wrapped := fmt.Errorf("building route graph: %w", err)
		c.store.RecordFailure(wrapped)
		return false, wrapped
	}
	if trigger.Revision > 0 && revision < trigger.Revision {
		wrapped := fmt.Errorf("route: graph revision %d is older than trigger revision %d", revision, trigger.Revision)
		c.store.RecordFailure(wrapped)
		return false, wrapped
	}
	results, err := c.calculator.Calculate(input)
	if err != nil {
		wrapped := fmt.Errorf("calculating routes for revision %d: %w", revision, err)
		c.store.RecordFailure(wrapped)
		return false, wrapped
	}
	// 只有完整计算成功后才一次性替换结果，读请求永远看不到半批节点对。
	c.store.Publish(revision, now, results)
	c.lastCalculated = now
	return true, nil
}

func (c *Coordinator) mergePending(trigger Trigger, notify bool) {
	c.pendingMu.Lock()
	if !c.hasPending || trigger.Revision >= c.pending.Revision {
		c.pending.Revision = trigger.Revision
	}
	c.pending.Emergency = c.pending.Emergency || trigger.Emergency
	c.hasPending = true
	c.pendingMu.Unlock()
	if notify {
		select {
		case c.signal <- struct{}{}:
		default:
		}
	}
}

func (c *Coordinator) takePending() (Trigger, bool) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if !c.hasPending {
		return Trigger{}, false
	}
	trigger := c.pending
	c.pending = Trigger{}
	c.hasPending = false
	return trigger, true
}

func (c *Coordinator) timeUntilAllowed() time.Duration {
	c.processMu.Lock()
	defer c.processMu.Unlock()
	remaining := c.lastCalculated.Add(c.config.MinInterval).Sub(c.config.Clock.Now())
	if remaining < 0 {
		return 0
	}
	return remaining
}

func resetTimer(timer *time.Timer, duration time.Duration) (*time.Timer, <-chan time.Time) {
	if duration < time.Millisecond {
		duration = time.Millisecond
	}
	if timer == nil {
		timer = time.NewTimer(duration)
		return timer, timer.C
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
	return timer, timer.C
}
