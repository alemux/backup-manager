// internal/discovery/discovery.go
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/backupmanager/backupmanager/internal/connector"
	"github.com/backupmanager/backupmanager/internal/database"
)

// DiscoveryChange describes a single change detected between two scans.
type DiscoveryChange struct {
	Type     string `json:"type"`     // "added", "removed", "changed"
	Category string `json:"category"` // "database", "vhost", "service", "process"
	Name     string `json:"name"`
	Details  string `json:"details"`
}

// CompareResults compares previous and current discovery results.
// Returns a list of changes (new databases, removed vhosts, new services, etc.)
func CompareResults(previous, current []DiscoveredService) []DiscoveryChange {
	var changes []DiscoveryChange

	prevMap := make(map[string]DiscoveredService, len(previous))
	for _, s := range previous {
		prevMap[s.Name] = s
	}
	currMap := make(map[string]DiscoveredService, len(current))
	for _, s := range current {
		currMap[s.Name] = s
	}

	// Services added
	for _, s := range current {
		if _, found := prevMap[s.Name]; !found {
			changes = append(changes, DiscoveryChange{
				Type:     "added",
				Category: "service",
				Name:     s.Name,
				Details:  "new service detected",
			})
		}
	}

	// Services removed
	for _, s := range previous {
		if _, found := currMap[s.Name]; !found {
			changes = append(changes, DiscoveryChange{
				Type:     "removed",
				Category: "service",
				Name:     s.Name,
				Details:  "service no longer detected",
			})
		}
	}

	// Compare shared services in detail
	for _, curr := range current {
		prev, found := prevMap[curr.Name]
		if !found {
			continue
		}
		switch curr.Name {
		case "nginx":
			changes = append(changes, compareNginxVhosts(prev.Data, curr.Data)...)
		case "mysql":
			changes = append(changes, compareMySQLDatabases(prev.Data, curr.Data)...)
		case "redis":
			changes = append(changes, compareRedis(prev.Data, curr.Data)...)
		case "pm2":
			changes = append(changes, comparePM2Processes(prev.Data, curr.Data)...)
		}
	}

	if changes == nil {
		return []DiscoveryChange{}
	}
	return changes
}

// compareNginxVhosts detects added/removed vhosts between two nginx data maps.
func compareNginxVhosts(prev, curr map[string]interface{}) []DiscoveryChange {
	var changes []DiscoveryChange

	prevVhosts := extractVhostNames(prev)
	currVhosts := extractVhostNames(curr)

	prevSet := toSet(prevVhosts)
	currSet := toSet(currVhosts)

	for name := range currSet {
		if !prevSet[name] {
			changes = append(changes, DiscoveryChange{
				Type:     "added",
				Category: "vhost",
				Name:     name,
				Details:  "new nginx vhost detected",
			})
		}
	}
	for name := range prevSet {
		if !currSet[name] {
			changes = append(changes, DiscoveryChange{
				Type:     "removed",
				Category: "vhost",
				Name:     name,
				Details:  "nginx vhost no longer present",
			})
		}
	}
	return changes
}

// compareMySQLDatabases detects added/removed databases between two mysql data maps.
func compareMySQLDatabases(prev, curr map[string]interface{}) []DiscoveryChange {
	var changes []DiscoveryChange

	prevDBs := extractStringList(prev, "databases")
	currDBs := extractStringList(curr, "databases")

	prevSet := toSet(prevDBs)
	currSet := toSet(currDBs)

	for name := range currSet {
		if !prevSet[name] {
			changes = append(changes, DiscoveryChange{
				Type:     "added",
				Category: "database",
				Name:     name,
				Details:  "new MySQL database detected",
			})
		}
	}
	for name := range prevSet {
		if !currSet[name] {
			changes = append(changes, DiscoveryChange{
				Type:     "removed",
				Category: "database",
				Name:     name,
				Details:  "MySQL database no longer present",
			})
		}
	}
	return changes
}

