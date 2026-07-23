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
