package packs

import "fmt"

// Error is the stable failure contract for source discovery.
// Callers should branch on Kind and Subtype rather than Message.
type Error struct {
	Kind      string
	Subtype   string
	Message   string
	Hint      string
	Retryable bool
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

func newError(kind, subtype, message, hint string) *Error {
	return &Error{Kind: kind, Subtype: subtype, Message: message, Hint: hint}
}

func wrapError(kind, subtype, message, hint string, err error) *Error {
	return &Error{Kind: kind, Subtype: subtype, Message: message, Hint: hint, Err: err}
}
