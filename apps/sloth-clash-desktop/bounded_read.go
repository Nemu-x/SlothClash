package main

import (
	"io"

	"SlothClashDesktop/internal/boundedio"
)

// Bounded reads for bodies the app parses live in internal/boundedio (shared
// with the config generation in internal/runtimecfg); these are main's names.

// readBounded is boundedio.Read: it refuses, rather than truncates, a body
// larger than limit.
func readBounded(reader io.Reader, what string, limit int64) ([]byte, error) {
	return boundedio.Read(reader, what, limit)
}

// decodeBoundedJSON is boundedio.DecodeJSON.
func decodeBoundedJSON(reader io.Reader, what string, limit int64, dest any) error {
	return boundedio.DecodeJSON(reader, what, limit, dest)
}

// isReadLimitError reports a boundedio.LimitError anywhere in err's chain.
func isReadLimitError(err error) bool {
	return boundedio.IsLimitError(err)
}
