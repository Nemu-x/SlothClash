package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func (a *App) ListProfiles() []Profile {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.profiles
}

func (a *App) ImportProfileFromURL(name string, rawURL string) (AppState, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return a.GetAppState(), errors.New("subscription url is required")
	}

	norm, err := normalizeSubscriptionURL(rawURL)
	if err != nil {
		return a.GetAppState(), err
	}

	finalName := strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	peek, peekErr := peekSubscription(ctx, norm, currentDesktopPrefs().Privacy.IsHwidEnabled())
	if finalName == "" {
		if peekErr != nil {
			return a.GetAppState(), peekErr
		}
		finalName = strings.TrimSpace(peek.SuggestedName)
		if finalName == "" {
			finalName = "Subscription"
		}
	} else if peekErr != nil {
		peek = SubscriptionPeek{}
	}
	subInfo := strings.TrimSpace(peek.SubscriptionInfo)
	supportURL := strings.TrimSpace(peek.SubscriptionSupportURL)
	announce := strings.TrimSpace(peek.SubscriptionAnnouncement)

	a.mu.Lock()
	defer a.mu.Unlock()

	p := Profile{
		ID:                        "profile-" + time.Now().Format("20060102150405"),
		Name:                      finalName,
		Type:                      "subscription",
		URL:                       norm,
		SubscriptionInfo:          subInfo,
		SubscriptionSupportURL:    supportURL,
		SubscriptionAnnouncement:  announce,
		LastUpdated:               time.Now().Unix(),
		AutoUpdateEnabled:         true,
		AutoUpdateIntervalMinutes: defaultProfileAutoUpdateMinutes,
	}
	a.profiles = append(a.profiles, p)
	a.state.Profile.Profiles = a.profiles
	if a.state.Profile.ActiveProfileID == "" {
		a.state.Profile.ActiveProfileID = p.ID
	}
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		return a.state, err
	}
	return a.state, nil
}

// ImportProfileFromText creates a "local" profile from pasted or file-loaded
// content: a Clash/mihomo YAML config, OR a list of share links (vless://,
// vmess://, ss://, trojan://, hysteria2://, tuic://) — a single link, a
// newline-separated list, or a base64 envelope. The content is validated, then
// seeded into the profile's body cache so the normal pipeline turns it into a
// runnable config on activation. Local profiles carry no URL and are never
// auto-refreshed.
func (a *App) ImportProfileFromText(name string, content string) (AppState, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return a.GetAppState(), errors.New("config content is required")
	}
	// Validate up front: it must parse as a Clash doc, base64-Clash, or a
	// supported share-link list. parseClashDocToMap covers all three.
	doc, skipped, perr := parseClashDocToMapReport([]byte(content))
	if perr != nil || len(doc) == 0 {
		if perr != nil {
			return a.GetAppState(), fmt.Errorf("not a valid config or supported share link: %w", perr)
		}
		return a.GetAppState(), errors.New("not a valid config or supported share link")
	}
	importedCount := 0
	if px, ok := doc["proxies"].([]any); ok {
		importedCount = len(px)
	}

	finalName := strings.TrimSpace(name)
	if finalName == "" {
		finalName = "Local config"
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	id := "profile-" + time.Now().Format("20060102150405")
	root, derr := slothDataRoot()
	if derr != nil {
		return a.state, derr
	}
	// Seed the per-profile body cache (the pipeline's cache-first source). For a
	// local profile this is the durable home of the content — there is no URL to
	// re-fetch from — so ResetSubscriptionCache refuses to wipe local caches.
	writeSubscriptionBodyCache(filepath.Join(root, "runtime", id), []byte(content))

	p := Profile{
		ID:                id,
		Name:              finalName,
		Type:              "local",
		LastUpdated:       time.Now().Unix(),
		AutoUpdateEnabled: false,
	}
	a.profiles = append(a.profiles, p)
	a.state.Profile.Profiles = a.profiles
	if a.state.Profile.ActiveProfileID == "" {
		a.state.Profile.ActiveProfileID = p.ID
	}
	// Surface partial imports instead of silently dropping nodes: a V2Ray list
	// with unsupported schemes / malformed links keeps the good nodes and tells
	// the user how many were skipped. (Frontend can render this as a toast.)
	if len(skipped) > 0 {
		a.state.Connection.LastWarning = fmt.Sprintf(
			"Imported %d node(s); skipped %d unsupported or invalid share link(s).",
			importedCount, len(skipped),
		)
	}
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		return a.state, err
	}
	return a.state, nil
}

