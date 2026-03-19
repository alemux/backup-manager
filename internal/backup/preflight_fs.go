package backup

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// mkdirAll creates all directories in the path.
func mkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

// joinPath joins path elements.
func joinPath(elem ...string) string {
	return filepath.Join(elem...)
}

// copyFile copies src to dst, creating dst if it does not exist.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// pruneOldDBBackups removes the oldest datestamped DB backup files from dir,
// keeping only the last `keep` files. Files must match the pattern
// backupmanager_YYYY-MM-DD.db.
func pruneOldDBBackups(dir string, keep int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var backups []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "backupmanager_") && strings.HasSuffix(name, ".db") {
			backups = append(backups, filepath.Join(dir, name))
		}
	}

	// Sort ascending by path (dates in filenames sort lexicographically).
	sort.Strings(backups)

	// Delete oldest entries beyond keep limit.
	if len(backups) > keep {
		toDelete := backups[:len(backups)-keep]
		for _, path := range toDelete {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}

	return nil
}
