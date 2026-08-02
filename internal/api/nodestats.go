package api

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// nodeStats reports node-level resource usage from /proc and the filesystem.
// It contains no agent data and no secrets — only aggregate OS-level numbers
// the dashboard's resource strip renders. The single-node deployment backs this
// directly; a fleet/telemetry change can later back the same endpoint with an
// OTel collector without the dashboard changing.
type nodeStats struct {
	CPUUsagePercent   float64 `json:"cpu_usage_percent"` // aggregate, 0-100
	MemoryBytes       uint64  `json:"memory_bytes"`      // used
	MemoryTotalBytes  uint64  `json:"memory_total_bytes"`
	StorageBytes      uint64  `json:"storage_bytes"`       // stonewall data dir size
	StorageTotalBytes uint64  `json:"storage_total_bytes"` // filesystem capacity
	UptimeSeconds     uint64  `json:"uptime_seconds"`
	NumGoroutines     int     `json:"num_goroutines"`
}

// prevCPU holds the previous /proc/stat + clock reading for delta CPU calc.
var (
	prevCPUStat cpuStatSample
	havePrev    bool
)

type cpuStatSample struct {
	busy uint64 // user+nice+system+irq+softirq+steal+guest
	idle uint64 // idle+iowait
}

func readProcStat() (cpuStatSample, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuStatSample{}, err
	}
	line := strings.SplitN(string(data), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuStatSample{}, errBadProcStat
	}
	// fields: cpu user nice system idle iowait irq softirq steal guest guest_nice
	vals := make([]uint64, 0, len(fields)-1)
	for _, f := range fields[1:] {
		v, _ := strconv.ParseUint(f, 10, 64)
		vals = append(vals, v)
	}
	busy := uint64(0)
	for i, v := range vals {
		if i == 3 || i == 4 { // idle, iowait
			continue
		}
		busy += v
	}
	idle := uint64(0)
	if len(vals) > 3 {
		idle += vals[3]
	}
	if len(vals) > 4 {
		idle += vals[4]
	}
	return cpuStatSample{busy: busy, idle: idle}, nil
}

var errBadProcStat = errString("malformed /proc/stat")

type errString string

func (e errString) Error() string { return string(e) }

func cpuUsagePercent() float64 {
	cur, err := readProcStat()
	if err != nil {
		return 0
	}
	if !havePrev {
		prevCPUStat = cur
		havePrev = true
		return 0
	}
	prev := prevCPUStat
	prevCPUStat = cur
	havePrev = true
	totalDelta := (cur.busy + cur.idle) - (prev.busy + prev.idle)
	if totalDelta == 0 {
		return 0
	}
	busyDelta := cur.busy - prev.busy
	if busyDelta > totalDelta {
		busyDelta = totalDelta
	}
	return float64(busyDelta) / float64(totalDelta) * 100.0
}

func readMemInfo() (used, total uint64, err error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	var memTotal, memFree, memAvail, buffers, cached uint64
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			memTotal = v * 1024
		case "MemFree:":
			memFree = v * 1024
		case "MemAvailable:":
			memAvail = v * 1024
		case "Buffers:":
			buffers = v * 1024
		case "Cached:":
			cached = v * 1024
		}
	}
	total = memTotal
	if memAvail > 0 {
		used = memTotal - memAvail
	} else {
		used = memTotal - memFree - buffers - cached
	}
	return used, total, nil
}

// dirSize sums the byte size of a directory tree (best-effort, non-fatal).
func dirSize(path string) uint64 {
	var total uint64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() {
			total += uint64(info.Size())
		}
		return nil
	})
	return total
}

// fsCapacity returns the total bytes of the filesystem holding path.
func fsCapacity(path string) uint64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0
	}
	return stat.Blocks * uint64(stat.Bsize)
}

// collectNodeStats gathers the node resource snapshot for the dashboard.
func collectNodeStats(dataDir string) nodeStats {
	cpu := cpuUsagePercent()
	used, total, _ := readMemInfo()
	st := nodeStats{
		CPUUsagePercent:   cpu,
		MemoryBytes:       used,
		MemoryTotalBytes:  total,
		StorageBytes:      dirSize(dataDir),
		StorageTotalBytes: fsCapacity(dataDir),
		NumGoroutines:     runtime.NumGoroutine(),
	}
	if h, err := os.Hostname(); err == nil {
		_ = h // available if needed; not exposed to avoid leaking identity
	}
	return st
}
