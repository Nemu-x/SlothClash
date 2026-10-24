package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

type DesyncUDPCounters struct {
	Packets     uint64 `json:"packets"`
	FakePackets uint64 `json:"fakePackets"`
	FakeErrors  uint64 `json:"fakeErrors"`
	Peers       uint64 `json:"peers"`
}

type DesyncUDPStatus struct {
	DirectFlows       int               `json:"directFlows"`
	ProfileFlows      int               `json:"profileFlows"`
	ProfileRoutes     []string          `json:"profileRoutes"`
	Upload            int64             `json:"upload"`
	Download          int64             `json:"download"`
	CountersAvailable bool              `json:"countersAvailable"`
	Counters          DesyncUDPCounters `json:"counters"`
	LastError         string            `json:"lastError,omitempty"`
}

func isDiscordUDPConnection(c ConnectionItem) bool {
	if !strings.EqualFold(c.Metadata.Network, "udp") {
		return false
	}
	if strings.EqualFold(path.Base(strings.ReplaceAll(c.Metadata.Process, `\`, "/")), "Discord.exe") {
		return true
	}
	for _, chain := range c.Chains {
		if chain == desyncDiscordUDPRoute {
			return true
		}
	}
	return false
}

func summarizeDiscordUDP(connections []ConnectionItem) DesyncUDPStatus {
	out := DesyncUDPStatus{ProfileRoutes: []string{}}
	routes := map[string]bool{}
	for _, c := range connections {
		if !isDiscordUDPConnection(c) {
			continue
		}
		direct := false
		for _, chain := range c.Chains {
			if chain == desyncDiscordUDPRoute {
				direct = true
				break
			}
		}
		if direct {
			out.DirectFlows++
		} else {
			out.ProfileFlows++
			if len(c.Chains) > 0 {
				routes[c.Chains[0]] = true
			}
		}
		out.Upload += c.Upload
		out.Download += c.Download
	}
	for route := range routes {
		out.ProfileRoutes = append(out.ProfileRoutes, route)
	}
	sort.Strings(out.ProfileRoutes)
	return out
}

// Only flow metadata and aggregate counters are returned; no voice packets,
// Discord account information or subscription credentials are inspected.
func (a *App) GetDesyncUDPStatus() DesyncUDPStatus {
	overview := a.FetchConnectionsOverview()
	out := summarizeDiscordUDP(overview.Connections)
	if !overview.Reachable {
		out.LastError = overview.LastError
		return out
	}
	s := currentDesyncSettings()
	if !s.Enabled || !s.DiscordUDP {
		return out
	}
	a.mu.RLock()
	endpoint, secret := a.effectiveCoreEndpointLocked(), a.coreSecret
	a.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := coreDoWithEndpoint(ctx, endpoint, secret, http.MethodGet, "/proxies/"+url.PathEscape(desyncDiscordUDPRoute), nil)
	if err != nil {
		out.LastError = err.Error()
		return out
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		out.LastError = fmt.Sprintf("UDP counters: core HTTP %d; runtime reload may still be pending", response.StatusCode)
		return out
	}
	var body struct {
		UDP *struct {
			Counters DesyncUDPCounters `json:"counters"`
		} `json:"desyncUDP"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&body); err != nil {
		out.LastError = err.Error()
		return out
	}
	if body.UDP != nil {
		out.CountersAvailable = true
		out.Counters = body.UDP.Counters
	}
	return out
}

// Called only by the user's explicit reconnect button. Other applications,
// TCP/Gateway sessions and subscription selections are left alone.
func (a *App) ReconnectDiscordUDP() (int, error) {
	overview := a.FetchConnectionsOverview()
	if !overview.Reachable {
		return 0, fmt.Errorf("Discord UDP reconnect: %s", overview.LastError)
	}
	a.mu.RLock()
	endpoint, secret := a.effectiveCoreEndpointLocked(), a.coreSecret
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
