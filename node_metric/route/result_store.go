package route

import (
	"sync"
	"time"

	"github.com/jursonmo/pathroute/floyd"
)

// Snapshot 是一次完整、对外可见的路由计算结果。
type Snapshot struct {
	Results      []floyd.PairResult `json:"results"`
	CostRevision uint64             `json:"cost_revision"`
	RouteVersion uint64             `json:"route_version"`
	CalculatedAt time.Time          `json:"calculated_at"`
	IsStale      bool               `json:"is_stale"`
	LastError    string             `json:"last_error,omitempty"`
}

// ResultStore 原子保存最近一次完整成功的路由结果。
type ResultStore struct {
	mu        sync.RWMutex
	snapshot  Snapshot
	available bool
	lastError string
}

// NewResultStore 创建最新路由结果仓储。
func NewResultStore() *ResultStore { return &ResultStore{} }

// Publish 原子替换完整结果并递增路由版本。
func (s *ResultStore) Publish(costRevision uint64, calculatedAt time.Time, results []floyd.PairResult) Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = Snapshot{
		Results:      cloneResults(results),
		CostRevision: costRevision,
		RouteVersion: s.snapshot.RouteVersion + 1,
		CalculatedAt: calculatedAt,
	}
	s.available = true
	s.lastError = ""
	return cloneSnapshot(s.snapshot)
}

// RecordFailure 保留上一份完整结果，仅附加陈旧和失败信息。
func (s *ResultStore) RecordFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = err.Error()
	if s.available {
		s.snapshot.IsStale = true
		s.snapshot.LastError = err.Error()
	}
}

// Latest 返回上一份完整结果；尚无成功结果时仍返回最近失败信息。
func (s *ResultStore) Latest() (Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.available {
		return Snapshot{Results: []floyd.PairResult{}, IsStale: s.lastError != "", LastError: s.lastError}, false
	}
	return cloneSnapshot(s.snapshot), true
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Results = cloneResults(snapshot.Results)
	return snapshot
}

func cloneResults(results []floyd.PairResult) []floyd.PairResult {
	cloned := make([]floyd.PairResult, len(results))
	for i, result := range results {
		cloned[i] = result
		cloned[i].Paths = clonePaths(result.Paths)
		cloned[i].ViaNeighborPaths = clonePaths(result.ViaNeighborPaths)
	}
	return cloned
}

func clonePaths(paths []floyd.PathDist) []floyd.PathDist {
	cloned := make([]floyd.PathDist, len(paths))
	for i, path := range paths {
		cloned[i] = path
		cloned[i].Path = append([]string{}, path.Path...)
	}
	return cloned
}
