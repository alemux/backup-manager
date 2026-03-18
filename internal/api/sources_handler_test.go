// internal/api/sources_handler_test.go
package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/backupmanager/backupmanager/internal/auth"
)

// createTestServer is a helper that creates a server and returns its ID.
func createTestServer(t *testing.T, db interface{ DB() interface{ Exec(string, ...interface{}) (interface{ LastInsertId() (int64, error) }, error) } }) int {
	t.Helper()
	return createTestServerViaAPI(t, db)
}

// createTestServerViaAPI creates a server using the API and returns its ID.
func createTestServerViaAPI(t *testing.T, db interface{}) int {
	t.Helper()
	return 0 // placeholder - use createServerForSources instead
}

// createServerForSources creates a linux/ssh server and returns its numeric ID.
func createServerForSources(t *testing.T, dbVal interface{}, authSvc *auth.Service) int {
	t.Helper()
	// We need the concrete *database.Database type; use a type assertion via the api package's newTestDB helper.
	// Since tests are in the same package, we can call the real helper directly.
	// Re-use the authenticatedRequest helper.
	// We can't import database here explicitly but we can use the local db variable via the test.
	// Instead, rely on the caller passing things properly.
	return 0
}

// sourcesTestSetup creates a fresh DB + authSvc and a test server, returning the server ID.
// It uses the package-level newTestDB and authenticatedRequest helpers.
func sourcesTestSetup(t *testing.T) (interface{}, *auth.Service, int) {
	t.Helper()
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	payload, _ := json.Marshal(map[string]interface{}{
		"name":            "Test Server",
		"type":            "linux",
		"host":            "192.168.1.1",
		"port":            22,
		"connection_type": "ssh",
		"username":        "admin",
	})
	w := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(payload), db, authSvc)
	if w.Code != http.StatusCreated {
		t.Fatalf("create test server failed: %d %s", w.Code, w.Body.String())
	}
	var created map[string]interface{}
	json.NewDecoder(w.Body).Decode(&created)
	serverID := int(created["id"].(float64))
	return db, authSvc, serverID
}

func TestCreateSourceWeb(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	// Create a server first
	serverPayload, _ := json.Marshal(map[string]interface{}{
		"name": "Web Server", "type": "linux", "host": "10.0.0.1",
		"port": 22, "connection_type": "ssh",
	})
	wSrv := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(serverPayload), db, authSvc)
	if wSrv.Code != http.StatusCreated {
		t.Fatalf("create server: %d %s", wSrv.Code, wSrv.Body.String())
	}
	var srv map[string]interface{}
	json.NewDecoder(wSrv.Body).Decode(&srv)
	srvID := int(srv["id"].(float64))

	payload, _ := json.Marshal(map[string]interface{}{
		"name":        "Web Files",
		"type":        "web",
		"source_path": "/var/www/",
	})
	w := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(payload), db, authSvc)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["id"] == nil {
		t.Error("expected id in response")
	}
	if result["name"] != "Web Files" {
		t.Errorf("expected name 'Web Files', got %v", result["name"])
	}
	if result["type"] != "web" {
		t.Errorf("expected type 'web', got %v", result["type"])
	}
	if result["source_path"] != "/var/www/" {
		t.Errorf("expected source_path '/var/www/', got %v", result["source_path"])
	}
	if result["server_id"] != float64(srvID) {
		t.Errorf("expected server_id %d, got %v", srvID, result["server_id"])
	}
}

func TestCreateSourceDatabase(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverPayload, _ := json.Marshal(map[string]interface{}{
		"name": "DB Server", "type": "linux", "host": "10.0.0.2",
		"port": 22, "connection_type": "ssh",
	})
	wSrv := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(serverPayload), db, authSvc)
	if wSrv.Code != http.StatusCreated {
		t.Fatalf("create server: %d %s", wSrv.Code, wSrv.Body.String())
	}
	var srv map[string]interface{}
	json.NewDecoder(wSrv.Body).Decode(&srv)
	srvID := int(srv["id"].(float64))

	payload, _ := json.Marshal(map[string]interface{}{
		"name":    "MySQL Production",
		"type":    "database",
		"db_name": "prod_db",
	})
	w := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(payload), db, authSvc)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["type"] != "database" {
		t.Errorf("expected type 'database', got %v", result["type"])
	}
	if result["db_name"] != "prod_db" {
		t.Errorf("expected db_name 'prod_db', got %v", result["db_name"])
	}
	if result["source_path"] != nil {
		t.Errorf("expected source_path null, got %v", result["source_path"])
	}
}

