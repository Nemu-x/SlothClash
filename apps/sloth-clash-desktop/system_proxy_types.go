package main

type SystemProxyServiceSnapshot struct {
	WebEnabled    bool
	WebServer     string
	WebPort       int
	SecureEnabled bool
	SecureServer  string
	SecurePort    int
}