// compareRedis detects database count changes in redis.
func compareRedis(prev, curr map[string]interface{}) []DiscoveryChange {
	var changes []DiscoveryChange

	prevCount := toInt(prev["databases"])
	currCount := toInt(curr["databases"])

	if prevCount != currCount {
		changes = append(changes, DiscoveryChange{
			Type:     "changed",
			Category: "database",
			Name:     "redis",
			Details:  fmt.Sprintf("database count changed from %d to %d", prevCount, currCount),
		})
	}
	return changes
}

// comparePM2Processes detects added/removed/changed PM2 processes.
func comparePM2Processes(prev, curr map[string]interface{}) []DiscoveryChange {
	var changes []DiscoveryChange

	prevProcs := extractProcessMap(prev)
	currProcs := extractProcessMap(curr)

	for name, currStatus := range currProcs {
		if prevStatus, found := prevProcs[name]; !found {
			changes = append(changes, DiscoveryChange{
				Type:     "added",
				Category: "process",
				Name:     name,
				Details:  "new PM2 process detected",
			})
		} else if prevStatus != currStatus {
			changes = append(changes, DiscoveryChange{
				Type:     "changed",
				Category: "process",
				Name:     name,
				Details:  fmt.Sprintf("PM2 process status changed from %q to %q", prevStatus, currStatus),
			})
		}
	}
	for name := range prevProcs {
		if _, found := currProcs[name]; !found {
			changes = append(changes, DiscoveryChange{
				Type:     "removed",
				Category: "process",
				Name:     name,
				Details:  "PM2 process no longer running",
			})
		}
	}
	return changes
}

// ── compare helpers ───────────────────────────────────────────────────────────

func extractVhostNames(data map[string]interface{}) []string {
	raw, ok := data["vhosts"]
	if !ok {
		return nil
	}
	var names []string
	switch v := raw.(type) {
	case []interface{}:
		for _, item := range v {
			switch m := item.(type) {
			case map[string]interface{}:
				if n, ok := m["name"].(string); ok && n != "" {
					names = append(names, n)
				}
			case map[string]string:
				if n, ok := m["name"]; ok && n != "" {
					names = append(names, n)
				}
			}
		}
	case []map[string]string:
		for _, m := range v {
			if n, ok := m["name"]; ok && n != "" {
				names = append(names, n)
			}
		}
	}
	return names
}