func TestCreateSourceConfig(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverPayload, _ := json.Marshal(map[string]interface{}{
		"name": "Config Server", "type": "linux", "host": "10.0.0.3",
		"port": 22, "connection_type": "ssh",
	})
	wSrv := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(serverPayload), db, authSvc)
	if wSrv.Code != http.StatusCreated {
		t.Fatalf("create server: %d %s", wSrv.Code, wSrv.Body.String())
	}
	var srv map[string]interface{}
	json.NewDecoder(wSrv.Body).Decode(&srv)
	srvID := int(srv["id"].(float64))

	payload, _ := json.Marshal(map[string]interface{}{
		"name":        "Nginx Config",
		"type":        "config",
		"source_path": "/etc/nginx/",
	})
	w := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(payload), db, authSvc)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["type"] != "config" {
		t.Errorf("expected type 'config', got %v", result["type"])
	}
	if result["source_path"] != "/etc/nginx/" {
		t.Errorf("expected source_path '/etc/nginx/', got %v", result["source_path"])
	}
}

func TestCreateSourceValidation(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverPayload, _ := json.Marshal(map[string]interface{}{
		"name": "Validation Server", "type": "linux", "host": "10.0.0.4",
		"port": 22, "connection_type": "ssh",
	})
	wSrv := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(serverPayload), db, authSvc)
	if wSrv.Code != http.StatusCreated {
		t.Fatalf("create server: %d %s", wSrv.Code, wSrv.Body.String())
	}
	var srv map[string]interface{}
	json.NewDecoder(wSrv.Body).Decode(&srv)
	srvID := int(srv["id"].(float64))

	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{
			name: "missing name",
			payload: map[string]interface{}{
				"type":        "web",
				"source_path": "/var/www/",
			},
		},
		{
			name: "invalid type",
			payload: map[string]interface{}{
				"name":        "Test Source",
				"type":        "ftp",
				"source_path": "/var/www/",
			},
		},
		{
			name: "web without path",
			payload: map[string]interface{}{
				"name": "Test Source",
				"type": "web",
			},
		},
		{
			name: "database without db_name",
			payload: map[string]interface{}{
				"name": "Test Source",
				"type": "database",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(tc.payload)
			w := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(payload), db, authSvc)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestListSources(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverPayload, _ := json.Marshal(map[string]interface{}{
		"name": "List Server", "type": "linux", "host": "10.0.0.5",
		"port": 22, "connection_type": "ssh",
	})
	wSrv := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(serverPayload), db, authSvc)
	if wSrv.Code != http.StatusCreated {
		t.Fatalf("create server: %d %s", wSrv.Code, wSrv.Body.String())
	}
	var srv map[string]interface{}
	json.NewDecoder(wSrv.Body).Decode(&srv)
	srvID := int(srv["id"].(float64))

	// Create source 1
	p1, _ := json.Marshal(map[string]interface{}{
		"name": "Source One", "type": "web", "source_path": "/var/www/one",
	})
	w1 := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(p1), db, authSvc)
	if w1.Code != http.StatusCreated {
		t.Fatalf("create source 1: %d %s", w1.Code, w1.Body.String())
	}

	// Create source 2
	p2, _ := json.Marshal(map[string]interface{}{
		"name": "Source Two", "type": "config", "source_path": "/etc/app/",
	})
	w2 := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(p2), db, authSvc)
	if w2.Code != http.StatusCreated {
		t.Fatalf("create source 2: %d %s", w2.Code, w2.Body.String())
	}

	// List
	wList := authenticatedRequest(t, http.MethodGet, fmt.Sprintf("/api/servers/%d/sources", srvID), nil, db, authSvc)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", wList.Code, wList.Body.String())
	}

	var results []map[string]interface{}
	if err := json.NewDecoder(wList.Body).Decode(&results); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 sources, got %d", len(results))
	}
}

