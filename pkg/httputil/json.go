package httputil

import (
	"encoding/json"
	"fmt"
	"io"
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
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		return err
	}

	var extra interface{}
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain a single JSON value")
		}
		return fmt.Errorf("request body must contain a single JSON value: %w", err)
	}
	return nil
}
