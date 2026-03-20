// internal/discovery/compare_test.go
package discovery

import (
	"testing"
)

// helper: build a DiscoveredService slice quickly
func services(svcs ...DiscoveredService) []DiscoveredService { return svcs }

func TestCompareResults_NewService(t *testing.T) {
	previous := services()
	current := services(DiscoveredService{
		Name: "redis",
		Data: map[string]interface{}{"version": "7.0", "databases": 2},
	})

	changes := CompareResults(previous, current)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %v", len(changes), changes)
	}
	c := changes[0]
	if c.Type != "added" {
		t.Errorf("type = %q, want added", c.Type)
	}
	if c.Category != "service" {
		t.Errorf("category = %q, want service", c.Category)
	}
	if c.Name != "redis" {
		t.Errorf("name = %q, want redis", c.Name)
	}
}

func TestCompareResults_RemovedService(t *testing.T) {
	previous := services(DiscoveredService{
		Name: "pm2",
		Data: map[string]interface{}{"processes": []interface{}{}},
	})
	current := services()

	changes := CompareResults(previous, current)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %v", len(changes), changes)
	}
	c := changes[0]
	if c.Type != "removed" {
		t.Errorf("type = %q, want removed", c.Type)
	}
	if c.Category != "service" {
		t.Errorf("category = %q, want service", c.Category)
	}
	if c.Name != "pm2" {
		t.Errorf("name = %q, want pm2", c.Name)
	}
}

func TestCompareResults_NewDatabase(t *testing.T) {
	previous := services(DiscoveredService{
		Name: "mysql",
		Data: map[string]interface{}{
			"version":   "8.0",
			"databases": []interface{}{"app_db"},
		},
	})
	current := services(DiscoveredService{
		Name: "mysql",
		Data: map[string]interface{}{
			"version":   "8.0",
			"databases": []interface{}{"app_db", "new_db"},
		},
	})

	changes := CompareResults(previous, current)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %v", len(changes), changes)
	}
	c := changes[0]
	if c.Type != "added" {
		t.Errorf("type = %q, want added", c.Type)
	}
	if c.Category != "database" {
		t.Errorf("category = %q, want database", c.Category)
	}
	if c.Name != "new_db" {
		t.Errorf("name = %q, want new_db", c.Name)
	}
}

func TestCompareResults_RemovedVhost(t *testing.T) {
	previous := services(DiscoveredService{
		Name: "nginx",
		Data: map[string]interface{}{
			"version": "1.18",
			"vhosts": []interface{}{
				map[string]interface{}{"name": "example.com", "root_path": ""},
				map[string]interface{}{"name": "old-site.com", "root_path": ""},
			},
		},
	})
	current := services(DiscoveredService{
		Name: "nginx",
		Data: map[string]interface{}{
			"version": "1.18",
			"vhosts": []interface{}{
				map[string]interface{}{"name": "example.com", "root_path": ""},
			},
		},
	})

	changes := CompareResults(previous, current)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %v", len(changes), changes)
	}
	c := changes[0]
	if c.Type != "removed" {
		t.Errorf("type = %q, want removed", c.Type)
	}
	if c.Category != "vhost" {
		t.Errorf("category = %q, want vhost", c.Category)
	}
	if c.Name != "old-site.com" {
		t.Errorf("name = %q, want old-site.com", c.Name)
	}
}

func TestCompareResults_NoChanges(t *testing.T) {
	svc := DiscoveredService{
		Name: "nginx",
		Data: map[string]interface{}{
			"version": "1.18",
			"vhosts": []interface{}{
				map[string]interface{}{"name": "example.com", "root_path": ""},
			},
		},
	}
	previous := services(svc)
	current := services(svc)

	changes := CompareResults(previous, current)

	if len(changes) != 0 {
		t.Errorf("expected 0 changes, got %d: %v", len(changes), changes)
	}
}

func TestCompareResults_NewPM2Process(t *testing.T) {
	previous := services(DiscoveredService{
		Name: "pm2",
		Data: map[string]interface{}{
			"processes": []interface{}{
				map[string]interface{}{"name": "api", "status": "online", "path": "/var/www/api"},
			},
		},
	})
	current := services(DiscoveredService{
		Name: "pm2",
		Data: map[string]interface{}{
			"processes": []interface{}{
				map[string]interface{}{"name": "api", "status": "online", "path": "/var/www/api"},
				map[string]interface{}{"name": "worker", "status": "online", "path": "/var/www/worker"},
			},
		},
	})

	changes := CompareResults(previous, current)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d: %v", len(changes), changes)
	}
	c := changes[0]
	if c.Type != "added" {
		t.Errorf("type = %q, want added", c.Type)
	}
	if c.Category != "process" {
		t.Errorf("category = %q, want process", c.Category)
	}
	if c.Name != "worker" {
		t.Errorf("name = %q, want worker", c.Name)
	}
}