func extractStringList(data map[string]interface{}, key string) []string {
	raw, ok := data[key]
	if !ok {
		return nil
	}
	var out []string
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func extractProcessMap(data map[string]interface{}) map[string]string {
	result := make(map[string]string)
	raw, ok := data["processes"]
	if !ok {
		return result
	}

	// Handle both []interface{} (from JSON unmarshal/DB) and
	// []map[string]interface{} (from fresh discovery).
	switch items := raw.(type) {
	case []interface{}:
		for _, item := range items {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			status, _ := m["status"].(string)
			if name != "" {
				result[name] = status
			}
		}
	case []map[string]interface{}:
		for _, m := range items {
			name, _ := m["name"].(string)
			status, _ := m["status"].(string)
			if name != "" {
				result[name] = status
			}
		}
	}
	return result
}

func toSet(items []string) map[string]bool {
	s := make(map[string]bool, len(items))
	for _, v := range items {
		s[v] = true
	}
	return s
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}

// DiscoveryService detects installed services on a remote Linux server.
type DiscoveryService struct {
	db *database.Database
}

// NewDiscoveryService creates a DiscoveryService backed by the given database.
func NewDiscoveryService(db *database.Database) *DiscoveryService {
	return &DiscoveryService{db: db}
}

// DiscoveredService holds the name and arbitrary data map for one detected service.
type DiscoveredService struct {
	Name string                 `json:"name"`
	Data map[string]interface{} `json:"data"`
}

// DiscoveryResult is the full scan result for a server.
type DiscoveryResult struct {
	ServerID  int                 `json:"server_id"`
	Services  []DiscoveredService `json:"services"`
	ScannedAt time.Time           `json:"scanned_at"`
}

// Discover runs all service detections against the already-connected conn.
// Each detector is independent: failures are ignored so others still run.
func (s *DiscoveryService) Discover(ctx context.Context, conn connector.Connector) (*DiscoveryResult, error) {
	result := &DiscoveryResult{
		ScannedAt: time.Now().UTC(),
		Services:  []DiscoveredService{},
	}

	detectors := []func(context.Context, connector.Connector) *DiscoveredService{
		detectNGINX,
		detectMySQL,
		detectRedis,
		detectPM2,
		detectCertbot,
		detectNode,
		detectCrontab,
		detectUFW,
	}

	for _, detect := range detectors {
		if svc := detect(ctx, conn); svc != nil {
			result.Services = append(result.Services, *svc)
		}
	}

	return result, nil
}

// SaveResults persists each discovered service to the discovery_results table.
func (s *DiscoveryService) SaveResults(serverID int, result *DiscoveryResult) error {
	now := result.ScannedAt.UTC().Format(time.RFC3339)

	// Delete previous results for this server so we always have fresh data.
	if _, err := s.db.DB().Exec(
		"DELETE FROM discovery_results WHERE server_id = ?", serverID,
	); err != nil {
		return fmt.Errorf("clear old discovery results: %w", err)
	}

	for _, svc := range result.Services {
		dataJSON, err := json.Marshal(svc.Data)
		if err != nil {
			return fmt.Errorf("marshal service data for %q: %w", svc.Name, err)
		}
		_, err = s.db.DB().Exec(
			`INSERT INTO discovery_results (server_id, service_name, service_data, discovered_at)
			 VALUES (?, ?, ?, ?)`,
			serverID, svc.Name, string(dataJSON), now,
		)
		if err != nil {
			return fmt.Errorf("insert discovery result for %q: %w", svc.Name, err)
		}
	}
	return nil
}

// LoadResults fetches the last saved discovery results for a server from the DB.
// Returns an empty DiscoveryResult (with no services) if none are saved yet.
func (s *DiscoveryService) LoadResults(serverID int) (*DiscoveryResult, error) {
	rows, err := s.db.DB().Query(
		`SELECT service_name, service_data, discovered_at
		 FROM discovery_results WHERE server_id = ? ORDER BY id ASC`,
		serverID,
	)
	if err != nil {
		return nil, fmt.Errorf("query discovery results: %w", err)
	}
	defer rows.Close()

	result := &DiscoveryResult{
		ServerID: serverID,
		Services: []DiscoveredService{},
	}

	for rows.Next() {
		var name, dataJSON, discoveredAt string
		if err := rows.Scan(&name, &dataJSON, &discoveredAt); err != nil {
			return nil, fmt.Errorf("scan discovery result: %w", err)
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
			data = map[string]interface{}{}
		}
		result.Services = append(result.Services, DiscoveredService{Name: name, Data: data})
		// Use the timestamp from the last row (they should all be identical within a scan).
		if t, err := time.Parse(time.RFC3339, discoveredAt); err == nil {
			result.ScannedAt = t.UTC()
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("discovery results iteration: %w", err)
	}
	return result, nil
}

// run executes a command and returns stdout. Returns ("", false) when the
// command is not found / exits with a non-zero code.
func run(ctx context.Context, conn connector.Connector, cmd string) (string, bool) {
	res, err := conn.RunCommand(ctx, cmd)
	if err != nil || res.ExitCode != 0 {
		return "", false
	}
	return strings.TrimSpace(res.Stdout), true
}

// runLogin executes a command inside a login shell (bash -l -c) so that the
// user's full PATH is available. Falls back to a plain run if bash -l fails.
func runLogin(ctx context.Context, conn connector.Connector, cmd string) (string, bool) {
	if out, ok := run(ctx, conn, "bash -l -c '"+cmd+"' 2>/dev/null"); ok {
		return out, true
	}
	return run(ctx, conn, cmd)
}

// exists checks whether `which <name>` succeeds (binary is in PATH).
// Uses a login shell so globally installed npm packages are found.
func exists(ctx context.Context, conn connector.Connector, name string) bool {
	_, ok := runLogin(ctx, conn, "which "+name)
	return ok
}

// ── Detectors ────────────────────────────────────────────────────────────────

func detectNGINX(ctx context.Context, conn connector.Connector) *DiscoveredService {
	if !exists(ctx, conn, "nginx") {
		return nil
	}

	version := ""
	if out, ok := run(ctx, conn, "nginx -v 2>&1"); ok {
		version = ParseNginxVersion(out)
	}

	lsOut, _ := run(ctx, conn, "ls /etc/nginx/sites-enabled/")
	vhosts := ParseNginxVhosts(lsOut)

	grepOut, _ := run(ctx, conn, "grep -r \"root \" /etc/nginx/sites-enabled/")
	roots := ParseNginxRoots(grepOut)

	// Merge root paths into vhosts.
	for i, v := range vhosts {
		if root, ok := roots[v["name"]]; ok {
			vhosts[i]["root_path"] = root
		}
	}

	vhostIface := make([]interface{}, len(vhosts))
	for i, v := range vhosts {
		vhostIface[i] = v
	}

	return &DiscoveredService{
		Name: "nginx",
		Data: map[string]interface{}{
			"version": version,
			"vhosts":  vhostIface,
		},
	}
}

func detectMySQL(ctx context.Context, conn connector.Connector) *DiscoveredService {
	if !exists(ctx, conn, "mysql") {
		return nil
	}

	version := ""
	if out, ok := run(ctx, conn, "mysql --version"); ok {
		version = ParseMySQLVersion(out)
	}

	return &DiscoveredService{
		Name: "mysql",
		Data: map[string]interface{}{
			"version":   version,
			"databases": []string{},
		},
	}
}

func detectRedis(ctx context.Context, conn connector.Connector) *DiscoveredService {
	if !exists(ctx, conn, "redis-server") {
		return nil
	}

	version := ""
	if out, ok := run(ctx, conn, "redis-server --version"); ok {
		version = ParseRedisVersion(out)
	}

	// Get RDB file path and database count
	rdbPath := ""
	dbCount := 0
	if out, ok := run(ctx, conn, "redis-cli CONFIG GET dir 2>/dev/null"); ok {
		rdbPath = ParseRedisConfigValue(out)
	}
	if rdbFile, ok := run(ctx, conn, "redis-cli CONFIG GET dbfilename 2>/dev/null"); ok {
		if file := ParseRedisConfigValue(rdbFile); file != "" && rdbPath != "" {
			rdbPath = rdbPath + "/" + file
		}
	}
	if out, ok := run(ctx, conn, "redis-cli INFO keyspace 2>/dev/null"); ok {
		dbCount = ParseRedisDBCount(out)
	}

	// Check if Redis is responding
	status := "unknown"
	if out, ok := run(ctx, conn, "redis-cli ping 2>/dev/null"); ok && strings.Contains(out, "PONG") {
		status = "running"
	}

	return &DiscoveredService{
		Name: "redis",
		Data: map[string]interface{}{
			"version":   version,
			"status":    status,
			"rdb_path":  rdbPath,
			"databases": dbCount,
		},
	}
}

func detectPM2(ctx context.Context, conn connector.Connector) *DiscoveredService {
	if !exists(ctx, conn, "pm2") {
		return nil
	}

	// Run pm2 jlist with login shell to ensure proper environment (PM2 needs node in PATH)
	jsonOut, _ := runLogin(ctx, conn, "pm2 jlist")
	procs := ParsePM2Processes(jsonOut)

	return &DiscoveredService{
		Name: "pm2",
		Data: map[string]interface{}{
			"processes": procs,
		},
	}
}

func detectCertbot(ctx context.Context, conn connector.Connector) *DiscoveredService {
	if !exists(ctx, conn, "certbot") {
		return nil
	}

	certOut, _ := run(ctx, conn, "certbot certificates 2>/dev/null")
	certs := ParseCertbotCerts(certOut)

	return &DiscoveredService{
		Name: "certbot",
		Data: map[string]interface{}{
			"certificates": certs,
		},
	}
}

func detectNode(ctx context.Context, conn connector.Connector) *DiscoveredService {
	if !exists(ctx, conn, "node") {
		return nil
	}

	version := ""
	if out, ok := run(ctx, conn, "node -v"); ok {
		version = ParseNodeVersion(out)
	}

	return &DiscoveredService{
		Name: "nodejs",
		Data: map[string]interface{}{
			"version": version,
		},
	}
}

func detectCrontab(ctx context.Context, conn connector.Connector) *DiscoveredService {
	out, _ := run(ctx, conn, "crontab -l 2>/dev/null")
	entries := ParseCrontab(out)

	return &DiscoveredService{
		Name: "crontab",
		Data: map[string]interface{}{
			"entries": entries,
		},
	}
}

func detectUFW(ctx context.Context, conn connector.Connector) *DiscoveredService {
	if !exists(ctx, conn, "ufw") {
		return nil
	}

	ufwOut, _ := run(ctx, conn, "sudo ufw status 2>/dev/null || ufw status 2>/dev/null")
	status, rules := ParseUFWStatus(ufwOut)

	return &DiscoveredService{
		Name: "ufw",
		Data: map[string]interface{}{
			"status": status,
			"rules":  rules,
		},
	}
}

// ── Parsing helpers (exported for unit testing) ───────────────────────────────

// ParseNginxVersion extracts the version string from `nginx -v 2>&1` output.
// nginx -v prints to stderr: "nginx version: nginx/1.18.0"
func ParseNginxVersion(output string) string {
	// output may look like: "nginx version: nginx/1.18.0"
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, "nginx/"); idx >= 0 {
			return strings.TrimSpace(line[idx+len("nginx/"):])
		}
	}
	return strings.TrimSpace(output)
}

