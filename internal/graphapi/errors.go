package graphapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

// Error codes of the Graph error body.
const (
	codeInvalidRequest  = "invalidRequest"
	codeUnauthenticated = "unauthenticated"
	codeAccessDenied    = "accessDenied"
	codeNotAllowed      = "notAllowed"
	codeNotImplemented  = "notImplemented"
	codeItemNotFound    = "itemNotFound"
	codeUnsupported     = "unsupportedMediaType"
	codeTooLarge        = "requestEntityTooLarge"
	codeNameExists      = "nameAlreadyExists"
	codeGeneral         = "generalException"
)

// apiError is a failed request: the status and the Graph error body to
// answer with.
type apiError struct {
	status  int
	code    string
	message string
	headers map[string]string
}

func newError(status int, code, message string) *apiError {
	return &apiError{status: status, code: code, message: message}
}

func badRequest(message string) *apiError {
	return newError(http.StatusBadRequest, codeInvalidRequest, message)
}

func notFound(message string) *apiError {
	return newError(http.StatusNotFound, codeItemNotFound, message)
}

func forbidden(message string) *apiError {
	return newError(http.StatusForbidden, codeAccessDenied, message)
}

func internalError() *apiError {
	return newError(http.StatusInternalServerError, codeGeneral, "internal error")
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code       string     `json:"code"`
	Message    string     `json:"message"`
	InnerError innerError `json:"innerError"`
}

type innerError struct {
	RequestID string `json:"request-id"`
	Date      string `json:"date"`
}

// writeError answers with the Graph error shape. The request id is also sent
// as the request-id header, as Graph does.
func writeError(w http.ResponseWriter, e *apiError) {
	id := requestID()
	for k, v := range e.headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("request-id", id)
	writeJSON(w, e.status, errorEnvelope{Error: errorBody{
		Code:    e.code,
		Message: e.message,
		InnerError: innerError{
			RequestID: id,
			Date:      time.Now().UTC().Format(time.RFC3339),
		},
	}})
}

func requestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
