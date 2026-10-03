package experiment

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mldsa-jwt-benchmark/internal/profile"
	"mldsa-jwt-benchmark/internal/signing"
)

// TestDockerK6AnalysisSmoke is opt-in because it builds a real image, starts
// Docker, and invokes k6. Run it once before collecting research data:
//
//	RUN_DOCKER_K6_SMOKE=1 go test ./internal/experiment -run TestDockerK6AnalysisSmoke -v
func TestDockerK6AnalysisSmoke(t *testing.T) {
	if os.Getenv("RUN_DOCKER_K6_SMOKE") != "1" {
		t.Skip("set RUN_DOCKER_K6_SMOKE=1 to run the Docker/k6 research smoke test")
	}
	for _, name := range []string{"docker", "k6", "go"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Fatalf("required command %s is unavailable: %v", name, err)
		}
	}
	if output, err := exec.Command("docker", "info").CombinedOutput(); err != nil {
		t.Fatalf("Docker daemon is unavailable: %v: %s", err, output)
	}

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	config, err := profile.Load(filepath.Join(root, "config", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Docker Desktop cannot necessarily bind-mount the Go test runner's /tmp
	// namespace, so place generated smoke keys under the shared project root.
	config.KeyDir, err = os.MkdirTemp(root, ".smoke-keys-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(config.KeyDir) })
	if err := signing.GenerateFiles(config); err != nil {
		t.Fatal(err)
	}

	unique := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	image := "skripsi-jwt-smoke-" + unique + ":test"
	container := "skripsi-jwt-smoke-" + unique
	run := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v failed: %v\n%s", name, args, err, output)
		}
		return strings.TrimSpace(string(output))
	}

	run("docker", "build", "-t", image, ".")
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "-f", container).Run()
		_ = exec.Command("docker", "image", "rm", image).Run()
	})
	run("docker", "run", "-d", "--name", container, "-p", "127.0.0.1::8080",
		"--mount", "type=bind,src="+config.KeyDir+",dst=/keys,readonly", image)
	port := run("docker", "port", container, "8080/tcp")
	if index := strings.LastIndex(port, ":"); index >= 0 {
		port = port[index+1:]
	}
	if port == "" {
		t.Fatal("Docker did not publish the server port")
	}
	baseURL := "http://127.0.0.1:" + port
	client := &http.Client{Timeout: 5 * time.Second}
	if err := waitReady(client, baseURL); err != nil {
		logs, _ := exec.Command("docker", "logs", container).CombinedOutput()
		t.Fatalf("container did not become ready: %v\n%s", err, logs)
	}

	workspace := t.TempDir()
	rawDir := filepath.Join(workspace, "raw")
	outDir := filepath.Join(workspace, "processed")
	if err := os.MkdirAll(rawDir, 0755); err != nil {
		t.Fatal(err)
	}
	for index, operation := range []string{"issue", "verify"} {
		runID := "smoke-" + operation
		token, err := issueAndVerify(client, baseURL, "ES256", config.Subject(1))
		if err != nil {
			t.Fatal(err)
		}
		payloadBytes, jwtBytes, expiration, err := tokenInfo(token)
		if err != nil {
			t.Fatal(err)
		}
		csvPath := filepath.Join(rawDir, runID+".csv")
		logPath := filepath.Join(rawDir, runID+".log")
		logFile, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		args := []string{"run", "--quiet", "--log-format", "raw", "--out", "csv=" + csvPath,
			"-e", "ALG=ES256", "-e", "OPERATION=" + operation, "-e", "RUN_ID=" + runID,
			"-e", "BASE_URL=" + baseURL}
		if operation == "verify" {
			args = append(args, "-e", "TOKEN="+token)
		}
		args = append(args, "load/smoke.js")
		cmd := exec.Command("k6", args...)
		cmd.Dir = root
		cmd.Stdout, cmd.Stderr = logFile, logFile
		k6Err := cmd.Run()
		closeErr := logFile.Close()
		if k6Err != nil {
			contents, _ := os.ReadFile(logPath)
			t.Fatalf("k6 %s smoke failed: %v\n%s", operation, k6Err, contents)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		phases, err := parsePhases(logPath)
		if err != nil {
			t.Fatal(err)
		}
		successes, err := countSuccesses(csvPath)
		if err != nil {
			t.Fatal(err)
		}
		if successes != 1 {
			t.Fatalf("%s successes = %d, want 1", operation, successes)
		}
		metadata := RunMetadata{
			RunID: runID, ScheduleIndex: index,
			Scenario:           Scenario{Round: 1, Alg: "ES256", Operation: operation, TargetVU: 1},
			Status:             "eligible",
			StartedAt:          utcNow(),
			FinishedAt:         utcNow(),
			Phases:             phases,
			PayloadBytes:       payloadBytes,
			JWTBytes:           jwtBytes,
			EarliestTokenExp:   expiration,
			SuccessfulInWindow: successes,
		}
		encoded, err := json.MarshalIndent(metadata, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rawDir, runID+".json"), append(encoded, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}

	analyze := exec.Command("go", "run", "./cmd/analyze", "-raw", rawDir, "-out", outDir,
		"-exclusions", filepath.Join(workspace, "missing-exclusions.json"))
	analyze.Dir = root
	analyze.Env = append(os.Environ(), "GOCACHE="+filepath.Join(workspace, "go-cache"))
	output, err := analyze.CombinedOutput()
	if err != nil {
		t.Fatalf("analysis failed: %v\n%s", err, output)
	}
	contents, err := os.ReadFile(filepath.Join(outDir, "per_run.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "smoke-issue") || !strings.Contains(string(contents), "smoke-verify") {
		t.Fatalf("analysis omitted smoke runs:\n%s", contents)
	}
}
