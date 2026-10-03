package experiment

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"mldsa-jwt-benchmark/internal/profile"
)

var Operations = []string{"issue", "verify"}
var VUs = []int{1, 10, 100, 1000}

type Scenario struct {
	Round     int    `json:"round"`
	Alg       string `json:"alg"`
	Operation string `json:"operation"`
	TargetVU  int    `json:"target_vu"`
}

type Plan struct {
	Runs []Scenario `json:"runs"`
}

func GeneratePlan() Plan {
	plan := Plan{Runs: make([]Scenario, 0, 240)}
	for _, alg := range profile.Algorithms {
		for _, op := range Operations {
			for _, vu := range VUs {
				for round := 1; round <= 5; round++ {
					plan.Runs = append(plan.Runs, Scenario{Round: round, Alg: alg, Operation: op, TargetVU: vu})
				}
			}
		}
	}
	return plan
}

func (p Plan) Validate() error {
	if len(p.Runs) != 240 {
		return fmt.Errorf("schedule must have 240 runs, got %d", len(p.Runs))
	}
	seen := make(map[string]bool, 240)
	for index, run := range p.Runs {
		if run.Round < 1 || run.Round > 5 || !profile.Supported(run.Alg) || !contains(Operations, run.Operation) || !containsInt(VUs, run.TargetVU) {
			return fmt.Errorf("invalid scenario at index %d", index)
		}
		key := strconv.Itoa(run.Round) + "/" + run.Alg + "/" + run.Operation + "/" + strconv.Itoa(run.TargetVU)
		if seen[key] {
			return fmt.Errorf("duplicate scenario at index %d", index)
		}
		seen[key] = true
	}
	generated := GeneratePlan()
	for i := range p.Runs {
		if p.Runs[i] != generated.Runs[i] {
			return fmt.Errorf("schedule is not sequential at index %d", i)
		}
	}
	return nil
}

func contains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

func containsInt(values []int, v int) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

func ReadPlan(path string) (Plan, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Plan{}, err
	}
	var p Plan
	if err := json.Unmarshal(b, &p); err != nil {
		return Plan{}, err
	}
	return p, p.Validate()
}

func WritePlan(path string, p Plan) error {
	if err := p.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
