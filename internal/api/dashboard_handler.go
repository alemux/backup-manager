// internal/api/dashboard_handler.go
package api

import (
	"net/http"
	"syscall"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

// DashboardHandler handles GET /api/dashboard/summary.
type DashboardHandler struct {
	db *database.Database
}

// NewDashboardHandler constructs a DashboardHandler.
func NewDashboardHandler(db *database.Database) *DashboardHandler {
	return &DashboardHandler{db: db}
}

// dashboardCheckResult mirrors health.CheckResult for JSON output.
type dashboardCheckResult struct {
	CheckType string `json:"check_type"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	Value     string `json:"value"`
}

// dashboardServerStatus is one server row with its latest health checks.
type dashboardServerStatus struct {
	ID        int                    `json:"id"`
	Name      string                 `json:"name"`
	Type      string                 `json:"type"`
	Status    string                 `json:"status"`
	LastCheck string                 `json:"last_check"`
	Checks    []dashboardCheckResult `json:"checks"`
}

// dashboardRecentRun is a recent backup run with job and server names.
type dashboardRecentRun struct {
	ID             int    `json:"id"`
	JobName        string `json:"job_name"`
	ServerName     string `json:"server_name"`
	Status         string `json:"status"`
	StartedAt      string `json:"started_at"`
	FinishedAt     string `json:"finished_at"`
	TotalSizeBytes int64  `json:"total_size_bytes"`
}

// dashboardDiskUsage describes disk space for the backup data directory.
type dashboardDiskUsage struct {
	Path        string  `json:"path"`
	TotalBytes  uint64  `json:"total_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsePercent  float64 `json:"use_percent"`
}

// dashboardAlert represents an active alert.
type dashboardAlert struct {
	ID         string `json:"id"`
	Severity   string `json:"severity"` // "critical", "warning", "info"
	Title      string `json:"title"`
	Message    string `json:"message"`
	ServerName string `json:"server_name"`
	Timestamp  string `json:"timestamp"`
}

// dashboardStats holds aggregate statistics.
type dashboardStats struct {
	TotalServers   int `json:"total_servers"`
	ServersOnline  int `json:"servers_online"`
	TotalJobs      int `json:"total_jobs"`
	Last24hRuns    int `json:"last_24h_runs"`
	Last24hSuccess int `json:"last_24h_success"`
	Last24hFailed  int `json:"last_24h_failed"`
}

// dashboardSummary is the full payload returned by GET /api/dashboard/summary.
type dashboardSummary struct {
	Servers    []dashboardServerStatus `json:"servers"`
	RecentRuns []dashboardRecentRun    `json:"recent_runs"`
	DiskUsage  []dashboardDiskUsage    `json:"disk_usage"`
	Alerts     []dashboardAlert        `json:"alerts"`
	Stats      dashboardStats          `json:"stats"`
}

// GetSummary handles GET /api/dashboard/summary.
func (h *DashboardHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// --- servers with latest health checks ---
	serverRows, err := h.db.DB().QueryContext(ctx,
		`SELECT id, name, type, COALESCE(status,'unknown') FROM servers ORDER BY id ASC`)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query servers")
		return
	}
	defer serverRows.Close()

	type serverBasic struct {
		id     int
		name   string
		typ    string
		status string
	}
	var srvList []serverBasic
	for serverRows.Next() {
		var s serverBasic
		if err := serverRows.Scan(&s.id, &s.name, &s.typ, &s.status); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan server")
			return
		}
		srvList = append(srvList, s)
	}
	if err := serverRows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	servers := make([]dashboardServerStatus, 0, len(srvList))
	for _, s := range srvList {
		dss := dashboardServerStatus{
			ID:     s.id,
			Name:   s.name,
			Type:   s.typ,
			Status: s.status,
			Checks: []dashboardCheckResult{},
		}

		// Fetch latest health checks for this server.
		hRows, err := h.db.DB().QueryContext(ctx, `
			SELECT hc.check_type, hc.status, hc.message, hc.value, hc.created_at
			FROM health_checks hc
			INNER JOIN (
				SELECT MAX(created_at) AS latest
				FROM health_checks
				WHERE server_id = ?
			) latest ON hc.created_at = latest.latest
			WHERE hc.server_id = ?
			ORDER BY hc.check_type
		`, s.id, s.id)
		if err == nil {
			for hRows.Next() {
				var cr dashboardCheckResult
				var createdAt string
				if err := hRows.Scan(&cr.CheckType, &cr.Status, &cr.Message, &cr.Value, &createdAt); err == nil {
					dss.Checks = append(dss.Checks, cr)
					if dss.LastCheck == "" {
						dss.LastCheck = createdAt
					}
				}
			}
			hRows.Close()
		}

		// Derive overall status from checks if available.
		if len(dss.Checks) > 0 {
			overall := "ok"
			for _, c := range dss.Checks {
				if c.Status == "critical" {
					overall = "critical"
					break
				}
				if c.Status == "warning" {
					overall = "warning"
				}
			}
			dss.Status = overall
		}

		servers = append(servers, dss)
	}

	// --- recent runs (last 24h) ---
	since := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)
	runRows, err := h.db.DB().QueryContext(ctx, `
		SELECT br.id, bj.name, s.name, br.status,
		       COALESCE(br.started_at,''), COALESCE(br.finished_at,''),
		       COALESCE(br.total_size_bytes,0)
		FROM backup_runs br
		INNER JOIN backup_jobs bj ON bj.id = br.job_id
		INNER JOIN servers s ON s.id = bj.server_id
		WHERE br.started_at >= ?
		ORDER BY br.id DESC
		LIMIT 50
	`, since)
	if err != nil {
		Error(w, http.StatusInternalServerError, "failed to query recent runs")
		return
	}
	defer runRows.Close()

	recentRuns := make([]dashboardRecentRun, 0)
	for runRows.Next() {
		var rr dashboardRecentRun
		if err := runRows.Scan(&rr.ID, &rr.JobName, &rr.ServerName, &rr.Status,
			&rr.StartedAt, &rr.FinishedAt, &rr.TotalSizeBytes); err != nil {
			Error(w, http.StatusInternalServerError, "failed to scan run")
			return
		}
		recentRuns = append(recentRuns, rr)
	}
	if err := runRows.Err(); err != nil {
		Error(w, http.StatusInternalServerError, "row iteration error")
		return
	}

	// --- disk usage ---
	diskUsage := make([]dashboardDiskUsage, 0)
	du := getDiskUsage("/")
	diskUsage = append(diskUsage, du)

	// --- alerts ---
	alerts := make([]dashboardAlert, 0)
	alertID := 0

	// Alert: servers with critical health status.
	for _, s := range servers {
		if s.Status == "critical" {
			alertID++
			alerts = append(alerts, dashboardAlert{
				ID:         formatAlertID(alertID),
				Severity:   "critical",
				Title:      "Server unreachable",
				Message:    "Health check reported critical status",
				ServerName: s.Name,
				Timestamp:  s.LastCheck,
			})
		} else if s.Status == "warning" {
			alertID++
			alerts = append(alerts, dashboardAlert{
				ID:         formatAlertID(alertID),
				Severity:   "warning",
				Title:      "Server health warning",
				Message:    "One or more health checks reported a warning",
				ServerName: s.Name,
				Timestamp:  s.LastCheck,
			})
		}
	}

	// Alert: failed backup runs (last 24h).
	for _, rr := range recentRuns {
		if rr.Status == "failed" || rr.Status == "timeout" {
			alertID++
			sev := "critical"
			title := "Backup failed"
			if rr.Status == "timeout" {
				sev = "warning"
				title = "Backup timed out"
			}
			alerts = append(alerts, dashboardAlert{
				ID:         formatAlertID(alertID),
				Severity:   sev,
				Title:      title,
				Message:    "Job: " + rr.JobName,
				ServerName: rr.ServerName,
				Timestamp:  rr.StartedAt,
			})
		}
	}

	// Alert: disk usage > 90%.
	for _, du := range diskUsage {
		if du.UsePercent >= 90 {
			alertID++
			alerts = append(alerts, dashboardAlert{
				ID:         formatAlertID(alertID),
				Severity:   "critical",
				Title:      "Disk space critical",
				Message:    "Usage above 90% on " + du.Path,
				ServerName: "local",
				Timestamp:  time.Now().UTC().Format(time.RFC3339),
			})
		} else if du.UsePercent >= 70 {
			alertID++
			alerts = append(alerts, dashboardAlert{
				ID:         formatAlertID(alertID),
				Severity:   "warning",
				Title:      "Disk space low",
				Message:    "Usage above 70% on " + du.Path,
				ServerName: "local",
				Timestamp:  time.Now().UTC().Format(time.RFC3339),
			})
		}
	}

	// --- stats ---
	var totalServers, serversOnline, totalJobs int
	var last24hRuns, last24hSuccess, last24hFailed int

	_ = h.db.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM servers`).Scan(&totalServers)
	_ = h.db.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM servers WHERE status='online' OR status='ok'`).Scan(&serversOnline)
	_ = h.db.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_jobs WHERE enabled=1`).Scan(&totalJobs)

	// Count online servers from computed health statuses.
	serversOnline = 0
	for _, s := range servers {
		if s.Status == "ok" || s.Status == "online" {
			serversOnline++
		}
	}

	last24hRuns = len(recentRuns)
	for _, rr := range recentRuns {
		switch rr.Status {
		case "success":
			last24hSuccess++
		case "failed", "timeout":
			last24hFailed++
		}
	}

	// Also count runs still pending/running.
	var runningCount int
	_ = h.db.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM backup_runs WHERE started_at >= ? AND status IN ('pending','running')`, since,
	).Scan(&runningCount)

	JSON(w, http.StatusOK, dashboardSummary{
		Servers:    servers,
		RecentRuns: recentRuns,
		DiskUsage:  diskUsage,
		Alerts:     alerts,
		Stats: dashboardStats{
			TotalServers:   totalServers,
			ServersOnline:  serversOnline,
			TotalJobs:      totalJobs,
			Last24hRuns:    last24hRuns,
			Last24hSuccess: last24hSuccess,
			Last24hFailed:  last24hFailed,
		},
	})
}

// getDiskUsage returns disk usage statistics for the given path.
func getDiskUsage(path string) dashboardDiskUsage {
	var stat syscall.Statfs_t
	du := dashboardDiskUsage{Path: path}
	if err := syscall.Statfs(path, &stat); err != nil {
		return du
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free
	var pct float64
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}
	du.TotalBytes = total
	du.UsedBytes = used
	du.FreeBytes = free
	du.UsePercent = pct
	return du
}

// formatAlertID returns a string alert ID from an integer counter.
func formatAlertID(n int) string {
	return "alert-" + dashboardItoa(n)
}

func dashboardItoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
