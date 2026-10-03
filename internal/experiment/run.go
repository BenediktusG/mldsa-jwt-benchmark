package experiment

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mldsa-jwt-benchmark/internal/profile"
)

type RunOptions struct {
	Root       string
	Schedule   string
	ServerCPUs string
	StartIndex int
	Limit      int
	BaseURL    string
}

type RunMetadata struct {
	RunID              string            `json:"run_id"`
	ScheduleIndex      int               `json:"schedule_index"`
	Scenario           Scenario          `json:"scenario"`
	Environment        map[string]string `json:"environment"`
	Status             string            `json:"status"`
	Deviation          *string           `json:"deviation"`
	ExclusionReason    *string           `json:"exclusion_reason"`
	StartedAt          string            `json:"started_at"`
	FinishedAt         string            `json:"finished_at"`
	K6ExitCode         *int              `json:"k6_exit_code,omitempty"`
	Phases             map[string]int64  `json:"phases,omitempty"`
	PreparedTokenCount int               `json:"prepared_token_count"`
	EarliestTokenExp   int64             `json:"earliest_token_exp"`
	PayloadBytes       int               `json:"payload_bytes"`
	JWTBytes           int               `json:"jwt_bytes"`
	SuccessfulInWindow int               `json:"successful_in_window"`
}

func utcNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func runCommand(root string, env []string, output io.Writer, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	cmd.Env = env
	cmd.Stdout = output
	cmd.Stderr = output
	return cmd.Run()
}

func commandOutput(root string, env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	cmd.Env = env
	b, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(b))
	if err != nil {
		if output == "" {
			return "", fmt.Errorf("%s failed: %w", name, err)
		}
		return "", fmt.Errorf("%s failed: %w: %s", name, err, output)
	}
	return output, nil
}

func composeImage(root string, env []string) (imageName string, imageID string, err error) {
	images, err := commandOutput(root, env, "docker", "compose", "config", "--images")
	if err != nil {
		return "", "", err
	}
	imageNames := strings.Fields(images)
	if len(imageNames) != 1 {
		return "", "", fmt.Errorf("expected one Compose image, found %d", len(imageNames))
	}
	imageID, err = commandOutput(root, env, "docker", "image", "inspect", "--format", "{{.Id}}", imageNames[0])
	if err != nil {
		return "", "", err
	}
	if imageID == "" {
		return "", "", fmt.Errorf("empty image ID for %s", imageNames[0])
	}
	return imageNames[0], imageID, nil
}

func safeCommand(root string, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = root
	b, err := cmd.Output()
	if err != nil {
		return "unavailable"
	}
	return strings.TrimSpace(string(b))
}

func environmentInfo(root, serverCPUs string) (map[string]string, error) {
	configHash, err := FileHash(filepath.Join(root, "config/config.json"))
	if err != nil {
		return nil, err
	}
	sourceHash, err := SourceHash(root)
	if err != nil {
		return nil, err
	}
	composeHash, err := FileHash(filepath.Join(root, "compose.yaml"))
	if err != nil {
		return nil, err
	}
	scenarioHash, err := FileHash(filepath.Join(root, "load/scenario.js"))
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"go_version":      safeCommand(root, "go", "version"),
		"docker_version":  safeCommand(root, "docker", "version", "--format", "{{.Server.Version}}"),
		"k6_version":      safeCommand(root, "k6", "version"),
		"os":              safeCommand(root, "uname", "-a"),
		"cpu_model":       parseCPUModelName([]byte(safeCommand(root, "lscpu", "--json", "--extended=MODELNAME"))),
		"power_profile":   safeCommand(root, "powerprofilesctl", "get"),
		"git_revision":    safeCommand(root, "git", "rev-parse", "HEAD"),
		"server_cpuset":   serverCPUs,
		"k6_cpu_affinity": "unrestricted by experiment runner",
		"memory_limit":    "1g",
		"gomaxprocs":      "4",
		"godebug":         "fips140=off",
		"network":         "127.0.0.1:8080 Docker published port, no TLS or proxy",
		"config_sha256":   configHash,
		"source_sha256":   sourceHash,
		"compose_sha256":  composeHash,
		"scenario_sha256": scenarioHash,
	}, nil
}

func jsonRequest(client *http.Client, method, url string, body any, auth string, target any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("HTTP %d from %s", response.StatusCode, req.URL.Path)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 16000)).Decode(target)
}

func waitReady(client *http.Client, base string) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var response struct {
			Status string `json:"status"`
		}
		if err := jsonRequest(client, "GET", base+"/healthz", nil, "", &response); err == nil && response.Status == "ready" {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("server readiness timed out")
}

func issueAndVerify(client *http.Client, base, alg, sub string) (string, error) {
	var issued struct {
		Token string `json:"token"`
	}
	if err := jsonRequest(client, "POST", base+"/token?alg="+alg, map[string]string{"sub": sub}, "", &issued); err != nil {
		return "", err
	}
	if len(strings.Split(issued.Token, ".")) != 3 {
		return "", errors.New("issued token has invalid structure")
	}
	var verified struct {
		Status string `json:"status"`
	}
	if err := jsonRequest(client, "GET", base+"/protected?alg="+alg, nil, issued.Token, &verified); err != nil {
		return "", err
	}
	if verified.Status != "success" {
		return "", errors.New("prepared token failed verification")
	}
	return issued.Token, nil
}

