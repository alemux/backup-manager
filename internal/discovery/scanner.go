// internal/discovery/scanner.go
package discovery

import (
	"context"
	"log"
	"time"

	"github.com/backupmanager/backupmanager/internal/connector"
	"github.com/backupmanager/backupmanager/internal/database"
)

// AutoScanner periodically rescans all Linux servers and reports changes.
type AutoScanner struct {
	db       *database.Database
	credKey  []byte // 32-byte AES key for decrypting server credentials; may be nil
	interval time.Duration
	stopCh   chan struct{}
	notifyFn func(serverName string, changes []DiscoveryChange)
}

// NewAutoScanner creates an AutoScanner with the given interval (default 24h if 0).
// credKey is the 32-byte credential encryption key; pass nil if credentials are not encrypted.
func NewAutoScanner(db *database.Database, credKey []byte, interval time.Duration) *AutoScanner {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return &AutoScanner{
		db:       db,
		credKey:  credKey,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// SetNotifyFn registers a callback that is called when changes are detected.
func (s *AutoScanner) SetNotifyFn(fn func(serverName string, changes []DiscoveryChange)) {
	s.notifyFn = fn
}

// Start launches the background scanning loop.
func (s *AutoScanner) Start() {
	go s.loop()
}

// Stop signals the background goroutine to exit.
func (s *AutoScanner) Stop() {
	close(s.stopCh)
}

func (s *AutoScanner) loop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runAllServers()
		}
	}
}

// runAllServers loads all Linux SSH servers and scans each one.
func (s *AutoScanner) runAllServers() {
	type serverRow struct {
		id             int
		name           string
		host           string
		port           int
		username       string
		encPassword    string
		encSSHKey      string
	}

	rows, err := s.db.DB().Query(
		`SELECT id, name, host, port, COALESCE(username,''), COALESCE(encrypted_password,''), COALESCE(ssh_key_path,'')
		 FROM servers WHERE type = 'linux' AND connection_type = 'ssh'`,
	)
	if err != nil {
		log.Printf("AutoScanner: query servers error: %v", err)
		return
	}
	defer rows.Close()

	var servers []serverRow
	for rows.Next() {
		var r serverRow
		if err := rows.Scan(&r.id, &r.name, &r.host, &r.port, &r.username, &r.encPassword, &r.encSSHKey); err != nil {
			log.Printf("AutoScanner: scan row error: %v", err)
			continue
		}
		servers = append(servers, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("AutoScanner: rows error: %v", err)
	}

	var credMgr *database.CredentialManager
	if len(s.credKey) == 32 {
		credMgr = database.NewCredentialManager(s.credKey)
	}
	discSvc := NewDiscoveryService(s.db)

	for _, srv := range servers {
		s.scanServer(discSvc, credMgr, srv.id, srv.name, srv.host, srv.port, srv.username, srv.encPassword, srv.encSSHKey)
	}
}

func (s *AutoScanner) scanServer(
	discSvc *DiscoveryService,
	credMgr *database.CredentialManager,
	serverID int, serverName, host string, port int,
	username, encPassword, encSSHKey string,
) {
	cfg := connector.SSHConfig{
		Host:     host,
		Port:     port,
		Username: username,
		Timeout:  30 * time.Second,
	}

	if encPassword != "" && credMgr != nil {
		if pw, err := credMgr.Decrypt(encPassword); err == nil {
			cfg.Password = pw
		}
	}
	if encSSHKey != "" && credMgr != nil {
		if key, err := credMgr.Decrypt(encSSHKey); err == nil {
			cfg.KeyPath = key
		}
	}

	conn := connector.NewSSHConnector(cfg)
	if err := conn.Connect(); err != nil {
		log.Printf("AutoScanner: SSH connect to %s (%s): %v", serverName, host, err)
		return
	}
	defer conn.Close()

	// Load previous results before overwriting.
	previous, err := discSvc.LoadResults(serverID)
	if err != nil {
		log.Printf("AutoScanner: load previous results for %s: %v", serverName, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	current, err := discSvc.Discover(ctx, conn)
	if err != nil {
		log.Printf("AutoScanner: discover %s: %v", serverName, err)
		return
	}
	current.ServerID = serverID

	changes := CompareResults(previous.Services, current.Services)

	if err := discSvc.SaveResults(serverID, current); err != nil {
		log.Printf("AutoScanner: save results for %s: %v", serverName, err)
	}

	if len(changes) > 0 {
		log.Printf("AutoScanner: %d change(s) detected on server %s", len(changes), serverName)
		for _, c := range changes {
			log.Printf("  [%s] %s/%s: %s", c.Type, c.Category, c.Name, c.Details)
		}
		if s.notifyFn != nil {
			s.notifyFn(serverName, changes)
		}
	}
}

