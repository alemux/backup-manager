// internal/api/jobs_handler_test.go
package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/database"
)

// jobsAuthRequest is a variant of authenticatedRequest that includes a TriggerFunc.
func jobsAuthRequest(t *testing.T, method, path string, body io.Reader, db *database.Database, authSvc *auth.Service, trigger TriggerFunc) *httptest.ResponseRecorder {
	t.Helper()

	hash, err := auth.HashPassword("testpass")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	var userID int
	row := db.DB().QueryRow("SELECT id FROM users WHERE username = 'testuser'")
	if err := row.Scan(&userID); err != nil {
		res, err := db.DB().Exec(
			"INSERT INTO users (username, email, password_hash, is_admin) VALUES (?, ?, ?, ?)",
			"testuser", "testuser@example.com", hash, 1,
		)
		if err != nil {
			t.Fatalf("insert test user: %v", err)
		}
		id, _ := res.LastInsertId()
		userID = int(id)
	}

	token, err := authSvc.GenerateToken(userID, "testuser", true)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	req := httptest.NewRequest(method, path, body)
	req.AddCookie(&http.Cookie{Name: "token", Value: token})
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	router := NewRouter(db, authSvc, trigger)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// createJobTestServer inserts a server and returns its ID.
func createJobTestServer(t *testing.T, db *database.Database) int {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.DB().Exec(
		`INSERT INTO servers (name, type, host, port, connection_type, username, status, created_at, updated_at)
		 VALUES (?, 'linux', '10.0.0.1', 22, 'ssh', 'user', 'unknown', ?, ?)`,
		"Test Server", now, now,
	)
	if err != nil {
		t.Fatalf("create test server: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// createJobTestSource inserts a backup source for the given server and returns its ID.
func createJobTestSource(t *testing.T, db *database.Database, serverID int) int {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := db.DB().Exec(
		`INSERT INTO backup_sources (server_id, name, type, source_path, priority, enabled, created_at)
		 VALUES (?, 'Test Source', 'web', '/var/www', 0, 1, ?)`,
		serverID, now,
	)
	if err != nil {
		t.Fatalf("create test source: %v", err)
	}
	id, _ := res.LastInsertId()
	return int(id)
}

// jobPayload builds a standard job creation payload.
func jobPayload(name string, serverID int, sourceIDs []int) []byte {
	payload, _ := json.Marshal(map[string]interface{}{
		"name":      name,
		"server_id": serverID,
		"schedule":  "0 3 * * *",
		"source_ids": sourceIDs,
	})
	return payload
}

// authReq is a shortcut to jobsAuthRequest with a nil trigger.
func authReq(t *testing.T, method, path string, body io.Reader, db *database.Database, authSvc *auth.Service) *httptest.ResponseRecorder {
	return jobsAuthRequest(t, method, path, body, db, authSvc, nil)
}

func TestCreateJob(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverID := createJobTestServer(t, db)
	srcID := createJobTestSource(t, db, serverID)

	w := authReq(t, http.MethodPost, "/api/jobs",
		bytes.NewReader(jobPayload("Nightly Backup", serverID, []int{srcID})),
		db, authSvc)

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
	if result["name"] != "Nightly Backup" {
		t.Errorf("expected name 'Nightly Backup', got %v", result["name"])
	}
	if result["schedule"] != "0 3 * * *" {
		t.Errorf("expected schedule '0 3 * * *', got %v", result["schedule"])
	}
	if result["server_id"] == nil {
		t.Error("expected server_id in response")
	}
	// Verify source_ids includes the source
	srcIDs, ok := result["source_ids"].([]interface{})
	if !ok {
		t.Fatalf("expected source_ids to be an array, got %T", result["source_ids"])
	}
	if len(srcIDs) != 1 || int(srcIDs[0].(float64)) != srcID {
		t.Errorf("expected source_ids [%d], got %v", srcID, srcIDs)
	}
	if result["last_run"] != nil {
		t.Errorf("expected last_run to be null for new job, got %v", result["last_run"])
	}
	if result["created_at"] == nil {
		t.Error("expected created_at in response")
	}
}

func TestCreateJobValidation(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)
	serverID := createJobTestServer(t, db)

	tests := []struct {
		name    string
		payload map[string]interface{}
	}{
		{
			name: "invalid cron schedule",
			payload: map[string]interface{}{
				"name":      "Bad Job",
				"server_id": serverID,
				"schedule":  "not-a-cron",
			},
		},
		{
			name: "missing name",
			payload: map[string]interface{}{
				"server_id": serverID,
				"schedule":  "0 3 * * *",
			},
		},
		{
			name: "missing server",
			payload: map[string]interface{}{
				"name":      "Orphan Job",
				"server_id": 99999,
				"schedule":  "0 3 * * *",
			},
		},
		{
			name: "missing server_id",
			payload: map[string]interface{}{
				"name":     "No Server",
				"schedule": "0 3 * * *",
			},
		},
		{
			name: "missing schedule",
			payload: map[string]interface{}{
				"name":      "No Schedule",
				"server_id": serverID,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(tc.payload)
			w := authReq(t, http.MethodPost, "/api/jobs", bytes.NewReader(payload), db, authSvc)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d; body: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestListJobs(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverID := createJobTestServer(t, db)

	// Create two jobs
	authReq(t, http.MethodPost, "/api/jobs",
		bytes.NewReader(jobPayload("Job One", serverID, []int{})),
		db, authSvc)
	authReq(t, http.MethodPost, "/api/jobs",
		bytes.NewReader(jobPayload("Job Two", serverID, []int{})),
		db, authSvc)

	w := authReq(t, http.MethodGet, "/api/jobs", nil, db, authSvc)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result []interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 jobs, got %d", len(result))
	}
}

func TestGetJob(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverID := createJobTestServer(t, db)
	srcID := createJobTestSource(t, db, serverID)

	wCreate := authReq(t, http.MethodPost, "/api/jobs",
		bytes.NewReader(jobPayload("Detail Job", serverID, []int{srcID})),
		db, authSvc)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", wCreate.Code, wCreate.Body.String())
	}

	var created map[string]interface{}
	json.NewDecoder(wCreate.Body).Decode(&created)
	id := int(created["id"].(float64))

	w := authReq(t, http.MethodGet, "/api/jobs/"+itoa(id), nil, db, authSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if int(result["id"].(float64)) != id {
		t.Errorf("expected id %d, got %v", id, result["id"])
	}
	if result["name"] != "Detail Job" {
		t.Errorf("expected name 'Detail Job', got %v", result["name"])
	}
	if result["server_name"] == nil || result["server_name"] == "" {
		t.Error("expected server_name in response")
	}
	srcIDs, ok := result["source_ids"].([]interface{})
	if !ok || len(srcIDs) != 1 {
		t.Errorf("expected source_ids with 1 element, got %v", result["source_ids"])
	}
}

func TestUpdateJob(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverID := createJobTestServer(t, db)

	wCreate := authReq(t, http.MethodPost, "/api/jobs",
		bytes.NewReader(jobPayload("Original Name", serverID, []int{})),
		db, authSvc)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", wCreate.Code)
	}
	var created map[string]interface{}
	json.NewDecoder(wCreate.Body).Decode(&created)
	id := int(created["id"].(float64))

	updatePayload, _ := json.Marshal(map[string]interface{}{
		"name":      "Updated Name",
		"server_id": serverID,
		"schedule":  "0 4 * * *",
		"source_ids": []int{},
	})
	w := authReq(t, http.MethodPut, "/api/jobs/"+itoa(id), bytes.NewReader(updatePayload), db, authSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["name"] != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %v", result["name"])
	}
	if result["schedule"] != "0 4 * * *" {
		t.Errorf("expected schedule '0 4 * * *', got %v", result["schedule"])
	}
}

func TestDeleteJob(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverID := createJobTestServer(t, db)

	wCreate := authReq(t, http.MethodPost, "/api/jobs",
		bytes.NewReader(jobPayload("To Delete", serverID, []int{})),
		db, authSvc)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", wCreate.Code)
	}
	var created map[string]interface{}
	json.NewDecoder(wCreate.Body).Decode(&created)
	id := int(created["id"].(float64))

	wDel := authReq(t, http.MethodDelete, "/api/jobs/"+itoa(id), nil, db, authSvc)
	if wDel.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d; body: %s", wDel.Code, wDel.Body.String())
	}

	wGet := authReq(t, http.MethodGet, "/api/jobs/"+itoa(id), nil, db, authSvc)
	if wGet.Code != http.StatusNotFound {
		t.Errorf("expected 404 after delete, got %d", wGet.Code)
	}
}

func TestTriggerJob(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverID := createJobTestServer(t, db)

	wCreate := authReq(t, http.MethodPost, "/api/jobs",
		bytes.NewReader(jobPayload("Trigger Me", serverID, []int{})),
		db, authSvc)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", wCreate.Code)
	}
	var created map[string]interface{}
	json.NewDecoder(wCreate.Body).Decode(&created)
	jobID := int(created["id"].(float64))

	// Fake trigger that inserts a pending run and returns its ID
	trigger := TriggerFunc(func(id int) (int, error) {
		now := time.Now().UTC().Format(time.RFC3339)
		res, err := db.DB().Exec(
			`INSERT INTO backup_runs (job_id, status, started_at, created_at)
			 VALUES (?, 'pending', ?, ?)`,
			id, now, now,
		)
		if err != nil {
			return 0, err
		}
		runID, _ := res.LastInsertId()
		return int(runID), nil
	})

	w := jobsAuthRequest(t, http.MethodPost, "/api/jobs/"+itoa(jobID)+"/trigger",
		nil, db, authSvc, trigger)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result["run_id"] == nil {
		t.Error("expected run_id in response")
	}
	runID := int(result["run_id"].(float64))
	if runID <= 0 {
		t.Errorf("expected positive run_id, got %d", runID)
	}
}

func TestListRuns(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverID := createJobTestServer(t, db)

	// Create a job
	wCreate := authReq(t, http.MethodPost, "/api/jobs",
		bytes.NewReader(jobPayload("Run Test Job", serverID, []int{})),
		db, authSvc)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", wCreate.Code)
	}
	var created map[string]interface{}
	json.NewDecoder(wCreate.Body).Decode(&created)
	jobID := int(created["id"].(float64))

	// Insert some runs directly
	now := time.Now().UTC().Format(time.RFC3339)
	db.DB().Exec(
		`INSERT INTO backup_runs (job_id, status, started_at, created_at) VALUES (?, 'success', ?, ?)`,
		jobID, now, now,
	)
	db.DB().Exec(
		`INSERT INTO backup_runs (job_id, status, started_at, created_at) VALUES (?, 'failed', ?, ?)`,
		jobID, now, now,
	)

	// List all runs
	w := authReq(t, http.MethodGet, "/api/runs", nil, db, authSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	runs, ok := result["runs"].([]interface{})
	if !ok {
		t.Fatalf("expected runs array, got %T", result["runs"])
	}
	if len(runs) != 2 {
		t.Errorf("expected 2 runs, got %d", len(runs))
	}
	if result["total"] == nil {
		t.Error("expected total in response")
	}

	// Filter by status
	wFiltered := authReq(t, http.MethodGet, "/api/runs?status=success", nil, db, authSvc)
	if wFiltered.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wFiltered.Code)
	}
	var filtered map[string]interface{}
	json.NewDecoder(wFiltered.Body).Decode(&filtered)
	filteredRuns, _ := filtered["runs"].([]interface{})
	if len(filteredRuns) != 1 {
		t.Errorf("expected 1 run with status=success, got %d", len(filteredRuns))
	}

	// Filter by job_id
	wByJob := authReq(t, http.MethodGet, "/api/runs?job_id="+itoa(jobID), nil, db, authSvc)
	if wByJob.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wByJob.Code)
	}
	var byJob map[string]interface{}
	json.NewDecoder(wByJob.Body).Decode(&byJob)
	byJobRuns, _ := byJob["runs"].([]interface{})
	if len(byJobRuns) != 2 {
		t.Errorf("expected 2 runs for job_id filter, got %d", len(byJobRuns))
	}
}

