package analysis

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mldsa-jwt-benchmark/internal/experiment"
)

var Metrics = []string{"throughput_rps", "mean_ms", "p99_ms"}

type RunResult struct {
	RunID                     string
	Round                     int
	Alg                       string
	Operation                 string
	TargetVU                  int
	SuccessfulInWindow        int
	SuccessfulStartedInWindow int
	ThroughputRPS             float64
	MeanMS                    *float64
	P99MS                     *float64
	PayloadBytes              int
	JWTBytes                  int
}

type Stat struct {
	N    int
	Mean *float64
	SD   *float64
	CV   *float64
}

type Summary struct {
	Operation    string
	TargetVU     int
	Alg          string
	Runs         int
	PayloadBytes int
	JWTBytes     int
	Stats        map[string]Stat
}

type Comparison struct {
	Operation string
	TargetVU  int
	MLDSA     string
	ECDSA     string
	Values    map[string][3]*float64 // ML-DSA mean, ECDSA mean, relative difference percent
}

func ptr(value float64) *float64 { return &value }

func CalculateRun(metricsPath string, metadata experiment.RunMetadata) (RunResult, error) {
	startWindow := metadata.Phases["measure_start_ms"]
	endWindow := metadata.Phases["measure_end_ms"]
	graceEnd := metadata.Phases["grace_end_ms"]
	if endWindow-startWindow != 60000 || graceEnd-endWindow != 30000 {
		return RunResult{}, errors.New("unexpected phase duration")
	}
	summary, err := experiment.ReadMetricSummary(metricsPath)
	if err != nil {
		return RunResult{}, err
	}
	if summary.RunID != metadata.RunID || summary.Alg != metadata.Scenario.Alg ||
		summary.Operation != metadata.Scenario.Operation || summary.TargetVU != metadata.Scenario.TargetVU {
		return RunResult{}, errors.New("metric summary does not match metadata")
	}
	if summary.SuccessfulInWindow != metadata.SuccessfulInWindow {
		return RunResult{}, errors.New("metric summary and metadata counts disagree")
	}
	result := RunResult{
		RunID: metadata.RunID, Round: metadata.Scenario.Round, Alg: metadata.Scenario.Alg,
		Operation: metadata.Scenario.Operation, TargetVU: metadata.Scenario.TargetVU,
		SuccessfulInWindow: summary.SuccessfulInWindow, SuccessfulStartedInWindow: summary.SuccessfulStartedInWindow,
		ThroughputRPS: float64(summary.SuccessfulInWindow) / 60, MeanMS: summary.MeanMS, P99MS: summary.P99MS,
		PayloadBytes: metadata.PayloadBytes, JWTBytes: metadata.JWTBytes,
	}
	return result, nil
}

func metricValue(run RunResult, metric string) (float64, bool) {
	switch metric {
	case "throughput_rps":
		return run.ThroughputRPS, true
	case "mean_ms":
		if run.MeanMS != nil {
			return *run.MeanMS, true
		}
	case "p99_ms":
		if run.P99MS != nil {
			return *run.P99MS, true
		}
	}
	return 0, false
}

