// internal/health/health.go
package health

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/backupmanager/backupmanager/internal/connector"
	"github.com/backupmanager/backupmanager/internal/database"
)

// CheckResult holds the outcome of a single health check.
type CheckResult struct {
	CheckType string `json:"check_type"` // "reachability", "disk", "nginx", "mysql", "pm2", "cpu", "ram", "ftp"
	Status    string `json:"status"`     // "ok", "warning", "critical"
	Message   string `json:"message"`
	Value     string `json:"value"` // raw value (e.g., "85%" for disk)
}

// ServerHealth aggregates all check results for one server.
type ServerHealth struct {
	ServerID  int           `json:"server_id"`
	Checks    []CheckResult `json:"checks"`
	CheckedAt time.Time     `json:"checked_at"`
	Overall   string        `json:"overall"` // worst status among checks
}

// HealthService runs health checks and persists results.
type HealthService struct {
	db *database.Database
}

// NewHealthService constructs a HealthService.
func NewHealthService(db *database.Database) *HealthService {
	return &HealthService{db: db}
}

// CheckServer runs all applicable health checks on a server.
// serverType must be "linux" or "windows".
// For linux: reachability, disk, nginx, mysql, pm2, cpu, ram.
// For windows: reachability, ftp login.
func (s *HealthService) CheckServer(ctx context.Context, conn connector.Connector, serverType string, host string, port int, ftpUser, ftpPass string) *ServerHealth {
	health := &ServerHealth{
		CheckedAt: time.Now().UTC(),
	}

	const reachTimeout = 5 * time.Second

	switch serverType {
	case "linux":
		// Reachability check.
		health.Checks = append(health.Checks, CheckReachability(host, port, reachTimeout))

		// Run SSH commands.
		if conn != nil {
			// Disk space.
			if result, err := conn.RunCommand(ctx, "df -h /"); err == nil {
				health.Checks = append(health.Checks, ParseDiskSpace(result.Stdout))
			} else {
				health.Checks = append(health.Checks, CheckResult{
					CheckType: "disk",
					Status:    "warning",
					Message:   fmt.Sprintf("could not run df: %v", err),
				})
			}

			// NGINX.
			if result, err := conn.RunCommand(ctx, "systemctl is-active nginx"); err == nil {
				health.Checks = append(health.Checks, ParseNginxStatus(result.Stdout))
			} else {
				health.Checks = append(health.Checks, CheckResult{
					CheckType: "nginx",
					Status:    "warning",
					Message:   fmt.Sprintf("could not check nginx: %v", err),
				})
			}

			// MySQL.
			if result, err := conn.RunCommand(ctx, "mysqladmin ping 2>/dev/null"); err == nil {
				health.Checks = append(health.Checks, ParseMySQLStatus(result.Stdout, result.ExitCode))
			} else {
				health.Checks = append(health.Checks, CheckResult{
					CheckType: "mysql",
					Status:    "warning",
					Message:   fmt.Sprintf("could not check mysql: %v", err),
				})
			}

			// PM2.
			if result, err := conn.RunCommand(ctx, "pm2 jlist 2>/dev/null"); err == nil {
				health.Checks = append(health.Checks, ParsePM2Status(result.Stdout))
			} else {
				health.Checks = append(health.Checks, CheckResult{
					CheckType: "pm2",
					Status:    "warning",
					Message:   fmt.Sprintf("could not check pm2: %v", err),
				})
			}

			// CPU load.
			if result, err := conn.RunCommand(ctx, "uptime"); err == nil {
				health.Checks = append(health.Checks, ParseCPULoad(result.Stdout))
			} else {
				health.Checks = append(health.Checks, CheckResult{
					CheckType: "cpu",
					Status:    "warning",
					Message:   fmt.Sprintf("could not check cpu: %v", err),
				})
			}

			// RAM.
			if result, err := conn.RunCommand(ctx, "free -m"); err == nil {
				health.Checks = append(health.Checks, ParseRAMUsage(result.Stdout))
			} else {
				health.Checks = append(health.Checks, CheckResult{
					CheckType: "ram",
					Status:    "warning",
					Message:   fmt.Sprintf("could not check ram: %v", err),
				})
			}
		}

	case "windows":
		// Reachability check.
		health.Checks = append(health.Checks, CheckReachability(host, port, reachTimeout))
		// FTP login check.
		health.Checks = append(health.Checks, CheckFTPLogin(host, port, ftpUser, ftpPass, reachTimeout))

	default:
		health.Checks = append(health.Checks, CheckResult{
			CheckType: "reachability",
			Status:    "warning",
			Message:   fmt.Sprintf("unknown server type: %s", serverType),
		})
	}

	health.Overall = worstStatus(health.Checks)
	return health
}

