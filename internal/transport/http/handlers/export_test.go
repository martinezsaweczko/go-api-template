package handlers

import "testing"

// SetMarshalJSON replaces the JSON marshaller used by the handlers until the
// test ends, so tests can force encoding failures.
func SetMarshalJSON(t testing.TB, fn func(any) ([]byte, error)) {
	t.Helper()
	old := marshalJSON
	marshalJSON = fn
	t.Cleanup(func() { marshalJSON = old })
}
