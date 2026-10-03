package experiment

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestGeneratePlan(t *testing.T) {
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
}

func TestExistingSchedule(t *testing.T) {
	_, err := ReadPlan(filepath.Join("..", "..", "runner", "schedule.json"))
	if err != nil {
		t.Fatal(err)
	}
}
