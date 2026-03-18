// internal/discovery/discovery_test.go
package discovery

import (
	"context"
	"io"
	"testing"

	"github.com/backupmanager/backupmanager/internal/connector"
)

// ── TestParseNginxVhosts ──────────────────────────────────────────────────────

func TestParseNginxVhosts(t *testing.T) {
	input := "default\nexample.com\napi.example.com\n"
	vhosts := ParseNginxVhosts(input)

	if len(vhosts) != 3 {
		t.Fatalf("expected 3 vhosts, got %d", len(vhosts))
	}
	names := []string{"default", "example.com", "api.example.com"}
	for i, want := range names {
		if got := vhosts[i]["name"]; got != want {
			t.Errorf("vhosts[%d].name = %q, want %q", i, got, want)
		}
	}
}

func TestParseNginxVhostsEmpty(t *testing.T) {
	vhosts := ParseNginxVhosts("")
	if len(vhosts) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(vhosts))
	}
}

// ── TestParseNginxRoots ───────────────────────────────────────────────────────

func TestParseNginxRoots(t *testing.T) {
	input := `/etc/nginx/sites-enabled/example.com:    root /var/www/example;
/etc/nginx/sites-enabled/api.example.com:  root /var/www/api;
`
	roots := ParseNginxRoots(input)

	if got := roots["example.com"]; got != "/var/www/example" {
		t.Errorf("roots[example.com] = %q, want /var/www/example", got)
	}
	if got := roots["api.example.com"]; got != "/var/www/api" {
		t.Errorf("roots[api.example.com] = %q, want /var/www/api", got)
	}
}

func TestParseNginxRootsEmpty(t *testing.T) {
	roots := ParseNginxRoots("")
	if len(roots) != 0 {
		t.Errorf("expected empty map, got %v", roots)
	}
}

func TestParseNginxRootsNoRootDirective(t *testing.T) {
	input := "/etc/nginx/sites-enabled/default:    server_name localhost;\n"
	roots := ParseNginxRoots(input)
	if len(roots) != 0 {
		t.Errorf("expected no roots, got %v", roots)
	}
}

// ── TestParsePM2Processes ─────────────────────────────────────────────────────

func TestParsePM2Processes(t *testing.T) {
	input := `[
	  {
	    "name": "myapp",
	    "pm2_env": {
	      "pm_cwd": "/var/www/myapp",
	      "status": "online"
	    }
	  },
	  {
	    "name": "worker",
	    "pm2_env": {
	      "pm_cwd": "/var/www/worker",
	      "status": "stopped"
	    }
	  }
	]`

	procs := ParsePM2Processes(input)
	if len(procs) != 2 {
		t.Fatalf("expected 2 processes, got %d", len(procs))
	}

	if procs[0]["name"] != "myapp" {
		t.Errorf("procs[0].name = %q, want myapp", procs[0]["name"])
	}
	if procs[0]["path"] != "/var/www/myapp" {
		t.Errorf("procs[0].path = %q, want /var/www/myapp", procs[0]["path"])
	}
	if procs[0]["status"] != "online" {
		t.Errorf("procs[0].status = %q, want online", procs[0]["status"])
	}
	if procs[1]["status"] != "stopped" {
		t.Errorf("procs[1].status = %q, want stopped", procs[1]["status"])
	}
}

func TestParsePM2ProcessesInvalidJSON(t *testing.T) {
	procs := ParsePM2Processes("not json")
	if procs == nil {
		t.Error("expected non-nil empty slice")
	}
	if len(procs) != 0 {
		t.Errorf("expected 0 processes, got %d", len(procs))
	}
}

// ── TestParseCertbotCerts ─────────────────────────────────────────────────────

func TestParseCertbotCerts(t *testing.T) {
	input := `Saving debug log to /var/log/letsencrypt/letsencrypt.log

- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -
Found the following certs:
  Certificate Name: example.com
    Domains: example.com www.example.com
    Expiry Date: 2024-06-01 12:00:00+00:00 (VALID: 89 days)
    Certificate Path: /etc/letsencrypt/live/example.com/fullchain.pem
    Private Key Path: /etc/letsencrypt/live/example.com/privkey.pem
  Certificate Name: api.example.com
    Domains: api.example.com
    Expiry Date: 2024-07-15 08:30:00+00:00 (VALID: 133 days)
- - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - -`

	certs := ParseCertbotCerts(input)
	if len(certs) != 2 {
		t.Fatalf("expected 2 certs, got %d", len(certs))
	}

	domains0, _ := certs[0]["domains"].([]string)
	if len(domains0) != 2 {
		t.Errorf("certs[0] domains: expected 2, got %v", domains0)
	}
	if certs[0]["expiry"] != "2024-06-01 12:00:00+00:00" {
		t.Errorf("certs[0].expiry = %q", certs[0]["expiry"])
	}

	domains1, _ := certs[1]["domains"].([]string)
	if len(domains1) != 1 || domains1[0] != "api.example.com" {
		t.Errorf("certs[1] domains unexpected: %v", domains1)
	}
}

func TestParseCertbotCertsEmpty(t *testing.T) {
	certs := ParseCertbotCerts("No certs found.")
	if len(certs) != 0 {
		t.Errorf("expected 0 certs, got %d", len(certs))
	}
}

// ── TestParseNodeVersion ──────────────────────────────────────────────────────

func TestParseNodeVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v18.17.0\n", "v18.17.0"},
		{"v20.5.1", "v20.5.1"},
		{"  v16.0.0  ", "v16.0.0"},
	}
	for _, tc := range tests {
		if got := ParseNodeVersion(tc.input); got != tc.want {
			t.Errorf("ParseNodeVersion(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ── TestParseCrontab ──────────────────────────────────────────────────────────

func TestParseCrontab(t *testing.T) {
	input := `# Edit this file to introduce tasks to be run by cron.
# m h  dom mon dow   command

*/5 * * * * /usr/bin/php /var/www/artisan schedule:run
0 2 * * * /home/user/backup.sh
`
	entries := ParseCrontab(input)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(entries), entries)
	}
	if entries[0] != "*/5 * * * * /usr/bin/php /var/www/artisan schedule:run" {
		t.Errorf("entries[0] unexpected: %q", entries[0])
	}
	if entries[1] != "0 2 * * * /home/user/backup.sh" {
		t.Errorf("entries[1] unexpected: %q", entries[1])
	}
}

func TestParseCrontabEmpty(t *testing.T) {
	entries := ParseCrontab("")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %v", entries)
	}
}

func TestParseCrontabOnlyComments(t *testing.T) {
	input := "# this is a comment\n# another comment\n"
	entries := ParseCrontab(input)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %v", entries)
	}
}

// ── TestParseUFWStatus ────────────────────────────────────────────────────────

func TestParseUFWStatus(t *testing.T) {
	input := `Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere
80/tcp                     ALLOW       Anywhere
443                        ALLOW       Anywhere
22/tcp (v6)                ALLOW       Anywhere (v6)
`
	status, rules := ParseUFWStatus(input)
	if status != "active" {
		t.Errorf("status = %q, want active", status)
	}
	if len(rules) != 4 {
		t.Errorf("expected 4 rules, got %d: %v", len(rules), rules)
	}
}

func TestParseUFWStatusInactive(t *testing.T) {
	input := "Status: inactive\n"
	status, rules := ParseUFWStatus(input)
	if status != "inactive" {
		t.Errorf("status = %q, want inactive", status)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %v", rules)
	}
}

func TestParseUFWStatusEmpty(t *testing.T) {
	status, rules := ParseUFWStatus("")
	if status != "unknown" {
		t.Errorf("status = %q, want unknown", status)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %v", rules)
	}
}

// ── TestDiscoverHandlesMissingService ─────────────────────────────────────────

// mockConnector implements connector.Connector for unit tests.
// It maps command strings to their CommandResult.
type mockConnector struct {
	responses map[string]*connector.CommandResult
}

func (m *mockConnector) Connect() error { return nil }
func (m *mockConnector) Close() error   { return nil }

func (m *mockConnector) RunCommand(_ context.Context, cmd string) (*connector.CommandResult, error) {
	if res, ok := m.responses[cmd]; ok {
		return res, nil
	}
	// Default: command not found, exit 1.
	return &connector.CommandResult{ExitCode: 1}, nil
}

func (m *mockConnector) CopyFile(_ context.Context, _, _ string) error   { return nil }
func (m *mockConnector) UploadFile(_ context.Context, _, _ string) error { return nil }
func (m *mockConnector) ListFiles(_ context.Context, _ string) ([]connector.FileInfo, error) {
	return nil, nil
}
func (m *mockConnector) ReadFile(_ context.Context, _ string, _ io.Writer) error {
	return nil
}
func (m *mockConnector) FileExists(_ context.Context, _ string) (bool, error) { return false, nil }
func (m *mockConnector) RemoveFile(_ context.Context, _ string) error          { return nil }

func TestDiscoverHandlesMissingService(t *testing.T) {
	// All `which` commands return exit 1 → no services should be detected
	// except crontab (which runs regardless) and pm2 (which uses ||).
	mc := &mockConnector{
		responses: map[string]*connector.CommandResult{
			// crontab always runs; return empty output.
			"crontab -l 2>/dev/null": {Stdout: "", ExitCode: 0},
			// pm2 check also fails.
			"which pm2 || command -v pm2": {Stdout: "", ExitCode: 1},
		},
	}

	svc := &DiscoveryService{} // no db needed for Discover itself
	result, err := svc.Discover(context.Background(), mc)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}

	// Only crontab should appear (always runs).
	if len(result.Services) != 1 {
		t.Errorf("expected 1 service (crontab), got %d: %v", len(result.Services), result.Services)
	}
	if result.Services[0].Name != "crontab" {
		t.Errorf("expected crontab service, got %q", result.Services[0].Name)
	}
}

func TestDiscoverDetectsNginxAndNode(t *testing.T) {
	mc := &mockConnector{
		responses: map[string]*connector.CommandResult{
			"which nginx":                                            {Stdout: "/usr/sbin/nginx", ExitCode: 0},
			"nginx -v 2>&1":                                         {Stdout: "nginx version: nginx/1.18.0", ExitCode: 0},
			"ls /etc/nginx/sites-enabled/":                          {Stdout: "example.com", ExitCode: 0},
			`grep -r "root " /etc/nginx/sites-enabled/`:             {Stdout: "/etc/nginx/sites-enabled/example.com:    root /var/www/html;", ExitCode: 0},
			"which node":                                            {Stdout: "/usr/bin/node", ExitCode: 0},
			"node -v":                                               {Stdout: "v18.17.0", ExitCode: 0},
			"crontab -l 2>/dev/null":                                {Stdout: "", ExitCode: 0},
			"which pm2 || command -v pm2":                           {Stdout: "", ExitCode: 1},
		},
	}

	svc := &DiscoveryService{}
	result, err := svc.Discover(context.Background(), mc)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}

	serviceNames := make(map[string]bool)
	for _, s := range result.Services {
		serviceNames[s.Name] = true
	}

	if !serviceNames["nginx"] {
		t.Error("expected nginx to be detected")
	}
	if !serviceNames["nodejs"] {
		t.Error("expected nodejs to be detected")
	}
}
