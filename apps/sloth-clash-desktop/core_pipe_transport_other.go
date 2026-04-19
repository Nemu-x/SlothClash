//go:build !windows

package main

import "net/http"

func coreTransportForListen(listen string) http.RoundTripper {
	_ = listen
	return nil
}
