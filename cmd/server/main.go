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
	"os/exec"
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

		go func() {
			// Small delay to allow WebSocket client to connect before first message
			time.Sleep(100 * time.Millisecond)

			broadcastLog("info", fmt.Sprintf("Starting backup job: %s on %s", jobName, serverName))

			// --- Phase 1: Auto-analysis ---
			broadcastLog("info", "═══════════════ ANALYSIS ═══════════════")
			broadcastLog("info", "Analyzing backup sources...")
			analysisCtx, analysisCancel := context.WithTimeout(context.Background(), 5*time.Minute)
			analysis, analysisErr := orchestrator.AnalyzeJob(analysisCtx, jobID)
			analysisCancel()

			var analysisTotal int64
			if analysisErr != nil {
				broadcastLog("warn", fmt.Sprintf("Analysis failed: %v (proceeding with backup anyway)", analysisErr))
			} else {
				analysisTotal = analysis.TotalBytesToTransfer

				// Check if nothing to transfer
				if analysis.TotalFilesToTransfer == 0 && analysis.TotalBytesToTransfer == 0 {
					broadcastLog("info", "Nothing to backup — all files are up to date")
					// Mark run as success with 0 files
					db.DB().Exec(
						`UPDATE backup_runs SET status = 'success', finished_at = datetime('now'),
						 total_size_bytes = 0, files_copied = 0 WHERE id = ?`, runID)
					hub.Broadcast(ws.Message{
						Type: ws.MessageProgress,
						Data: map[string]interface{}{
							"job_id":       jobID,
							"job_name":     jobName,
							"server_name":  serverName,
							"percent":      100,
							"bytes_done":   0,
							"bytes_total":  0,
							"eta_seconds":  0,
							"current_file": "",
							"status":       "skipped",
							"message":      "Nothing to backup",
						},
						Timestamp: time.Now(),
					})
					return
				}

				broadcastLog("info", fmt.Sprintf("Analysis complete: %d files, %s to transfer",
					analysis.TotalFilesToTransfer, analysis.HumanTotalTransfer))

				// Log per-source analysis
				for _, src := range analysis.Sources {
					if src.Error != "" {
						broadcastLog("warn", fmt.Sprintf("  %s: %s", src.SourceName, src.Error))
					} else {
						broadcastLog("info", fmt.Sprintf("  %s: %d files, %s",
							src.SourceName, src.FilesToTransfer, src.HumanSize))
					}
				}
			}

			// --- Phase 2: Backup with live streaming ---
			broadcastLog("info", "═══════════════ BACKUP ═══════════════")
			tracker := bmsync.NewProcessTracker()
			var lastSeenFile string

			logFunc := func(line string) {
				// Track current file from rsync/lftp output
				trimmed := strings.TrimSpace(line)
				if trimmed != "" && !strings.HasPrefix(trimmed, "Number of") &&
					!strings.HasPrefix(trimmed, "Total") &&
					!strings.HasPrefix(trimmed, "sent ") &&
					!strings.HasPrefix(trimmed, "total size") {
					lastSeenFile = trimmed
				}
				broadcastLog("output", line)
			}

			// Start progress goroutine if we have analysis data
			progressStop := make(chan struct{})
			if analysisTotal > 0 {
				go func() {
					startTime := time.Now()
					ticker := time.NewTicker(10 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							// Get current destination size using du
							var currentSize int64
							var destPath string
							db.DB().QueryRow(
								"SELECT path FROM destinations WHERE is_primary = 1 AND enabled = 1 LIMIT 1",
							).Scan(&destPath)
							if destPath == "" {
								destPath = "/var/backups/backupmanager"
							}
							serverDir := fmt.Sprintf("%s/%s", destPath, serverName)

							duCmd := exec.Command("du", "-s", serverDir)
							if duOut, duErr := duCmd.Output(); duErr == nil {
								fields := strings.Fields(string(duOut))
								if len(fields) >= 1 {
									if kb, parseErr := fmt.Sscanf(fields[0], "%d", &currentSize); parseErr == nil && kb > 0 {
										currentSize = currentSize * 1024 // du reports in KB
									}
								}
							}

							var percent float64
							if analysisTotal > 0 {
								percent = float64(currentSize) / float64(analysisTotal) * 100
								if percent > 100 {
									percent = 99 // cap at 99% until complete
								}
							}

							// Calculate ETA
							elapsed := time.Since(startTime).Seconds()
							var etaSeconds float64
							if percent > 0 && elapsed > 0 {
								etaSeconds = elapsed / percent * (100 - percent)
							}

							hub.Broadcast(ws.Message{
								Type: ws.MessageProgress,
								Data: map[string]interface{}{
									"job_id":       jobID,
									"job_name":     jobName,
									"server_name":  serverName,
									"bytes_done":   currentSize,
									"bytes_total":  analysisTotal,
									"percent":      int(percent),
									"eta_seconds":  int(etaSeconds),
									"current_file": lastSeenFile,
								},
								Timestamp: time.Now(),
							})
						case <-progressStop:
							return
						}
					}
				}()
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
			defer cancel()
			runResult, runErr := runner.RunWithOptions(ctx, jobID, logFunc, tracker)

			// Stop progress goroutine
			close(progressStop)

			// Determine final bytes/files for the complete progress message
			var finalBytes int64
			var finalFiles int
			if runErr == nil && runResult != nil {
				finalBytes = runResult.TotalSize
				finalFiles = runResult.FilesCopied
			}

			// Send 100% progress
			hub.Broadcast(ws.Message{
				Type: ws.MessageProgress,
				Data: map[string]interface{}{
					"job_id":       jobID,
					"job_name":     jobName,
					"server_name":  serverName,
					"percent":      100,
					"bytes_done":   finalBytes,
					"bytes_total":  finalBytes,
					"eta_seconds":  0,
					"current_file": fmt.Sprintf("%d files transferred", finalFiles),
					"status":       "complete",
					"message":      fmt.Sprintf("Backup complete: %d files, %s", finalFiles, bmsync.HumanizeBytes(finalBytes)),
				},
				Timestamp: time.Now(),
			})

			// Process secondary destination sync queue
			destSyncer := bmsync.NewDestinationSyncer(db)
			if syncErr := destSyncer.ProcessQueue(ctx); syncErr != nil {
				log.Printf("Secondary sync error for job %d: %v", jobID, syncErr)
				broadcastLog("warn", fmt.Sprintf("Secondary sync warning: %v", syncErr))
			}

			// Send notification
			if runErr != nil {
				log.Printf("Backup job %d failed: %v", jobID, runErr)
				broadcastLog("error", fmt.Sprintf("ERROR: %s", runErr.Error()))
				notifMgr.Notify(notification.NotificationEvent{
					Type:       notification.EventBackupFailed,
					ServerName: serverName,
					Title:      "Backup Failed: " + jobName,
					Message:    runErr.Error(),
				})
			} else {
				log.Printf("Backup job %d completed: status=%s, size=%d", jobID, runResult.Status, runResult.TotalSize)

				// Log per-source results
				for _, sr := range runResult.SourceResults {
					broadcastLog("info", fmt.Sprintf("Source %s: %d files, %s",
						sr.SourceName, sr.FilesCopied, bmsync.HumanizeBytes(sr.Size)))
				}

				level := "info"
				if runResult.Status != "success" {
					level = "error"
				}
				broadcastLog(level, fmt.Sprintf("Backup complete: %s, %d files, %s",
					runResult.Status, runResult.FilesCopied, bmsync.HumanizeBytes(runResult.TotalSize)))

				if runResult.Status == "success" {
					notifMgr.Notify(notification.NotificationEvent{
						Type:       notification.EventBackupSuccess,
						ServerName: serverName,
						Title:      "Backup OK: " + jobName,
						Message:    fmt.Sprintf("%d files, %s", runResult.FilesCopied, bmsync.HumanizeBytes(runResult.TotalSize)),
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

	// 8b2. Create stop function
	stopFn := func(jobID int) error {
		return orchestrator.StopJob(jobID)
	}

	// 8c. Create API router with WebSocket support, notifications, trigger, analyze, and stop functions
	apiRouter := api.NewRouterFull(db, authSvc, notifMgr, hub, analyzeFn, stopFn, triggerFn)

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
