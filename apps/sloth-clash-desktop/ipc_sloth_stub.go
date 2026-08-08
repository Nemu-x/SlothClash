//go:build !windows && !darwin

package main

import "context"

func windowsEnsureSlothIPCReachable(ctx context.Context) error {
	_ = ctx
	return nil
}

func ipcSlothStartClash(ctx context.Context, p slothIPCStartParams) error {
	_ = ctx
	_ = p
	return nil
}

func ipcSlothStopCore(ctx context.Context) error {
	_ = ctx
	return nil
}

func ipcSlothRemoveTun(ctx context.Context) (int, error) {
	_ = ctx
	return 0, nil
}

// Corp-VPN sidecar is macOS-only in P1; these should never be reached because
// openconnectComponentSpec reports unsupported first, but keep them total.
func ipcSlothStartCorpVpn(ctx context.Context, payload []byte) (int, []byte, error) {
	_, _ = ctx, payload
	return 0, nil, errCorpVpnUnsupported
}

func ipcSlothStopCorpVpn(ctx context.Context) (int, []byte, error) {
	_ = ctx
	return 0, nil, errCorpVpnUnsupported
}

// ipcSlothEnsureCorpDriver: no TAP driver on these platforms (never reached — the
// caller gates on the Windows-only component spec). Present for compilation.
func ipcSlothEnsureCorpDriver(_ context.Context, _ string) error { return nil }

func ipcSlothCorpVpnStatus(ctx context.Context) (int, []byte, error) {
	_ = ctx
	return 0, nil, errCorpVpnUnsupported
}
