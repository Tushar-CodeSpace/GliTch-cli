package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

// SystemTelemetry models the full hardware and OS telemetry snapshot.
type SystemTelemetry struct {
	Timestamp   time.Time         `json:"timestamp"`
	Host        HostMetrics       `json:"host"`
	CPU         CPUMetrics        `json:"cpu"`
	Memory      MemoryMetrics     `json:"memory"`
	Disk        DiskMetrics       `json:"disk"`
	Network     NetworkMetrics    `json:"network"`
	ProcessInfo ProcessMetrics    `json:"process_info"`
}

type HostMetrics struct {
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Platform        string `json:"platform"`
	PlatformFamily  string `json:"platform_family"`
	PlatformVersion string `json:"platform_version"`
	KernelVersion   string `json:"kernel_version"`
	KernelArch      string `json:"kernel_arch"`
	UptimeSeconds   uint64 `json:"uptime_seconds"`
	UptimeFormatted string `json:"uptime_formatted"`
	BootTime        uint64 `json:"boot_time"`
}

type CPUMetrics struct {
	ModelName      string    `json:"model_name"`
	LogicalCores   int       `json:"logical_cores"`
	PhysicalCores  int       `json:"physical_cores"`
	TotalUsagePct  float64   `json:"total_usage_pct"`
	CoreUsagesPct  []float64 `json:"core_usages_pct"`
	LoadAvg1       float64   `json:"load_avg_1"`
	LoadAvg5       float64   `json:"load_avg_5"`
	LoadAvg15      float64   `json:"load_avg_15"`
}

type MemoryMetrics struct {
	TotalBytes       uint64  `json:"total_bytes"`
	UsedBytes        uint64  `json:"used_bytes"`
	FreeBytes        uint64  `json:"free_bytes"`
	AvailableBytes   uint64  `json:"available_bytes"`
	UsedPercent      float64 `json:"used_percent"`
	SwapTotalBytes   uint64  `json:"swap_total_bytes"`
	SwapUsedBytes    uint64  `json:"swap_used_bytes"`
	SwapFreeBytes    uint64  `json:"swap_free_bytes"`
	SwapUsedPercent  float64 `json:"swap_used_percent"`
	FormattedTotal   string  `json:"formatted_total"`
	FormattedUsed    string  `json:"formatted_used"`
	FormattedFree    string  `json:"formatted_free"`
}

