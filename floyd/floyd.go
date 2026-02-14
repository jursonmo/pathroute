package floyd

import (
	"math"

	"github.com/jursonmo/pathroute/graph"
)

const Inf = math.MaxInt

const (
	MaxShortestPaths    = 4
	MaxViaNeighborPaths = 3
)

// PairResult holds shortest distance and up to MaxShortestPaths paths for one (From, To).
type PairResult struct {
	From     string     `json:"from"`
	To       string     `json:"to"`
	Distance int        `json:"distance"` // Inf or -1 for unreachable
	Paths    [][]string `json:"paths"`    // at most MaxShortestPaths
	// ViaNeighborPaths: paths S -> N -> ... -> D that do not contain S (except start); at most MaxViaNeighborPaths
	ViaNeighborPaths []PathDist `json:"via_neighbor_paths,omitempty"`
}

// PathDist is a path with its total distance.
type PathDist struct {
	Path     []string `json:"path"`
	Distance int      `json:"distance"`
}

// AllPairsResult holds results for all pairs and the graph (for via-neighbor computation).
type AllPairsResult struct {
	Results []PairResult
	g       *graph.Graph
	dist    [][]int
	pred    [][][]int // pred[i][j] = list of predecessors k on shortest i->j path (dist[i][k]+w(k,j)==dist[i][j])
}

// RunFloyd builds distance matrix and predecessor lists from g, then enumerates up to MaxShortestPaths per pair.
func RunFloyd(g *graph.Graph) *AllPairsResult {
	N := g.NumNodes()
	dist := make([][]int, N)
	for i := 0; i < N; i++ {
		dist[i] = make([]int, N)
		for j := 0; j < N; j++ {
			dist[i][j] = Inf
			if i == j {
				dist[i][j] = 0
			} else if w := g.Weight(i, j); w > 0 {
				dist[i][j] = w
			}
		}
	}
	for k := 0; k < N; k++ {
		for i := 0; i < N; i++ {
			if dist[i][k] == Inf {
				continue
			}
			for j := 0; j < N; j++ {
				if dist[k][j] == Inf {
					continue
				}
				d := dist[i][k] + dist[k][j]
				if d < dist[i][j] {
					dist[i][j] = d
				}
			}
		}
	}
	// Predecessors: pred[i][j] = list of m (m != i) such that edge (m,j) exists and dist[i][m]+w(m,j)==dist[i][j]
	// Exclude m==i to avoid cycles (i->i->j).
	pred := make([][][]int, N)
	for i := 0; i < N; i++ {
		pred[i] = make([][]int, N)
		for j := 0; j < N; j++ {
			if i == j || dist[i][j] == Inf {
				continue
			}
			for m := 0; m < N; m++ {
				if m == i {
					continue
				}
				w := g.Weight(m, j)
				if w > 0 && dist[i][m] != Inf && dist[i][m]+w == dist[i][j] {
					pred[i][j] = append(pred[i][j], m)
				}
			}
		}
	}
	// Build path list by backtracking: for i->j, paths go i -> ... -> m -> j for m in pred[i][j]
	// We need to enumerate paths. Use recursion: path from i to j = for each k in pred[i][j],
	// path(i,k) + path(k,j) with k not repeated in the middle. Actually pred[i][j] are predecessors of j,
	// so edge (k,j) is on shortest path. So dist[i][k] + w(k,j) = dist[i][j]. So path = path(i,k) + [j].
	// Recursively path(i,k) = for each pred of k, path(i, pred) + [k]. We need to avoid cycles; with
	// positive weights shortest paths are acyclic. So we can recursively enumerate and cap at 4.
	results := make([]PairResult, 0, N*N)
	for i := 0; i < N; i++ {
		for j := 0; j < N; j++ {
			pr := PairResult{
				From:     g.Name(i),
				To:       g.Name(j),
				Distance: dist[i][j],
				Paths:    nil,
			}
			if dist[i][j] == Inf {
				pr.Distance = -1
			} else {
				pr.Paths = enumeratePaths(g, dist, pred, i, j, MaxShortestPaths)
			}
			results = append(results, pr)
		}
	}
	return &AllPairsResult{Results: results, g: g, dist: dist, pred: pred}
}

// enumeratePaths returns up to maxPaths shortest paths from i to j using pred.
func enumeratePaths(g *graph.Graph, dist [][]int, pred [][][]int, i, j int, maxPaths int) [][]string {
	if i == j {
		return [][]string{{g.Name(i)}}
	}
	if dist[i][j] == Inf {
		return nil
	}
	var out [][]string
	seen := make(map[string]bool)
	collectPaths(g, dist, pred, i, j, []string{g.Name(j)}, &out, seen, maxPaths)
	return out
}

