package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jursonmo/pathroute/floyd"
	"github.com/jursonmo/pathroute/graph"
)

// formatPathWithCosts returns "[A-50->B-20->C] sum: 70" style string.
func formatPathWithCosts(g *graph.Graph, path []string, total int) string {
	if len(path) == 0 {
		return ""
	}
	if len(path) == 1 {
		return "[" + path[0] + "] sum: 0"
	}
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < len(path)-1; i++ {
		idxA, okA := g.Index(path[i])
		idxB, okB := g.Index(path[i+1])
		w := 0
		if okA && okB {
			w = g.Cost(idxA, idxB)
		}
		b.WriteString(path[i])
		b.WriteString("-")
		b.WriteString(strconv.Itoa(w))
		b.WriteString("-> ")
	}
	b.WriteString(path[len(path)-1])
	b.WriteString("] sum: ")
	b.WriteString(strconv.Itoa(total))
	return b.String()
}

func main() {
	dataPath := flag.String("data", "data/graph.json", "path to graph JSON file")
	outPath := flag.String("out", "", "optional path to write results JSON; stdout only if empty")
	flag.Parse()

	g, err := graph.NewFromJSON(*dataPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load graph: %v\n", err)
		os.Exit(1)
	}

	r := floyd.RunFloyd(g)
	r.FillViaNeighborPaths()

	// Print to stdout
	for _, pr := range r.Results {
		if pr.From == pr.To {
			continue
		}
		if pr.Distance < 0 {
			fmt.Printf("%s -> %s: no path\n", pr.From, pr.To)
			continue
		}
		fmt.Printf("%s -> %s", pr.From, pr.To)
		if len(pr.Paths) > 0 {
			fmt.Printf(", shortest distance: %d, paths (top 4, got %d):\n", pr.Paths[0].Distance, len(pr.Paths))
			for _, p := range pr.Paths {
				fmt.Printf("    %s\n", formatPathWithCosts(g, p.Path, p.Distance))
			}
		} else {
			fmt.Println()
		}
		if len(pr.ViaNeighborPaths) > 0 {
			fmt.Printf("  via-neighbor paths(%d):\n", len(pr.ViaNeighborPaths))
			for _, v := range pr.ViaNeighborPaths {
				fmt.Printf("    %s\n", formatPathWithCosts(g, v.Path, v.Distance))
			}
		}
	}

	if *outPath != "" {
		type outStruct struct {
			Pairs []floyd.PairResult `json:"pairs"`
		}
		enc := outStruct{Pairs: r.Results}
		data, err := json.MarshalIndent(enc, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "marshal results: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*outPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", *outPath, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Results written to %s\n", *outPath)
	}
}

/*
//运行结果：
go run cmd/main.go

A -> B, shortest distance: 50, paths (top 4, got 1):
    [A-50-> B] sum: 50
  via-neighbor paths(1):
    [A-50-> B] sum: 50
A -> C, shortest distance: 10, paths (top 4, got 2):
    [A-10-> C] sum: 10
    [A-50-> B-20-> C] sum: 70
  via-neighbor paths(2):
    [A-10-> C] sum: 10
    [A-50-> B-20-> C] sum: 70
A -> D, shortest distance: 20, paths (top 4, got 4):
    [A-10-> C-10-> D] sum: 20
    [A-15-> E-10-> D] sum: 25
    [A-50-> B-15-> D] sum: 65
    [A-50-> B-20-> C-10-> D] sum: 80
  via-neighbor paths(3):
    [A-10-> C-10-> D] sum: 20
    [A-15-> E-10-> D] sum: 25
    [A-50-> B-15-> D] sum: 65
A -> E, shortest distance: 15, paths (top 4, got 1):
    [A-15-> E] sum: 15
  via-neighbor paths(1):
    [A-15-> E] sum: 15
A -> F, shortest distance: 30, paths (top 4, got 4):
    [A-10-> C-10-> D-10-> F] sum: 30
    [A-15-> E-10-> D-10-> F] sum: 35
    [A-50-> B-15-> D-10-> F] sum: 75
    [A-50-> B-20-> C-10-> D-10-> F] sum: 90
  via-neighbor paths(3):
    [A-10-> C-10-> D-10-> F] sum: 30
    [A-15-> E-10-> D-10-> F] sum: 35
    [A-50-> B-15-> D-10-> F] sum: 75
B -> A, shortest distance: 80, paths (top 4, got 1):
    [B-80-> A] sum: 80
  via-neighbor paths(1):
    [B-80-> A] sum: 80
B -> C, shortest distance: 20, paths (top 4, got 2):
    [B-20-> C] sum: 20
    [B-80-> A-10-> C] sum: 90
  via-neighbor paths(2):
    [B-20-> C] sum: 20
    [B-80-> A-10-> C] sum: 90
B -> D, shortest distance: 15, paths (top 4, got 4):
    [B-15-> D] sum: 15
    [B-20-> C-10-> D] sum: 30
    [B-80-> A-10-> C-10-> D] sum: 100
    [B-80-> A-15-> E-10-> D] sum: 105
  via-neighbor paths(3):
    [B-15-> D] sum: 15
    [B-20-> C-10-> D] sum: 30
    [B-80-> A-10-> C-10-> D] sum: 100
B -> E, shortest distance: 95, paths (top 4, got 1):
    [B-80-> A-15-> E] sum: 95
  via-neighbor paths(1):
    [B-80-> A-15-> E] sum: 95
B -> F, shortest distance: 25, paths (top 4, got 4):
    [B-15-> D-10-> F] sum: 25
    [B-20-> C-10-> D-10-> F] sum: 40
    [B-80-> A-10-> C-10-> D-10-> F] sum: 110
    [B-80-> A-15-> E-10-> D-10-> F] sum: 115
  via-neighbor paths(3):
    [B-15-> D-10-> F] sum: 25
    [B-20-> C-10-> D-10-> F] sum: 40
    [B-80-> A-10-> C-10-> D-10-> F] sum: 110
C -> A: no path
C -> B: no path
C -> D, shortest distance: 10, paths (top 4, got 1):
    [C-10-> D] sum: 10
  via-neighbor paths(1):
    [C-10-> D] sum: 10
C -> E: no path
C -> F, shortest distance: 20, paths (top 4, got 1):
    [C-10-> D-10-> F] sum: 20
  via-neighbor paths(1):
    [C-10-> D-10-> F] sum: 20
D -> A: no path
D -> B: no path
D -> C: no path
D -> E: no path
D -> F, shortest distance: 10, paths (top 4, got 1):
    [D-10-> F] sum: 10
  via-neighbor paths(1):
    [D-10-> F] sum: 10
E -> A: no path
E -> B: no path
E -> C: no path
E -> D, shortest distance: 10, paths (top 4, got 1):
    [E-10-> D] sum: 10
  via-neighbor paths(1):
    [E-10-> D] sum: 10
E -> F, shortest distance: 20, paths (top 4, got 1):
    [E-10-> D-10-> F] sum: 20
  via-neighbor paths(1):
    [E-10-> D-10-> F] sum: 20
F -> A: no path
F -> B: no path
F -> C: no path
F -> D: no path
F -> E: no path
*/
