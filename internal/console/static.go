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

		rel := strings.TrimPrefix(r.URL.Path, "/")
		if rel == "" || rel == "." {
			http.ServeFile(w, r, indexPath)
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
			http.ServeFile(w, r, indexPath)
			return
		}

		http.ServeFile(w, r, full)
	})
}
