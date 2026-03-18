// internal/health/health_test.go
package health

import (
	"fmt"
	"net"
	"testing"
	"time"
)

// --- CheckReachability ---

func TestCheckReachability_Success(t *testing.T) {
	// Start a temporary listener on a random port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port

	result := CheckReachability("127.0.0.1", port, 5*time.Second)
	if result.Status != "ok" {
		t.Errorf("expected status ok, got %q (message: %s)", result.Status, result.Message)
	}
	if result.CheckType != "reachability" {
		t.Errorf("expected check_type reachability, got %q", result.CheckType)
	}
}

func TestCheckReachability_Failure(t *testing.T) {
	// Find a port that is not listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to find free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // Close immediately so the port is free.

	result := CheckReachability("127.0.0.1", port, 2*time.Second)
	if result.Status != "critical" {
		t.Errorf("expected status critical, got %q (message: %s)", result.Status, result.Message)
	}
}

// --- ParseDiskSpace ---

func makeDfOutput(usedPct int) string {
	free := 100 - usedPct
	return fmt.Sprintf("Filesystem      Size  Used Avail Use%% Mounted on\n/dev/sda1       100G   %dG   %dG  %d%% /\n", usedPct, free, usedPct)
}

func TestParseDiskSpace_Normal(t *testing.T) {
	output := makeDfOutput(45)
	result := ParseDiskSpace(output)
	if result.Status != "ok" {
		t.Errorf("expected ok for 45%% used, got %q (msg: %s)", result.Status, result.Message)
	}
	if result.CheckType != "disk" {
		t.Errorf("expected check_type disk, got %q", result.CheckType)
	}
}

func TestParseDiskSpace_Warning(t *testing.T) {
	output := makeDfOutput(85)
	result := ParseDiskSpace(output)
	if result.Status != "warning" {
		t.Errorf("expected warning for 85%% used (15%% free), got %q (msg: %s)", result.Status, result.Message)
	}
}

func TestParseDiskSpace_Critical(t *testing.T) {
	output := makeDfOutput(95)
	result := ParseDiskSpace(output)
	if result.Status != "critical" {
		t.Errorf("expected critical for 95%% used (5%% free), got %q (msg: %s)", result.Status, result.Message)
	}
}

func TestParseDiskSpace_MalformedOutput(t *testing.T) {
	result := ParseDiskSpace("garbage input that makes no sense\n")
	if result.Status != "warning" {
		t.Errorf("expected warning for malformed output, got %q (msg: %s)", result.Status, result.Message)
	}
	if result.CheckType != "disk" {
		t.Errorf("expected check_type disk, got %q", result.CheckType)
	}
}

// --- ParseNginxStatus ---

func TestParseNginxStatus_Active(t *testing.T) {
	result := ParseNginxStatus("active\n")
	if result.Status != "ok" {
		t.Errorf("expected ok, got %q (msg: %s)", result.Status, result.Message)
	}
	if result.CheckType != "nginx" {
		t.Errorf("expected check_type nginx, got %q", result.CheckType)
	}
}

func TestParseNginxStatus_Inactive(t *testing.T) {
	result := ParseNginxStatus("inactive\n")
	if result.Status != "critical" {
		t.Errorf("expected critical, got %q (msg: %s)", result.Status, result.Message)
	}
}

// --- ParseMySQLStatus ---

func TestParseMySQLStatus_Running(t *testing.T) {
	result := ParseMySQLStatus("mysqld is alive\n", 0)
	if result.Status != "ok" {
		t.Errorf("expected ok, got %q (msg: %s)", result.Status, result.Message)
	}
	if result.CheckType != "mysql" {
		t.Errorf("expected check_type mysql, got %q", result.CheckType)
	}
}

func TestParseMySQLStatus_NotRunning(t *testing.T) {
	result := ParseMySQLStatus("", 1)
	if result.Status != "critical" {
		t.Errorf("expected critical, got %q (msg: %s)", result.Status, result.Message)
	}
}

// --- ParsePM2Status ---

func TestParsePM2Status_AllOnline(t *testing.T) {
	jsonInput := `[{"name":"api","pm2_env":{"status":"online"}},{"name":"worker","pm2_env":{"status":"online"}}]`
	result := ParsePM2Status(jsonInput)
	if result.Status != "ok" {
		t.Errorf("expected ok, got %q (msg: %s)", result.Status, result.Message)
	}
	if result.CheckType != "pm2" {
		t.Errorf("expected check_type pm2, got %q", result.CheckType)
	}
}

func TestParsePM2Status_SomeStopped(t *testing.T) {
	jsonInput := `[{"name":"api","pm2_env":{"status":"online"}},{"name":"worker","pm2_env":{"status":"stopped"}}]`
	result := ParsePM2Status(jsonInput)
	if result.Status != "warning" {
		t.Errorf("expected warning, got %q (msg: %s)", result.Status, result.Message)
	}
}

// --- ParseCPULoad ---

func TestParseCPULoad_Normal(t *testing.T) {
	output := " 14:30:00 up 5 days,  3:42,  2 users,  load average: 0.50, 0.40, 0.30\n"
	result := ParseCPULoad(output)
	if result.Status != "ok" {
		t.Errorf("expected ok for load 0.50, got %q (msg: %s)", result.Status, result.Message)
	}
	if result.CheckType != "cpu" {
		t.Errorf("expected check_type cpu, got %q", result.CheckType)
	}
}

func TestParseCPULoad_High(t *testing.T) {
	output := " 14:30:00 up 1 day,  3:42,  2 users,  load average: 0.95, 0.80, 0.70\n"
	result := ParseCPULoad(output)
	if result.Status != "critical" {
		t.Errorf("expected critical for load 0.95, got %q (msg: %s)", result.Status, result.Message)
	}
}

// --- ParseRAMUsage ---

func TestParseRAMUsage_Normal(t *testing.T) {
	// total=8000, used=3200, free=800, shared=200, buff/cache=4000, available=4800
	// available/total = 4800/8000 = 60%
	output := "              total        used        free      shared  buff/cache   available\nMem:           8000        3200         800         200        4000        4800\nSwap:          2047           0        2047\n"
	result := ParseRAMUsage(output)
	if result.Status != "ok" {
		t.Errorf("expected ok for 60%% available RAM, got %q (msg: %s)", result.Status, result.Message)
	}
	if result.CheckType != "ram" {
		t.Errorf("expected check_type ram, got %q", result.CheckType)
	}
}

func TestParseRAMUsage_Critical(t *testing.T) {
	// total=8000, available=400 → 5% available → critical
	output := "              total        used        free      shared  buff/cache   available\nMem:           8000        7800         100         100         100         400\nSwap:          2047           0        2047\n"
	result := ParseRAMUsage(output)
	if result.Status != "critical" {
		t.Errorf("expected critical for ~5%% available RAM, got %q (msg: %s)", result.Status, result.Message)
	}
}
