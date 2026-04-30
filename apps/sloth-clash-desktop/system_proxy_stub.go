//go:build !windows && !darwin

package main

func (a *App) applySystemProxyIfNeededLocked() error {
	return nil
}

func (a *App) applySystemProxyFromSnapshot() error {
	return nil
}

func (a *App) clearSystemProxyFromSnapshot() error {
	return nil
}

func (a *App) clearSystemProxyLocked() {
	a.systemProxyLeased = false
}

func (a *App) maybeWindowsSysProxyReconcile() {}

func (a *App) handleMixedPortChangeForWindowsSysProxy(prevPort, newPort int) {
	_, _ = prevPort, newPort
}
