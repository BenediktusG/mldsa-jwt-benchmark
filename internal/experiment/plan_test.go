package experiment

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestScheduleAndCPUSet(t *testing.T) {
	one := GeneratePlan()
	two := GeneratePlan()
	if !reflect.DeepEqual(one, two) || len(one.Runs) != 240 {
		t.Fatal("plan is not reproducible")
	}
	if err := one.Validate(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		want := Scenario{Round: i + 1, Alg: "ES256", Operation: "issue", TargetVU: 1}
		if one.Runs[i] != want {
			t.Fatalf("run %d = %+v, want %+v", i, one.Runs[i], want)
		}
	}
	if want := (Scenario{Round: 1, Alg: "ES256", Operation: "issue", TargetVU: 10}); one.Runs[5] != want {
		t.Fatalf("run 5 = %+v, want %+v", one.Runs[5], want)
	}
	one.Runs[0], one.Runs[1] = one.Runs[1], one.Runs[0]
	if err := one.Validate(); err == nil {
		t.Fatal("validator accepted a non-sequential order")
	}
	one.Runs[0], one.Runs[1] = one.Runs[1], one.Runs[0]
	one.Runs[1] = one.Runs[0]
	if err := one.Validate(); err == nil {
		t.Fatal("duplicate scenario accepted")
	}
	set, err := ParseCPUSet("0,2-4")
	if err != nil || len(set) != 4 || !set[3] {
		t.Fatalf("CPU set parse failed: %v %v", set, err)
	}
	if _, err := ParseCPUSet("4-2"); err == nil {
		t.Fatal("invalid CPU range accepted")
	}
}

func TestExistingSchedule(t *testing.T) {
	_, err := ReadPlan(filepath.Join("..", "..", "runner", "schedule.json"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateServerCoreThreads(t *testing.T) {
	if err := validateServerCoreThreads(map[string]int{"0/0": 2, "0/1": 2}); err != nil {
		t.Fatal(err)
	}
	for _, cores := range []map[string]int{
		{"0/0": 2},
		{"0/0": 1, "0/1": 3},
		{"0/0": 2, "0/1": 1, "0/2": 1},
	} {
		if err := validateServerCoreThreads(cores); err == nil {
			t.Fatalf("invalid core allocation accepted: %v", cores)
		}
	}
}

func TestParseCPUModelName(t *testing.T) {
	input := []byte(`{"cpus":[{"modelname":"Example CPU 123"},{"modelname":"Example CPU 123"}]}`)
	if got := parseCPUModelName(input); got != "Example CPU 123" {
		t.Fatalf("model name = %q", got)
	}
	for _, input := range [][]byte{nil, []byte(`{"cpus":[]}`), []byte(`{"cpus":[{"modelname":""}]}`)} {
		if got := parseCPUModelName(input); got != "unavailable" {
			t.Fatalf("invalid input produced model name %q", got)
		}
	}
}
