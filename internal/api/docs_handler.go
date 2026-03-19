// internal/api/docs_handler.go
package api

import (
	"net/http"
	"strings"

	"github.com/backupmanager/backupmanager/internal/docs"
)

// DocsHandler handles /api/docs/* routes.
type DocsHandler struct{}

// NewDocsHandler constructs a DocsHandler.
func NewDocsHandler() *DocsHandler {
	return &DocsHandler{}
}

// List handles GET /api/docs — returns the list of available docs.
func (h *DocsHandler) List(w http.ResponseWriter, r *http.Request) {
	entries := docs.ListDocs()
	if entries == nil {
		entries = []docs.DocEntry{}
	}
	JSON(w, http.StatusOK, entries)
}

// Get handles GET /api/docs/{slug} — returns the markdown content of a doc.
func (h *DocsHandler) Get(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		Error(w, http.StatusBadRequest, "slug is required")
		return
	}

	content, err := docs.GetDoc(slug)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			Error(w, http.StatusNotFound, err.Error())
		} else {
			Error(w, http.StatusBadRequest, err.Error())
		}
		return
	}

	JSON(w, http.StatusOK, map[string]string{"content": content})
}

// Search handles GET /api/docs/search?q=... — searches docs.
func (h *DocsHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	results := docs.SearchDocs(q)
	if results == nil {
		results = []docs.DocEntry{}
	}
	JSON(w, http.StatusOK, results)
}
