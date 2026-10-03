package experiment

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

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

func TestParseCPUSet(t *testing.T) {
	set, err := ParseCPUSet("0,2-4")
	if err != nil || len(set) != 4 || !set[3] {
		t.Fatalf("CPU set parse failed: %v %v", set, err)
	}
	if _, err := ParseCPUSet("4-2"); err == nil {
		t.Fatal("invalid CPU range accepted")
	}
}

func TestCheckServerCPUAllocationUsesOnlineCPUs(t *testing.T) {
	root := t.TempDir()
	cmdline := filepath.Join(root, "cmdline")
	if err := os.WriteFile(filepath.Join(root, "online"), []byte("0-5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "isolated"), []byte("1-4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cmdline, []byte("quiet irqaffinity=0,5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for cpu, core := range map[int]string{1: "1", 2: "1", 3: "2", 4: "2"} {
		topology := filepath.Join(root, "cpu"+strconv.Itoa(cpu), "topology")
		if err := os.MkdirAll(topology, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(topology, "physical_package_id"), []byte("0\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(topology, "core_id"), []byte(core+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkServerCPUAllocation("1-4", root, cmdline); err != nil {
		t.Fatalf("online isolated-style allocation rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "online"), []byte("0-3,5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkServerCPUAllocation("1-4", root, cmdline); err == nil || !strings.Contains(err.Error(), "server CPU 4 is offline") {
		t.Fatalf("offline CPU was not rejected correctly: %v", err)
	}
}

func TestCheckServerCPUAllocationRequiresIsolation(t *testing.T) {
	root := t.TempDir()
	cmdline := filepath.Join(root, "cmdline")
	if err := os.WriteFile(filepath.Join(root, "online"), []byte("0-5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "isolated"), []byte("1-3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cmdline, []byte("irqaffinity=0,5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkServerCPUAllocation("1-4", root, cmdline); err == nil || !strings.Contains(err.Error(), "server CPU 4 is not scheduler-domain isolated") {
		t.Fatalf("non-isolated server CPU was not rejected correctly: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "isolated"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkServerCPUAllocation("1-4", root, cmdline); err == nil || !strings.Contains(err.Error(), "no scheduler-domain-isolated CPUs") {
		t.Fatalf("empty isolated CPU set was not rejected correctly: %v", err)
	}
}

func TestCheckServerCPUAllocationRequiresIRQAffinity(t *testing.T) {
	root := t.TempDir()
	cmdline := filepath.Join(root, "cmdline")
	if err := os.WriteFile(filepath.Join(root, "online"), []byte("0-5\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "isolated"), []byte("1-4\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for contents, want := range map[string]string{
		"quiet\n":                     "missing irqaffinity",
		"irqaffinity=0-5\n":           "includes server CPU",
		"quiet irqaffinity=0\n":       "excludes housekeeping CPU",
		"irqaffinity=0 irqaffinity=5": "excludes housekeeping CPU",
	} {
		if err := os.WriteFile(cmdline, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
		err := checkServerCPUAllocation("1-4", root, cmdline)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("cmdline %q produced %v, want error containing %q", contents, err, want)
		}
	}
}

func TestKernelArgumentUsesLastValue(t *testing.T) {
	got, err := kernelArgument([]byte("quiet irqaffinity=0 irqaffinity=0,5"), "irqaffinity")
	if err != nil {
		t.Fatal(err)
	}
	if got != "0,5" {
		t.Fatalf("irqaffinity = %q, want %q", got, "0,5")
	}
}

func TestCPUAllowedList(t *testing.T) {
	status := []byte("Name:\ttest\nCpus_allowed_list:\t0,5-17\n")
	got, err := cpuAllowedList(status)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0,5-17" {
		t.Fatalf("CPU affinity = %q, want %q", got, "0,5-17")
	}
	if _, err := cpuAllowedList([]byte("Name:\ttest\n")); err == nil {
		t.Fatal("missing CPU affinity accepted")
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
