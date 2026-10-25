package runtimecfg

import "runtime"

// testPrefs are the prefs of a never-touched install, as main resolves them:
// IPv6 on, smart DNS and LAN off, desync off with its default routes.
func testPrefs() Prefs {
	return Prefs{
		DNSIPv6: true,
		Desync:  DesyncSettings{YouTube: true, Discord: runtime.GOOS == "windows"},
	}
}

// testEnv is a generation env with testPrefs, no corp-VPN split, an anonymous
// subscription client and no desync network key.
func testEnv() Env {
	return Env{Prefs: testPrefs()}
}

// finalizeRuntimeConfigPipeline runs the finalize stages with testEnv and no
// script — the form most pipeline tests assert against.
func finalizeRuntimeConfigPipeline(
	m map[string]any,
	dataDir string,
	mixedPort, ctrlPort int,
	secret, traffic string,
	withExternalController bool,
	enableTun bool,
) error {
	return finalizeRuntimeConfigPipelineWithScript(m, dataDir, mixedPort, ctrlPort, secret, traffic, withExternalController, enableTun, "", nil, testEnv())
}