func tokenInfo(compact string) (payloadBytes, jwtBytes int, exp int64, err error) {
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return 0, 0, 0, errors.New("invalid token")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, 0, 0, err
	}
	var payload struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		return 0, 0, 0, err
	}
	return len(b), len(compact), payload.Exp, nil
}

func prepare(client *http.Client, base string, run Scenario, config profile.Config) (
	tokens []string,
	payloadBytes int,
	jwtBytes int,
	earliestExpiration int64,
	err error,
) {
	count := 1
	if run.Operation == "verify" {
		count = run.TargetVU
	}
	for attempt := 0; attempt < 3; attempt++ {
		tokens := make([]string, count)
		var payloadSize, jwtSize int
		minExp := int64(^uint64(0) >> 1)
		for i := 1; i <= count; i++ {
			token, err := issueAndVerify(client, base, run.Alg, config.Subject(i))
			if err != nil {
				return nil, 0, 0, 0, err
			}
			p, j, exp, err := tokenInfo(token)
			if err != nil {
				return nil, 0, 0, 0, err
			}
			if i > 1 && (p != payloadSize || j != jwtSize) {
				return nil, 0, 0, 0, errors.New("payload or token length differs across VUs")
			}
			payloadSize, jwtSize = p, j
			if exp < minExp {
				minExp = exp
			}
			tokens[i-1] = token
		}
		if minExp-time.Now().Unix() >= 120 {
			if run.Operation == "issue" {
				tokens = nil
			}
			return tokens, payloadSize, jwtSize, minExp, nil
		}
	}
	return nil, 0, 0, 0, errors.New("prepared tokens have insufficient remaining lifetime")
}

func countSuccesses(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		return 0, err
	}
	metricColumn := -1
	for index, name := range header {
		if name == "metric_name" {
			metricColumn = index
		}
	}
	if metricColumn < 0 {
		return 0, errors.New("k6 CSV missing metric_name")
	}
	count := 0
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
		if metricColumn < len(row) && row[metricColumn] == "successful_in_window" {
			count++
		}
	}
	return count, nil
}