func TestUpdateSource(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverPayload, _ := json.Marshal(map[string]interface{}{
		"name": "Update Server", "type": "linux", "host": "10.0.0.6",
		"port": 22, "connection_type": "ssh",
	})
	wSrv := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(serverPayload), db, authSvc)
	if wSrv.Code != http.StatusCreated {
		t.Fatalf("create server: %d %s", wSrv.Code, wSrv.Body.String())
	}
	var srv map[string]interface{}
	json.NewDecoder(wSrv.Body).Decode(&srv)
	srvID := int(srv["id"].(float64))

	// Create source
	createPayload, _ := json.Marshal(map[string]interface{}{
		"name": "Original Name", "type": "web", "source_path": "/var/www/old/",
	})
	wCreate := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(createPayload), db, authSvc)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create source: %d %s", wCreate.Code, wCreate.Body.String())
	}
	var created map[string]interface{}
	json.NewDecoder(wCreate.Body).Decode(&created)
	srcID := int(created["id"].(float64))

	// Update
	updatePayload, _ := json.Marshal(map[string]interface{}{
		"name": "Updated Name", "type": "web", "source_path": "/var/www/new/",
	})
	wUpdate := authenticatedRequest(t, http.MethodPut, fmt.Sprintf("/api/sources/%d", srcID), bytes.NewReader(updatePayload), db, authSvc)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", wUpdate.Code, wUpdate.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(wUpdate.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["name"] != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %v", result["name"])
	}
	if result["source_path"] != "/var/www/new/" {
		t.Errorf("expected source_path '/var/www/new/', got %v", result["source_path"])
	}
}

func TestDeleteSource(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverPayload, _ := json.Marshal(map[string]interface{}{
		"name": "Delete Server", "type": "linux", "host": "10.0.0.7",
		"port": 22, "connection_type": "ssh",
	})
	wSrv := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(serverPayload), db, authSvc)
	if wSrv.Code != http.StatusCreated {
		t.Fatalf("create server: %d %s", wSrv.Code, wSrv.Body.String())
	}
	var srv map[string]interface{}
	json.NewDecoder(wSrv.Body).Decode(&srv)
	srvID := int(srv["id"].(float64))

	// Create source
	createPayload, _ := json.Marshal(map[string]interface{}{
		"name": "To Delete", "type": "web", "source_path": "/tmp/",
	})
	wCreate := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(createPayload), db, authSvc)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create source: %d %s", wCreate.Code, wCreate.Body.String())
	}
	var created map[string]interface{}
	json.NewDecoder(wCreate.Body).Decode(&created)
	srcID := int(created["id"].(float64))

	// Delete
	wDel := authenticatedRequest(t, http.MethodDelete, fmt.Sprintf("/api/sources/%d", srcID), nil, db, authSvc)
	if wDel.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d; body: %s", wDel.Code, wDel.Body.String())
	}

	// List should return empty
	wList := authenticatedRequest(t, http.MethodGet, fmt.Sprintf("/api/servers/%d/sources", srvID), nil, db, authSvc)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", wList.Code, wList.Body.String())
	}
	var results []map[string]interface{}
	json.NewDecoder(wList.Body).Decode(&results)
	if len(results) != 0 {
		t.Errorf("expected empty list after delete, got %d items", len(results))
	}
}

