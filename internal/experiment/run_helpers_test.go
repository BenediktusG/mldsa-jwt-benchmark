package experiment

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenInfo(t *testing.T) {
	payload := []byte(`{"sub":"vu-0001","exp":1800000900}`)
	compact := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	payloadBytes, jwtBytes, exp, err := tokenInfo(compact)
	if err != nil {
		t.Fatal(err)
	}
	if payloadBytes != len(payload) || jwtBytes != len(compact) || exp != 1800000900 {
		t.Fatalf("tokenInfo = (%d, %d, %d), want (%d, %d, 1800000900)", payloadBytes, jwtBytes, exp, len(payload), len(compact))
	}
	for _, invalid := range []string{"one.two", "a.!!!.b", "a." + base64.RawURLEncoding.EncodeToString([]byte(`{"exp":`)) + ".b"} {
		if _, _, _, err := tokenInfo(invalid); err == nil {
			t.Errorf("invalid token accepted: %q", invalid)
		}
	}
}

func TestReadMetricSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "results.metrics.json")
	contents := `{"schema_version":1,"run_id":"r1","alg":"ES256","operation":"issue","target_vu":1,"successful_in_window":2,"successful_started_in_window":3,"mean_ms":10,"p99_ms":20}`
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	summary, err := ReadMetricSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SuccessfulInWindow != 2 || summary.SuccessfulStartedInWindow != 3 || summary.MeanMS == nil || *summary.MeanMS != 10 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMetricSummary(path); err == nil {
		t.Fatal("invalid metric summary was accepted")
	}
}

func TestParsePhases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.log")
	contents := "k6 startup\nINFO PHASE_JSON:{\"measure_start_ms\":15000,\"measure_end_ms\":75000,\"grace_end_ms\":105000}\n"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	phases, err := parsePhases(path)
	if err != nil {
		t.Fatal(err)
	}
	if phases["measure_start_ms"] != 15000 || phases["measure_end_ms"] != 75000 || phases["grace_end_ms"] != 105000 {
		t.Fatalf("unexpected phases: %v", phases)
	}
	for name, contents := range map[string]string{"missing": "ordinary log\n", "malformed": "PHASE_JSON:{\n"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "run.log")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := parsePhases(path); err == nil {
				t.Fatal("invalid phase log was accepted")
			}
		})
	}
}

func TestVerifyRunInputsDetectsChanges(t *testing.T) {
	type fixture struct {
		root, schedule string
		environment    map[string]string
		keys           map[string]string
		composeEnv     []string
	}
	newFixture := func(t *testing.T) fixture {
		t.Helper()
		root := t.TempDir()
		writeTestFile(t, root, "compose.yaml", "services: {}\n")
		writeTestFile(t, root, "load/scenario.js", "export default function () {}\n")
		writeTestFile(t, root, "runner/schedule.json", "{}\n")
		writeTestFile(t, root, "keys/ES256.json", "key material\n")
		schedule := filepath.Join(root, "runner", "schedule.json")
		composeHash, _ := FileHash(filepath.Join(root, "compose.yaml"))
		scenarioHash, _ := FileHash(filepath.Join(root, "load", "scenario.js"))
		scheduleHash, _ := FileHash(schedule)
		keyHash, _ := FileHash(filepath.Join(root, "keys", "ES256.json"))
		binDir := filepath.Join(root, "bin")
		if err := os.MkdirAll(binDir, 0700); err != nil {
			t.Fatal(err)
		}
		docker := filepath.Join(binDir, "docker")
		script := "#!/bin/sh\n" +
			"if [ \"$1 $2 $3\" = \"compose config --images\" ]; then printf '%s\\n' 'research-server'; exit 0; fi\n" +
			"if [ \"$1 $2 $3\" = \"image inspect --format\" ]; then printf '%s\\n' 'sha256:fixed'; exit 0; fi\n" +
			"exit 1\n"
		if err := os.WriteFile(docker, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", binDir)
		return fixture{
			root: root, schedule: schedule,
			environment: map[string]string{"compose_sha256": composeHash, "scenario_sha256": scenarioHash, "schedule_sha256": scheduleHash, "server_image_id": "sha256:fixed"},
			keys:        map[string]string{"ES256": keyHash},
			composeEnv:  []string{"PATH=" + binDir},
		}
	}

	baseline := newFixture(t)
	if err := verifyRunInputs(baseline.root, baseline.schedule, baseline.environment, baseline.keys, baseline.composeEnv); err != nil {
		t.Fatalf("unchanged inputs rejected: %v", err)
	}

	tests := map[string]func(t *testing.T, f fixture){
		"Compose configuration": func(t *testing.T, f fixture) { writeTestFile(t, f.root, "compose.yaml", "changed\n") },
		"load scenario":         func(t *testing.T, f fixture) { writeTestFile(t, f.root, "load/scenario.js", "changed\n") },
		"schedule":              func(t *testing.T, f fixture) { writeTestFile(t, f.root, "runner/schedule.json", "changed\n") },
		"key pair":              func(t *testing.T, f fixture) { writeTestFile(t, f.root, "keys/ES256.json", "changed\n") },
		"server image":          func(t *testing.T, f fixture) { f.environment["server_image_id"] = "sha256:other" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			mutate(t, f)
			err := verifyRunInputs(f.root, f.schedule, f.environment, f.keys, f.composeEnv)
			if err == nil {
				t.Fatal("changed input was accepted")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(strings.Fields(name)[0])) {
				t.Fatalf("error %q does not identify changed %s", err, name)
			}
		})
	}
}
