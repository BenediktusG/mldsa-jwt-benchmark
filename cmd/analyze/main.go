package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"mldsa-jwt-benchmark/internal/analysis"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	raw := flag.String("raw", filepath.Join(root, "results/raw"), "k6 metric summaries and run metadata directory")
	out := flag.String("out", filepath.Join(root, "results/processed"), "reproducible output directory")
	exclusions := flag.String("exclusions", filepath.Join(root, "runner/exclusions.json"), "documented exclusion map")
	flag.Parse()
	result, err := analysis.Process(analysis.Options{Raw: *raw, Out: *out, Exclusions: *exclusions})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Printf("Processed %d eligible runs; %d scenario summaries\n", result.EligibleRuns, result.Summaries)
}
