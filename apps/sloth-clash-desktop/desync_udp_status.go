package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"SlothClashDesktop/internal/desync"
)

type DesyncUDPCounters struct {
	Packets     uint64 `json:"packets"`
	FakePackets uint64 `json:"fakePackets"`
	FakeErrors  uint64 `json:"fakeErrors"`
	Peers       uint64 `json:"peers"`
}

func isDiscordUDPConnection(c ConnectionItem) bool {
	if !strings.EqualFold(c.Metadata.Network, "udp") {
		return false
	}
	if strings.EqualFold(path.Base(strings.ReplaceAll(c.Metadata.Process, `\`, "/")), "Discord.exe") {
		return true
	}
	for _, chain := range c.Chains {
		if chain == desync.DiscordUDPRoute {
			return true
		}
	}
	return false
}

// Called only by the user's explicit reconnect button. Other applications,
// TCP/Gateway sessions and subscription selections are left alone.
func (a *App) ReconnectDiscordUDP() (int, error) {
	overview := a.FetchConnectionsOverview()
	if !overview.Reachable {
		return 0, fmt.Errorf("Discord UDP reconnect: %s", overview.LastError)
	}
	a.mu.RLock()
	endpoint, secret := a.effectiveCoreEndpointLocked(), a.core.secret
	a.mu.RUnlock()
	closed := 0
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	overall, overallCancel := context.WithTimeout(parent, 15*time.Second)
	defer overallCancel()
	for _, c := range overview.Connections {
		if !isDiscordUDPConnection(c) {
			continue
		}
		ctx, cancel := context.WithTimeout(overall, 3*time.Second)
		response, err := coreDoWithEndpoint(ctx, endpoint, secret, http.MethodDelete, "/connections/"+url.PathEscape(c.ID), nil)
		cancel()
		if err != nil {
			return closed, err
		}
		response.Body.Close()
		if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNotFound {
			return closed, fmt.Errorf("Discord UDP reconnect: core HTTP %d", response.StatusCode)
		}
		closed++
	}
	a.appendRuntimeDiag("desync.udp.reconnect", fmt.Sprintf("closed_flows=%d", closed))
	return closed, nil
}
