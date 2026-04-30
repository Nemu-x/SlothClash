package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type ConnectionMeta struct {
	Network         string `json:"network,omitempty"`
	Type            string `json:"type,omitempty"`
	Host            string `json:"host,omitempty"`
	SourceIP        string `json:"sourceIP,omitempty"`
	SourcePort      string `json:"sourcePort,omitempty"`
	DestinationIP   string `json:"destinationIP,omitempty"`
	DestinationPort string `json:"destinationPort,omitempty"`
	Process         string `json:"process,omitempty"`
}

type ConnectionItem struct {
	ID          string         `json:"id"`
	Metadata    ConnectionMeta `json:"metadata"`
	Upload      int64          `json:"upload"`
	Download    int64          `json:"download"`
	Start       string         `json:"start,omitempty"`
	Rule        string         `json:"rule,omitempty"`
	RulePayload string         `json:"rulePayload,omitempty"`
	Chains      []string       `json:"chains,omitempty"`
}

type ConnectionsOverview struct {
	Controller   string           `json:"controller"`
	Reachable    bool             `json:"reachable"`
	LastError    string           `json:"lastError,omitempty"`
	UploadTotal  int64            `json:"uploadTotal"`
	DownloadTotal int64           `json:"downloadTotal"`
	Connections  []ConnectionItem `json:"connections"`
}

// FetchConnectionsOverview reads /connections from the active mihomo controller.
func (a *App) FetchConnectionsOverview() ConnectionsOverview {
	a.mu.RLock()
	conn := strings.TrimSpace(a.state.Connection.Status)
	running := a.state.Core.Running
	ep := a.effectiveCoreEndpointLocked()
	secret := a.coreSecret
	a.mu.RUnlock()

	if ep == "" || (conn != "connected" && !running) {
		base := strings.TrimSpace(os.Getenv("SLOTH_CLASH_CONTROLLER"))
		if base == "" {
			return ConnectionsOverview{LastError: "connect Sloth or set SLOTH_CLASH_CONTROLLER for external core"}
		}
		if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
			base = "http://" + base
		}
		base = strings.TrimRight(base, "/")
		ep = strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://")
		secret = strings.TrimSpace(os.Getenv("SLOTH_CLASH_SECRET"))
	}

	out := ConnectionsOverview{Connections: make([]ConnectionItem, 0, 32)}
	if isWinPipeEndpoint(ep) {
		out.Controller = ep
	} else {
		out.Controller = "http://" + ep
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	resp, err := coreDoWithEndpoint(ctx, ep, secret, http.MethodGet, "/connections", nil)
	if err != nil {
		out.LastError = err.Error()
		return out
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		out.LastError = err.Error()
		return out
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		out.LastError = "GET /connections: HTTP " + resp.Status
		return out
	}

	var raw struct {
		UploadTotal   int64 `json:"uploadTotal"`
		DownloadTotal int64 `json:"downloadTotal"`
		Connections   []struct {
			ID          string   `json:"id"`
			Upload      int64    `json:"upload"`
			Download    int64    `json:"download"`
			Start       string   `json:"start"`
			Chains      []string `json:"chains"`
			Rule        string   `json:"rule"`
			RulePayload string   `json:"rulePayload"`
			Metadata    struct {
				Network         string `json:"network"`
				Type            string `json:"type"`
				Host            string `json:"host"`
				SourceIP        string `json:"sourceIP"`
				SourcePort      string `json:"sourcePort"`
				DestinationIP   string `json:"destinationIP"`
				DestinationPort string `json:"destinationPort"`
				Process         string `json:"process"`
			} `json:"metadata"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		out.LastError = err.Error()
		return out
	}

	out.Reachable = true
	out.UploadTotal = raw.UploadTotal
	out.DownloadTotal = raw.DownloadTotal
	limit := len(raw.Connections)
	if limit > 500 {
		limit = 500
	}
	out.Connections = make([]ConnectionItem, 0, limit)
	for i := 0; i < limit; i++ {
		c := raw.Connections[i]
		if strings.TrimSpace(c.ID) == "" {
			continue
		}
		out.Connections = append(out.Connections, ConnectionItem{
			ID:       c.ID,
			Upload:   c.Upload,
			Download: c.Download,
			Start:    c.Start,
			Chains:   c.Chains,
			Rule:     c.Rule,
			RulePayload: c.RulePayload,
			Metadata: ConnectionMeta{
				Network:         c.Metadata.Network,
				Type:            c.Metadata.Type,
				Host:            c.Metadata.Host,
				SourceIP:        c.Metadata.SourceIP,
				SourcePort:      c.Metadata.SourcePort,
				DestinationIP:   c.Metadata.DestinationIP,
				DestinationPort: c.Metadata.DestinationPort,
				Process:         c.Metadata.Process,
			},
		})
	}
	return out
}

func (a *App) CloseAllConnections() error {
	a.mu.RLock()
	ep := a.effectiveCoreEndpointLocked()
	secret := a.coreSecret
	a.mu.RUnlock()
	if strings.TrimSpace(ep) == "" {
		return errors.New("core controller is unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	resp, err := coreDoWithEndpoint(ctx, ep, secret, http.MethodDelete, "/connections", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("close all failed: HTTP " + resp.Status)
	}
	return nil
}
