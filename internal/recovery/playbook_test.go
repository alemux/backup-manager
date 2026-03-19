// internal/recovery/playbook_test.go
package recovery

import (
	"strings"
	"testing"
)

func serverAndSources(webNames, dbNames, configNames []string) (ServerInfo, []SourceInfo) {
	srv := ServerInfo{ID: 1, Name: "test-server", Host: "192.168.1.100"}
	var sources []SourceInfo
	id := 1
	for _, n := range webNames {
		sources = append(sources, SourceInfo{ID: id, Name: n, Type: "web", SourcePath: "/var/www/" + n})
		id++
	}
	for _, n := range dbNames {
		sources = append(sources, SourceInfo{ID: id, Name: n, Type: "database", DBName: n})
		id++
	}
	for _, n := range configNames {
		sources = append(sources, SourceInfo{ID: id, Name: n, Type: "config", SourcePath: "/etc/" + n})
		id++
	}
	return srv, sources
}

func TestGeneratePlaybooks_FullServer(t *testing.T) {
	srv, sources := serverAndSources(
		[]string{"mysite"},
		[]string{"mydb"},
		[]string{"nginx"},
	)

	playbooks := GeneratePlaybooks(srv, sources)

	if len(playbooks) == 0 {
		t.Fatal("expected at least one playbook, got none")
	}

	// First playbook should always be full_server
	full := playbooks[0]
	if full.Scenario != "full_server" {
		t.Errorf("expected scenario 'full_server', got '%s'", full.Scenario)
	}
	if full.ServerID == nil || *full.ServerID != srv.ID {
		t.Error("expected server ID to be set on playbook")
	}
	if len(full.Steps) == 0 {
		t.Error("expected full server playbook to have steps")
	}
	if !strings.Contains(full.Title, srv.Name) {
		t.Errorf("expected playbook title to contain server name, got: %s", full.Title)
	}
}

func TestGeneratePlaybooks_DatabaseOnly(t *testing.T) {
	srv, sources := serverAndSources(nil, []string{"app_db", "logs_db"}, nil)

	playbooks := GeneratePlaybooks(srv, sources)

	// Should have: full_server + 2 single_database (no web/config sources so no certs/config playbooks)
	scenarios := make(map[string]int)
	for _, p := range playbooks {
		scenarios[p.Scenario]++
	}

	if scenarios["full_server"] != 1 {
		t.Errorf("expected 1 full_server playbook, got %d", scenarios["full_server"])
	}
	if scenarios["single_database"] != 2 {
		t.Errorf("expected 2 single_database playbooks, got %d", scenarios["single_database"])
	}
	// No web sources → no certificates or single_project
	if scenarios["certificates"] != 0 {
		t.Errorf("expected 0 certificate playbooks, got %d", scenarios["certificates"])
	}
	if scenarios["single_project"] != 0 {
		t.Errorf("expected 0 single_project playbooks, got %d", scenarios["single_project"])
	}
}

func TestGeneratePlaybooks_WebOnly(t *testing.T) {
	srv, sources := serverAndSources([]string{"shop", "blog"}, nil, nil)

	playbooks := GeneratePlaybooks(srv, sources)

	scenarios := make(map[string]int)
	for _, p := range playbooks {
		scenarios[p.Scenario]++
	}

	if scenarios["full_server"] != 1 {
		t.Errorf("expected 1 full_server playbook, got %d", scenarios["full_server"])
	}
	if scenarios["single_project"] != 2 {
		t.Errorf("expected 2 single_project playbooks, got %d", scenarios["single_project"])
	}
	// Web sources trigger certificate playbook
	if scenarios["certificates"] != 1 {
		t.Errorf("expected 1 certificate playbook, got %d", scenarios["certificates"])
	}
}

func TestGeneratePlaybooks_StepsHaveCommands(t *testing.T) {
	srv, sources := serverAndSources([]string{"mysite"}, []string{"mydb"}, []string{"nginx"})

	playbooks := GeneratePlaybooks(srv, sources)

	for _, p := range playbooks {
		hasCommand := false
		for _, step := range p.Steps {
			if step.Command != "" {
				hasCommand = true
				break
			}
			if step.Title == "" {
				t.Errorf("playbook '%s': step %d has empty title", p.Title, step.Order)
			}
			if step.Description == "" {
				t.Errorf("playbook '%s': step %d has empty description", p.Title, step.Order)
			}
		}
		if !hasCommand {
			t.Errorf("playbook '%s' has no steps with commands", p.Title)
		}
	}
}
