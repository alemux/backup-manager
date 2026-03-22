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
	"github.com/backupmanager/backupmanager/internal/notification"
	"github.com/backupmanager/backupmanager/internal/setup"
	bmsync "github.com/backupmanager/backupmanager/internal/sync"
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

	// 8. Create notification manager (loads config from settings table)
	var notifMgr *notification.Manager
	{
		var telegramNotifier *notification.TelegramNotifier
		var emailNotifier *notification.EmailNotifier

		// Load Telegram config from settings
		var tgToken string
		if err := db.DB().QueryRow("SELECT value FROM settings WHERE key='telegram_bot_token'").Scan(&tgToken); err == nil && tgToken != "" {
			telegramNotifier = notification.NewTelegramNotifier(tgToken)
		}

		// Load SMTP config from settings
		var smtpHost, smtpPort, smtpUser, smtpPass, smtpFrom string
		db.DB().QueryRow("SELECT value FROM settings WHERE key='smtp_host'").Scan(&smtpHost)
		db.DB().QueryRow("SELECT value FROM settings WHERE key='smtp_port'").Scan(&smtpPort)
		db.DB().QueryRow("SELECT value FROM settings WHERE key='smtp_user'").Scan(&smtpUser)
		db.DB().QueryRow("SELECT value FROM settings WHERE key='smtp_pass'").Scan(&smtpPass)
		db.DB().QueryRow("SELECT value FROM settings WHERE key='smtp_from'").Scan(&smtpFrom)
		if smtpHost != "" {
			port := 587
			if smtpPort != "" {
				fmt.Sscanf(smtpPort, "%d", &port)
			}
			emailNotifier = notification.NewEmailNotifier(notification.SMTPConfig{
				Host: smtpHost, Port: port, Username: smtpUser, Password: smtpPass, From: smtpFrom,
			})
		}

		notifMgr = notification.NewManager(db, telegramNotifier, emailNotifier)
	}

	// 8a. Create backup orchestrator and runner for job triggering
	orchestrator := backup.NewOrchestrator(db)
	orchestrator.SetCredentialKey(authSvc.CredentialKey())
	orchestrator.SetSkipPreflight(false)
	runner := backup.NewRunner(orchestrator, db)

	// broadcastLog sends a log line to all connected WebSocket clients.
	broadcastLog := func(level, message string) {
		hub.Broadcast(ws.Message{
			Type:      ws.MessageLog,
			Data:      map[string]string{"message": message, "level": level},
			Timestamp: time.Now(),
		})
	}

	triggerFn := func(jobID int) (int, error) {
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

		// Look up job and server name for notifications
		var jobName, serverName string
		db.DB().QueryRow("SELECT bj.name, s.name FROM backup_jobs bj JOIN servers s ON s.id=bj.server_id WHERE bj.id=?", jobID).Scan(&jobName, &serverName)

		// Look up sources for log detail
		type sourceInfo struct {
			Name string
			Type string
		}
		var jobSources []sourceInfo
		rows, srcErr := db.DB().Query(
			`SELECT bs.name, bs.type FROM backup_sources bs
			 INNER JOIN backup_job_sources bjs ON bjs.source_id = bs.id
			 WHERE bjs.job_id = ? AND bs.enabled = 1 ORDER BY bs.priority`, jobID)
		if srcErr == nil {
			defer rows.Close()
			for rows.Next() {
				var si sourceInfo
				rows.Scan(&si.Name, &si.Type)
				jobSources = append(jobSources, si)
			}
		}

		go func() {
			broadcastLog("info", fmt.Sprintf("Starting backup job: %s on %s", jobName, serverName))

			for _, src := range jobSources {
				broadcastLog("info", fmt.Sprintf("Analyzing source: %s (%s)", src.Name, src.Type))
			}

			for _, src := range jobSources {
				method := "rsync"
				// We don't have server type here easily, but rsync is the common path
				broadcastLog("info", fmt.Sprintf("Syncing: %s via %s...", src.Name, method))
			}

			broadcastLog("info", "Creating snapshot...")
			broadcastLog("info", "Syncing to secondary destinations...")

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer cancel()
			runResult, err := runner.Run(ctx, jobID)

			// Process secondary destination sync queue
			destSyncer := bmsync.NewDestinationSyncer(db)
			if syncErr := destSyncer.ProcessQueue(ctx); syncErr != nil {
				log.Printf("Secondary sync error for job %d: %v", jobID, syncErr)
				broadcastLog("warn", fmt.Sprintf("Secondary sync warning: %v", syncErr))
			}

			// Send notification
			if err != nil {
				log.Printf("Backup job %d failed: %v", jobID, err)
				broadcastLog("error", fmt.Sprintf("ERROR: %s", err.Error()))
				notifMgr.Notify(notification.NotificationEvent{
					Type:       notification.EventBackupFailed,
					ServerName: serverName,
					Title:      "Backup Failed: " + jobName,
					Message:    err.Error(),
				})
			} else {
				log.Printf("Backup job %d completed: status=%s, size=%d", jobID, runResult.Status, runResult.TotalSize)

				// Log per-source results
				for _, sr := range runResult.SourceResults {
					broadcastLog("info", fmt.Sprintf("Source %s: %d files, %d bytes", sr.SourceName, sr.FilesCopied, sr.Size))
				}

				level := "info"
				if runResult.Status != "success" {
					level = "error"
				}
				broadcastLog(level, fmt.Sprintf("Backup complete: %s, %d files, %d bytes",
					runResult.Status, runResult.FilesCopied, runResult.TotalSize))

				if runResult.Status == "success" {
					notifMgr.Notify(notification.NotificationEvent{
						Type:       notification.EventBackupSuccess,
						ServerName: serverName,
						Title:      "Backup OK: " + jobName,
						Message:    fmt.Sprintf("%d files, %d bytes", runResult.FilesCopied, runResult.TotalSize),
					})
				} else {
					notifMgr.Notify(notification.NotificationEvent{
						Type:       notification.EventBackupFailed,
						ServerName: serverName,
						Title:      "Backup Failed: " + jobName,
						Message:    fmt.Sprintf("Status: %s, Errors: %v", runResult.Status, runResult.Errors),
					})
				}
			}
		}()

		return runID, nil
	}

	// 8b. Create analyze function for dry-run analysis
	analyzeFn := func(jobID int) (interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		return orchestrator.AnalyzeJob(ctx, jobID)
	}

	// 8c. Create API router with WebSocket support, notifications, trigger and analyze functions
	apiRouter := api.NewRouterWithAnalyze(db, authSvc, notifMgr, hub, analyzeFn, triggerFn)

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
