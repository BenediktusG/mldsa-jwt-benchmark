package experiment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func parseCPUModelName(data []byte) string {
	var output struct {
		CPUs []struct {
			ModelName string `json:"modelname"`
		} `json:"cpus"`
	}
	if err := json.Unmarshal(data, &output); err != nil || len(output.CPUs) == 0 || output.CPUs[0].ModelName == "" {
		return "unavailable"
	}
	return output.CPUs[0].ModelName
}

func ParseCPUSet(value string) (map[int]bool, error) {
	result := make(map[int]bool)
	for _, part := range strings.Split(value, ",") {
		bounds := strings.Split(part, "-")
		if len(bounds) < 1 || len(bounds) > 2 {
			return nil, fmt.Errorf("invalid CPU set %q", value)
		}
		first, err := strconv.Atoi(bounds[0])
		if err != nil || first < 0 {
			return nil, fmt.Errorf("invalid CPU set %q", value)
		}
		last := first
		if len(bounds) == 2 {
			last, err = strconv.Atoi(bounds[1])
			if err != nil || last < first {
				return nil, fmt.Errorf("invalid CPU set %q", value)
			}
		}
		if last-first > 100000 {
			return nil, fmt.Errorf("CPU range too large")
		}
		for cpu := first; cpu <= last; cpu++ {
			result[cpu] = true
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("empty CPU set")
	}
	return result, nil
}

func CheckServerCPUAllocation(serverValue string) error {
	server, err := ParseCPUSet(serverValue)
	if err != nil {
		return err
	}
	if len(server) != 4 {
		return fmt.Errorf("server allocation needs exactly four logical CPUs")
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return err
	}
	var available map[int]bool
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Cpus_allowed_list:") {
			available, err = ParseCPUSet(strings.TrimSpace(strings.TrimPrefix(line, "Cpus_allowed_list:")))
			if err != nil {
				return err
			}
			break
		}
	}
	if available == nil {
		return fmt.Errorf("CPU affinity unavailable")
	}
	for cpu := range server {
		if !available[cpu] {
			return fmt.Errorf("server CPU %d is unavailable", cpu)
		}
	}
	cores := make(map[string]int)
	for cpu := range server {
		root := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/topology", cpu)
		packageID, err := os.ReadFile(filepath.Join(root, "physical_package_id"))
		if err != nil {
			return err
		}
		coreID, err := os.ReadFile(filepath.Join(root, "core_id"))
		if err != nil {
			return err
		}
		cores[strings.TrimSpace(string(packageID))+"/"+strings.TrimSpace(string(coreID))]++
	}
	return validateServerCoreThreads(cores)
}

func validateServerCoreThreads(cores map[string]int) error {
	if len(cores) != 2 {
		return fmt.Errorf("server CPUs must contain both threads from exactly two physical cores")
	}
	for core, threads := range cores {
		if threads != 2 {
			return fmt.Errorf("physical core %s must contribute exactly two logical CPUs", core)
		}
	}
	return nil
}
