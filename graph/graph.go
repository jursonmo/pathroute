package graph

import (
	"encoding/json"
	"fmt"
	"os"
)

const (
	MinWeight = 1
	MaxWeight = 1000
)

// Edge represents a directed edge in the JSON input.
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Weight int    `json:"weight"`
}

// GraphJSON is the root structure for loading graph from JSON.
// Nodes are always []string for the algorithm. NewFromJSON accepts files where
// "nodes" is either ["A","B",...] or [{"id":"A","x":0,"y":0},...].
type GraphJSON struct {
	Nodes []string `json:"nodes"`
	Edges []Edge   `json:"edges"`
}

// nodeObject is used when parsing "nodes" as array of objects (id, optional x, y).
type nodeObject struct {
	ID string  `json:"id"`
	X  float64 `json:"x"`
	Y  float64 `json:"y"`
}

// rawGraphFile is used to parse the JSON file with flexible nodes format.
type rawGraphFile struct {
	Nodes json.RawMessage `json:"nodes"`
	Edges []Edge          `json:"edges"`
}

// Graph holds nodes and directed edges with weights.
type Graph struct {
	Nodes       []string
	NameToIndex map[string]int
	// AdjMatrix[i][j] = weight from node i to j; 0 means no edge (use Inf for unreachable in algo)
	AdjMatrix [][]int
}

// NewFromJSON loads a graph from a JSON file. Weights must be in [MinWeight, MaxWeight].
// If nodes is empty, nodes are inferred from edges.
// The "nodes" field may be either ["A","B",...] or [{"id":"A","x":0,"y":0},...]; x,y are ignored.
func NewFromJSON(path string) (*Graph, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw rawGraphFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	nodeIDs, err := parseNodeIDs(raw.Nodes)
	if err != nil {
		return nil, err
	}
	gj := &GraphJSON{Nodes: nodeIDs, Edges: raw.Edges}
	return NewFromStruct(gj)
}

// parseNodeIDs interprets raw (JSON array) as either []string or []nodeObject and returns node ids in order.
func parseNodeIDs(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err == nil {
		return ids, nil
	}
	var objs []nodeObject
	if err := json.Unmarshal(raw, &objs); err != nil {
		return nil, err
	}
	ids = make([]string, 0, len(objs))
	for _, o := range objs {
		ids = append(ids, o.ID)
	}
	return ids, nil
}

// NewFromStruct builds a Graph from GraphJSON. Validates weights in [1, 1000].
func NewFromStruct(gj *GraphJSON) (*Graph, error) {
	nodeSet := make(map[string]struct{})
	for _, n := range gj.Nodes {
		nodeSet[n] = struct{}{}
	}
	for _, e := range gj.Edges {
		nodeSet[e.From] = struct{}{}
		nodeSet[e.To] = struct{}{}
		if e.Weight < MinWeight || e.Weight > MaxWeight {
			return nil, fmt.Errorf("edge %s -> %s weight %d out of range [%d, %d]", e.From, e.To, e.Weight, MinWeight, MaxWeight)
		}
	}
	// stable order: first from Nodes, then any from edges
	nodes := make([]string, 0, len(nodeSet))
	seen := make(map[string]bool)
	for _, n := range gj.Nodes {
		if !seen[n] {
			seen[n] = true
			nodes = append(nodes, n)
		}
	}
	for _, e := range gj.Edges {
		for _, n := range []string{e.From, e.To} {
			if !seen[n] {
				seen[n] = true
				nodes = append(nodes, n)
			}
		}
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("graph has no nodes")
	}
	nameToIndex := make(map[string]int)
	for i, n := range nodes {
		nameToIndex[n] = i
	}
	N := len(nodes)
	adj := make([][]int, N)
	for i := range adj {
		adj[i] = make([]int, N)
	}
	for _, e := range gj.Edges {
		from, to := nameToIndex[e.From], nameToIndex[e.To]
		adj[from][to] = e.Weight
	}
	return &Graph{
		Nodes:       nodes,
		NameToIndex: nameToIndex,
		AdjMatrix:   adj,
	}, nil
}

// NumNodes returns the number of nodes.
func (g *Graph) NumNodes() int { return len(g.Nodes) }

// Index returns node index by name; ok is false if name not found.
func (g *Graph) Index(name string) (int, bool) {
	i, ok := g.NameToIndex[name]
	return i, ok
}

// Name returns node name by index.
func (g *Graph) Name(i int) string { return g.Nodes[i] }

// Weight returns the weight of edge from i to j; 0 means no edge.
func (g *Graph) Weight(i, j int) int { return g.AdjMatrix[i][j] }

// Neighbors returns out-neighbors of node index i (nodes j such that edge i->j exists).
func (g *Graph) Neighbors(i int) []int {
	var out []int
	for j := 0; j < len(g.AdjMatrix[i]); j++ {
		if g.AdjMatrix[i][j] > 0 {
			out = append(out, j)
		}
	}
	return out
}

// CopyWithoutNode returns a new graph with the same nodes and edges, but with node excludeIdx
// removed (smaller node set and reindexed). Used for G\S when computing via-neighbor paths.
// It also returns the new index mapping: newIndex[oldIndex] = new index, or -1 if excluded.
func (g *Graph) CopyWithoutNode(excludeIdx int) (*Graph, []int) {
	oldN := g.NumNodes()
	newNodes := make([]string, 0, oldN-1)
	oldToNew := make([]int, oldN)
	for i := 0; i < oldN; i++ {
		if i == excludeIdx {
			oldToNew[i] = -1
			continue
		}
		oldToNew[i] = len(newNodes)
		newNodes = append(newNodes, g.Nodes[i])
	}
	N := len(newNodes)
	adj := make([][]int, N)
	for i := range adj {
		adj[i] = make([]int, N)
	}
	for i := 0; i < oldN; i++ {
		if i == excludeIdx {
			continue
		}
		ni := oldToNew[i]
		for j := 0; j < oldN; j++ {
			if j == excludeIdx || g.AdjMatrix[i][j] == 0 {
				continue
			}
			nj := oldToNew[j]
			adj[ni][nj] = g.AdjMatrix[i][j]
		}
	}
	nameToIndex := make(map[string]int)
	for i, n := range newNodes {
		nameToIndex[n] = i
	}
	return &Graph{
		Nodes:       newNodes,
		NameToIndex: nameToIndex,
		AdjMatrix:   adj,
	}, oldToNew
}
