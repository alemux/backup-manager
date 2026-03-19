// internal/api/ratelimit_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRateLimit_AllowsUnderLimit verifies that up to 5 attempts are all allowed.
// The middleware calls Check first, then RecordAttempt on success, so we mirror that order.
func TestRateLimit_AllowsUnderLimit(t *testing.T) {
	db := newTestDB(t)
	rl := NewRateLimiter(db)

	ip := "192.168.1.1"
	for i := 0; i < rateLimitMax; i++ {
		if !rl.Check(ip) {
			t.Errorf("attempt %d should be allowed (under limit)", i+1)
		}
		rl.RecordAttempt(ip)
	}
}

// TestRateLimit_BlocksAfterExceeding verifies that the 6th attempt is blocked.
func TestRateLimit_BlocksAfterExceeding(t *testing.T) {
	db := newTestDB(t)
	rl := NewRateLimiter(db)

	ip := "192.168.1.2"

	// Record exactly rateLimitMax attempts to fill up the window
	for i := 0; i < rateLimitMax; i++ {
		rl.RecordAttempt(ip)
	}

	// The next check should be blocked
	if rl.Check(ip) {
		t.Error("expected 6th attempt to be blocked")
	}
}

// TestRateLimit_DifferentIPsIndependent verifies different IPs are tracked separately.
func TestRateLimit_DifferentIPsIndependent(t *testing.T) {
	db := newTestDB(t)
	rl := NewRateLimiter(db)

	ip1 := "10.0.0.1"
	ip2 := "10.0.0.2"

	// Exhaust ip1
	for i := 0; i < rateLimitMax; i++ {
		rl.RecordAttempt(ip1)
	}

	// ip1 should be blocked
	if rl.Check(ip1) {
		t.Error("expected ip1 to be blocked")
	}

	// ip2 should still be allowed
	if !rl.Check(ip2) {
		t.Error("expected ip2 to be allowed (different IP)")
	}
}

// TestRateLimit_PersistsBlock verifies that a block is persisted in the DB
// and survives a new RateLimiter instance (simulating a restart).
func TestRateLimit_PersistsBlock(t *testing.T) {
	db := newTestDB(t)
	rl := NewRateLimiter(db)

	ip := "172.16.0.1"
	// Manually block the IP
	if err := rl.Block(ip, rateLimitBlockDur); err != nil {
		t.Fatalf("Block failed: %v", err)
	}

	// Create a new instance with the same DB — simulates restart
	rl2 := NewRateLimiter(db)
	if !rl2.IsBlocked(ip) {
		t.Error("expected block to persist across RateLimiter instances")
	}
}

// TestRateLimit_ExpiresAfterDuration verifies that a block expires after its duration.
func TestRateLimit_ExpiresAfterDuration(t *testing.T) {
	db := newTestDB(t)
	rl := NewRateLimiter(db)

	ip := "172.16.0.2"
	// Block with a very short duration (already expired)
	if err := rl.Block(ip, -1*time.Second); err != nil {
		t.Fatalf("Block failed: %v", err)
	}

	// Should not be blocked since the block has already expired
	if rl.IsBlocked(ip) {
		t.Error("expected block to have expired")
	}
}

// TestRateLimit_CleanupExpired verifies that CleanupExpired removes old blocks.
func TestRateLimit_CleanupExpired(t *testing.T) {
	db := newTestDB(t)
	rl := NewRateLimiter(db)

	ip := "172.16.0.3"
	// Block with an already-expired duration
	if err := rl.Block(ip, -1*time.Second); err != nil {
		t.Fatalf("Block failed: %v", err)
	}

	// Verify the entry exists in the DB before cleanup
	var count int
	db.DB().QueryRow("SELECT COUNT(*) FROM settings WHERE key = 'rate_block:' || ?", ip).Scan(&count)
	if count == 0 {
		t.Fatal("expected block entry in settings before cleanup")
	}

	// Run cleanup
	if err := rl.CleanupExpired(); err != nil {
		t.Fatalf("CleanupExpired failed: %v", err)
	}

	// The expired entry should be gone
	db.DB().QueryRow("SELECT COUNT(*) FROM settings WHERE key = 'rate_block:' || ?", ip).Scan(&count)
	if count != 0 {
		t.Error("expected expired block entry to be removed after cleanup")
	}
}

// TestRateLimitMiddleware_Blocks verifies that the middleware rejects requests
// from a blocked IP with 429 Too Many Requests.
func TestRateLimitMiddleware_Blocks(t *testing.T) {
	db := newTestDB(t)
	rl := NewRateLimiter(db)

	ip := "203.0.113.1"
	if err := rl.Block(ip, rateLimitBlockDur); err != nil {
		t.Fatalf("Block failed: %v", err)
	}

	handler := RateLimitMiddleware(rl, okHandler)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = ip + ":12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 for blocked IP, got %d", w.Code)
	}
}

// TestRateLimitMiddleware_Allows verifies that the middleware passes through
// requests from non-blocked IPs.
func TestRateLimitMiddleware_Allows(t *testing.T) {
	db := newTestDB(t)
	rl := NewRateLimiter(db)

	handler := RateLimitMiddleware(rl, okHandler)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "203.0.113.2:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for allowed IP, got %d", w.Code)
	}
}