func parsePhases(path string) (map[string]int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if index := strings.Index(line, "PHASE_JSON:"); index >= 0 {
			var phase map[string]int64
			if err := json.Unmarshal([]byte(line[index+len("PHASE_JSON:"):]), &phase); err != nil {
				return nil, err
			}
			return phase, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("k6 phase marker missing")
}

func verifyRunInputs(root, schedule string, environment map[string]string, keys map[string]string, composeEnv []string) error {
	composeHash, err := FileHash(filepath.Join(root, "compose.yaml"))
	if err != nil || composeHash != environment["compose_sha256"] {
		return errors.New("Compose configuration changed during experiment")
	}
	scenarioHash, err := FileHash(filepath.Join(root, "load/scenario.js"))
	if err != nil || scenarioHash != environment["scenario_sha256"] {
		return errors.New("load scenario changed during experiment")
	}
	scheduleHash, err := FileHash(schedule)
	if err != nil || scheduleHash != environment["schedule_sha256"] {
		return errors.New("schedule changed during experiment")
	}
	for alg, original := range keys {
		current, err := FileHash(filepath.Join(root, "keys", alg+".json"))
		if err != nil || current != original {
			return fmt.Errorf("key pair changed during experiment: %s", alg)
		}
	}
	_, imageID, err := composeImage(root, composeEnv)
	if err != nil {
		return fmt.Errorf("inspect server image: %w", err)
	}
	if imageID != environment["server_image_id"] {
		return errors.New("server image changed during experiment")
	}
	return nil
}

func runOne(options RunOptions, plan Plan, index int, config profile.Config, environment map[string]string, keyHashes map[string]string) (string, error) {
	row := plan.Runs[index]
	runID := fmt.Sprintf("r%d-%s-%s-vu%04d-%s", row.Round, row.Alg, row.Operation, row.TargetVU, time.Now().UTC().Format("20060102T150405.000000000Z"))
	rawDir := filepath.Join(options.Root, "results/raw")
	if err := os.MkdirAll(rawDir, 0755); err != nil {
		return "", err
	}
	csvPath := filepath.Join(rawDir, runID+".csv")
	logPath := filepath.Join(rawDir, runID+".log")
	metadataPath := filepath.Join(rawDir, runID+".json")
	for _, path := range []string{csvPath, logPath, metadataPath} {
		if _, err := os.Stat(path); err == nil {
			return "", fmt.Errorf("raw file already exists: %s", path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	metadata := RunMetadata{RunID: runID, ScheduleIndex: index, Scenario: row, Environment: environment, Status: "review_required", StartedAt: utcNow()}
	composeEnv := append(os.Environ(), "SERVER_CPUSET="+options.ServerCPUs)
	client := &http.Client{Timeout: 30 * time.Second}
	var runErr error
	func() {
		if runErr = verifyRunInputs(options.Root, options.Schedule, environment, keyHashes, composeEnv); runErr != nil {
			return
		}
		if runErr = runCommand(options.Root, composeEnv, io.Discard, "docker", "compose", "up", "-d", "--force-recreate", "jwt"); runErr != nil {
			return
		}
		if runErr = waitReady(client, options.BaseURL); runErr != nil {
			return
		}
		var tokens []string
		tokens, metadata.PayloadBytes, metadata.JWTBytes, metadata.EarliestTokenExp, runErr = prepare(client, options.BaseURL, row, config)
		if runErr != nil {
			return
		}
		metadata.PreparedTokenCount = len(tokens)
		tokenFile, err := os.CreateTemp("", "skripsi-tokens-*.json")
		if err != nil {
			runErr = err
			return
		}
		defer os.Remove(tokenFile.Name())
		if err = json.NewEncoder(tokenFile).Encode(tokens); err != nil {
			tokenFile.Close()
			runErr = err
			return
		}
		if err = tokenFile.Close(); err != nil {
			runErr = err
			return
		}
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			runErr = err
			return
		}
		k6Env := append(os.Environ(),
			"ALG="+row.Alg, "OPERATION="+row.Operation, "TARGET_VU="+strconv.Itoa(row.TargetVU),
			"RUN_ID="+runID, "BASE_URL="+options.BaseURL, "TOKENS_FILE="+tokenFile.Name(),
			"SUBJECT_PREFIX="+config.SubjectPrefix, "SUBJECT_WIDTH="+strconv.Itoa(config.SubjectWidth))
		k6Err := runCommand(options.Root, k6Env, logFile, "k6", "run", "--quiet", "--log-format", "raw", "--out", "csv="+csvPath, "load/scenario.js")
		if err := logFile.Close(); err != nil && k6Err == nil {
			k6Err = err
		}
		exitCode := 0
		if k6Err != nil {
			var exit *exec.ExitError
			if errors.As(k6Err, &exit) {
				exitCode = exit.ExitCode()
			} else {
				exitCode = -1
			}
		}
		metadata.K6ExitCode = &exitCode
		metadata.Phases, err = parsePhases(logPath)
		if err != nil {
			runErr = err
			return
		}
		metadata.SuccessfulInWindow, err = countSuccesses(csvPath)
		if err != nil {
			runErr = err
			return
		}
		if k6Err != nil {
			runErr = fmt.Errorf("k6 failed: %w", k6Err)
			return
		}
		if metadata.SuccessfulInWindow == 0 {
			runErr = errors.New("zero successful requests; methodology review required")
			return
		}
		metadata.Status = "eligible"
	}()
	_ = runCommand(options.Root, composeEnv, io.Discard, "docker", "compose", "down")
	if runErr != nil {
		deviation := "execution failure: " + runErr.Error()
		metadata.Deviation = &deviation
	}
	metadata.FinishedAt = utcNow()
	b, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return metadata.Status, err
	}
	if err := os.WriteFile(metadataPath, append(b, '\n'), 0644); err != nil {
		return metadata.Status, err
	}
	return metadata.Status, runErr
}

func Run(options RunOptions) error {
	plan, err := ReadPlan(options.Schedule)
	if err != nil {
		return err
	}
	if err := CheckServerCPUAllocation(options.ServerCPUs); err != nil {
		return err
	}
	if options.StartIndex < 0 || options.Limit < 1 || options.StartIndex+options.Limit > len(plan.Runs) {
		return errors.New("invalid run range")
	}
	config, err := profile.Load(filepath.Join(options.Root, "config/config.json"))
	if err != nil {
		return err
	}
	environment, err := environmentInfo(options.Root, options.ServerCPUs)
	if err != nil {
		return err
	}
	environment["schedule_sha256"], err = FileHash(options.Schedule)
	if err != nil {
		return err
	}
	keyHashes := make(map[string]string, len(profile.Algorithms))
	for _, alg := range profile.Algorithms {
		keyHashes[alg], err = FileHash(filepath.Join(options.Root, "keys", alg+".json"))
		if err != nil {
			return err
		}
	}
	composeEnv := append(os.Environ(), "SERVER_CPUSET="+options.ServerCPUs)
	if err := runCommand(options.Root, composeEnv, os.Stdout, "docker", "compose", "build", "jwt"); err != nil {
		return err
	}
	imageName, imageID, err := composeImage(options.Root, composeEnv)
	if err != nil {
		return fmt.Errorf("inspect built server image: %w", err)
	}
	environment["server_image"] = imageName
	environment["server_image_id"] = imageID
	for index := options.StartIndex; index < options.StartIndex+options.Limit; index++ {
		row := plan.Runs[index]
		fmt.Printf("[%d/240] round=%d alg=%s operation=%s VU=%d\n", index+1, row.Round, row.Alg, row.Operation, row.TargetVU)
		status, err := runOne(options, plan, index, config, environment, keyHashes)
		if err != nil {
			return fmt.Errorf("run %d needs review (%s): %w", index+1, status, err)
		}
		if status != "eligible" {
			return fmt.Errorf("run %d needs review", index+1)
		}
	}
	return nil
}
