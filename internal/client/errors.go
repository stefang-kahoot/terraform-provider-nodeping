package client

import (
	"errors"
	"fmt"
	"time"
)

type APIError struct {
	StatusCode int
	Message    string
	RequestID  string
	// RetryAfter is how long a 429 asked to wait, if it said.
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("NodePing API error (status %d, request %s): %s", e.StatusCode, e.RequestID, e.Message)
	}
	return fmt.Sprintf("NodePing API error (status %d): %s", e.StatusCode, e.Message)
}

func (e *APIError) IsNotFound() bool {
	return e.StatusCode == 404
}

func (e *APIError) IsUnauthorized() bool {
	return e.StatusCode == 401 || e.StatusCode == 403
}

func (e *APIError) IsRetryable() bool {
	return e.StatusCode == 429 || e.StatusCode >= 500
}

// UncertainWriteError is a request that adds something and failed after it
// may have reached NodePing: the connection dropped, it timed out, or
// NodePing answered with a server error. NodePing may have carried it out,
// and sending it again could add the same thing twice, so it is not retried.
type UncertainWriteError struct {
	Err error
}

func (e *UncertainWriteError) Error() string {
	return "outcome unknown: " + e.Err.Error()
}

func (e *UncertainWriteError) Unwrap() error {
	return e.Err
}

// createError is the error of a failed create of the kind of thing called
// name. If NodePing may have created it anyway, it says so: the object would
// be in NodePing but not in state, and the next apply would create it again.
func createError(kind, name string, err error) error {
	if _, ok := errors.AsType[*UncertainWriteError](err); ok {
		return fmt.Errorf("failed to create %s %q, but NodePing may have created it: look for it there and import it, or delete it, before applying again: %w", kind, name, err)
	}
	return fmt.Errorf("failed to create %s: %w", kind, err)
}

type NotFoundError struct {
	ResourceType string
	ResourceID   string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s with ID %q not found", e.ResourceType, e.ResourceID)
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("validation error for field %q: %s", e.Field, e.Message)
	}
	return fmt.Sprintf("validation error: %s", e.Message)
}

type RateLimitError struct {
	RetryAfter int
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("rate limit exceeded, retry after %d seconds", e.RetryAfter)
	}
	return "rate limit exceeded"
}