func (a *App) ActivateProfile(profileID string) (AppState, error) {
	a.mu.Lock()
	if profileID == "" {
		a.mu.Unlock()
		return a.state, errors.New("profile id is required")
	}
	found := false
	for _, p := range a.profiles {
		if p.ID == profileID {
			found = true
			break
		}
	}
	if !found {
		a.mu.Unlock()
		return a.state, errors.New("profile not found")
	}
	connected := a.state.Connection.Status == "connected"
	profileChanged := a.state.Profile.ActiveProfileID != profileID
	a.state.Profile.ActiveProfileID = profileID
	if profileChanged {
		// ActiveGroup + cached /proxies belong to the previous profile —
		// drop them so the UI doesn't briefly highlight a group that
		// doesn't exist in the new subscription. LastGoodGroup is
		// per-profile; re-hydrate it from the freshly activated profile
		// so the UI shows the user's sticky pick (or nothing, on a fresh
		// profile) immediately, before /proxies finishes loading.
		a.state.Proxy.ActiveGroup = ""
		a.state.Proxy.Groups = []ProxyGroup{}
		a.state.Proxy.LastGoodGroup = a.activeProfileLastGoodGroupLocked()
	}
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.state, err
	}
	a.mu.Unlock()
	if connected {
		go a.reconnectActiveProfile()
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

func (a *App) DeleteProfile(profileID string) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}

	a.mu.Lock()
	idx := -1
	for i := range a.profiles {
		if a.profiles[i].ID == profileID {
			idx = i
			break
		}
	}
	if idx < 0 {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("profile not found")
	}

	wasActive := a.state.Profile.ActiveProfileID == profileID
	a.profiles = append(a.profiles[:idx], a.profiles[idx+1:]...)
	a.state.Profile.Profiles = a.profiles
	if wasActive {
		if len(a.profiles) > 0 {
			a.state.Profile.ActiveProfileID = a.profiles[0].ID
		} else {
			a.state.Profile.ActiveProfileID = ""
		}
		// Deleting or rotating the active profile invalidates the
		// cached /proxies snapshot. Re-hydrate LastGoodGroup from
		// whatever profile is active now (or blank if we deleted the
		// last one).
		a.state.Proxy.ActiveGroup = ""
		a.state.Proxy.Groups = []ProxyGroup{}
		a.state.Proxy.LastGoodGroup = a.activeProfileLastGoodGroupLocked()
	}
	a.state.UpdatedAt = time.Now().Unix()
	connected := a.state.Connection.Status == "connected"
	nextActiveID := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.GetAppState(), err
	}
	a.mu.Unlock()

	if wasActive && connected {
		if nextActiveID != "" {
			go a.reconnectActiveProfile()
		} else {
			go func() { _ = a.Disconnect() }()
		}
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

// RenameProfile updates the display name only (subscription URL unchanged).
func (a *App) RenameProfile(profileID string, newName string) (AppState, error) {
	return a.UpdateProfileInfo(profileID, newName, "")
}

// UpdateProfileInfo updates display name and optionally the subscription URL (empty url = leave unchanged).
func (a *App) UpdateProfileInfo(profileID string, displayName string, subscriptionURL string) (AppState, error) {
	displayName = strings.TrimSpace(displayName)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	if displayName == "" {
		return a.GetAppState(), errors.New("name is required")
	}
	subscriptionURL = strings.TrimSpace(subscriptionURL)

	a.mu.Lock()
	defer a.mu.Unlock()
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		a.profiles[i].Name = displayName
		if subscriptionURL != "" {
			norm, err := normalizeSubscriptionURL(subscriptionURL)
			if err != nil {
				return a.state, err
			}
			a.profiles[i].URL = norm
		}
		a.profiles[i].LastUpdated = time.Now().Unix()
		break
	}
	if !found {
		return a.state, errors.New("profile not found")
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		return a.state, err
	}
	return a.state, nil
}

// AgeKeyPair is the result of GenerateAgeKeyPair: the public recipient goes to
// the subscription provider, the secret identity goes on the profile.
type AgeKeyPair struct {
	PublicKey string `json:"publicKey"`
	SecretKey string `json:"secretKey"`
}

// GenerateAgeKeyPair mints a fresh age identity for age-encrypted
// subscriptions. kind: "x25519" (default) or "hybrid" (MLKEM768-X25519,
// post-quantum). Nothing is persisted — the user explicitly saves the secret
// on a profile (SetProfileAgeSecretKey) and hands the public key to the
// provider.
func (a *App) GenerateAgeKeyPair(kind string) (AgeKeyPair, error) {
	pub, sec, err := generateAgeKeyPair(kind)
	if err != nil {
		return AgeKeyPair{}, err
	}
	return AgeKeyPair{PublicKey: pub, SecretKey: sec}, nil
}

