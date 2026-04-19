//go:build windows

package main

import (
	"context"
	"net"
	"net/http"

	"github.com/Microsoft/go-winio"
)

func coreTransportForListen(listen string) http.RoundTripper {
	if !isWinPipeEndpoint(listen) {
		return nil
	}
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return winio.DialPipeContext(ctx, listen)
		},
		DisableKeepAlives: true,
	}
}
