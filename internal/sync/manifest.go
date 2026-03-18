package sync

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ManifestEntry tracks one file's state.
type ManifestEntry struct {
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time,omitempty"` // may be zero if FTP server doesn't provide it
	Checksum string    `json:"checksum"`           // SHA256, authoritative change detector
}

// Manifest tracks all files from a sync source.
type Manifest struct {
	Entries   map[string]ManifestEntry `json:"entries"` // key = file path
	UpdatedAt time.Time                `json:"updated_at"`
}

// NewManifest creates a new, empty Manifest.
func NewManifest() *Manifest {
	return &Manifest{
		Entries:   make(map[string]ManifestEntry),
		UpdatedAt: time.Now(),
	}
}

// LoadManifest reads a manifest from a JSON file on disk.
// If the file does not exist, a new empty manifest is returned without error.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewManifest(), nil
		}
		return nil, fmt.Errorf("read manifest %q: %w", path, err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %q: %w", path, err)
	}
	if m.Entries == nil {
		m.Entries = make(map[string]ManifestEntry)
	}
	return &m, nil
}

// Save writes the manifest to the given path as JSON.
func (m *Manifest) Save(path string) error {
	m.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return fmt.Errorf("write manifest %q: %w", path, err)
	}
	return nil
}

// Compare returns lists of new, modified, and deleted files by comparing the
// current manifest against a fresh remote listing.
//
// Optimisation: if a remote entry's mtime is non-zero, matches the manifest
// entry's mtime AND the size is unchanged, the checksum comparison is skipped
// and the file is treated as unmodified.
func (m *Manifest) Compare(remote []ManifestEntry) (newFiles, modified, deleted []string) {
	// Build a set of remote paths for the deleted-file sweep.
	remoteSeen := make(map[string]struct{}, len(remote))

	for _, r := range remote {
		remoteSeen[r.Path] = struct{}{}

		existing, found := m.Entries[r.Path]
		if !found {
			newFiles = append(newFiles, r.Path)
			continue
		}

		// Optimisation: skip checksum comparison when mtime and size are stable.
		if !r.ModTime.IsZero() && !existing.ModTime.IsZero() &&
			r.ModTime.Equal(existing.ModTime) && r.Size == existing.Size {
			continue // unchanged
		}

		// Fall back to checksum comparison.
		if r.Checksum != existing.Checksum {
			modified = append(modified, r.Path)
		}
	}

	// Every manifest entry absent from the remote listing has been deleted.
	for path := range m.Entries {
		if _, ok := remoteSeen[path]; !ok {
			deleted = append(deleted, path)
		}
	}

	return newFiles, modified, deleted
}