func TestListRunsPagination(t *testing.T) {
	db := newTestDB(t)
	authSvc := auth.NewService(testSecret)

	serverID := createJobTestServer(t, db)

	// Create a job
	wCreate := authReq(t, http.MethodPost, "/api/jobs",
		bytes.NewReader(jobPayload("Pagination Job", serverID, []int{})),
		db, authSvc)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create failed: %d", wCreate.Code)
	}
	var created map[string]interface{}
	json.NewDecoder(wCreate.Body).Decode(&created)
	jobID := int(created["id"].(float64))

	// Insert 5 runs
	now := time.Now().UTC().Format(time.RFC3339)
	for i := 0; i < 5; i++ {
		db.DB().Exec(
			`INSERT INTO backup_runs (job_id, status, started_at, created_at) VALUES (?, 'success', ?, ?)`,
			jobID, now, now,
		)
	}

	// Page 1, 2 per page
	w := authReq(t, http.MethodGet, "/api/runs?page=1&per_page=2", nil, db, authSvc)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	runs, _ := result["runs"].([]interface{})
	if len(runs) != 2 {
		t.Errorf("expected 2 runs on page 1, got %d", len(runs))
	}
	if int(result["total"].(float64)) != 5 {
		t.Errorf("expected total=5, got %v", result["total"])
	}
	if int(result["page"].(float64)) != 1 {
		t.Errorf("expected page=1, got %v", result["page"])
	}
	if int(result["per_page"].(float64)) != 2 {
		t.Errorf("expected per_page=2, got %v", result["per_page"])
	}

	// Page 2, 2 per page
	w2 := authReq(t, http.MethodGet, "/api/runs?page=2&per_page=2", nil, db, authSvc)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w2.Code, w2.Body.String())
	}
	var result2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&result2)
	runs2, _ := result2["runs"].([]interface{})
	if len(runs2) != 2 {
		t.Errorf("expected 2 runs on page 2, got %d", len(runs2))
	}

	// Page 3, 2 per page — should have 1
	w3 := authReq(t, http.MethodGet, "/api/runs?page=3&per_page=2", nil, db, authSvc)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w3.Code)
	}
	var result3 map[string]interface{}
	json.NewDecoder(w3.Body).Decode(&result3)
	runs3, _ := result3["runs"].([]interface{})
	if len(runs3) != 1 {
		t.Errorf("expected 1 run on page 3, got %d", len(runs3))
	}
}
