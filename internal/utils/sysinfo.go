package utils

import (
	"fmt"
	"strconv"
	"strings"
)

// GetCPUUsage returns the current CPU usage percentage.
// This is a simplified example. Real CPU usage can be complex.
func GetCPUUsage() (float64, error) {
	// Using 'top -l 1' and parsing its output for CPU usage.
	// This is macOS specific.
	output, err := ExecuteCommand("top", "-l", "1")
	if err != nil {
		return 0, fmt.Errorf("failed to get CPU usage: %w", err)
	}

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "CPU usage:") {
			parts := strings.Fields(line)
			if len(parts) >= 7 {
				idleStr := strings.TrimSuffix(parts[6], "%")
				idle, err := strconv.ParseFloat(idleStr, 64)
				if err != nil {
					return 0, fmt.Errorf("failed to parse idle CPU: %w", err)
				}
				return 100.0 - idle, nil
			}
		}
	}
	return 0, fmt.Errorf("could not parse CPU usage from top output")
}

// GetMemoryUsage returns current and total memory usage in GB.
func GetMemoryUsage() (float64, float64, error) {
	// Using 'sysctl -n hw.memsize' for total memory and 'vm_stat' for active/wired memory.
	// This is macOS specific.

	// Get total memory
	totalMemBytesStr, err := ExecuteCommand("sysctl", "-n", "hw.memsize")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get total memory: %w", err)
	}
	totalMemBytes, err := strconv.ParseInt(strings.TrimSpace(totalMemBytesStr), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse total memory bytes: %w", err)
	}
	totalMemGB := float64(totalMemBytes) / (1024 * 1024 * 1024)

	// Get used memory from vm_stat
	vmStatOutput, err := ExecuteCommand("vm_stat")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get vm_stat: %w", err)
	}

	pageSize := 4096 // macOS page size is typically 4KB
	var activePages, wiredPages int64

	lines := strings.Split(vmStatOutput, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Pages active:") {
			fmt.Sscanf(line, "Pages active: %d.", &activePages)
		} else if strings.Contains(line, "Pages wired down:") {
			fmt.Sscanf(line, "Pages wired down: %d.", &wiredPages)
		}
	}

	usedMemBytes := (activePages + wiredPages) * int64(pageSize)
	usedMemGB := float64(usedMemBytes) / (1024 * 1024 * 1024)

	return usedMemGB, totalMemGB, nil
}

// GetDiskUsage returns current and total disk usage in GB for the root partition.
func GetDiskUsage() (float64, float64, error) {
	// Using 'df -h /' for disk usage.
	output, err := ExecuteCommand("df", "-h") // -g for GB units
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get disk usage: %w", err)
	}

	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		return 0, 0, fmt.Errorf("unexpected df output format")
	}

	// Expected format: Filesystem    1G-blocks Used   Avail Capacity iused ifree %iused  Mounted on
	//                  /dev/disk1s5s1      465  100   360    22% 100000 100000   1%   /
	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return 0, 0, fmt.Errorf("unexpected df fields count")
	}

	totalGB, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse total disk GB: %w", err)
	}
	usedGB, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse used disk GB: %w", err)
	}

	return usedGB, totalGB, nil
}
