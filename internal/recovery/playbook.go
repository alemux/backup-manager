// internal/recovery/playbook.go
package recovery

import "time"

// Playbook represents a disaster recovery playbook.
type Playbook struct {
	ID        int       `json:"id"`
	ServerID  *int      `json:"server_id"`
	Title     string    `json:"title"`
	Scenario  string    `json:"scenario"` // "full_server", "single_database", "single_project", "config_only", "certificates"
	Steps     []Step    `json:"steps"`
	CreatedAt time.Time `json:"created_at"`
}

// Step represents a single recovery step within a playbook.
type Step struct {
	Order       int    `json:"order"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Command     string `json:"command,omitempty"`  // copyable command
	Verify      string `json:"verify,omitempty"`   // verification command
	Notes       string `json:"notes,omitempty"`
}

// ServerInfo contains server metadata used for playbook generation.
type ServerInfo struct {
	ID   int
	Name string
	Host string
}

// SourceInfo describes a single backup source configured for a server.
type SourceInfo struct {
	ID         int
	Name       string
	Type       string // "web", "database", "config"
	SourcePath string
	DBName     string
}
