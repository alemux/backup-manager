package sync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewManifest(t *testing.T) {
	m := NewManifest()
	if m == nil {
		t.Fatal("expected non-nil manifest")
	}
	if m.Entries == nil {
		t.Error("expected non-nil Entries map")
	}
	if len(m.Entries) != 0 {
		t.Errorf("expected empty Entries, got %d", len(m.Entries))
	}
}

func TestManifestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := NewManifest()
	now := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	m.Entries["/backup/file.txt"] = ManifestEntry{
		Path:     "/backup/file.txt",
		Size:     1024,
		ModTime:  now,
		Checksum: "abc123",
	}

	if err := m.Save(path); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	loaded, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded.Entries))
	}

	e, ok := loaded.Entries["/backup/file.txt"]
	if !ok {
		t.Fatal("expected entry for /backup/file.txt")
	}
	if e.Size != 1024 {
		t.Errorf("size: want 1024, got %d", e.Size)
	}
	if e.Checksum != "abc123" {
		t.Errorf("checksum: want abc123, got %s", e.Checksum)
	}
	if !e.ModTime.Equal(now) {
		t.Errorf("mod_time: want %v, got %v", now, e.ModTime)
	}
}

func TestManifestSaveLoad_FileNotExist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	// LoadManifest on a missing file must return an empty manifest, not an error.
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manifest")
	}
	if len(m.Entries) != 0 {
		t.Errorf("expected empty manifest, got %d entries", len(m.Entries))
	}
}

func TestManifestSaveLoad_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json{{{"), 0o640); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// ── Compare tests ────────────────────────────────────────────────────────────

func TestManifestCompare_NewFiles(t *testing.T) {
	m := NewManifest()
	// Manifest is empty; all remote files are new.
	remote := []ManifestEntry{
		{Path: "/a.txt", Size: 100, Checksum: "aaaa"},
		{Path: "/b.txt", Size: 200, Checksum: "bbbb"},
	}

	newFiles, modified, deleted := m.Compare(remote)

	if len(newFiles) != 2 {
		t.Errorf("newFiles: want 2, got %d", len(newFiles))
	}
	if len(modified) != 0 {
		t.Errorf("modified: want 0, got %d", len(modified))
	}
	if len(deleted) != 0 {
		t.Errorf("deleted: want 0, got %d", len(deleted))
	}
}

func TestManifestCompare_ModifiedFiles(t *testing.T) {
	m := NewManifest()
	m.Entries["/a.txt"] = ManifestEntry{Path: "/a.txt", Size: 100, Checksum: "old-checksum"}

	remote := []ManifestEntry{
		{Path: "/a.txt", Size: 100, Checksum: "new-checksum"},
	}

	newFiles, modified, deleted := m.Compare(remote)

	if len(newFiles) != 0 {
		t.Errorf("newFiles: want 0, got %d", len(newFiles))
	}
	if len(modified) != 1 || modified[0] != "/a.txt" {
		t.Errorf("modified: want [/a.txt], got %v", modified)
	}
	if len(deleted) != 0 {
		t.Errorf("deleted: want 0, got %d", len(deleted))
	}
}

func TestManifestCompare_DeletedFiles(t *testing.T) {
	m := NewManifest()
	m.Entries["/gone.txt"] = ManifestEntry{Path: "/gone.txt", Size: 50, Checksum: "dddd"}

	// Remote listing is empty — file has been deleted.
	remote := []ManifestEntry{}

	newFiles, modified, deleted := m.Compare(remote)

	if len(newFiles) != 0 {
		t.Errorf("newFiles: want 0, got %d", len(newFiles))
	}
	if len(modified) != 0 {
		t.Errorf("modified: want 0, got %d", len(modified))
	}
	if len(deleted) != 1 || deleted[0] != "/gone.txt" {
		t.Errorf("deleted: want [/gone.txt], got %v", deleted)
	}
}

func TestManifestCompare_UnchangedFiles(t *testing.T) {
	m := NewManifest()
	m.Entries["/same.txt"] = ManifestEntry{Path: "/same.txt", Size: 500, Checksum: "same-checksum"}

	remote := []ManifestEntry{
		// Same checksum → not modified.
		{Path: "/same.txt", Size: 500, Checksum: "same-checksum"},
	}

	newFiles, modified, deleted := m.Compare(remote)

	if len(newFiles) != 0 {
		t.Errorf("newFiles: want 0, got %d", len(newFiles))
	}
	if len(modified) != 0 {
		t.Errorf("modified: want 0, got %d", len(modified))
	}
	if len(deleted) != 0 {
		t.Errorf("deleted: want 0, got %d", len(deleted))
	}
}

func TestManifestCompare_MtimeOptimization(t *testing.T) {
	// When mtime and size are both unchanged the checksum field on the remote
	// entry is ignored (it may be empty because we haven't downloaded the file
	// yet). The file must be treated as unmodified.
	ts := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	m := NewManifest()
	m.Entries["/opt.txt"] = ManifestEntry{
		Path:     "/opt.txt",
		Size:     256,
		ModTime:  ts,
		Checksum: "known-checksum",
	}

	remote := []ManifestEntry{
		{
			Path:    "/opt.txt",
			Size:    256,
			ModTime: ts,
			// Deliberately different checksum — should be ignored due to mtime+size match.
			Checksum: "different-checksum",
		},
	}

	newFiles, modified, deleted := m.Compare(remote)

	if len(newFiles) != 0 {
		t.Errorf("newFiles: want 0, got %d", len(newFiles))
	}
	if len(modified) != 0 {
		t.Errorf("modified: want 0, got %d (mtime optimisation should skip checksum)", len(modified))
	}
	if len(deleted) != 0 {
		t.Errorf("deleted: want 0, got %d", len(deleted))
	}
}

func TestManifestCompare_MtimeOptimization_SizeChanged(t *testing.T) {
	// Same mtime but different size → must fall back to checksum comparison.
	ts := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)

	m := NewManifest()
	m.Entries["/sized.txt"] = ManifestEntry{
		Path:     "/sized.txt",
		Size:     100,
		ModTime:  ts,
		Checksum: "old",
	}

	remote := []ManifestEntry{
		{Path: "/sized.txt", Size: 200, ModTime: ts, Checksum: "new"},
	}

	_, modified, _ := m.Compare(remote)

	if len(modified) != 1 {
		t.Errorf("modified: want 1 (size changed), got %d", len(modified))
	}
}
