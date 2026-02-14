package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/jursonmo/pathroute/floyd"
	"github.com/jursonmo/pathroute/graph"
)

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
		fmt.Printf("%s -> %s, distance: %d", pr.From, pr.To, pr.Distance)
		if len(pr.Paths) > 0 {
			fmt.Printf(", paths(%d):", len(pr.Paths))
			for _, p := range pr.Paths {
				fmt.Printf(" %v", p)
			}
		}
		fmt.Println()
		if len(pr.ViaNeighborPaths) > 0 {
			fmt.Printf("  via-neighbor paths(%d):", len(pr.ViaNeighborPaths))
			for _, v := range pr.ViaNeighborPaths {
				fmt.Printf(" %v %d", v.Path, v.Distance)
			}
			fmt.Println()
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
