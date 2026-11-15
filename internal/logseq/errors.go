package logseq

import "fmt"

// APIError is an unsuccessful HTTP response, with its status separate from the diagnostic text.
type APIError struct {
	StatusCode int
	Method     string
	Status     string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("logseq api: %s: HTTP %s: %s", e.Method, e.Status, e.Message)
}
