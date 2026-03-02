package cooked

import (
	"encoding/json"
	"log"
	"net/http"
)

// Response represents an HTTP response to be written.
type Response struct {
	StatusCode int
	Body       any
	Headers    map[string]string
}

// Header returns a copy of the response with an additional header set.
func (r Response) Header(key, value string) Response {
	if r.Headers == nil {
		r.Headers = make(map[string]string)
	}
	r.Headers[key] = value
	return r
}

// Write serializes the response to an http.ResponseWriter.
func (r Response) Write(w http.ResponseWriter) {
	for k, v := range r.Headers {
		w.Header().Set(k, v)
	}

	if r.Body == nil {
		if r.StatusCode == 0 {
			r.StatusCode = http.StatusNoContent
		}
		w.WriteHeader(r.StatusCode)
		return
	}

	if r.StatusCode == 0 {
		r.StatusCode = http.StatusOK
	}

	data, err := json.Marshal(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		if _, writeErr := w.Write([]byte(`{"error":"internal server error"}`)); writeErr != nil {
			log.Printf("pickle: failed to write error response: %v", writeErr)
		}
		return
	}

	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(r.StatusCode)
	if _, err := w.Write(data); err != nil {
		log.Printf("pickle: failed to write response: %v", err)
	}
}
