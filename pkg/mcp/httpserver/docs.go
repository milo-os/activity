package httpserver

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"go.miloapis.com/activity/pkg/mcp/agentdocs"
)

const (
	// knowledgePath serves the knowledge document.
	knowledgePath = "/llms-full.txt"

	// runbookPrefix serves the skills. The URL says "runbooks" while the
	// embedded directory says "skills", matching the URL shape Patch's
	// capability documents use for every service.
	runbookPrefix = "/runbooks/"

	textContentType     = "text/plain; charset=utf-8"
	markdownContentType = "text/markdown; charset=utf-8"
)

// document is one static file, read out of the embedded FS at startup.
type document struct {
	body        []byte
	contentType string
}

// knowledgeHandler serves the knowledge document and skills over plain HTTP.
//
// Routing is an exact-match lookup in a table built at startup, never a path
// join against a directory, so "..", an absolute path or an escaped separator
// has nothing to traverse to.
//
// These documents carry no auth check: the assistant fetches them before it
// holds any project context, and they are static text with no tenant data.
// /mcp is the only credential-bearing surface.
type knowledgeHandler struct {
	docs map[string]document
}

// newKnowledgeHandler reads every embedded document into memory. A failure is
// a build problem, so the server refuses to start rather than serve 404s.
func newKnowledgeHandler() (*knowledgeHandler, error) {
	docs := make(map[string]document)

	knowledge, err := agentdocs.FS.ReadFile(agentdocs.KnowledgeFile)
	if err != nil {
		return nil, fmt.Errorf("reading embedded knowledge: %w", err)
	}
	docs[knowledgePath] = document{body: knowledge, contentType: textContentType}

	entries, err := fs.ReadDir(agentdocs.FS, agentdocs.SkillsDir)
	if err != nil {
		return nil, fmt.Errorf("reading embedded skills: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		body, err := agentdocs.FS.ReadFile(path.Join(agentdocs.SkillsDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading embedded skill %s: %w", entry.Name(), err)
		}
		docs[runbookPrefix+entry.Name()] = document{body: body, contentType: markdownContentType}
	}

	if len(docs) < 2 {
		return nil, fmt.Errorf("no skills embedded from %s", agentdocs.SkillsDir)
	}
	return &knowledgeHandler{docs: docs}, nil
}

func (h *knowledgeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	doc, ok := h.docs[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", doc.contentType)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(doc.body))
}

// paths returns the served URL paths, sorted.
func (h *knowledgeHandler) paths() []string {
	out := make([]string, 0, len(h.docs))
	for p := range h.docs {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}
