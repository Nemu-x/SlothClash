//go:build !windows && !darwin

package main

func (a *App) applyWindowsSystemProxyIfNeededLocked() error {
	return nil
}

func (a *App) applyWindowsSystemProxyFromSnapshot() error {
	return nil
}

func (a *App) clearWindowsSystemProxyFromSnapshot() error {
	return nil
}

func (a *App) clearWindowsSystemProxyLocked() {
	a.systemProxyLeased = false
}