func TestDependsOn(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverPayload, _ := json.Marshal(map[string]interface{}{
		"name": "Dep Server", "type": "linux", "host": "10.0.0.8",
		"port": 22, "connection_type": "ssh",
	})
	wSrv := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(serverPayload), db, authSvc)
	if wSrv.Code != http.StatusCreated {
		t.Fatalf("create server: %d %s", wSrv.Code, wSrv.Body.String())
	}
	var srv map[string]interface{}
	json.NewDecoder(wSrv.Body).Decode(&srv)
	srvID := int(srv["id"].(float64))

	// Create source A
	pA, _ := json.Marshal(map[string]interface{}{
		"name": "Source A", "type": "web", "source_path": "/a/",
	})
	wA := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(pA), db, authSvc)
	if wA.Code != http.StatusCreated {
		t.Fatalf("create A: %d %s", wA.Code, wA.Body.String())
	}
	var srcA map[string]interface{}
	json.NewDecoder(wA.Body).Decode(&srcA)
	idA := int(srcA["id"].(float64))

	// Create source B with depends_on A
	pB, _ := json.Marshal(map[string]interface{}{
		"name": "Source B", "type": "config", "source_path": "/b/",
		"depends_on": idA,
	})
	wB := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(pB), db, authSvc)
	if wB.Code != http.StatusCreated {
		t.Fatalf("expected 201 for B depends_on A, got %d; body: %s", wB.Code, wB.Body.String())
	}

	var srcB map[string]interface{}
	json.NewDecoder(wB.Body).Decode(&srcB)
	if srcB["depends_on"] != float64(idA) {
		t.Errorf("expected depends_on %d, got %v", idA, srcB["depends_on"])
	}
}

func TestCycleDetection(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverPayload, _ := json.Marshal(map[string]interface{}{
		"name": "Cycle Server", "type": "linux", "host": "10.0.0.9",
		"port": 22, "connection_type": "ssh",
	})
	wSrv := authenticatedRequest(t, http.MethodPost, "/api/servers", bytes.NewReader(serverPayload), db, authSvc)
	if wSrv.Code != http.StatusCreated {
		t.Fatalf("create server: %d %s", wSrv.Code, wSrv.Body.String())
	}
	var srv map[string]interface{}
	json.NewDecoder(wSrv.Body).Decode(&srv)
	srvID := int(srv["id"].(float64))

	// Create A
	pA, _ := json.Marshal(map[string]interface{}{
		"name": "A", "type": "web", "source_path": "/a/",
	})
	wA := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(pA), db, authSvc)
	if wA.Code != http.StatusCreated {
		t.Fatalf("create A: %d %s", wA.Code, wA.Body.String())
	}
	var srcA map[string]interface{}
	json.NewDecoder(wA.Body).Decode(&srcA)
	idA := int(srcA["id"].(float64))

	// Create B depends_on A
	pB, _ := json.Marshal(map[string]interface{}{
		"name": "B", "type": "web", "source_path": "/b/",
		"depends_on": idA,
	})
	wB := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(pB), db, authSvc)
	if wB.Code != http.StatusCreated {
		t.Fatalf("create B depends_on A: %d %s", wB.Code, wB.Body.String())
	}
	var srcB map[string]interface{}
	json.NewDecoder(wB.Body).Decode(&srcB)
	idB := int(srcB["id"].(float64))

	// Create C depends_on B
	pC, _ := json.Marshal(map[string]interface{}{
		"name": "C", "type": "web", "source_path": "/c/",
		"depends_on": idB,
	})
	wC := authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/api/servers/%d/sources", srvID), bytes.NewReader(pC), db, authSvc)
	if wC.Code != http.StatusCreated {
		t.Fatalf("create C depends_on B: %d %s", wC.Code, wC.Body.String())
	}
	var srcC map[string]interface{}
	json.NewDecoder(wC.Body).Decode(&srcC)
	idC := int(srcC["id"].(float64))

	// Try to update A to depend on C — this should create A→C→B→A cycle → 400
	updatePayload, _ := json.Marshal(map[string]interface{}{
		"name": "A", "type": "web", "source_path": "/a/",
		"depends_on": idC,
	})
	wUpdate := authenticatedRequest(t, http.MethodPut, fmt.Sprintf("/api/sources/%d", idA), bytes.NewReader(updatePayload), db, authSvc)
	if wUpdate.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cycle, got %d; body: %s", wUpdate.Code, wUpdate.Body.String())
	}
}