// DeriveAgePublicKey re-derives the public recipient (age1…) from a secret
// identity so the user can copy it for the provider at any time. Pure; nothing
// persisted or logged.
func (a *App) DeriveAgePublicKey(secretKey string) (string, error) {
	return deriveAgePublicKeys(secretKey)
}

// SetProfileAgeSecretKey stores (or clears, when empty) the age identity used
// to decrypt this profile's age-encrypted subscription body. The key is
// validated before persisting and is never logged.
func (a *App) SetProfileAgeSecretKey(profileID string, secretKey string) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	secretKey = strings.TrimSpace(secretKey)
	if secretKey != "" {
		if err := validateAgeSecretKey(secretKey); err != nil {
			return a.GetAppState(), fmt.Errorf("invalid AGE secret key: %w", err)
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		a.profiles[i].AgeSecretKey = secretKey
		a.profiles[i].LastUpdated = time.Now().Unix()
		break
	}
	if !found {
		return a.state, errors.New("profile not found")
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = time.Now().Unix()
	if err := a.persistProfilesLocked(); err != nil {
		return a.state, err
	}
	return a.state, nil
}

// SetProfileMergeTemplate stores the Verge-style merge YAML for a profile and clears manual-config pinning.
func (a *App) SetProfileMergeTemplate(profileID string, template string) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	a.mu.Lock()
	active := false
	connected := a.state.Connection.Status == "connected"
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		a.profiles[i].MergeTemplate = template
		a.profiles[i].SkipAutoConfig = false
		a.profiles[i].LastUpdated = time.Now().Unix()
		break
	}
	if !found {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("profile not found")
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = time.Now().Unix()
	active = a.state.Profile.ActiveProfileID == profileID
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.GetAppState(), err
	}
	a.mu.Unlock()

	if active && connected {
		go a.reconnectActiveProfile()
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

// SetProfileProxyTemplate stores proxy-groups editor YAML (separate from extend config).
func (a *App) SetProfileProxyTemplate(profileID string, template string) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	a.mu.Lock()
	active := false
	connected := a.state.Connection.Status == "connected"
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		a.profiles[i].ProxyTemplate = template
		a.profiles[i].SkipAutoConfig = false
		a.profiles[i].LastUpdated = time.Now().Unix()
		break
	}
	if !found {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("profile not found")
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = time.Now().Unix()
	active = a.state.Profile.ActiveProfileID == profileID
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.GetAppState(), err
	}
	a.mu.Unlock()
	if active && connected {
		go a.reconnectActiveProfile()
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

// SetProfileRulesTemplate stores rules editor YAML (separate from extend config).
func (a *App) SetProfileRulesTemplate(profileID string, template string) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	a.mu.Lock()
	active := false
	connected := a.state.Connection.Status == "connected"
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		a.profiles[i].RulesTemplate = template
		a.profiles[i].SkipAutoConfig = false
		a.profiles[i].LastUpdated = time.Now().Unix()
		break
	}
	if !found {
		a.mu.Unlock()
		return a.GetAppState(), errors.New("profile not found")
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = time.Now().Unix()
	active = a.state.Profile.ActiveProfileID == profileID
	if err := a.persistProfilesLocked(); err != nil {
		a.mu.Unlock()
		return a.GetAppState(), err
	}
	a.mu.Unlock()
	if active && connected {
		// A rules change must take effect on live traffic, not just new
		// connections — flush existing sockets after the reload lands.
		a.reconnectFlushConns.Store(true)
		go a.reconnectActiveProfile()
	}
	a.emitAppStateChanged()
	return a.GetAppState(), nil
}

// activeProfileIndexLocked returns the index of the currently active
// profile inside a.profiles, or -1 if no profile is active / found.
// Caller must hold a.mu.
func (a *App) activeProfileIndexLocked() int {
	id := strings.TrimSpace(a.state.Profile.ActiveProfileID)
	if id == "" {
		return -1
	}
	for i := range a.profiles {
		if a.profiles[i].ID == id {
			return i
		}
	}
	return -1
}

// activeProfileLastGoodGroupLocked returns the per-profile sticky pick.
// Caller must hold a.mu.
func (a *App) activeProfileLastGoodGroupLocked() string {
	idx := a.activeProfileIndexLocked()
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(a.profiles[idx].LastGoodGroup)
}
