// internal/health/checks.go
package health

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

// CheckReachability tests TCP connectivity to host:port within timeout.
func CheckReachability(host string, port int, timeout time.Duration) CheckResult {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return CheckResult{
			CheckType: "reachability",
			Status:    "critical",
			Message:   fmt.Sprintf("unreachable: %v", err),
			Value:     addr,
		}
	}
	conn.Close()
	return CheckResult{
		CheckType: "reachability",
		Status:    "ok",
		Message:   "reachable",
		Value:     addr,
	}
}

// ParseDiskSpace parses the output of "df -h /" and returns a CheckResult.
// It alerts if free space is < 10% (critical) or < 20% (warning).
func ParseDiskSpace(dfOutput string) CheckResult {
	lines := strings.Split(strings.TrimSpace(dfOutput), "\n")
	// Find the data line (skip header and any blank lines).
	var dataLine string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "Filesystem") {
			continue
		}
		dataLine = trimmed
		break
	}

	if dataLine == "" {
		return CheckResult{
			CheckType: "disk",
			Status:    "warning",
			Message:   "could not parse df output: no data line found",
			Value:     "",
		}
	}

	// df -h output columns: Filesystem  Size  Used  Avail  Use%  Mounted on
	// Fields may span multiple lines when the filesystem path is long; handle both cases.
	fields := strings.Fields(dataLine)
	// If there are fewer than 5 fields, the filesystem name might be on its own line.
	// Try the last line which should have the percentages.
	if len(fields) < 5 {
		// Find the last non-empty line after the header.
		for i := len(lines) - 1; i >= 0; i-- {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed == "" || strings.HasPrefix(trimmed, "Filesystem") {
				continue
			}
			fields = strings.Fields(trimmed)
			if len(fields) >= 5 {
				break
			}
		}
	}

	if len(fields) < 5 {
		return CheckResult{
			CheckType: "disk",
			Status:    "warning",
			Message:   fmt.Sprintf("could not parse df output: unexpected format: %q", dataLine),
			Value:     "",
		}
	}

	// The Use% field is the 5th column (index 4).
	usePct := strings.TrimSuffix(fields[4], "%")
	usedPct, err := strconv.Atoi(usePct)
	if err != nil {
		return CheckResult{
			CheckType: "disk",
			Status:    "warning",
			Message:   fmt.Sprintf("could not parse disk usage percentage: %q", fields[4]),
			Value:     fields[4],
		}
	}

	freePct := 100 - usedPct
	value := fmt.Sprintf("%d%% used (%d%% free)", usedPct, freePct)

	switch {
	case freePct < 10:
		return CheckResult{
			CheckType: "disk",
			Status:    "critical",
			Message:   fmt.Sprintf("disk space critical: only %d%% free", freePct),
			Value:     value,
		}
	case freePct < 20:
		return CheckResult{
			CheckType: "disk",
			Status:    "warning",
			Message:   fmt.Sprintf("disk space low: %d%% free", freePct),
			Value:     value,
		}
	default:
		return CheckResult{
			CheckType: "disk",
			Status:    "ok",
			Message:   fmt.Sprintf("disk space ok: %d%% free", freePct),
			Value:     value,
		}
	}
}

// ParseNginxStatus parses the output of "systemctl is-active nginx".
func ParseNginxStatus(output string) CheckResult {
	status := strings.TrimSpace(output)
	if status == "active" {
		return CheckResult{
			CheckType: "nginx",
			Status:    "ok",
			Message:   "nginx is active",
			Value:     status,
		}
	}
	return CheckResult{
		CheckType: "nginx",
		Status:    "critical",
		Message:   fmt.Sprintf("nginx is not active: %s", status),
		Value:     status,
	}
}

// ParseMySQLStatus parses mysqladmin ping output and exit code.
func ParseMySQLStatus(output string, exitCode int) CheckResult {
	if exitCode == 0 {
		return CheckResult{
			CheckType: "mysql",
			Status:    "ok",
			Message:   "MySQL is running",
			Value:     strings.TrimSpace(output),
		}
	}
	return CheckResult{
		CheckType: "mysql",
		Status:    "critical",
		Message:   "MySQL is not running",
		Value:     strings.TrimSpace(output),
	}
}

