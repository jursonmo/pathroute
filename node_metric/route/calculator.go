package route

import (
	"errors"

	"github.com/jursonmo/pathroute/floyd"
	"github.com/jursonmo/pathroute/graph"
)

// Calculator 使用一份完整图计算所有节点对的路由结果。
type Calculator interface {
	Calculate(graph *graph.Graph) ([]floyd.PairResult, error)
}

// FloydCalculator 适配项目现有 Floyd 实现。
type FloydCalculator struct{}

// NewFloydCalculator 创建 Floyd 路由计算器。
func NewFloydCalculator() *FloydCalculator { return &FloydCalculator{} }

// Calculate 计算所有节点对结果。
func (c *FloydCalculator) Calculate(input *graph.Graph) ([]floyd.PairResult, error) {
	if input == nil {
		return nil, errors.New("route: graph is required")
	}
	result := floyd.RunFloyd(input)
	result.FillViaNeighborPaths()
	return result.Results, nil
}
