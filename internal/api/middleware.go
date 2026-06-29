package api

import "net/http"

// withCORS wraps a handler so browsers served from a different origin (the Vite
// dev server on :5173) may call this API on :8080. The browser enforces the
// same-origin policy; CORS headers are how a server opts specific cross-origin
// callers back in.
//
// This permits any origin ("*"), which is fine here because the API has no
// cookies or credentials to protect. A production deployment should echo back
// only an allow-listed set of known origins instead.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		// A browser sends a preflight OPTIONS request before a "non-simple"
		// request (e.g. POST with a JSON Content-Type) to ask whether the real
		// request is allowed. We answer it here and stop — it never needs to
		// reach the route handlers.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