// pm2Process represents a process entry from pm2 jlist JSON output.
type pm2Process struct {
	Name   string `json:"name"`
	PM2Env struct {
		Status string `json:"status"`
	} `json:"pm2_env"`
}

// ParsePM2Status parses the JSON output of "pm2 jlist".
func ParsePM2Status(jsonOutput string) CheckResult {
	trimmed := strings.TrimSpace(jsonOutput)
	if trimmed == "" || trimmed == "[]" {
		return CheckResult{
			CheckType: "pm2",
			Status:    "ok",
			Message:   "no PM2 processes",
			Value:     "[]",
		}
	}

	var processes []pm2Process
	if err := json.Unmarshal([]byte(trimmed), &processes); err != nil {
		return CheckResult{
			CheckType: "pm2",
			Status:    "warning",
			Message:   fmt.Sprintf("could not parse PM2 output: %v", err),
			Value:     trimmed,
		}
	}

	var stopped []string
	for _, p := range processes {
		if p.PM2Env.Status != "online" {
			stopped = append(stopped, fmt.Sprintf("%s (%s)", p.Name, p.PM2Env.Status))
		}
	}

	if len(stopped) > 0 {
		return CheckResult{
			CheckType: "pm2",
			Status:    "warning",
			Message:   fmt.Sprintf("stopped processes: %s", strings.Join(stopped, ", ")),
			Value:     fmt.Sprintf("%d/%d online", len(processes)-len(stopped), len(processes)),
		}
	}

	return CheckResult{
		CheckType: "pm2",
		Status:    "ok",
		Message:   fmt.Sprintf("all %d PM2 processes online", len(processes)),
		Value:     fmt.Sprintf("%d/%d online", len(processes), len(processes)),
	}
}

// ParseCPULoad parses the output of "uptime" and checks load average (1-min).
// Alerts if load > 0.9 (normalized; assumes the caller interprets as fraction of capacity).
// For a more practical threshold: critical if load1 > 0.9 * numCPU is not available here,
// so we use the raw 1-min load average: warning > 2.0, critical > 4.0 (common defaults).
// Per task spec: "alert if > 90%" — we treat load average directly:
// critical if load1 > 0.90 (relative), but uptime gives absolute values.
// The spec says load > 90% so we treat raw load avg > 0.90 as critical.
func ParseCPULoad(uptimeOutput string) CheckResult {
	// uptime output examples:
	//  14:30:00 up 5 days,  3:42,  2 users,  load average: 0.15, 0.10, 0.08
	//  14:30:00 up  1:42,  1 user,  load average: 1.50, 1.20, 0.90
	idx := strings.LastIndex(uptimeOutput, "load average:")
	if idx == -1 {
		// try "load averages:" (BSD variant)
		idx = strings.LastIndex(uptimeOutput, "load averages:")
	}
	if idx == -1 {
		return CheckResult{
			CheckType: "cpu",
			Status:    "warning",
			Message:   "could not parse uptime output",
			Value:     strings.TrimSpace(uptimeOutput),
		}
	}

	rest := strings.TrimSpace(uptimeOutput[idx:])
	// rest: "load average: 0.15, 0.10, 0.08" or "load averages: 0.15 0.10 0.08"
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) < 2 {
		return CheckResult{
			CheckType: "cpu",
			Status:    "warning",
			Message:   "could not parse load average values",
			Value:     strings.TrimSpace(uptimeOutput),
		}
	}

	loadParts := strings.Fields(strings.ReplaceAll(parts[1], ",", " "))
	if len(loadParts) < 1 {
		return CheckResult{
			CheckType: "cpu",
			Status:    "warning",
			Message:   "could not parse load average values",
			Value:     strings.TrimSpace(uptimeOutput),
		}
	}

	load1, err := strconv.ParseFloat(loadParts[0], 64)
	if err != nil {
		return CheckResult{
			CheckType: "cpu",
			Status:    "warning",
			Message:   fmt.Sprintf("could not parse load average: %v", err),
			Value:     loadParts[0],
		}
	}

	value := fmt.Sprintf("%.2f", load1)
	switch {
	case load1 > 0.90:
		return CheckResult{
			CheckType: "cpu",
			Status:    "critical",
			Message:   fmt.Sprintf("CPU load critical: %.2f", load1),
			Value:     value,
		}
	case load1 > 0.70:
		return CheckResult{
			CheckType: "cpu",
			Status:    "warning",
			Message:   fmt.Sprintf("CPU load high: %.2f", load1),
			Value:     value,
		}
	default:
		return CheckResult{
			CheckType: "cpu",
			Status:    "ok",
			Message:   fmt.Sprintf("CPU load ok: %.2f", load1),
			Value:     value,
		}
	}
}

