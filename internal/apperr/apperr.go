package apperr

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeInvalid     Code = "invalid_argument"
	CodeNotFound    Code = "not_found"
	CodeConflict    Code = "conflict"
	CodeUnavailable Code = "unavailable"
	CodeInternal    Code = "internal"
)

type Error struct {
	Code Code
	Msg  string
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Msg
	}
	return fmt.Sprintf("%s: %v", e.Msg, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func New(code Code, msg string) error {
	return &Error{Code: code, Msg: msg}
}

func Wrap(code Code, msg string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: code, Msg: msg, Err: err}
}

func CodeOf(err error) Code {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Code
	}
	return CodeInternal
}

func MessageOf(err error) string {
	var appErr *Error
	if errors.As(err, &appErr) && appErr.Msg != "" {
		return appErr.Msg
	}
	return "internal error"
}
