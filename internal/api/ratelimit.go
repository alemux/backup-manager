// internal/api/ratelimit.go
package api

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/backupmanager/backupmanager/internal/database"
)

const (
	rateLimitMax      = 5              // Max attempts allowed
	rateLimitWindow   = 5 * time.Minute // Window for counting attempts
	rateLimitBlockDur = 15 * time.Minute // How long to block after exceeding
)

// RateLimiter tracks login attempts per IP and persists blocks in SQLite.
type RateLimiter struct {
	db       *database.Database
	mu       sync.Mutex
	attempts map[string][]time.Time // IP → timestamps of attempts
}

// NewRateLimiter creates a new RateLimiter backed by the given database.
func NewRateLimiter(db *database.Database) *RateLimiter {
	return &RateLimiter{
		db:       db,
		attempts: make(map[string][]time.Time),
	}
}

// Check returns true if the request is allowed, false if rate limited.
// Tracks by IP address. Max 5 attempts per 5 minutes.
// After exceeding, blocks for 15 minutes.
func (rl *RateLimiter) Check(ip string) bool {
	// First check persistent block in DB
	if rl.IsBlocked(ip) {
		return false
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rateLimitWindow)

	// Filter out old attempts outside the window
	var recent []time.Time
	for _, t := range rl.attempts[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	rl.attempts[ip] = recent

	if len(recent) >= rateLimitMax {
		// Block the IP persistently
		go func() {
			_ = rl.Block(ip, rateLimitBlockDur)
		}()
		return false
	}

	return true
}

// RecordAttempt records a login attempt for an IP.
func (rl *RateLimiter) RecordAttempt(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.attempts[ip] = append(rl.attempts[ip], time.Now())
}

// IsBlocked checks if an IP is blocked (persisted in SQLite).
func (rl *RateLimiter) IsBlocked(ip string) bool {
	key := fmt.Sprintf("rate_block:%s", ip)
	var value string
	err := rl.db.DB().QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err != nil {
		// No entry found → not blocked
		return false
	}

	expiry, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return false
	}

	if time.Now().Unix() > expiry {
		// Block has expired; clean it up
		rl.db.DB().Exec("DELETE FROM settings WHERE key = ?", key)
		return false
	}

	return true
}

// Block blocks an IP for the specified duration (persists in SQLite).
func (rl *RateLimiter) Block(ip string, duration time.Duration) error {
	key := fmt.Sprintf("rate_block:%s", ip)
	expiry := time.Now().Add(duration).Unix()
	_, err := rl.db.DB().Exec(
		"INSERT OR REPLACE INTO settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)",
		key, strconv.FormatInt(expiry, 10),
	)
	if err != nil {
		return fmt.Errorf("block IP %s: %w", ip, err)
	}
	return nil
}

// CleanupExpired removes expired blocks from the database.
func (rl *RateLimiter) CleanupExpired() error {
	now := strconv.FormatInt(time.Now().Unix(), 10)
	_, err := rl.db.DB().Exec(
		"DELETE FROM settings WHERE key LIKE 'rate_block:%' AND CAST(value AS INTEGER) < ?",
		now,
	)
	if err != nil {
		return fmt.Errorf("cleanup expired rate limit blocks: %w", err)
	}
	return nil
}

// RateLimitMiddleware wraps the login handler with rate limiting.
func RateLimitMiddleware(rl *RateLimiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)

		if !rl.Check(ip) {
			Error(w, http.StatusTooManyRequests, "too many login attempts, please try again later")
			return
		}

		// Record the attempt before passing to the next handler
		rl.RecordAttempt(ip)

		next.ServeHTTP(w, r)
	})
}

// extractIP extracts the client IP from the request, preferring X-Forwarded-For.
func extractIP(r *http.Request) string {
	// Check X-Forwarded-For first (for reverse proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	// Fall back to RemoteAddr (strip port)
	ip := r.RemoteAddr
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] == ':' {
			return ip[:i]
		}
	}
	return ip
}
