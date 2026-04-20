//go:build !windows

package main

import (
	"context"
	"net"
	"net/http"
)

func coreTransportForListen(listen string) http.RoundTripper {
	if !isUnixSocketEndpoint(listen) {
		return nil
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", listen)
		},
		DisableKeepAlives: true,
	}
}
