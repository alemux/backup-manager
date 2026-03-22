package sync

import (
	"context"
	"fmt"
	"time"
)

// SyncResult holds the outcome of a sync operation.
type SyncResult struct {
	FilesCopied  int
	BytesCopied  int64
	FilesDeleted int
	Duration     time.Duration
	Errors       []string
}

// DryRunResult holds the outcome of an rsync --dry-run --stats analysis.
type DryRunResult struct {
	TotalFiles      int    `json:"total_files"`
	FilesToTransfer int    `json:"files_to_transfer"`
	BytesToTransfer int64  `json:"bytes_to_transfer"`
	BytesTotal      int64  `json:"bytes_total"`
	HumanSize       string `json:"human_size"`
}

// HumanizeBytes converts a byte count to a human-readable string (e.g. "1.2 GB").
func HumanizeBytes(b int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case b >= TB:
		return fmt.Sprintf("%.1f TB", float64(b)/float64(TB))
	case b >= GB:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// SyncOptions configures sync behavior.
type SyncOptions struct {
	BandwidthLimitKBps int      // rsync --bwlimit in KB/s, 0 = unlimited
	Exclude            []string // paths to exclude
	DryRun             bool     // don't actually copy, just report
	Delete             bool     // delete files on dest that don't exist on source
}

// Syncer defines the interface for syncing files from remote to local.
type Syncer interface {
	Sync(ctx context.Context, source SyncSource, destPath string, opts SyncOptions) (*SyncResult, error)
}

// SyncSource describes what to sync and from where.
type SyncSource struct {
	Host       string
	Port       int
	Username   string
	Password   string
	KeyPath    string
	RemotePath string
}