func stat(values []float64) Stat {
	result := Stat{N: len(values)}
	if len(values) == 0 {
		return result
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	mean := sum / float64(len(values))
	result.Mean = ptr(mean)
	if len(values) >= 2 {
		variance := 0.0
		for _, value := range values {
			variance += (value - mean) * (value - mean)
		}
		sd := math.Sqrt(variance / float64(len(values)-1))
		result.SD = ptr(sd)
		if mean != 0 {
			result.CV = ptr(sd / mean)
		}
	}
	return result
}

func Summarize(runs []RunResult) ([]Summary, error) {
	groups := make(map[string][]RunResult)
	for _, run := range runs {
		key := fmt.Sprintf("%s/%04d/%s", run.Operation, run.TargetVU, run.Alg)
		groups[key] = append(groups[key], run)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	output := make([]Summary, 0, len(keys))
	for _, key := range keys {
		items := groups[key]
		first := items[0]
		summary := Summary{Operation: first.Operation, TargetVU: first.TargetVU, Alg: first.Alg,
			Runs: len(items), PayloadBytes: first.PayloadBytes, JWTBytes: first.JWTBytes, Stats: make(map[string]Stat)}
		for _, item := range items {
			if item.PayloadBytes != first.PayloadBytes || item.JWTBytes != first.JWTBytes {
				return nil, fmt.Errorf("token size changed across repetitions: %s", key)
			}
		}
		for _, metric := range Metrics {
			values := make([]float64, 0, len(items))
			for _, item := range items {
				if value, ok := metricValue(item, metric); ok {
					values = append(values, value)
				}
			}
			summary.Stats[metric] = stat(values)
		}
		output = append(output, summary)
	}
	return output, nil
}

func Compare(summaries []Summary) []Comparison {
	lookup := make(map[string]Summary)
	for _, s := range summaries {
		lookup[fmt.Sprintf("%s/%d/%s", s.Operation, s.TargetVU, s.Alg)] = s
	}
	pairs := [][2]string{{"ML-DSA-44", "ES256"}, {"ML-DSA-65", "ES384"}, {"ML-DSA-87", "ES512"}}
	output := make([]Comparison, 0)
	for _, operation := range []string{"issue", "verify"} {
		for _, vu := range experiment.VUs {
			for _, pair := range pairs {
				a, existsA := lookup[fmt.Sprintf("%s/%d/%s", operation, vu, pair[0])]
				b, existsB := lookup[fmt.Sprintf("%s/%d/%s", operation, vu, pair[1])]
				if !existsA || !existsB {
					continue
				}
				c := Comparison{Operation: operation, TargetVU: vu, MLDSA: pair[0], ECDSA: pair[1], Values: make(map[string][3]*float64)}
				for _, metric := range Metrics {
					av, bv := a.Stats[metric].Mean, b.Stats[metric].Mean
					var difference *float64
					if av != nil && bv != nil && *bv != 0 {
						difference = ptr((*av - *bv) / *bv * 100)
					}
					c.Values[metric] = [3]*float64{av, bv, difference}
				}
				output = append(output, c)
			}
		}
	}
	return output
}

type Options struct{ Raw, Out, Exclusions string }

type ProcessResult struct {
	EligibleRuns int
	Summaries    int
}

func Process(options Options) (ProcessResult, error) {
	if err := os.MkdirAll(options.Out, 0755); err != nil {
		return ProcessResult{}, err
	}
	exclusions := make(map[string]string)
	if b, err := os.ReadFile(options.Exclusions); err == nil {
		if err := json.Unmarshal(b, &exclusions); err != nil {
			return ProcessResult{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return ProcessResult{}, err
	}
	for runID, reason := range exclusions {
		if runID == "" || strings.TrimSpace(reason) == "" {
			return ProcessResult{}, errors.New("exclusions must map run IDs to nonempty reasons")
		}
	}
	files, err := filepath.Glob(filepath.Join(options.Raw, "*.json"))
	if err != nil {
		return ProcessResult{}, err
	}
	sort.Strings(files)
	runs := make([]RunResult, 0, len(files))
	indexes := make(map[int]bool)
	for _, path := range files {
		if strings.HasSuffix(path, ".metrics.json") {
			continue
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return ProcessResult{}, err
		}
		var metadata experiment.RunMetadata
		if err := json.Unmarshal(b, &metadata); err != nil {
			return ProcessResult{}, err
		}
		if exclusions[metadata.RunID] != "" || metadata.Status != "eligible" {
			continue
		}
		if indexes[metadata.ScheduleIndex] {
			return ProcessResult{}, fmt.Errorf("multiple eligible runs for schedule index %d", metadata.ScheduleIndex)
		}
		indexes[metadata.ScheduleIndex] = true
		result, err := CalculateRun(filepath.Join(options.Raw, metadata.RunID+".metrics.json"), metadata)
		if err != nil {
			return ProcessResult{}, fmt.Errorf("%s: %w", metadata.RunID, err)
		}
		runs = append(runs, result)
	}
	summaries, err := Summarize(runs)
	if err != nil {
		return ProcessResult{}, err
	}
	if err := writeReports(options.Out, runs, summaries, Compare(summaries), exclusions); err != nil {
		return ProcessResult{}, err
	}
	return ProcessResult{EligibleRuns: len(runs), Summaries: len(summaries)}, nil
}
