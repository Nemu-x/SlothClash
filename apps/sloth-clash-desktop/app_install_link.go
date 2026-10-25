package main

import (
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/options"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// OnSecondInstance is wired from main.go when SingleInstanceLock fires (e.g. slothclash:// opened while running).
func (a *App) OnSecondInstance(data options.SecondInstanceData) {
	a.tryInstallConfigFromArgs(data.Args)
	if a.ctx != nil {
		wailsrt.WindowShow(a.ctx)
		wailsrt.WindowUnminimise(a.ctx)
	}
}

func (a *App) tryInstallConfigFromArgs(args []string) {
	raw := findSlothclashInstallConfigURL(args)
	if raw == "" {
		return
	}
	go a.handleInstallConfigURL(raw)
}

// handleInstallConfigURL validates a slothclash:// deep link and ASKS the user
// before importing. A page the user merely visited can trigger the registered
// scheme, so a silent import would let any site plant a subscription profile.
// The actual import happens in ConfirmInstallConfigFromLink once the user accepts.
func (a *App) handleInstallConfigURL(raw string) {
	name, subURL, err := ParseInstallConfigURL(raw)
	if err != nil {
		a.emitInstallConfigResult(false, err.Error(), "", "")
		return
	}
	norm, err := normalizeSubscriptionURL(subURL)
	if err != nil {
		a.emitInstallConfigResult(false, err.Error(), "", "")
		return
	}
	// Untrusted origin: never let a link aim us at the LAN / cloud metadata.
	if err := validateUntrustedSubscriptionURL(norm); err != nil {
		a.emitInstallConfigResult(false, err.Error(), "", "")
		return
	}
	a.emitInstallConfigRequest(name, norm)
}

// ConfirmInstallConfigFromLink performs the import a deep link asked for, after
// the user accepted the confirmation dialog. It re-validates the URL: the
// frontend must not be able to widen what the link was allowed to reach.
func (a *App) ConfirmInstallConfigFromLink(name string, subscriptionURL string) (AppState, error) {
	norm, err := normalizeSubscriptionURL(subscriptionURL)
	if err != nil {
		a.emitInstallConfigResult(false, err.Error(), "", "")
		return a.GetAppState(), err
	}
	if err := validateUntrustedSubscriptionURL(norm); err != nil {
		a.emitInstallConfigResult(false, err.Error(), "", "")
		return a.GetAppState(), err
	}
	st, err := a.ImportProfileFromURL(name, norm)
	if err != nil {
		a.emitInstallConfigResult(false, err.Error(), "", "")
		return st, err
	}
	a.emitAppStateChanged()
	pid := strings.TrimSpace(st.Profile.ActiveProfileID)
	var pname string
	for _, p := range st.Profile.Profiles {
		if p.ID == pid {
			pname = p.Name
			break
		}
	}
	a.emitInstallConfigResult(true, "Subscription added", pid, pname)
	return st, nil
}

// emitInstallConfigRequest asks the UI to confirm a deep-link subscription import.
func (a *App) emitInstallConfigRequest(name, subscriptionURL string) {
	if a.ctx == nil {
		return
	}
	payload := map[string]any{
		"name": name,
		"url":  subscriptionURL,
	}
	if u, err := url.Parse(subscriptionURL); err == nil {
		payload["host"] = u.Hostname()
	}
	go wailsrt.EventsEmit(a.ctx, "app:install-config-request", payload)
}

func (a *App) emitInstallConfigResult(success bool, message, profileID, profileName string) {
	if a.ctx == nil {
		return
	}
	payload := map[string]any{
		"success": success,
		"message": message,
	}
	if profileID != "" {
		payload["profileId"] = profileID
	}
	if profileName != "" {
		payload["profileName"] = profileName
	}
	go wailsrt.EventsEmit(a.ctx, "app:install-config", payload)
}