// ParseNginxVhosts parses the output of `ls /etc/nginx/sites-enabled/` into a
// slice of {"name": filename} maps.
func ParseNginxVhosts(lsOutput string) []map[string]string {
	var vhosts []map[string]string
	for _, line := range strings.Split(lsOutput, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		vhosts = append(vhosts, map[string]string{"name": name, "root_path": ""})
	}
	if vhosts == nil {
		vhosts = []map[string]string{}
	}
	return vhosts
}

// ParseNginxRoots parses the output of `grep -r "root " /etc/nginx/sites-enabled/`
// and returns a map of vhost-filename → root path.
// Lines look like: /etc/nginx/sites-enabled/example.com:    root /var/www/example;
func ParseNginxRoots(grepOutput string) map[string]string {
	roots := make(map[string]string)
	for _, line := range strings.Split(grepOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Split on the first colon to get file:content
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		filePart := line[:colonIdx]
		content := strings.TrimSpace(line[colonIdx+1:])

		// Extract the filename from the path.
		parts := strings.Split(filePart, "/")
		filename := parts[len(parts)-1]

		// Extract the root directive value, strip trailing semicolon.
		fields := strings.Fields(content)
		if len(fields) >= 2 && fields[0] == "root" {
			rootPath := strings.TrimSuffix(fields[1], ";")
			if _, already := roots[filename]; !already {
				roots[filename] = rootPath
			}
		}
	}
	return roots
}

