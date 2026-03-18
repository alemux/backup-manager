package sync

import (
	"context"
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
