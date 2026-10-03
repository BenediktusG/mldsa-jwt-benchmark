package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"mldsa-jwt-benchmark/internal/experiment"
)

func projectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("project root not found")
		}
		dir = parent
	}
}

func resolve(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func run() error {
	root, err := projectRoot()
	if err != nil {
		return err
	}
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: go run ./cmd/experiment plan|run [options]")
	}
	switch os.Args[1] {
	case "plan":
		flags := flag.NewFlagSet("plan", flag.ContinueOnError)
		output := flags.String("output", "runner/schedule.json", "new schedule file")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 {
			return fmt.Errorf("unexpected plan arguments")
		}
		if err := experiment.WritePlan(resolve(root, *output), experiment.GeneratePlan()); err != nil {
			return err
		}
		fmt.Println(resolve(root, *output))
		return nil
	case "run":
		flags := flag.NewFlagSet("run", flag.ContinueOnError)
		schedule := flags.String("schedule", "runner/schedule.json", "240-run schedule")
		serverCPUs := flags.String("server-cpus", "", "four logical CPUs from two physical cores")
		startIndex := flags.Int("start-index", 0, "zero-based first schedule index")
		limit := flags.Int("limit", 240, "number of runs")
		baseURL := flags.String("base-url", "http://127.0.0.1:8080", "server URL")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *serverCPUs == "" {
			return fmt.Errorf("run requires --server-cpus")
		}
		return experiment.Run(experiment.RunOptions{Root: root, Schedule: resolve(root, *schedule), ServerCPUs: *serverCPUs, StartIndex: *startIndex, Limit: *limit, BaseURL: *baseURL})
	default:
		return fmt.Errorf("unknown action %q", os.Args[1])
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