type DiskMetrics struct {
	Path           string  `json:"path"`
	FSType         string  `json:"fs_type"`
	TotalBytes     uint64  `json:"total_bytes"`
	FreeBytes      uint64  `json:"free_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	UsedPercent    float64 `json:"used_percent"`
	FormattedTotal string  `json:"formatted_total"`
	FormattedUsed  string  `json:"formatted_used"`
	FormattedFree  string  `json:"formatted_free"`
}

type NetworkMetrics struct {
	BytesSent      uint64 `json:"bytes_sent"`
	BytesRecv      uint64 `json:"bytes_recv"`
	PacketsSent    uint64 `json:"packets_sent"`
	PacketsRecv    uint64 `json:"packets_recv"`
	FormattedSent  string `json:"formatted_sent"`
	FormattedRecv  string `json:"formatted_recv"`
}

type ProcessMetrics struct {
	TotalCount int `json:"total_count"`
}

// TelemetryMonitor coordinates periodic non-blocking background collection.
type TelemetryMonitor struct {
	mu           sync.RWMutex
	lastSnapshot SystemTelemetry
	subscribers  map[chan SystemTelemetry]struct{}
	subMu        sync.Mutex
}

var monitor *TelemetryMonitor

func newTelemetryMonitor() *TelemetryMonitor {
	m := &TelemetryMonitor{
		subscribers: make(map[chan SystemTelemetry]struct{}),
	}
	m.collect()
	go m.backgroundLoop()
	return m
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatUptime(seconds uint64) string {
	days := seconds / 86400
	hours := (seconds % 86400) / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, secs)
	}
	return fmt.Sprintf("%dm %ds", minutes, secs)
}

func (m *TelemetryMonitor) collect() {
	var snap SystemTelemetry
	snap.Timestamp = time.Now().UTC()

	// Host
	if hInfo, err := host.Info(); err == nil && hInfo != nil {
		snap.Host = HostMetrics{
			Hostname:        hInfo.Hostname,
			OS:              hInfo.OS,
			Platform:        hInfo.Platform,
			PlatformFamily:  hInfo.PlatformFamily,
			PlatformVersion: hInfo.PlatformVersion,
			KernelVersion:   hInfo.KernelVersion,
			KernelArch:      hInfo.KernelArch,
			UptimeSeconds:   hInfo.Uptime,
			UptimeFormatted: formatUptime(hInfo.Uptime),
			BootTime:        hInfo.BootTime,
		}
	}

	// CPU
	var totalPct float64
	if pcts, err := cpu.Percent(0, false); err == nil && len(pcts) > 0 {
		totalPct = pcts[0]
	}
	corePcts, _ := cpu.Percent(0, true)

	logicalCores, _ := cpu.Counts(true)
	physicalCores, _ := cpu.Counts(false)

	modelName := "Standard Processor"
	if cpuInfos, err := cpu.Info(); err == nil && len(cpuInfos) > 0 {
		modelName = cpuInfos[0].ModelName
	}

	var l1, l5, l15 float64
	if lAvg, err := load.Avg(); err == nil && lAvg != nil {
		l1 = lAvg.Load1
		l5 = lAvg.Load5
		l15 = lAvg.Load15
	}

	snap.CPU = CPUMetrics{
		ModelName:     modelName,
		LogicalCores:  logicalCores,
		PhysicalCores: physicalCores,
		TotalUsagePct: totalPct,
		CoreUsagesPct: corePcts,
		LoadAvg1:      l1,
		LoadAvg5:      l5,
		LoadAvg15:     l15,
	}

	// Memory
	if vMem, err := mem.VirtualMemory(); err == nil && vMem != nil {
		snap.Memory.TotalBytes = vMem.Total
		snap.Memory.UsedBytes = vMem.Used
		snap.Memory.FreeBytes = vMem.Free
		snap.Memory.AvailableBytes = vMem.Available
		snap.Memory.UsedPercent = vMem.UsedPercent
		snap.Memory.FormattedTotal = formatBytes(vMem.Total)
		snap.Memory.FormattedUsed = formatBytes(vMem.Used)
		snap.Memory.FormattedFree = formatBytes(vMem.Free)
	}

	if sMem, err := mem.SwapMemory(); err == nil && sMem != nil {
		snap.Memory.SwapTotalBytes = sMem.Total
		snap.Memory.SwapUsedBytes = sMem.Used
		snap.Memory.SwapFreeBytes = sMem.Free
		snap.Memory.SwapUsedPercent = sMem.UsedPercent
	}

	// Disk
	targetPath := "/"
	if dUsage, err := disk.Usage(targetPath); err == nil && dUsage != nil {
		snap.Disk = DiskMetrics{
			Path:           targetPath,
			FSType:         dUsage.Fstype,
			TotalBytes:     dUsage.Total,
			FreeBytes:      dUsage.Free,
			UsedBytes:      dUsage.Used,
			UsedPercent:    dUsage.UsedPercent,
			FormattedTotal: formatBytes(dUsage.Total),
			FormattedUsed:  formatBytes(dUsage.Used),
			FormattedFree:  formatBytes(dUsage.Free),
		}
	}

	// Network
	if ioCounters, err := net.IOCounters(false); err == nil && len(ioCounters) > 0 {
		snap.Network = NetworkMetrics{
			BytesSent:     ioCounters[0].BytesSent,
			BytesRecv:     ioCounters[0].BytesRecv,
			PacketsSent:   ioCounters[0].PacketsSent,
			PacketsRecv:   ioCounters[0].PacketsRecv,
			FormattedSent: formatBytes(ioCounters[0].BytesSent),
			FormattedRecv: formatBytes(ioCounters[0].BytesRecv),
		}
	}

	// Processes
	if pids, err := process.Pids(); err == nil {
		snap.ProcessInfo.TotalCount = len(pids)
	}

	m.mu.Lock()
	m.lastSnapshot = snap
	m.mu.Unlock()

	// Broadcast to SSE subscribers
	m.subMu.Lock()
	for ch := range m.subscribers {
		select {
		case ch <- snap:
		default:
		}
	}
	m.subMu.Unlock()
}

func (m *TelemetryMonitor) backgroundLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.collect()
	}
}

func (m *TelemetryMonitor) GetSnapshot() SystemTelemetry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastSnapshot
}

func handleGetStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	snapshot := monitor.GetSnapshot()
	json.NewEncoder(w).Encode(snapshot)
}

func handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan SystemTelemetry, 10)
	monitor.subMu.Lock()
	monitor.subscribers[ch] = struct{}{}
	monitor.subMu.Unlock()

	defer func() {
		monitor.subMu.Lock()
		delete(monitor.subscribers, ch)
		close(ch)
		monitor.subMu.Unlock()
	}()

	notify := r.Context().Done()

	// Send initial snapshot immediately
	initData, _ := json.Marshal(monitor.GetSnapshot())
	fmt.Fprintf(w, "data: %s\n\n", initData)
	flusher.Flush()

	for {
		select {
		case <-notify:
			return
		case snap := <-ch:
			data, err := json.Marshal(snap)
			if err == nil {
				fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "healthy",
		"service": "telemetry-service",
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8082"
	}

	log.Println("Initializing hardware telemetry monitor...")
	monitor = newTelemetryMonitor()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/telemetry/stats", handleGetStats)
	mux.HandleFunc("GET /api/v1/telemetry/stream", handleStream)
	mux.HandleFunc("GET /health", handleHealth)
	mux.HandleFunc("GET /api/v1/telemetry/health", handleHealth)

	log.Printf("⚡ GliTch Telemetry Microservice running on port %s...", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Telemetry service shutdown: %v", err)
	}
}
