package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultProfileAutoUpdateMinutes = 360
	profileAutoUpdateTick           = time.Minute
)

// RefreshProfileSubscription force-refreshes one subscription profile metadata.
// It updates Subscription-Userinfo (when provider sends it), bumps LastUpdated,
// and reconnects if this is the active connected profile so runtime config is re-applied.
func (a *App) RefreshProfileSubscription(profileID string) (AppState, error) {
	return a.refreshProfileSubscription(profileID, true)
}

func (a *App) refreshProfileSubscription(profileID string, reconnectActive bool) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}

	var target Profile
	a.mu.RLock()
	found := false
	for _, p := range a.profiles {
		if p.ID == profileID {
			target = p
			found = true
			break
		}
	}
	a.mu.RUnlock()
	if !found {
		return a.GetAppState(), errors.New("profile not found")
	}
	if strings.TrimSpace(target.URL) == "" {
		return a.GetAppState(), errors.New("profile has no subscription url")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	peek, err := peekSubscription(ctx, target.URL)
	if err != nil {
		return a.GetAppState(), err
	}

	// Also refresh the on-disk subscription body cache. Connect now reads
	// from this cache synchronously (cache-first policy in
	// tryWriteMergedFullProfile), so if we don't update it here the
	// reconnect triggered below would come up on the OLD body even though
	// the user just clicked "Refresh subscription". Fetch failure is
	// tolerated: the existing cache stays in place and peek already told
	// us the subscription is reachable, so the background kicker will
	// retry on the next Connect.
	//
	// Routes through runSubscriptionFetchOnce so an in-flight background
	// refresh isn't trampled by this explicit click — both callers attach
	// to the same singleflight slot per dataDir and share the outcome.
	if root, derr := slothDataRoot(); derr == nil {
		dataDir := filepath.Join(root, "runtime", profileID)
		bodyCtx, bodyCancel := context.WithTimeout(context.Background(), 40*time.Second)
		_ = runSubscriptionFetchOnce(bodyCtx, dataDir, target.URL)
		bodyCancel()
	}

	now := time.Now().Unix()
	a.mu.Lock()
	defer a.mu.Unlock()
	found = false
	activeAndConnected := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		if s := strings.TrimSpace(peek.SubscriptionInfo); s != "" {
			a.profiles[i].SubscriptionInfo = s
		}
		if s := strings.TrimSpace(peek.SubscriptionSupportURL); s != "" {
			a.profiles[i].SubscriptionSupportURL = s
		}
		if s := strings.TrimSpace(peek.SubscriptionAnnouncement); s != "" {
			a.profiles[i].SubscriptionAnnouncement = s
		}
		if a.profiles[i].AutoUpdateIntervalMinutes <= 0 {
			a.profiles[i].AutoUpdateIntervalMinutes = defaultProfileAutoUpdateMinutes
		}
		a.profiles[i].LastUpdated = now
		activeAndConnected =
			a.state.Connection.Status == "connected" &&
				a.state.Profile.ActiveProfileID == profileID
		break
	}
	if !found {
		return a.state, errors.New("profile not found")
	}
	a.state.Profile.Profiles = a.profiles
	a.state.UpdatedAt = now
	if err := a.persistProfilesLocked(); err != nil {
		return a.state, err
	}

	// Reconnect without blocking the bridge thread.
	if reconnectActive && activeAndConnected {
		go a.reconnectActiveProfile()
	}
	a.emitAppStateChanged()
	return a.state, nil
}

// SetProfileAutoUpdate updates periodic subscription refresh settings for one profile.
func (a *App) SetProfileAutoUpdate(profileID string, enabled bool, intervalMinutes int) (AppState, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return a.GetAppState(), errors.New("profile id is required")
	}
	if intervalMinutes <= 0 {
		intervalMinutes = defaultProfileAutoUpdateMinutes
	}
	// Keep guardrails practical; too-frequent background updates create noisy traffic.
	if intervalMinutes < 5 {
		intervalMinutes = 5
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	found := false
	for i := range a.profiles {
		if a.profiles[i].ID != profileID {
			continue
		}
		found = true
		a.profiles[i].AutoUpdateEnabled = enabled
		a.profiles[i].AutoUpdateIntervalMinutes = intervalMinutes
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

func (a *App) startProfileAutoUpdateLoop(ctx context.Context) {
	ticker := time.NewTicker(profileAutoUpdateTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.refreshDueProfiles()
		}
	}
}

func (a *App) refreshDueProfiles() {
	now := time.Now().Unix()
	dueIDs := make([]string, 0, 4)
	a.mu.RLock()
	for _, p := range a.profiles {
		if strings.TrimSpace(p.URL) == "" {
			continue
		}
		enabled := p.AutoUpdateEnabled
		// Backward compatibility: old profiles (without the new field) default to enabled.
		if p.AutoUpdateEnabled == false && p.AutoUpdateIntervalMinutes == 0 {
			enabled = true
		}
		if !enabled {
			continue
		}
		interval := p.AutoUpdateIntervalMinutes
		if interval <= 0 {
			interval = defaultProfileAutoUpdateMinutes
		}
		if p.LastUpdated <= 0 || (now-p.LastUpdated) >= int64(interval)*60 {
			dueIDs = append(dueIDs, p.ID)
		}
	}
	a.mu.RUnlock()

	for _, id := range dueIDs {
		_, _ = a.refreshProfileSubscription(id, false)
	}
}
