package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Size caps for JSON read from the mihomo controller. They exist only to bound
// memory if the core misbehaves; a cap that a healthy core can exceed is a bug,
// because the old io.ReadAll(io.LimitReader(...)) pattern silently truncated
// the body and the user saw "unexpected end of JSON input" over an empty page.
//
// Sizes measured with the mihomo-shaped fixtures in core_json_fixture_test.go
// (every field the core emits, long names, full 10-entry delay history):
//   - /connections: ~920 B per connection; 20k connections = 17.6 MiB.
//     32 MiB leaves room for ~36k connections (TUN with a busy browser plus
//     torrent/game clients).
//   - /proxies: ~2.5 KB per proxy (history + two extra test URLs) plus every
//     group listing all nodes; 10k proxies = 23.6 MiB. 48 MiB leaves room for
//     ~20k proxies.
//   - Anything else (/proxies/<name>, /configs, /version, desync endpoints):
//     the largest is a single group detail whose "all" lists every node, about
//     45 B per member, so 4 MiB covers groups with ~90k members.
const (
	coreConnectionsMaxJSONBytes = 32 << 20
	coreProxiesMaxJSONBytes     = 48 << 20
	coreDefaultMaxJSONBytes     = 4 << 20
	// HTTP error bodies are only quoted in messages; truncating them is fine.
	coreErrorBodyMaxBytes = 4096
)

// coreJSONLimitForRoute picks the cap for a controller route (query ignored).
func coreJSONLimitForRoute(route string) int64 {
	path, _, _ := strings.Cut(route, "?")
	switch strings.TrimRight(path, "/") {
	case "/connections":
		return coreConnectionsMaxJSONBytes
	case "/proxies":
		return coreProxiesMaxJSONBytes
	}
	return coreDefaultMaxJSONBytes
}

// readCoreJSON reads a whole controller response body, refusing (rather than
// truncating) one larger than limit. what names the endpoint in the error,
// e.g. "GET /connections".
func readCoreJSON(reader io.Reader, what string, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s response exceeds the %s limit", what, formatByteLimit(limit))
	}
	return body, nil
}

// decodeCoreJSON is readCoreJSON followed by json.Unmarshal into dest.
func decodeCoreJSON(reader io.Reader, what string, limit int64, dest any) error {
	body, err := readCoreJSON(reader, what, limit)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, dest)
}

// readCoreErrorBody returns the (possibly truncated) text of a non-2xx body.
func readCoreErrorBody(reader io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(reader, coreErrorBodyMaxBytes))
	return strings.TrimSpace(string(b))
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
