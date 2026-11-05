package graphapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

// maxJSONBody is the largest JSON request body, in bytes.
const maxJSONBody = 1 << 20

// decodeJSON reads one JSON object of at most maxJSONBody bytes into v.
// Unknown fields are ignored, as Graph does. It returns the error to answer
// with, or nil.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) *apiError {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return newError(http.StatusUnsupportedMediaType, codeUnsupported, "content type must be application/json")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	if err := dec.Decode(v); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return newError(http.StatusRequestEntityTooLarge, codeTooLarge, "request body too large")
		}
		return badRequest("request body must be a JSON object")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return newError(http.StatusRequestEntityTooLarge, codeTooLarge, "request body too large")
			}
		}
		return badRequest("request body must be a single JSON object")
	}
	return nil
}
