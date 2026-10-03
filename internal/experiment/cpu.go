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
	return checkServerCPUAllocation(serverValue, "/sys/devices/system/cpu", "/proc/cmdline")
}

func checkServerCPUAllocation(serverValue, cpuRoot, cmdlinePath string) error {
	server, err := ParseCPUSet(serverValue)
	if err != nil {
		return err
	}
	if len(server) != 4 {
		return fmt.Errorf("server allocation needs exactly four logical CPUs")
	}
	onlineData, err := os.ReadFile(filepath.Join(cpuRoot, "online"))
	if err != nil {
		return err
	}
	online, err := ParseCPUSet(strings.TrimSpace(string(onlineData)))
	if err != nil {
		return fmt.Errorf("parse online CPUs: %w", err)
	}
	isolatedData, err := os.ReadFile(filepath.Join(cpuRoot, "isolated"))
	if err != nil {
		return err
	}
	isolatedValue := strings.TrimSpace(string(isolatedData))
	if isolatedValue == "" {
		return fmt.Errorf("no scheduler-domain-isolated CPUs; configure isolcpus=domain,<cpu-list> and reboot")
	}
	isolated, err := ParseCPUSet(isolatedValue)
	if err != nil {
		return fmt.Errorf("parse isolated CPUs: %w", err)
	}
	for cpu := range server {
		if !online[cpu] {
			return fmt.Errorf("server CPU %d is offline", cpu)
		}
		if !isolated[cpu] {
			return fmt.Errorf("server CPU %d is not scheduler-domain isolated", cpu)
		}
	}
	cmdline, err := os.ReadFile(cmdlinePath)
	if err != nil {
		return err
	}
	irqAffinity, err := kernelArgument(cmdline, "irqaffinity")
	if err != nil {
		return err
	}
	irqCPUs, err := ParseCPUSet(irqAffinity)
	if err != nil {
		return fmt.Errorf("parse irqaffinity: %w", err)
	}
	for cpu := range server {
		if irqCPUs[cpu] {
			return fmt.Errorf("irqaffinity includes server CPU %d", cpu)
		}
	}
	for cpu := range online {
		if !server[cpu] && !irqCPUs[cpu] {
			return fmt.Errorf("irqaffinity excludes housekeeping CPU %d", cpu)
		}
	}
	cores := make(map[string]int)
	for cpu := range server {
		root := filepath.Join(cpuRoot, fmt.Sprintf("cpu%d/topology", cpu))
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

func kernelArgument(cmdline []byte, name string) (string, error) {
	prefix := name + "="
	value := ""
	for _, argument := range strings.Fields(string(cmdline)) {
		if strings.HasPrefix(argument, prefix) {
			value = strings.TrimPrefix(argument, prefix)
		}
	}
	if value == "" {
		return "", fmt.Errorf("kernel command line is missing %s=<cpu-list>", name)
	}
	return value, nil
}

func currentKernelArgument(name string) (string, error) {
	cmdline, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return "", err
	}
	return kernelArgument(cmdline, name)
}

func cpuAllowedList(status []byte) (string, error) {
	for _, line := range strings.Split(string(status), "\n") {
		if strings.HasPrefix(line, "Cpus_allowed_list:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "Cpus_allowed_list:"))
			if _, err := ParseCPUSet(value); err != nil {
				return "", err
			}
			return value, nil
		}
	}
	return "", fmt.Errorf("CPU affinity unavailable")
}

func currentCPUAllowedList() (string, error) {
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return "", err
	}
	return cpuAllowedList(status)
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