func collectPaths(g *graph.Graph, dist [][]int, pred [][][]int, i, j int, suffix []string, out *[][]string, seen map[string]bool, maxPaths int) {
	if len(*out) >= maxPaths {
		return
	}
	if i == j {
		path := make([]string, 0, len(suffix)+1)
		path = append(path, g.Name(i))
		path = append(path, suffix...)
		key := pathKey(path)
		if !seen[key] {
			seen[key] = true
			*out = append(*out, path)
		}
		return
	}
	// Direct edge (i,j): add path [i,j,...] if it is a shortest path (avoids cycle from pred with m==i).
	if w := g.Weight(i, j); w > 0 && w == dist[i][j] {
		path := make([]string, 0, len(suffix)+1)
		path = append(path, g.Name(i))
		path = append(path, suffix...)
		key := pathKey(path)
		if !seen[key] {
			seen[key] = true
			*out = append(*out, path)
		}
	}
	for _, m := range pred[i][j] {
		// path i->j = path(i,m) + [j]; recurse with tail [m,...,j] so output is [i,...,m,...,j]
		tail := append([]string{g.Name(m)}, suffix...)
		collectPaths(g, dist, pred, i, m, tail, out, seen, maxPaths)
	}
}

func pathKey(path []string) string {
	s := ""
	for _, p := range path {
		if s != "" {
			s += "|"
		}
		s += p
	}
	return s
}

// FillViaNeighborPaths computes for each pair (S,D) up to MaxViaNeighborPaths paths of the form
// S -> N -> ... -> D where N is an out-neighbor of S and the path N->...->D does not contain S.
func (r *AllPairsResult) FillViaNeighborPaths() {
	g := r.g
	N := g.NumNodes()
	for fromIdx := 0; fromIdx < N; fromIdx++ {
		neighbors := g.Neighbors(fromIdx)
		if len(neighbors) == 0 {
			continue
		}
		sub, oldToNew := g.CopyWithoutNode(fromIdx)
		subDist, subPred := runFloydOnSubgraph(sub)
		fromName := g.Name(fromIdx)
		for toIdx := 0; toIdx < N; toIdx++ {
			if toIdx == fromIdx {
				continue
			}
			toName := g.Name(toIdx)
			newTo := oldToNew[toIdx]
			if newTo < 0 {
				continue
			}
			var candidates []PathDist
			for _, nb := range neighbors {
				wSN := g.Weight(fromIdx, nb)
				newNb := oldToNew[nb]
				if newNb < 0 {
					continue
				}
				if subDist[newNb][newTo] == Inf {
					continue
				}
				d := wSN + subDist[newNb][newTo]
				paths := enumeratePathsOnSub(sub, subDist, subPred, newNb, newTo, MaxViaNeighborPaths)
				for _, p := range paths {
					fullPath := append([]string{fromName}, p...)
					candidates = append(candidates, PathDist{Path: fullPath, Distance: d})
				}
			}
			// Sort by distance and take up to MaxViaNeighborPaths unique paths (by path key)
			dedup := dedupPathsByKey(candidates, MaxViaNeighborPaths)
			// Find the PairResult for (fromName, toName)
			for i := range r.Results {
				if r.Results[i].From == fromName && r.Results[i].To == toName {
					r.Results[i].ViaNeighborPaths = dedup
					break
				}
			}
		}
	}
}

func runFloydOnSubgraph(g *graph.Graph) (dist [][]int, pred [][][]int) {
	n := g.NumNodes()
	dist = make([][]int, n)
	for i := 0; i < n; i++ {
		dist[i] = make([]int, n)
		for j := 0; j < n; j++ {
			dist[i][j] = Inf
			if i == j {
				dist[i][j] = 0
			} else if w := g.Weight(i, j); w > 0 {
				dist[i][j] = w
			}
		}
	}
	for k := 0; k < n; k++ {
		for i := 0; i < n; i++ {
			if dist[i][k] == Inf {
				continue
			}
			for j := 0; j < n; j++ {
				if dist[k][j] == Inf {
					continue
				}
				d := dist[i][k] + dist[k][j]
				if d < dist[i][j] {
					dist[i][j] = d
				}
			}
		}
	}
	pred = make([][][]int, n)
	for i := 0; i < n; i++ {
		pred[i] = make([][]int, n)
		for j := 0; j < n; j++ {
			if i == j || dist[i][j] == Inf {
				continue
			}
			for m := 0; m < n; m++ {
				if m == i {
					continue
				}
				w := g.Weight(m, j)
				if w > 0 && dist[i][m] != Inf && dist[i][m]+w == dist[i][j] {
					pred[i][j] = append(pred[i][j], m)
				}
			}
		}
	}
	return dist, pred
}

func enumeratePathsOnSub(g *graph.Graph, dist [][]int, pred [][][]int, i, j int, maxPaths int) [][]string {
	if i == j {
		return [][]string{{g.Name(i)}}
	}
	if dist[i][j] == Inf {
		return nil
	}
	var out [][]string
	seen := make(map[string]bool)
	collectPaths(g, dist, pred, i, j, []string{g.Name(j)}, &out, seen, maxPaths)
	return out
}

// dedupPathsByKey sorts by distance and returns up to max paths, deduplicated by path key.
func dedupPathsByKey(candidates []PathDist, max int) []PathDist {
	if len(candidates) == 0 {
		return nil
	}
	// simple sort by distance
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Distance < candidates[i].Distance {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}
	var result []PathDist
	seen := make(map[string]bool)
	for _, c := range candidates {
		if len(result) >= max {
			break
		}
		key := pathKey(c.Path)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, c)
	}
	return result
}
