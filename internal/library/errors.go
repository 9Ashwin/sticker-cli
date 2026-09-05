package library

import "fmt"

// Error is a stable, machine-readable error returned by the library layer.
// Callers should branch on Kind and Subtype rather than Message.
type Error struct {
	Kind      string
	Subtype   string
	Message   string
	Hint      string
	Retryable bool
	Committed bool
	Err       error
}

func (e *Error) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s/%s", e.Kind, e.Subtype)
}

func (e *Error) Unwrap() error { return e.Err }

func errorf(kind, subtype, hint, format string, args ...any) *Error {
	return &Error{Kind: kind, Subtype: subtype, Hint: hint, Message: fmt.Sprintf(format, args...)}
}

func wrapError(kind, subtype, hint string, err error) *Error {
	return &Error{Kind: kind, Subtype: subtype, Hint: hint, Message: err.Error(), Err: err}
}
