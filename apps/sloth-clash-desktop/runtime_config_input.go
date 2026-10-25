package main

// runtimeConfigInput is everything one runtime config generation consumes:
// where to write, what to build it from, and the core endpoint it must carry.
type runtimeConfigInput struct {
	// DataDir is the profile's runtime directory; config.yaml, the subscription
	// body cache and the providers/ruleset dirs live in it.
	DataDir string
	SubURL  string
	AgeKey  string

	// The profile's declarative merge templates and JavaScript override.
	ExtendTemplate string
	ProxyTemplate  string
	RulesTemplate  string
	Script         string

	// The local endpoint the app reaches its core on. CtrlPort is only written
	// when WithExternalController is set.
	CtrlPort               int
	MixedPort              int
	Secret                 string
	WithExternalController bool

	// Traffic is the traffic mode ("proxy" / "tun"); EnableTun is the effective
	// tun.enable (connected && traffic == "tun").
	Traffic   string
	EnableTun bool

	// Prefs is the desktop prefs snapshot the caller captured for this
	// generation (currentRuntimeConfigPrefs()).
	Prefs runtimeConfigPrefs
}
