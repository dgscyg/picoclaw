package muninndb

import (
	"errors"
	"fmt"
)

var (
	ErrTemporary = errors.New("temporary failure")
	ErrRequest   = errors.New("request failed")
)

// APIError describes a non-success HTTP response from MuninnDB.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("muninndb API error: status %d", e.StatusCode)
	}
	return fmt.Sprintf("muninndb API error: status %d: %s", e.StatusCode, e.Body)
}

func (e *APIError) Temporary() bool {
	return e.StatusCode == 429 || e.StatusCode >= 500
}
