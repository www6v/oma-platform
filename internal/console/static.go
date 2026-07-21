package console

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// NewStaticHandler serves a Vite/React build with index.html SPA fallback.
func NewStaticHandler(root string) http.Handler {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = root
	}
	indexPath := filepath.Join(absRoot, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		serveIndex := func() {
			// Always revalidate the SPA shell so deploys/rebuilds are not
			// stuck behind a cached index.html pointing at stale hashed assets.
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, indexPath)
		}

		rel := strings.TrimPrefix(r.URL.Path, "/")
		if rel == "" || rel == "." {
			serveIndex()
			return
		}

		clean := filepath.Clean(rel)
		if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
			http.NotFound(w, r)
			return
		}

		full := filepath.Join(absRoot, clean)
		if !strings.HasPrefix(full, absRoot+string(os.PathSeparator)) && full != absRoot {
			http.NotFound(w, r)
			return
		}

		info, err := os.Stat(full)
		if err != nil || info.IsDir() {
			serveIndex()
			return
		}

		// Content-hashed Vite assets can be cached aggressively; the shell
		// (index.html) above stays no-cache so clients discover new hashes.
		if strings.HasPrefix(clean, "assets"+string(os.PathSeparator)) ||
			strings.HasPrefix(clean, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.ServeFile(w, r, full)
	})
}
