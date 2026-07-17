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
	Cookies    []*http.Cookie
}

// renderedView is intentionally package-private. Only generated renderers in
// the application's HTTP package can construct an HTML response without JSON
// serialization; controllers cannot bless arbitrary request strings as HTML.
type renderedView string

func renderedViewResponse(_ *Context, body string) Response {
	return Response{
		StatusCode: http.StatusOK,
		Body:       renderedView(body),
		Headers:    map[string]string{"Content-Type": "text/html; charset=utf-8"},
	}
}

// Header returns a copy of the response with an additional header set.
func (r Response) Header(key, value string) Response {
	if r.Headers == nil {
		r.Headers = make(map[string]string)
	}
	r.Headers[key] = value
	return r
}

// WithCookie returns a copy of the response with an additional cookie to set.
func (r Response) WithCookie(c *http.Cookie) Response {
	r.Cookies = append(r.Cookies, c)
	return r
}

// Write serializes the response to an http.ResponseWriter.
func (r Response) Write(w http.ResponseWriter) {
	for _, c := range r.Cookies {
		http.SetCookie(w, c)
	}
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

	if body, ok := r.Body.(renderedView); ok {
		w.WriteHeader(r.StatusCode)
		if _, err := w.Write([]byte(body)); err != nil {
			log.Printf("pickle: failed to write response: %v", err)
		}
		return
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
