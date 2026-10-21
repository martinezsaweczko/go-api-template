package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// marshalJSON encodes response bodies. It is a variable so tests can force
// encoding failures.
var marshalJSON = json.Marshal

// internalErrorBody is the pre-encoded model.HTTPError sent when a response
// body cannot be encoded.
const internalErrorBody = `{"message":"internal server error"}` + "\n"

// writeJSON writes v as a JSON response with the given status code. The body
// is encoded before anything is written, so an encoding failure still sends a
// clean 500 instead of a 200 with a truncated body. It returns the encoding
// or write error for logging; nothing more can be sent once writing started.
func writeJSON(w http.ResponseWriter, status int, v any) error {
	body, err := marshalJSON(v)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(internalErrorBody))
		return fmt.Errorf("encode response: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}
