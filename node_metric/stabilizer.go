package nodemetric

import (
	"errors"
	"math"
	"sync"
)

// StabilizerConfig 保存普通 cost 发布的相对阈值和连续确认次数。
type StabilizerConfig struct {
	ChangeThreshold float64
	Confirmations   int
}

// PublishDecision 描述候选 cost 是否已经满足发布条件。
type PublishDecision struct {
	ShouldPublish bool
	IsEmergency   bool
	// IsInitial 标识该边第一次产生动态 cost；路由应立即采用首份动态权重。
	IsInitial bool
}

type candidateState struct {
	direction int
	count     int
}

// CostStabilizer 为每条有向边独立累计普通 cost 变化确认次数。
type CostStabilizer struct {
	config     StabilizerConfig
	mu         sync.Mutex
	candidates map[string]candidateState
}

// NewCostStabilizer 创建 cost 防抖器。
func NewCostStabilizer(config StabilizerConfig) (*CostStabilizer, error) {
	if math.IsNaN(config.ChangeThreshold) || math.IsInf(config.ChangeThreshold, 0) ||
		config.ChangeThreshold < 0 || config.ChangeThreshold > 1 {
		return nil, errors.New("node metric: stabilizer threshold must be between 0 and 1")
	}
	if config.Confirmations < 1 {
		return nil, errors.New("node metric: stabilizer confirmations must be positive")
	}
	return &CostStabilizer{
		config:     config,
		candidates: map[string]candidateState{},
	}, nil
}

// Consider 比较候选值与当前已发布值，并更新该边的连续确认状态。
func (s *CostStabilizer) Consider(current *EdgeCostSnapshot, candidate EdgeCostSnapshot) PublishDecision {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := candidate.EdgeKey.String()
	if current == nil {
		delete(s.candidates, key)
		// 首份快照若已经是“指标全部过期”，仍属于紧急降级，路由不能等待普通最小间隔。
		isEmergency := candidate.IsDegraded && candidate.DegradeReason == DegradeReasonMetricsExpired
		return PublishDecision{ShouldPublish: true, IsEmergency: isEmergency, IsInitial: true}
	}
	becameExpired := candidate.IsDegraded &&
		candidate.DegradeReason == DegradeReasonMetricsExpired &&
		!current.IsDegraded
	if becameExpired {
		delete(s.candidates, key)
		return PublishDecision{ShouldPublish: true, IsEmergency: true}
	}

	delta := candidate.Cost - current.Cost
	denominator := math.Max(math.Abs(float64(current.Cost)), 1)
	changeRatio := math.Abs(float64(delta)) / denominator
	if delta == 0 || changeRatio < s.config.ChangeThreshold {
		delete(s.candidates, key)
		return PublishDecision{}
	}
	direction := 1
	if delta < 0 {
		direction = -1
	}
	state := s.candidates[key]
	if state.direction != direction {
		state = candidateState{direction: direction}
	}
	state.count++
	if state.count < s.config.Confirmations {
		s.candidates[key] = state
		return PublishDecision{}
	}
	delete(s.candidates, key)
	return PublishDecision{ShouldPublish: true}
}
