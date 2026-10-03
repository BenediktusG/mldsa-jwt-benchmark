package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"mldsa-jwt-benchmark/internal/experiment"
)

type check struct {
	Command  []string `json:"command"`
	ExitCode int      `json:"exit_code"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
}

type record struct {
	Timestamp    string  `json:"timestamp"`
	SourceSHA256 string  `json:"source_sha256"`
	ConfigSHA256 string  `json:"config_sha256"`
	GoVersion    string  `json:"go_version"`
	Checks       []check `json:"checks"`
}

func rootDir() (string, error) {
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

func execute(root string, arguments []string) check {
	cmd := exec.Command(arguments[0], arguments[1:]...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(os.TempDir(), "skripsi-go-cache"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			exitCode = exit.ExitCode()
		} else {
			exitCode = -1
			stderr.WriteString(err.Error())
		}
	}
	return check{Command: arguments, ExitCode: exitCode, Stdout: stdout.String(), Stderr: stderr.String()}
}

func run() error {
	root, err := rootDir()
	if err != nil {
		return err
	}
	sourceHash, err := experiment.SourceHash(root)
	if err != nil {
		return err
	}
	configHash, err := experiment.FileHash(filepath.Join(root, "config/config.json"))
	if err != nil {
		return err
	}
	goVersion := execute(root, []string{"go", "version"})
	report := record{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), SourceSHA256: sourceHash,
		ConfigSHA256: configHash, GoVersion: goVersion.Stdout, Checks: make([]check, 0, 3)}
	commands := [][]string{
		{"go", "test", "-race", "./..."},
		{"go", "vet", "./..."},
		{"k6", "inspect", "-e", "ALG=ES256", "-e", "OPERATION=issue", "-e", "TARGET_VU=1", "-e", "RUN_ID=validation", "-e", "METRICS_PATH=results/validation/inspect.metrics.json", "load/scenario.js"},
	}
	failed := false
	for _, command := range commands {
		result := execute(root, command)
		if result.ExitCode != 0 {
			failed = true
		}
		report.Checks = append(report.Checks, result)
	}
	target := filepath.Join(root, "results/validation")
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}
	path := filepath.Join(target, time.Now().UTC().Format("20060102T150405.000000000Z")+".json")
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0644); err != nil {
		return err
	}
	fmt.Println(path)
	if failed {
		return fmt.Errorf("validation checks failed; inspect %s", path)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
