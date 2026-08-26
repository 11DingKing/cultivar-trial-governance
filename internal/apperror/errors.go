package apperror

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrValidation      = errors.New("validation failed")
	ErrUnauthenticated = errors.New("authentication required")
	ErrForbidden       = errors.New("operation forbidden")
	ErrNotFound        = errors.New("resource not found")
	ErrConflict        = errors.New("resource conflict")
	ErrStaleVersion    = errors.New("stale resource version")
	ErrCapacity        = errors.New("resource capacity exhausted")
	ErrInvalidState    = errors.New("invalid state transition")
	ErrExpired         = errors.New("resource expired")
	ErrPermanent       = errors.New("permanent worker failure")
)

// Error keeps a stable public code while retaining the internal error chain.
type Error struct {
	Code    string
	Message string
	Op      string
	Entity  string
	ID      string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	text := e.Message
	if text == "" && e.Err != nil {
		text = e.Err.Error()
	}
	if e.Op != "" {
		text = e.Op + ": " + text
	}
	if e.Entity != "" {
		text += fmt.Sprintf(" (%s", e.Entity)
		if e.ID != "" {
			text += " " + e.ID
		}
		text += ")"
	}
	return text
}

func (e *Error) Unwrap() error { return e.Err }

func Wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Op: op, Err: err}
}

func WithEntity(op, entity, id string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Op: op, Entity: entity, ID: id, Err: err}
}

func Public(code, message, op string, err error) error {
	return &Error{Code: code, Message: message, Op: op, Err: err}
}

func Code(err error) string {
	var target *Error
	if errors.As(err, &target) && target.Code != "" {
		return target.Code
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "request_cancelled"
	case errors.Is(err, ErrValidation):
		return "validation_error"
	case errors.Is(err, ErrUnauthenticated):
		return "unauthenticated"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrConflict), errors.Is(err, ErrStaleVersion), errors.Is(err, ErrCapacity), errors.Is(err, ErrInvalidState):
		return "conflict"
	case errors.Is(err, ErrExpired):
		return "expired"
	default:
		return "internal_error"
	}
}
