// cmd/server/main.go
package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/backupmanager/backupmanager/internal/api"
	"github.com/backupmanager/backupmanager/internal/auth"
	"github.com/backupmanager/backupmanager/internal/backup"
	"github.com/backupmanager/backupmanager/internal/config"
	"github.com/backupmanager/backupmanager/internal/database"
	"github.com/backupmanager/backupmanager/internal/discovery"
	"github.com/backupmanager/backupmanager/internal/setup"
	ws "github.com/backupmanager/backupmanager/internal/websocket"
)

//go:embed static
var staticFS embed.FS

func main() {
	// 1. Load config from environment
	cfg := config.Load()

	// 2. Ensure data directories exist
	if err := setup.EnsureDataDirs(cfg.DataDir); err != nil {
		log.Fatalf("Failed to create data directories: %v", err)
	}

	// 3. Open SQLite database and run migrations
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Printf("ERROR: database migration failed: %v", err)
		os.Exit(1)
	}

	// 3a. Recover from any previous crash before starting the scheduler.
	recoveryResult := backup.RecoverFromCrash(db, cfg.BackupDir)
	if recoveryResult.RunsRecovered > 0 || recoveryResult.SyncsRecovered > 0 || recoveryResult.FilesCleanedUp > 0 {
		log.Printf("RecoverFromCrash: runs_recovered=%d syncs_recovered=%d files_cleaned=%d",
			recoveryResult.RunsRecovered, recoveryResult.SyncsRecovered, recoveryResult.FilesCleanedUp)
	}
	for _, e := range recoveryResult.Errors {
		log.Printf("RecoverFromCrash WARNING: %s", e)
	}

	// 4. Ensure admin user exists (first-run setup)
	if err := setup.EnsureAdminUser(db, "admin", "admin@localhost", "admin"); err != nil {
		log.Fatalf("Failed to ensure admin user: %v", err)
	} else {
		var count int
		db.DB().QueryRow("SELECT COUNT(*) FROM users WHERE is_admin = 1 AND username = 'admin' AND password_hash != ''").Scan(&count)
		if count > 0 {
			log.Println("WARNING: Default admin credentials are in use. Change your password after first login.")
		}
	}

	// 5. Generate or load JWT secret
	jwtSecret := cfg.JWTSecret
	if jwtSecret == "" {
		// Try to load from settings table
		var stored string
		err := db.DB().QueryRow("SELECT value FROM settings WHERE key = 'jwt_secret'").Scan(&stored)
		if err == nil && stored != "" {
			jwtSecret = stored
		} else {
			// Generate a new random 32-byte secret
			secretBytes := make([]byte, 32)
			if _, err := rand.Read(secretBytes); err != nil {
				log.Fatalf("Failed to generate JWT secret: %v", err)
			}
			jwtSecret = hex.EncodeToString(secretBytes)
			// Persist to settings
			_, err = db.DB().Exec(
				"INSERT OR REPLACE INTO settings (key, value) VALUES ('jwt_secret', ?)",
				jwtSecret,
			)
			if err != nil {
				log.Printf("WARNING: could not persist JWT secret to database: %v", err)
			}
		}
	}

	// 6. Create auth service
	authSvc := auth.NewService(jwtSecret)

	// 7. Create WebSocket hub
	hub := ws.NewHub(authSvc)
	go hub.Run()

	// 7a. Start auto-scanner (24h interval)
	autoScanner := discovery.NewAutoScanner(db, authSvc.CredentialKey(), 0)
	autoScanner.Start()

	// 8. Create backup orchestrator and runner for job triggering
	orchestrator := backup.NewOrchestrator(db)
	orchestrator.SetCredentialKey(authSvc.CredentialKey())
	orchestrator.SetSkipPreflight(false)
	runner := backup.NewRunner(orchestrator, db)

	triggerFn := func(jobID int) (int, error) {
		// Create a pending run record and return its ID immediately
		now := time.Now().UTC().Format(time.RFC3339)
		result, err := db.DB().Exec(
			`INSERT INTO backup_runs (job_id, status, started_at, created_at) VALUES (?, 'pending', ?, ?)`,
			jobID, now, now,
		)
		if err != nil {
			return 0, fmt.Errorf("failed to create run record: %w", err)
		}
		runID64, _ := result.LastInsertId()
		runID := int(runID64)

		// Run backup in background goroutine
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer cancel()
			runResult, err := runner.Run(ctx, jobID)
			if err != nil {
				log.Printf("Backup job %d failed: %v", jobID, err)
			} else {
				log.Printf("Backup job %d completed: status=%s, size=%d", jobID, runResult.Status, runResult.TotalSize)
			}
		}()

		return runID, nil
	}

	// 8a. Create API router with WebSocket support and trigger function
	apiRouter := api.NewRouterWithWebSocket(db, authSvc, nil, hub, triggerFn)

	// 9. Wrap with RefreshMiddleware
	apiRouter = authSvc.RefreshMiddleware(apiRouter)

	// 9. Serve embedded frontend for non-API/ws routes (SPA fallback)
	subFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("Failed to create frontend sub-filesystem: %v", err)
	}
	fileServer := http.FileServer(http.FS(subFS))

	// 10. Build main mux: API routes + SPA fallback
	mux := http.NewServeMux()

	// Delegate all /api/ and /ws/ traffic to the API router
	mux.Handle("/api/", apiRouter)
	mux.Handle("/ws/", apiRouter)

	// SPA fallback: serve index.html for any non-asset path
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// If the path contains a dot it's likely a static asset — try to serve it directly
		path := r.URL.Path
		if path != "/" && !strings.Contains(path, ".") {
			// Rewrite to index.html for SPA client-side routing
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	// 11. Start HTTP server
	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// 12. Graceful shutdown on SIGINT/SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("BackupManager starting on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-quit
	log.Println("Shutting down server...")
	autoScanner.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server stopped cleanly.")
}