// ParseMySQLVersion extracts a version from `mysql --version` output.
// e.g. "mysql  Ver 8.0.33 Distrib 8.0.33, for Linux (x86_64)"
func ParseMySQLVersion(output string) string {
	output = strings.TrimSpace(output)
	fields := strings.Fields(output)
	// Look for a token that looks like a version number (contains dots and digits).
	for _, f := range fields {
		// Strip trailing comma.
		f = strings.TrimSuffix(f, ",")
		if looksLikeVersion(f) {
			return f
		}
	}
	return output
}

func looksLikeVersion(s string) bool {
	if s == "" {
		return false
	}
	dots := 0
	for _, c := range s {
		if c == '.' {
			dots++
		} else if c < '0' || c > '9' {
			return false
		}
	}
	return dots >= 1
}

// ParsePM2Processes parses the JSON output of `pm2 jlist`.
// Each entry should have at minimum: name, pm2_env.pm_cwd, pm2_env.status.
func ParsePM2Processes(jsonOutput string) []map[string]interface{} {
	var raw []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonOutput), &raw); err != nil {
		return []map[string]interface{}{}
	}

	procs := make([]map[string]interface{}, 0, len(raw))
	for _, entry := range raw {
		name, _ := entry["name"].(string)
		path := ""
		status := ""
		if env, ok := entry["pm2_env"].(map[string]interface{}); ok {
			path, _ = env["pm_cwd"].(string)
			status, _ = env["status"].(string)
		}
		procs = append(procs, map[string]interface{}{
			"name":   name,
			"path":   path,
			"status": status,
		})
	}
	return procs
}