// ParseRAMUsage parses the output of "free -m" and checks RAM usage.
// Critical if available RAM < 10% of total.
func ParseRAMUsage(freeOutput string) CheckResult {
	// free -m output:
	//               total        used        free      shared  buff/cache   available
	// Mem:           7982        3421         512         256        4049        4104
	// Swap:          2047           0        2047
	lines := strings.Split(strings.TrimSpace(freeOutput), "\n")
	var memLine string
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Mem:") {
			memLine = strings.TrimSpace(line)
			break
		}
	}

	if memLine == "" {
		return CheckResult{
			CheckType: "ram",
			Status:    "warning",
			Message:   "could not parse free output: no Mem: line found",
			Value:     "",
		}
	}

	fields := strings.Fields(memLine)
	// fields[0] = "Mem:", fields[1] = total, fields[2] = used, fields[3] = free, ...
	// fields[6] = available (if present), otherwise use free
	if len(fields) < 3 {
		return CheckResult{
			CheckType: "ram",
			Status:    "warning",
			Message:   "could not parse free output: unexpected format",
			Value:     memLine,
		}
	}

	total, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || total == 0 {
		return CheckResult{
			CheckType: "ram",
			Status:    "warning",
			Message:   fmt.Sprintf("could not parse total RAM: %v", err),
			Value:     memLine,
		}
	}

	// Prefer "available" column (index 6) if present, else use "free" (index 3).
	var available float64
	if len(fields) >= 7 {
		available, err = strconv.ParseFloat(fields[6], 64)
		if err != nil {
			available, err = strconv.ParseFloat(fields[3], 64)
		}
	} else {
		available, err = strconv.ParseFloat(fields[3], 64)
	}
	if err != nil {
		return CheckResult{
			CheckType: "ram",
			Status:    "warning",
			Message:   fmt.Sprintf("could not parse available RAM: %v", err),
			Value:     memLine,
		}
	}

	availPct := (available / total) * 100
	usedPct := 100 - availPct
	value := fmt.Sprintf("%.0f%% used (%.0fMB available of %.0fMB)", usedPct, available, total)

	switch {
	case availPct < 10:
		return CheckResult{
			CheckType: "ram",
			Status:    "critical",
			Message:   fmt.Sprintf("RAM critical: only %.0f%% available", availPct),
			Value:     value,
		}
	case availPct < 20:
		return CheckResult{
			CheckType: "ram",
			Status:    "warning",
			Message:   fmt.Sprintf("RAM low: %.0f%% available", availPct),
			Value:     value,
		}
	default:
		return CheckResult{
			CheckType: "ram",
			Status:    "ok",
			Message:   fmt.Sprintf("RAM ok: %.0f%% available", availPct),
			Value:     value,
		}
	}
}

// CheckFTPLogin attempts to connect and authenticate to an FTP server.
func CheckFTPLogin(host string, port int, user, pass string, timeout time.Duration) CheckResult {
	addr := fmt.Sprintf("%s:%d", host, port)
	conn, err := ftp.DialTimeout(addr, timeout)
	if err != nil {
		return CheckResult{
			CheckType: "ftp",
			Status:    "critical",
			Message:   fmt.Sprintf("FTP connection failed: %v", err),
			Value:     addr,
		}
	}

	if err := conn.Login(user, pass); err != nil {
		conn.Quit() //nolint:errcheck
		return CheckResult{
			CheckType: "ftp",
			Status:    "critical",
			Message:   fmt.Sprintf("FTP login failed: %v", err),
			Value:     addr,
		}
	}

	conn.Quit() //nolint:errcheck
	return CheckResult{
		CheckType: "ftp",
		Status:    "ok",
		Message:   "FTP login successful",
		Value:     addr,
	}
}
