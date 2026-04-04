package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// metricsState holds the latest sampled system metrics, updated by the
// background collector goroutine and read by handleMetrics.
type metricsState struct {
	mu         sync.RWMutex
	cpuPercent float64
	memPercent float64
	netInBps   float64
	netOutBps  float64
}

type cpuSample struct{ total, idle uint64 }
type netSample struct{ rxBytes, txBytes uint64 }

// startMetricsCollector launches a background goroutine that samples
// /proc/stat (CPU) and /proc/net/dev (network I/O) every 750 ms and
// stores the latest rates in s.metrics. On non-Linux hosts both sources
// return zero-value samples gracefully, leaving all metrics at 0.
func (s *Server) startMetricsCollector() {
	prevCPU := readCPUSample()
	prevNet := readNetSample()
	prevTime := time.Now()

	go func() {
		ticker := time.NewTicker(750 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			now := time.Now()
			elapsed := now.Sub(prevTime).Seconds()
			if elapsed <= 0 {
				continue
			}

			cpu := readCPUSample()
			net := readNetSample()

			totalDelta := cpu.total - prevCPU.total
			idleDelta := cpu.idle - prevCPU.idle
			var cpuPct float64
			if totalDelta > 0 {
				cpuPct = (1.0 - float64(idleDelta)/float64(totalDelta)) * 100
			}

			inBps := float64(net.rxBytes-prevNet.rxBytes) / elapsed
			outBps := float64(net.txBytes-prevNet.txBytes) / elapsed
			if inBps < 0 {
				inBps = 0
			}
			if outBps < 0 {
				outBps = 0
			}

			memPct := readMemPercent()

			s.metrics.mu.Lock()
			s.metrics.cpuPercent = cpuPct
			s.metrics.memPercent = memPct
			s.metrics.netInBps = inBps
			s.metrics.netOutBps = outBps
			s.metrics.mu.Unlock()

			prevCPU = cpu
			prevNet = net
			prevTime = now
		}
	}()
}

// handleMetrics serves GET /api/metrics with the latest cached system stats.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.metrics.mu.RLock()
	out := struct {
		CPUPct    float64 `json:"cpu_pct"`
		MemPct    float64 `json:"mem_pct"`
		NetInBps  float64 `json:"net_in_bps"`
		NetOutBps float64 `json:"net_out_bps"`
	}{
		CPUPct:    s.metrics.cpuPercent,
		MemPct:    s.metrics.memPercent,
		NetInBps:  s.metrics.netInBps,
		NetOutBps: s.metrics.netOutBps,
	}
	s.metrics.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out) //nolint:errcheck
}

// readCPUSample reads the aggregate CPU time counters from /proc/stat.
// Returns a zero-value sample on non-Linux systems or read errors.
func readCPUSample() cpuSample {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuSample{}
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		// fields: [cpu user nice system idle iowait irq softirq steal ...]
		fields := strings.Fields(line)
		if len(fields) < 5 {
			break
		}
		var total uint64
		for _, fv := range fields[1:] {
			v, _ := strconv.ParseUint(fv, 10, 64)
			total += v
		}
		idle, _ := strconv.ParseUint(fields[4], 10, 64)
		return cpuSample{total: total, idle: idle}
	}
	return cpuSample{}
}

// readNetSample sums RX/TX bytes across all non-loopback interfaces from
// /proc/net/dev. Returns zero values on non-Linux or read errors.
func readNetSample() netSample {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return netSample{}
	}
	defer f.Close()

	var rx, tx uint64
	sc := bufio.NewScanner(f)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		if lineNum <= 2 {
			continue // skip two header lines
		}
		line := sc.Text()
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		if strings.TrimSpace(line[:idx]) == "lo" {
			continue
		}
		// fields after colon: rx_bytes packets errs ... tx_bytes packets ...
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return netSample{rxBytes: rx, txBytes: tx}
}

// readMemPercent reads MemTotal and MemAvailable from /proc/meminfo and
// returns used memory as a percentage. Returns 0 on non-Linux or read errors.
func readMemPercent() float64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()

	var total, available uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			available = v
		}
		if total > 0 && available > 0 {
			break
		}
	}
	if total == 0 {
		return 0
	}
	used := total - available
	return float64(used) / float64(total) * 100
}
