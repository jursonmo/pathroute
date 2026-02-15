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

// formatPathWithWeights returns "[A-50->B-20->C] sum: 70" style string.
func formatPathWithWeights(g *graph.Graph, path []string, total int) string {
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
			w = g.Weight(idxA, idxB)
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
				fmt.Printf("    %s\n", formatPathWithWeights(g, p.Path, p.Distance))
			}
		} else {
			fmt.Println()
		}
		if len(pr.ViaNeighborPaths) > 0 {
			fmt.Printf("  via-neighbor paths(%d):\n", len(pr.ViaNeighborPaths))
			for _, v := range pr.ViaNeighborPaths {
				fmt.Printf("    %s\n", formatPathWithWeights(g, v.Path, v.Distance))
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
