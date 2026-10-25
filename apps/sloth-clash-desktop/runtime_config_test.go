package main

import "SlothClashDesktop/internal/runtimecfg"

// finalizeRuntimeConfigPipeline runs runtimecfg.Finalize with no script and
// the env captured from the live (test-controlled) desktop prefs, so tests here
// assert the prefs -> generated config chain end to end.
func finalizeRuntimeConfigPipeline(
	m map[string]any,
	dataDir string,
	mixedPort, ctrlPort int,
	secret, traffic string,
	withExternalController bool,
	enableTun bool,
) error {
	return runtimecfg.Finalize(m, runtimecfg.Input{
		DataDir:                dataDir,
		CtrlPort:               ctrlPort,
		MixedPort:              mixedPort,
		Secret:                 secret,
		WithExternalController: withExternalController,
		Traffic:                traffic,
		EnableTun:              enableTun,
		Env:                    currentRuntimeConfigEnv(),
	}, nil)
}
