package api

import (
	"net/http"
	"os"
	"path/filepath"
)

// spaHandler serves static files from dir, falling back to index.html for any
// path that isn't a real file. That fallback is what makes a single-page app
// work: deep links like /jobs/abc return index.html so the client router can
// take over, rather than a 404.
//
// Path traversal is not a concern here: r.URL.Path is rooted at "/", so
// filepath.Clean collapses any "../" back to the root before we Join with dir,
// and http.FileServer independently rejects escapes.
func spaHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Join(dir, filepath.Clean(r.URL.Path))
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r) // real asset (JS, CSS, etc.)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, "index.html"))
	})
}
