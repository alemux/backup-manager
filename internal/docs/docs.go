// Package docs provides embedded markdown documentation files and helpers
// to list, retrieve, and search them.
package docs

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"
)

//go:embed content/*.md
var DocsFS embed.FS

// DocEntry is the metadata for a single documentation page.
type DocEntry struct {
	Slug     string `json:"slug"`
	Title    string `json:"title"`
	Category string `json:"category"`
}

// docOrder defines the display order of slugs. Unknown slugs are appended last.
var docOrder = []string{
	"getting-started",
	"servers",
	"backups",
	"scheduling",
	"recovery",
	"notifications",
	"faq",
}

// parseMeta reads the <!-- title: ... --> and <!-- category: ... --> comments
// from the beginning of a markdown file and returns their values.
func parseMeta(content string) (title, category string) {
	for _, line := range strings.SplitN(content, "\n", 10) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "<!-- title:") {
			title = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "<!-- title:"), "-->"))
		}
		if strings.HasPrefix(line, "<!-- category:") {
			category = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "<!-- category:"), "-->"))
		}
	}
	return
}

// slugFromFilename converts "content/getting-started.md" → "getting-started".
func slugFromFilename(name string) string {
	name = strings.TrimPrefix(name, "content/")
	name = strings.TrimSuffix(name, ".md")
	return name
}

// listAll reads the embedded FS and returns all entries in docOrder order.
func listAll() []DocEntry {
	entries := make(map[string]DocEntry)

	_ = fs.WalkDir(DocsFS, "content", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		data, readErr := DocsFS.ReadFile(path)
		if readErr != nil {
			return nil
		}
		slug := slugFromFilename(path)
		title, category := parseMeta(string(data))
		if title == "" {
			title = slug
		}
		if category == "" {
			category = "General"
		}
		entries[slug] = DocEntry{Slug: slug, Title: title, Category: category}
		return nil
	})

	// Return in docOrder; append anything not in the order list at the end.
	seen := map[string]bool{}
	result := make([]DocEntry, 0, len(entries))
	for _, slug := range docOrder {
		if e, ok := entries[slug]; ok {
			result = append(result, e)
			seen[slug] = true
		}
	}
	for slug, e := range entries {
		if !seen[slug] {
			result = append(result, e)
		}
	}
	return result
}

// ListDocs returns metadata for all available documentation pages.
func ListDocs() []DocEntry {
	return listAll()
}

// GetDoc returns the raw markdown content for the given slug.
// Returns an error if the slug is not found.
func GetDoc(slug string) (string, error) {
	// Basic sanitisation: slugs must be alphanumeric + hyphens only.
	for _, c := range slug {
		if !(c >= 'a' && c <= 'z') && !(c >= '0' && c <= '9') && c != '-' {
			return "", fmt.Errorf("invalid slug: %q", slug)
		}
	}
	path := "content/" + slug + ".md"
	data, err := DocsFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("doc not found: %q", slug)
	}
	return string(data), nil
}

// SearchDocs performs a case-insensitive substring search across titles and
// the full content of each document. It returns matching DocEntry values.
func SearchDocs(query string) []DocEntry {
	if query == "" {
		return listAll()
	}
	lower := strings.ToLower(query)
	all := listAll()
	var matches []DocEntry
	for _, entry := range all {
		// Check title match first.
		if strings.Contains(strings.ToLower(entry.Title), lower) ||
			strings.Contains(strings.ToLower(entry.Category), lower) {
			matches = append(matches, entry)
			continue
		}
		// Check full content match.
		content, err := GetDoc(entry.Slug)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(content), lower) {
			matches = append(matches, entry)
		}
	}
	return matches
}
