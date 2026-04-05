package httputil

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes a JSON response with the given status code.
func WriteJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// MaxBodySize is the default maximum request body size (1 MB).
const MaxBodySize = 1 << 20

// ReadJSON decodes JSON from the request body into v.
// It limits the body to MaxBodySize bytes to prevent abuse.
func ReadJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(nil, r.Body, MaxBodySize)
	return json.NewDecoder(r.Body).Decode(v)
}