// SaveResults stores check results in the health_checks table.
func (s *HealthService) SaveResults(serverID int, health *ServerHealth) error {
	now := health.CheckedAt.Format(time.RFC3339)
	for _, check := range health.Checks {
		_, err := s.db.DB().Exec(
			`INSERT INTO health_checks (server_id, check_type, status, message, value, created_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			serverID, check.CheckType, check.Status, check.Message, check.Value, now,
		)
		if err != nil {
			return fmt.Errorf("insert health check (server=%d, type=%s): %w", serverID, check.CheckType, err)
		}
	}
	return nil
}

// GetCurrentHealth returns the latest health status for all servers.
// It fetches the most recent check batch per server.
func (s *HealthService) GetCurrentHealth(ctx context.Context) ([]ServerHealth, error) {
	// For each server, get all checks from the latest check round (by max created_at).
	rows, err := s.db.DB().QueryContext(ctx, `
		SELECT hc.server_id, hc.check_type, hc.status, hc.message, hc.value, hc.created_at
		FROM health_checks hc
		INNER JOIN (
			SELECT server_id, MAX(created_at) AS latest
			FROM health_checks
			GROUP BY server_id
		) latest ON hc.server_id = latest.server_id AND hc.created_at = latest.latest
		ORDER BY hc.server_id, hc.check_type
	`)
	if err != nil {
		return nil, fmt.Errorf("query current health: %w", err)
	}
	defer rows.Close()

	return scanServerHealthRows(rows)
}

// GetServerHistory returns health check history for one server, grouped by check time.
func (s *HealthService) GetServerHistory(serverID int, limit int) ([]ServerHealth, error) {
	if limit <= 0 {
		limit = 50
	}

	// Find the `limit` most recent distinct check times for this server.
	timeRows, err := s.db.DB().Query(`
		SELECT DISTINCT created_at
		FROM health_checks
		WHERE server_id = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, serverID, limit)
	if err != nil {
		return nil, fmt.Errorf("query health history times: %w", err)
	}
	defer timeRows.Close()

	var times []string
	for timeRows.Next() {
		var t string
		if err := timeRows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan history time: %w", err)
		}
		times = append(times, t)
	}
	if err := timeRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate history times: %w", err)
	}

	if len(times) == 0 {
		return []ServerHealth{}, nil
	}

	// Build one ServerHealth per distinct time.
	result := make([]ServerHealth, 0, len(times))
	for _, t := range times {
		rows, err := s.db.DB().Query(`
			SELECT check_type, status, message, value, created_at
			FROM health_checks
			WHERE server_id = ? AND created_at = ?
			ORDER BY check_type
		`, serverID, t)
		if err != nil {
			return nil, fmt.Errorf("query health at %s: %w", t, err)
		}

		sh := ServerHealth{ServerID: serverID}
		for rows.Next() {
			var cr CheckResult
			var createdAt string
			if err := rows.Scan(&cr.CheckType, &cr.Status, &cr.Message, &cr.Value, &createdAt); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan health check: %w", err)
			}
			sh.Checks = append(sh.Checks, cr)
			if sh.CheckedAt.IsZero() {
				sh.CheckedAt = parseHealthTime(createdAt)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate health checks: %w", err)
		}

		sh.Overall = worstStatus(sh.Checks)
		result = append(result, sh)
	}

	return result, nil
}

// --- helpers ---

// worstStatus returns the most severe status among all checks.
// Priority: critical > warning > ok.
func worstStatus(checks []CheckResult) string {
	worst := "ok"
	for _, c := range checks {
		switch c.Status {
		case "critical":
			return "critical"
		case "warning":
			worst = "warning"
		}
	}
	return worst
}

