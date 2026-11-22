package keep

import "fmt"

// APIError is an unsuccessful HTTP response, with its status separate
// from the diagnostic text.
type APIError struct {
	StatusCode int
	Op         string
	Status     string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("keep api: %s: HTTP %s: %s", e.Op, e.Status, e.Message)
}
