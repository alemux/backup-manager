// internal/discovery/ftp_folders.go
package discovery

import (
	"github.com/backupmanager/backupmanager/internal/connector"
)

// DetectNewFTPFolders compares the current FTP directory listing with configured
// backup source paths. It returns folder paths that exist on the FTP server but
// have no matching backup source. Only directories are considered.
//
// If any source has source_path="/", this is "copy all" mode and no new folders
// are reported (everything is already covered).
func DetectNewFTPFolders(ftpEntries []connector.FileInfo, existingSourcePaths []string) []string {
	// Check for copy-all mode
	for _, sp := range existingSourcePaths {
		if sp == "/" {
			return nil
		}
	}

	sourceSet := make(map[string]bool, len(existingSourcePaths))
	for _, sp := range existingSourcePaths {
		sourceSet[sp] = true
	}

	var newFolders []string
	for _, entry := range ftpEntries {
		if entry.IsDir && !sourceSet[entry.Path] {
			newFolders = append(newFolders, entry.Path)
		}
	}
	return newFolders
}
