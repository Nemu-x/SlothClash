package main

import (
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
//   - Anything else (/proxies/<name>, /configs, desync endpoints):
//     the largest is a single group detail whose "all" lists every node, about
//     45 B per member, so 4 MiB covers groups with ~90k members.
const (
	coreConnectionsMaxJSONBytes = 32 << 20
	coreProxiesMaxJSONBytes     = 48 << 20
	coreDefaultMaxJSONBytes     = 4 << 20
	// Fixed-shape replies: {"version":..,"meta":..} and {"delay":N}.
	coreVersionMaxJSONBytes = 64 << 10
	coreDelayMaxJSONBytes   = 4 << 10
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

// readCoreErrorBody returns the (possibly truncated) text of a non-2xx body.
func readCoreErrorBody(reader io.Reader) string {
	b, _ := io.ReadAll(io.LimitReader(reader, coreErrorBodyMaxBytes))
	return strings.TrimSpace(string(b))
}
