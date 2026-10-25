package main

import (
	"encoding/json"
	"fmt"
	"io"
)

const rulesOverviewMaxJSONBytes = 8 * 1024 * 1024

// Rules responses are parsed by the frontend. Truncating their text corrupts
// JSON and turns a connected, populated core into an apparently empty table.
func readRulesOverviewJSON(reader io.Reader) ([]byte, error) {
	body, err := readBounded(reader, "rules", rulesOverviewMaxJSONBytes)
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("core returned invalid rules JSON")
	}
	return body, nil
}
