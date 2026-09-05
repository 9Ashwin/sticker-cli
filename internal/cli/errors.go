package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const maxOutputBytes = 256 * 1024

type cliError struct {
	Type      string
	Subtype   string
	Message   string
	Hint      string
	ExitCode  int
	Retryable bool
}

func (e *cliError) Error() string { return e.Message }

func validationError(subtype, message, hint string) error {
	return &cliError{
		Type:      "validation",
		Subtype:   subtype,
		Message:   message,
		Hint:      hint,
		ExitCode:  2,
		Retryable: false,
	}
}

type errorEnvelope struct {
	OK    bool      `json:"ok"`
	Error errorBody `json:"error"`
}

type errorBody struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
	Retryable bool   `json:"retryable"`
}

func writeError(w io.Writer, err error) int {
	if err == nil {
		return 0
	}
	var coded *cliError
	if !errors.As(err, &coded) {
		coded = &cliError{
			Type:     "internal",
			Subtype:  "unexpected",
			Message:  err.Error(),
			ExitCode: 1,
		}
	}
	if coded.ExitCode == 0 {
		coded.ExitCode = 1
	}
	body := errorEnvelope{
		OK: false,
		Error: errorBody{
			Type:      coded.Type,
			Subtype:   coded.Subtype,
			Message:   coded.Message,
			Hint:      coded.Hint,
			Retryable: coded.Retryable,
		},
	}
	encoded, marshalErr := json.Marshal(body)
	if marshalErr != nil || len(encoded)+1 > maxOutputBytes {
		coded = &cliError{
			Type:      "internal",
			Subtype:   "unexpected",
			Message:   "failed to encode error",
			ExitCode:  1,
			Retryable: false,
		}
		body = errorEnvelope{
			OK: false,
			Error: errorBody{
				Type:      coded.Type,
				Subtype:   coded.Subtype,
				Message:   coded.Message,
				Hint:      coded.Hint,
				Retryable: coded.Retryable,
			},
		}
		encoded, _ = json.Marshal(body)
	}
	_, _ = fmt.Fprintf(w, "%s\n", encoded)
	return coded.ExitCode
}
