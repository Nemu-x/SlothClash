//go:build !windows

package main

func takeoverConflictingTunServices() ([]tunServiceTakeover, error) {
	return nil, nil
}

func (a *App) restoreTakenOverTunServicesLocked() {}
