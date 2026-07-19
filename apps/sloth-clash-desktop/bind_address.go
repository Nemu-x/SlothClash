package main

import (
	"net"
	"strconv"
	"strings"
)

// isLoopbackBindAddress reports whether a mihomo `bind-address` value keeps the
// listener on the local machine only.
//
// This matters when the user enables `allow-lan`: a loopback bind-address
// inherited from the subscription silently defeats it — the core reports
// allow-lan but LAN clients still cannot connect (clash-verge-rev hit the same
// bug). Callers rewrite such values to "*".
//
// Beyond the obvious forms, Windows and most resolvers accept the IPv4
// shorthands `127.1` and `127.0.1` — and Go's net.ParseIP rejects those, so
// they are expanded explicitly rather than trusted to the stdlib.
func isLoopbackBindAddress(raw string) bool {
	v := strings.TrimSpace(raw)
	if v == "" {
		// Absent value = mihomo default ("*"), i.e. not loopback-restricted.
		return false
	}
	// "*" / "0.0.0.0" / "::" all mean "every interface".
	switch v {
	case "*", "0.0.0.0", "::", "[::]":
		return false
	}
	if strings.EqualFold(v, "localhost") {
		return true
	}
	// Strip brackets from IPv6 literals ("[::1]").
	v = strings.TrimSuffix(strings.TrimPrefix(v, "["), "]")
	if ip := net.ParseIP(v); ip != nil {
		return ip.IsLoopback()
	}
	return isShorthandIPv4Loopback(v)
}

// isShorthandIPv4Loopback handles the abbreviated dotted forms net.ParseIP
// refuses: "127.1" (= 127.0.0.1) and "127.0.1" (= 127.0.0.1). Anything whose
// first octet is 127 is loopback per RFC 1122, so only that octet is checked —
// after confirming every part is a valid number.
func isShorthandIPv4Loopback(v string) bool {
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 0xFFFFFF {
			return false
		}
	}
	first, err := strconv.Atoi(parts[0])
	return err == nil && first == 127
}
