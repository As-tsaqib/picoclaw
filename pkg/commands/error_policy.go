package commands

import (
	"errors"
	"fmt"
	"strings"
)

// UserError marks an error message as safe to render to the requesting user.
// Infrastructure and unexpected errors must remain ordinary errors so the
// command boundary can fail closed without exposing implementation details.
type UserError struct {
	message string
}

func (e UserError) Error() string { return e.message }

// NewUserError returns an explicitly user-safe domain error.
func NewUserError(message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "The request could not be completed."
	}
	return UserError{message: message}
}

// UserErrorf formats an explicitly user-safe domain error.
func UserErrorf(format string, args ...any) error {
	return NewUserError(fmt.Sprintf(format, args...))
}

// UserFacingError returns an explicitly safe domain error when one is present;
// all other errors are mapped to a fixed, non-sensitive fallback.
func UserFacingError(err error, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		fallback = "The request could not be completed. Please try again."
	}
	if err == nil {
		return fallback
	}
	var safe UserError
	if errors.As(err, &safe) {
		if message := strings.TrimSpace(safe.Error()); message != "" {
			return message
		}
	}
	return fallback
}
