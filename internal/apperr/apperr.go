// Package apperr defines errors that carry an HTTP status and a message that is
// safe to show to end users. Services return them; the API layer renders them.
package apperr

import (
	"errors"
	"net/http"
)

// Error is a user-facing error with an HTTP status code.
type Error struct {
	Status int
	Msg    string
	Err    error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

// Wrap attaches an underlying cause without changing the user-facing message.
func (e *Error) Wrap(err error) *Error {
	return &Error{Status: e.Status, Msg: e.Msg, Err: err}
}

func newErr(status int, msg string) *Error { return &Error{Status: status, Msg: msg} }

func Invalid(msg string) *Error      { return newErr(http.StatusBadRequest, msg) }
func Unauthorized(msg string) *Error { return newErr(http.StatusUnauthorized, msg) }
func Forbidden(msg string) *Error    { return newErr(http.StatusForbidden, msg) }
func NotFound(msg string) *Error     { return newErr(http.StatusNotFound, msg) }
func Conflict(msg string) *Error     { return newErr(http.StatusConflict, msg) }
func TooMany(msg string) *Error      { return newErr(http.StatusTooManyRequests, msg) }

// Upstream is used when a cloud provider API call fails in a way the user
// should see (bad credentials, missing permission, network problems).
func Upstream(msg string) *Error { return newErr(http.StatusBadGateway, msg) }

// As extracts an *Error from err.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