// scanServerHealthRows reads rows of (server_id, check_type, status, message, value, created_at)
// and groups them into ServerHealth values.
func scanServerHealthRows(rows *sql.Rows) ([]ServerHealth, error) {
	healthMap := make(map[int]*ServerHealth)
	var order []int

	for rows.Next() {
		var serverID int
		var cr CheckResult
		var createdAt string
		if err := rows.Scan(&serverID, &cr.CheckType, &cr.Status, &cr.Message, &cr.Value, &createdAt); err != nil {
			return nil, fmt.Errorf("scan health row: %w", err)
		}

		sh, exists := healthMap[serverID]
		if !exists {
			sh = &ServerHealth{
				ServerID:  serverID,
				CheckedAt: parseHealthTime(createdAt),
			}
			healthMap[serverID] = sh
			order = append(order, serverID)
		}
		sh.Checks = append(sh.Checks, cr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate health rows: %w", err)
	}

	result := make([]ServerHealth, 0, len(order))
	for _, id := range order {
		sh := healthMap[id]
		sh.Overall = worstStatus(sh.Checks)
		result = append(result, *sh)
	}
	return result, nil
}

// parseHealthTime parses datetime strings from SQLite.
func parseHealthTime(s string) time.Time {
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

// HealthMonitor periodically runs health checks on all servers.
type HealthMonitor struct {
	service  *HealthService
	db       *database.Database
	interval time.Duration
	stopCh   chan struct{}
}

// NewHealthMonitor creates a HealthMonitor.
func NewHealthMonitor(service *HealthService, db *database.Database, interval time.Duration) *HealthMonitor {
	return &HealthMonitor{
		service:  service,
		db:       db,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// Start begins periodic health checking of all servers.
func (m *HealthMonitor) Start() {
	go m.run()
}

// Stop halts the monitoring loop.
func (m *HealthMonitor) Stop() {
	close(m.stopCh)
}

type serverRow struct {
	id             int
	serverType     string
	host           string
	port           int
	connectionType string
	username       string
	password       sql.NullString
	sshKeyPath     sql.NullString
}

func (m *HealthMonitor) run() {
	// Run immediately, then on interval.
	m.checkAll()

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkAll()
		case <-m.stopCh:
			return
		}
	}
}

func (m *HealthMonitor) checkAll() {
	ctx := context.Background()

	rows, err := m.db.DB().QueryContext(ctx,
		`SELECT id, type, host, port, connection_type,
		        COALESCE(username,''), encrypted_password, ssh_key_path
		 FROM servers ORDER BY id ASC`)
	if err != nil {
		log.Printf("[health] failed to query servers: %v", err)
		return
	}
	defer rows.Close()

	var servers []serverRow
	for rows.Next() {
		var s serverRow
		if err := rows.Scan(&s.id, &s.serverType, &s.host, &s.port, &s.connectionType,
			&s.username, &s.password, &s.sshKeyPath); err != nil {
			log.Printf("[health] failed to scan server row: %v", err)
			continue
		}
		servers = append(servers, s)
	}
	if err := rows.Err(); err != nil {
		log.Printf("[health] row iteration error: %v", err)
		return
	}

	for _, srv := range servers {
		m.checkServer(ctx, srv)
	}
}

func (m *HealthMonitor) checkServer(ctx context.Context, srv serverRow) {
	var conn connector.Connector
	var health *ServerHealth

	switch srv.connectionType {
	case "ssh":
		cfg := connector.SSHConfig{
			Host:     srv.host,
			Port:     srv.port,
			Username: srv.username,
			Timeout:  10 * time.Second,
		}
		if srv.password.Valid {
			cfg.Password = srv.password.String
		}
		if srv.sshKeyPath.Valid {
			cfg.KeyPath = srv.sshKeyPath.String
		}
		sshConn := connector.NewSSHConnector(cfg)
		if err := sshConn.Connect(); err != nil {
			log.Printf("[health] server %d: SSH connect failed: %v", srv.id, err)
			// Still run reachability check (will fail), no SSH commands.
		} else {
			conn = sshConn
			defer sshConn.Close()
		}
		health = m.service.CheckServer(ctx, conn, srv.serverType, srv.host, srv.port, srv.username, "")

	case "ftp":
		pass := ""
		if srv.password.Valid {
			pass = srv.password.String
		}
		health = m.service.CheckServer(ctx, nil, srv.serverType, srv.host, srv.port, srv.username, pass)

	default:
		log.Printf("[health] server %d: unknown connection type %q", srv.id, srv.connectionType)
		return
	}

	health.ServerID = srv.id

	if err := m.service.SaveResults(srv.id, health); err != nil {
		log.Printf("[health] server %d: save results failed: %v", srv.id, err)
		return
	}

	log.Printf("[health] server %d checked: overall=%s (%d checks)", srv.id, health.Overall, len(health.Checks))
	// Note: status-change notifications will be integrated in Task 24.
}
