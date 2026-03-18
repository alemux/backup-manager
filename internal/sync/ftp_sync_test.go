package sync

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// TestFTPSyncer_ImplementsInterface is a compile-time assertion that
// FTPSyncer satisfies the Syncer interface.
func TestFTPSyncer_ImplementsInterface(t *testing.T) {
	var _ Syncer = (*FTPSyncer)(nil)
}

// TestNewFTPSyncer verifies the constructor returns a non-nil instance.
func TestNewFTPSyncer(t *testing.T) {
	s := NewFTPSyncer()
	if s == nil {
		t.Fatal("NewFTPSyncer returned nil")
	}
}

// ── calculateFileChecksum ─────────────────────────────────────────────────────

// TestCalculateFileChecksum verifies that calculateFileChecksum returns a
// valid, stable SHA-256 hex digest for a known file.
func TestCalculateFileChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := calculateFileChecksum(path)
	if err != nil {
		t.Fatalf("calculateFileChecksum returned error: %v", err)
	}

	if got == "" {
		t.Error("expected non-empty checksum")
	}
	if len(got) != 64 {
		t.Errorf("SHA-256 hex must be 64 chars, got %d", len(got))
	}
	for _, c := range got {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("non-hex character in checksum: %q", got)
			break
		}
	}

	// Same content → same digest.
	got2, err := calculateFileChecksum(path)
	if err != nil {
		t.Fatalf("second calculateFileChecksum returned error: %v", err)
	}
	if got != got2 {
		t.Errorf("checksum not stable: %q vs %q", got, got2)
	}
}

func TestCalculateFileChecksum_DifferentContents(t *testing.T) {
	dir := t.TempDir()

	writeFile := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	c1, err := calculateFileChecksum(writeFile("a.txt", "content A"))
	if err != nil {
		t.Fatal(err)
	}
	c2, err := calculateFileChecksum(writeFile("b.txt", "content B"))
	if err != nil {
		t.Fatal(err)
	}
	if c1 == c2 {
		t.Error("different files must produce different checksums")
	}
}

func TestCalculateFileChecksum_NotFound(t *testing.T) {
	_, err := calculateFileChecksum("/nonexistent/path/file.txt")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// ── rateLimitedReader ─────────────────────────────────────────────────────────

// TestRateLimitedCopy verifies that rateLimitedReader enforces a minimum
// transfer time when a bandwidth limit is configured.
//
// Strategy: transfer 50 KB at 10 KB/s. With a burst of 10 KB the limiter
// must refill tokens across ~4 additional seconds, so the total transfer
// takes at least ~4 seconds. We assert >= 3.5 s for a 12.5% timing margin.
func TestRateLimitedCopy(t *testing.T) {
	dataSize := 50 * 1024  // 50 KB total
	limitKBps := 10        // 10 KB/s
	burstKB := 10          // burst = 10 KB (≥ any single OS read chunk)

	data := make([]byte, dataSize)
	for i := range data {
		data[i] = byte(i % 256)
	}

	bytesPerSec := limitKBps * 1024
	burst := burstKB * 1024
	limiter := rate.NewLimiter(rate.Limit(bytesPerSec), burst)
	src := &rateLimitedReader{
		r:       strings.NewReader(string(data)),
		limiter: limiter,
		ctx:     context.Background(),
	}

	start := time.Now()
	n, err := io.Copy(io.Discard, src)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("rate-limited copy failed: %v", err)
	}
	if n != int64(dataSize) {
		t.Errorf("expected %d bytes copied, got %d", dataSize, n)
	}

	// 50 KB / 10 KB/s = 5 s total; subtract burst of 10 KB → at least 4 s.
	// We use 3.5 s as the lower bound to accommodate CI scheduling jitter.
	minExpected := 3500 * time.Millisecond
	if elapsed < minExpected {
		t.Errorf("rate limiting did not slow transfer: elapsed %v, expected >= %v", elapsed, minExpected)
	}
}

// TestRateLimitedCopy_Unlimited verifies that a plain io.Copy (no limiter)
// finishes quickly for small data.
func TestRateLimitedCopy_Unlimited(t *testing.T) {
	dataSize := 10 * 1024
	data := make([]byte, dataSize)

	start := time.Now()
	_, err := io.Copy(io.Discard, strings.NewReader(string(data)))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	// 10 KB without throttle should finish in well under 500 ms on any machine.
	if elapsed > 500*time.Millisecond {
		t.Errorf("unlimited copy took too long: %v", elapsed)
	}
}

// TestRateLimitedCopy_ContextCancel checks that the rate-limited reader
// respects context cancellation.
func TestRateLimitedCopy_ContextCancel(t *testing.T) {
	dataSize := 100 * 1024 // 100 KB
	data := make([]byte, dataSize)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	// 1 KB/s — very slow, so the cancel fires long before completion.
	// Burst must be >= the largest single Read chunk (≤32 KB).
	limiter := rate.NewLimiter(rate.Limit(1024), 32*1024)
	src := &rateLimitedReader{
		r:       strings.NewReader(string(data)),
		limiter: limiter,
		ctx:     ctx,
	}

	_, err := io.Copy(io.Discard, src)
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

// ── localFilePath ─────────────────────────────────────────────────────────────

func TestLocalFilePath(t *testing.T) {
	cases := []struct {
		dest       string
		remoteRoot string
		remotePath string
		want       string
	}{
		{
			dest:       "/backups",
			remoteRoot: "/remote",
			remotePath: "/remote/subdir/file.txt",
			want:       filepath.Join("/backups", "subdir", "file.txt"),
		},
		{
			dest:       "/backups",
			remoteRoot: "/remote",
			remotePath: "/remote/file.txt",
			want:       filepath.Join("/backups", "file.txt"),
		},
	}

	for _, tc := range cases {
		got := localFilePath(tc.dest, tc.remoteRoot, tc.remotePath)
		if got != tc.want {
			t.Errorf("localFilePath(%q, %q, %q) = %q; want %q",
				tc.dest, tc.remoteRoot, tc.remotePath, got, tc.want)
		}
	}
}
