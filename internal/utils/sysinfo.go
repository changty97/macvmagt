package utils

import (
	"log"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

// GetCPUUsage returns the current CPU usage percentage.
func GetCPUUsage() float64 {
	percentages, err := cpu.Percent(0, false) // 0 means non-blocking, false means total CPU
	if err != nil {
		log.Printf("Error getting CPU usage: %v", err)
		return 0.0
	}
	if len(percentages) > 0 {
		return percentages[0]
	}
	return 0.0
}

// GetMemoryUsage returns current and total memory in GB.
func GetMemoryUsage() (usedGB, totalGB float64) {
	v, err := mem.VirtualMemory()
	if err != nil {
		log.Printf("Error getting memory info: %v", err)
		return 0.0, 0.0
	}
	return float64(v.Used) / (1024 * 1024 * 1024), float64(v.Total) / (1024 * 1024 * 1024)
}

// GetDiskUsage returns current and total disk usage in GB for the root partition.
func GetDiskUsage() (usedGB, totalGB float64) {
	usage, err := disk.Usage("/") // Assuming "/" is the root partition
	if err != nil {
		log.Printf("Error getting disk usage: %v", err)
		return 0.0, 0.0
	}
	return float64(usage.Used) / (1024 * 1024 * 1024), float64(usage.Total) / (1024 * 1024 * 1024)
}
