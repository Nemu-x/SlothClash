package main

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
// valid while missing nodes. readBounded reads limit+1 bytes instead and
// refuses an oversized body with an explicit, user-visible error.

// readLimitError reports a body larger than its cap. It is a distinct type so
// callers that normally swallow fetch errors can still surface this one: it
// is deterministic (retrying fetches the same oversized body) and actionable.
type readLimitError struct {
	What  string
	Limit int64
}

func (e *readLimitError) Error() string {
	return fmt.Sprintf("%s response exceeds the %s limit", e.What, formatByteLimit(e.Limit))
}

func isReadLimitError(err error) bool {
	var limitErr *readLimitError
	return errors.As(err, &limitErr)
}

// readBounded reads a whole body, refusing (rather than truncating) one larger
// than limit. what names the source in the error, e.g. "GET /connections".
func readBounded(reader io.Reader, what string, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, &readLimitError{What: what, Limit: limit}
	}
	return body, nil
}

// decodeBoundedJSON is readBounded followed by json.Unmarshal into dest.
func decodeBoundedJSON(reader io.Reader, what string, limit int64, dest any) error {
	body, err := readBounded(reader, what, limit)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dest)
}

func formatByteLimit(limit int64) string {
	switch {
	case limit >= 1<<20 && limit%(1<<20) == 0:
		return fmt.Sprintf("%d MiB", limit>>20)
	case limit >= 1<<10 && limit%(1<<10) == 0:
		return fmt.Sprintf("%d KiB", limit>>10)
	}
	return fmt.Sprintf("%d bytes", limit)
}
