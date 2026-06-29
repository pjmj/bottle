package api

import (
	"encoding/json"
	"net/http"
)

// errorResponse is the uniform shape of every error the API returns, so
// clients can always parse {"error": "..."} regardless of which endpoint
// failed.
type errorResponse struct {
	Error string `json:"error"`
}

// decode reads and parses a JSON request body into a value of type T. Using a
// generic helper means every handler decodes the same way — same content
// rules, same error handling — instead of repeating boilerplate.
//
// DisallowUnknownFields makes the API strict: a request with a misspelled or
// unexpected field is rejected rather than silently ignored, which catches
// client bugs early.
func decode[T any](r *http.Request) (T, error) {
	var v T
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, err
	}
	return v, nil
}

// writeJSON serializes v as JSON with the given status code. It sets the
// Content-Type header BEFORE WriteHeader, because once WriteHeader is called
// the status and headers are flushed and can no longer be changed.
func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status/headers are already sent, so we can't change the response
		// now — the best we can do is log that the body write failed.
		s.logger.Error("encode response", "err", err)
	}
}

// writeError is the single funnel for error responses, guaranteeing a
// consistent body shape.
func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, errorResponse{Error: msg})
}