// ParseCertbotCerts parses the text output of `certbot certificates`.
// It extracts domain lists and expiry dates.
//
// Example output block:
//
//	Certificate Name: example.com
//	  Domains: example.com www.example.com
//	  Expiry Date: 2024-06-01 12:00:00+00:00 (VALID: 89 days)
func ParseCertbotCerts(certOutput string) []map[string]interface{} {
	var certs []map[string]interface{}
	var current map[string]interface{}

	for _, line := range strings.Split(certOutput, "\n") {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "Certificate Name:") {
			if current != nil {
				certs = append(certs, current)
			}
			current = map[string]interface{}{
				"domains": []string{},
				"expiry":  "",
			}
			continue
		}

		if current == nil {
			continue
		}

		if strings.HasPrefix(line, "Domains:") {
			domainsStr := strings.TrimSpace(strings.TrimPrefix(line, "Domains:"))
			current["domains"] = strings.Fields(domainsStr)
			continue
		}

		if strings.HasPrefix(line, "Expiry Date:") {
			expiryStr := strings.TrimSpace(strings.TrimPrefix(line, "Expiry Date:"))
			// Strip the "(VALID: ...)" or "(INVALID: ...)" suffix.
			if idx := strings.Index(expiryStr, " ("); idx >= 0 {
				expiryStr = strings.TrimSpace(expiryStr[:idx])
			}
			current["expiry"] = expiryStr
		}
	}

	if current != nil {
		certs = append(certs, current)
	}

	if certs == nil {
		certs = []map[string]interface{}{}
	}
	return certs
}

// ParseNodeVersion returns a cleaned version string from `node -v` output.
// e.g. "v18.17.0" → "v18.17.0"
func ParseNodeVersion(versionOutput string) string {
	return strings.TrimSpace(versionOutput)
}

// ParseCrontab returns non-empty, non-comment lines from `crontab -l` output.
func ParseCrontab(crontabOutput string) []string {
	var entries []string
	for _, line := range strings.Split(crontabOutput, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		entries = append(entries, trimmed)
	}
	if entries == nil {
		entries = []string{}
	}
	return entries
}

// ParseRedisVersion extracts the version from `redis-server --version` output.
// e.g. "Redis server v=7.0.11 sha=00000000:0 malloc=jemalloc-5.2.1 bits=64"
func ParseRedisVersion(output string) string {
	for _, field := range strings.Fields(output) {
		if strings.HasPrefix(field, "v=") {
			return strings.TrimPrefix(field, "v=")
		}
	}
	return strings.TrimSpace(output)
}

// ParseRedisConfigValue extracts the value from `redis-cli CONFIG GET key` output.
// Output format is two lines: the key name, then the value.
func ParseRedisConfigValue(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) >= 2 {
		return strings.TrimSpace(lines[1])
	}
	return ""
}

// ParseRedisDBCount counts how many databases have keys from `redis-cli INFO keyspace`.
// Lines like "db0:keys=42,expires=0,avg_ttl=0"
func ParseRedisDBCount(output string) int {
	count := 0
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "db") && strings.Contains(line, "keys=") {
			count++
		}
	}
	return count
}

// ParseUFWStatus parses the text output of `ufw status`.
// Returns the overall status string and a slice of rule lines.
//
// Example:
//
//	Status: active
//	To                         Action      From
//	--                         ------      ----
//	22/tcp                     ALLOW       Anywhere
func ParseUFWStatus(ufwOutput string) (string, []string) {
	status := "unknown"
	var rules []string

	lines := strings.Split(ufwOutput, "\n")
	inRules := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "Status:") {
			status = strings.TrimSpace(strings.TrimPrefix(trimmed, "Status:"))
			continue
		}

		// The rules section starts after the "To / Action / From" header line
		// and the dashes separator line.
		if strings.HasPrefix(trimmed, "To ") && strings.Contains(trimmed, "Action") {
			inRules = true
			continue
		}
		if inRules && strings.HasPrefix(trimmed, "--") {
			continue
		}
		if inRules && trimmed != "" {
			rules = append(rules, trimmed)
		}
	}

	if rules == nil {
		rules = []string{}
	}
	return status, rules
}
