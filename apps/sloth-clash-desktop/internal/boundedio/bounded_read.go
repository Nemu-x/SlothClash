// Package boundedio reads whole bodies under a size cap, refusing an
// oversized one with a LimitError instead of silently truncating it.
package boundedio

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Bounded reads for bodies the app parses (controller JSON, subscriptions,
// GitHub API, service IPC). The former io.ReadAll(io.LimitReader(r, n))
// pattern silently truncated anything past n: JSON then failed with
// "unexpected end of JSON input", and a subscription YAML could even stay
// valid while missing nodes. Read reads limit+1 bytes instead and
// refuses an oversized body with an explicit, user-visible error.

// LimitError reports a body larger than its cap. It is a distinct type so
// callers that normally swallow fetch errors can still surface this one: it
// is deterministic (retrying fetches the same oversized body) and actionable.
type LimitError struct {
	What  string
	Limit int64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("%s response exceeds the %s limit", e.What, FormatLimit(e.Limit))
}

// IsLimitError reports whether err (or anything it wraps) is a LimitError.
func IsLimitError(err error) bool {
	var limitErr *LimitError
	return errors.As(err, &limitErr)
}

// Read reads a whole body, refusing (rather than truncating) one larger
// than limit. what names the source in the error, e.g. "GET /connections".
func Read(reader io.Reader, what string, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, &LimitError{What: what, Limit: limit}
	}
	return body, nil
}

// DecodeJSON is Read followed by json.Unmarshal into dest.
func DecodeJSON(reader io.Reader, what string, limit int64, dest any) error {
	body, err := Read(reader, what, limit)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dest)
}

// FormatLimit renders a byte cap for messages ("32 MiB", "4 KiB", "4 bytes").
func FormatLimit(limit int64) string {
	switch {
	case limit >= 1<<20 && limit%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", limit>>20)
	case limit >= 1<<10 && limit%(1<<10) == 0:
		return fmt.Sprintf("%d KiB", limit>>10)
	}
	return fmt.Sprintf("%d bytes", limit)
}
