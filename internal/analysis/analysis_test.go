package analysis

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mldsa-jwt-benchmark/internal/experiment"
)

func writeFixture(t *testing.T, summary experiment.MetricSummary) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "run.metrics.json")
	b, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPhaseSelectionAndSummary(t *testing.T) {
	metadata := experiment.RunMetadata{
		RunID: "r1", SuccessfulInWindow: 1, PayloadBytes: 100, JWTBytes: 200,
		Scenario: experiment.Scenario{Round: 1, Alg: "ES256", Operation: "issue", TargetVU: 1},
		Phases:   map[string]int64{"measure_start_ms": 15000, "measure_end_ms": 75000, "grace_end_ms": 105000},
	}
	path := writeFixture(t, experiment.MetricSummary{
		SchemaVersion: 1, RunID: "r1", Alg: "ES256", Operation: "issue", TargetVU: 1,
		SuccessfulInWindow: 1, SuccessfulStartedInWindow: 2, MeanMS: ptr(15), P99MS: ptr(19.9),
	})
	run, err := CalculateRun(path, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if run.SuccessfulInWindow != 1 || run.SuccessfulStartedInWindow != 2 || math.Abs(run.ThroughputRPS-1.0/60) > 1e-12 || run.MeanMS == nil || *run.MeanMS != 15 || run.P99MS == nil || math.Abs(*run.P99MS-19.9) > 1e-9 {
		t.Fatalf("incorrect phase selection: %+v", run)
	}
	summaries, err := Summarize([]RunResult{run})
	if err != nil {
		t.Fatal(err)
	}
	if summaries[0].Stats["p99_ms"].SD != nil || summaries[0].Stats["p99_ms"].N != 1 {
		t.Fatal("single repetition must not have sample standard deviation")
	}
	if err := MakeSVG(filepath.Join(t.TempDir(), "chart.svg"), summaries, "issue", "p99_ms"); err != nil {
		t.Fatal(err)
	}
}

func TestMetricSummaryMismatch(t *testing.T) {
	metadata := experiment.RunMetadata{
		RunID: "r1", Scenario: experiment.Scenario{Round: 1, Alg: "ES256", Operation: "issue", TargetVU: 1},
		Phases: map[string]int64{"measure_start_ms": 15000, "measure_end_ms": 75000, "grace_end_ms": 105000},
	}
	path := writeFixture(t, experiment.MetricSummary{
		SchemaVersion: 1, RunID: "another-run", Alg: "ES256", Operation: "issue", TargetVU: 1,
		SuccessfulInWindow: 1, SuccessfulStartedInWindow: 1, MeanMS: ptr(10), P99MS: ptr(10),
	})
	if _, err := CalculateRun(path, metadata); err == nil {
		t.Fatal("metric summary for another run was accepted")
	}
}

func TestSampleStatistics(t *testing.T) {
	runs := []RunResult{
		{Operation: "issue", TargetVU: 1, Alg: "ES256", PayloadBytes: 100, JWTBytes: 200, ThroughputRPS: 2, MeanMS: ptr(10), P99MS: ptr(20)},
		{Operation: "issue", TargetVU: 1, Alg: "ES256", PayloadBytes: 100, JWTBytes: 200, ThroughputRPS: 4, MeanMS: ptr(14), P99MS: ptr(24)},
		{Operation: "issue", TargetVU: 1, Alg: "ML-DSA-44", PayloadBytes: 100, JWTBytes: 300, ThroughputRPS: 1, MeanMS: ptr(24), P99MS: ptr(40)},
	}
	summaries, err := Summarize(runs)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 || summaries[0].Stats["throughput_rps"].Mean == nil || *summaries[0].Stats["throughput_rps"].Mean != 3 {
		t.Fatalf("wrong summary: %+v", summaries)
	}
	if math.Abs(*summaries[0].Stats["throughput_rps"].SD-math.Sqrt2) > 1e-12 {
		t.Fatal("sample standard deviation is wrong")
	}
	comparison := Compare(summaries)
	if len(comparison) != 1 || comparison[0].Values["mean_ms"][2] == nil || *comparison[0].Values["mean_ms"][2] != 100 {
		t.Fatalf("wrong comparison: %+v", comparison)
	}
}

func TestProcessWritesReports(t *testing.T) {
	root := t.TempDir()
	raw := filepath.Join(root, "raw")
	out := filepath.Join(root, "processed")
	if err := os.MkdirAll(raw, 0755); err != nil {
		t.Fatal(err)
	}
	metadata := experiment.RunMetadata{
		RunID: "r1", ScheduleIndex: 0, Status: "eligible", SuccessfulInWindow: 1,
		PayloadBytes: 100, JWTBytes: 200,
		Scenario: experiment.Scenario{Round: 1, Alg: "ES256", Operation: "issue", TargetVU: 1},
		Phases:   map[string]int64{"measure_start_ms": 15000, "measure_end_ms": 75000, "grace_end_ms": 105000},
	}
	b, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(raw, "r1.json"), b, 0644); err != nil {
		t.Fatal(err)
	}
	fixture := writeFixture(t, experiment.MetricSummary{
		SchemaVersion: 1, RunID: "r1", Alg: "ES256", Operation: "issue", TargetVU: 1,
		SuccessfulInWindow: 1, SuccessfulStartedInWindow: 1, MeanMS: ptr(10), P99MS: ptr(10),
	})
	metricBytes, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(raw, "r1.metrics.json"), metricBytes, 0644); err != nil {
		t.Fatal(err)
	}
	result, err := Process(Options{Raw: raw, Out: out, Exclusions: filepath.Join(root, "exclusions.json")})
	if err != nil || result.EligibleRuns != 1 || result.Summaries != 1 {
		t.Fatalf("pipeline failed: %+v %v", result, err)
	}
	for _, name := range []string{"per_run.csv", "summary.csv", "comparison.csv", "exclusions.csv", "issue_throughput_rps.svg"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatal(err)
		}
	}
	assertContents := func(name, want string) {
		t.Helper()
		got, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("%s contents:\n%s\nwant:\n%s", name, got, want)
		}
	}
	assertContents("per_run.csv", "run_id,round,alg,operation,target_vu,successful_in_window,successful_started_in_window,throughput_rps,mean_ms,p99_ms,payload_bytes,jwt_bytes\n"+
		"r1,1,ES256,issue,1,1,1,0.016666666666666666,10,10,100,200\n")
	assertContents("summary.csv", "operation,target_vu,alg,runs,payload_bytes,jwt_bytes,throughput_rps_n,throughput_rps_mean,throughput_rps_sd,throughput_rps_cv,mean_ms_n,mean_ms_mean,mean_ms_sd,mean_ms_cv,p99_ms_n,p99_ms_mean,p99_ms_sd,p99_ms_cv\n"+
		"issue,1,ES256,1,100,200,1,0.016666666666666666,,,1,10,,,1,10,,\n")
	assertContents("comparison.csv", "")
	assertContents("exclusions.csv", "")
	svg, err := os.ReadFile(filepath.Join(out, "issue_throughput_rps.svg"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(svg), "issue: throughput_rps") || !strings.Contains(string(svg), "ES256") {
		t.Fatal("generated SVG does not contain the expected operation, metric, and algorithm")
	}
}
