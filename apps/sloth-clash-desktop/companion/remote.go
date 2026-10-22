package companion

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Nemu-x/clash-companion/go/controller"
	"github.com/Nemu-x/clash-companion/go/protocol"
)

// Remote buttons (PROTOCOL.md §8.6–§8.19) and the event stream (§9.2) of a paired
// agent. Every call goes through clientFor, and every result is converted to the
// JSON-friendly views below so no protocol type reaches the Wails bindings.
// Callers check StatusView.Capabilities first: an unadvertised capability comes
// back as a RemoteError with code not_found.

// RemoteError is an agent's structured error (PROTOCOL.md §5.5). Its message
// starts with the code in brackets, `[not_found] …`, which is what the frontend
// sees of a Wails error; Code is the machine-readable part.
type RemoteError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *RemoteError) Error() string { return "[" + e.Code + "] " + e.Message }

// remoteErr converts a controller.APIError into a RemoteError and passes any
// other error (network, pin mismatch, timeout) through.
func remoteErr(err error) error {
	var api *controller.APIError
	if errors.As(err, &api) {
		msg := strings.TrimSpace(api.Message)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", api.Status)
		}
		return &RemoteError{Code: api.Code, Message: msg}
	}
	return err
}

// IsUnauthorized reports whether err means the agent no longer accepts our
// token (revoked or unpaired on the agent side).
func IsUnauthorized(err error) bool {
	var re *RemoteError
	if errors.As(err, &re) {
		return re.Code == protocol.CodeUnauthorized
	}
	var api *controller.APIError
	return errors.As(err, &api) && api.Code == protocol.CodeUnauthorized
}

// --- views ---

// SummaryView is the optional status summary (PROTOCOL.md §8.1).
type SummaryView struct {
	Mode        string `json:"mode"`
	Profile     string `json:"profile"`
	Proxy       string `json:"proxy"`
	AppVersion  string `json:"appVersion"`
	CoreVersion string `json:"coreVersion"`
}

type ProxyNodeView struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Delay int    `json:"delay"` // ms; 0 = unknown or failed
}

type ProxyGroupView struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	Now        string          `json:"now"`
	Fixed      string          `json:"fixed"` // pinned node of an auto group; "" when none
	Selectable bool            `json:"selectable"`
	Nodes      []ProxyNodeView `json:"nodes"`
}

type SelectResult struct {
	Group string `json:"group"`
	Now   string `json:"now"`
}

type DelayView struct {
	Name  string `json:"name"`
	Delay int    `json:"delay"` // ms; 0 = failed or timed out
}

type ModeView struct {
	Mode  string   `json:"mode"`
	Modes []string `json:"modes"`
}

type UsageView struct {
	Upload   int64 `json:"upload"`
	Download int64 `json:"download"`
	Total    int64 `json:"total"`
	Expire   int64 `json:"expire"` // unix seconds; 0 = never/unknown
}

type ProfileView struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Type      string     `json:"type"`      // remote | local
	UpdatedAt int64      `json:"updatedAt"` // unix seconds; 0 = unknown
	Usage     *UsageView `json:"usage,omitempty"`
}

type ProfilesView struct {
	Active   string        `json:"active"`
	Profiles []ProfileView `json:"profiles"`
}

type ProviderView struct {
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`    // proxy | rule
	Vehicle   string     `json:"vehicle"` // http | file | inline
	Count     int        `json:"count"`
	UpdatedAt int64      `json:"updatedAt"`
	Usage     *UsageView `json:"usage,omitempty"`
}

type ConnectionView struct {
	ID       string   `json:"id"`
	Host     string   `json:"host"`
	Network  string   `json:"network"`
	Process  string   `json:"process"`
	Rule     string   `json:"rule"`
	Chains   []string `json:"chains"`
	Upload   int64    `json:"upload"`
	Download int64    `json:"download"`
	Start    int64    `json:"start"` // unix ms
}

type ConnectionsView struct {
	UploadTotal   int64            `json:"uploadTotal"`
	DownloadTotal int64            `json:"downloadTotal"`
	Connections   []ConnectionView `json:"connections"`
}

type TrafficView struct {
	Up        int64 `json:"up"` // bytes/s
	Down      int64 `json:"down"`
	UpTotal   int64 `json:"upTotal"`
	DownTotal int64 `json:"downTotal"`
}

type LogLineView struct {
	Time  int64  `json:"time"` // unix ms
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

// SettingView is one typed toggle: Value is a bool for type "bool" and a string
// (one of Options) for type "enum".
type SettingView struct {
	Key     string   `json:"key"`
	Type    string   `json:"type"`
	Value   any      `json:"value"`
	Options []string `json:"options"`
	Label   string   `json:"label"`
}

type ActionView struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func summaryView(s *protocol.Summary) *SummaryView {
	if s == nil {
		return nil
	}
	return &SummaryView{Mode: s.Mode, Profile: s.Profile, Proxy: s.Proxy, AppVersion: s.AppVersion, CoreVersion: s.CoreVersion}
}

func statusView(st protocol.Status) *StatusView {
	caps := st.Capabilities
	if caps == nil {
		caps = []string{}
	}
	return &StatusView{
		DeviceID: st.ID, Name: st.Name, App: st.App, Ver: st.Ver,
		Power: st.Power, Capabilities: caps, Summary: summaryView(st.Summary),
	}
}

func usageView(u *protocol.Usage) *UsageView {
	if u == nil {
		return nil
	}
	return &UsageView{Upload: u.Upload, Download: u.Download, Total: u.Total, Expire: u.Expire}
}

func trafficView(t protocol.Traffic) TrafficView {
	return TrafficView{Up: t.Up, Down: t.Down, UpTotal: t.UpTotal, DownTotal: t.DownTotal}
}

func logLineView(l protocol.LogLine) LogLineView {
	return LogLineView{Time: l.Time, Level: l.Level, Msg: l.Msg}
}

func settingView(s protocol.Setting) SettingView {
	opts := s.Options
	if opts == nil {
		opts = []string{}
	}
	return SettingView{Key: s.Key, Type: s.Type, Value: s.Value, Options: opts, Label: s.Label}
}

// withClient runs fn against a paired agent and converts its error.
func (m *Manager) withClient(ctx context.Context, deviceID string, fn func(*controller.Client) error) error {
	c, err := m.clientFor(ctx, deviceID)
	if err != nil {
		return err
	}
	return remoteErr(fn(c))
}

// --- buttons ---

func (m *Manager) Proxies(ctx context.Context, deviceID string) ([]ProxyGroupView, error) {
	out := []ProxyGroupView{}
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		pl, err := c.Proxies(ctx)
		if err != nil {
			return err
		}
		for _, g := range pl.Groups {
			gv := ProxyGroupView{Name: g.Name, Type: g.Type, Now: g.Now, Fixed: g.Fixed, Selectable: g.Selectable, Nodes: []ProxyNodeView{}}
			for _, n := range g.Nodes {
				gv.Nodes = append(gv.Nodes, ProxyNodeView{Name: n.Name, Type: n.Type, Delay: n.Delay})
			}
			out = append(out, gv)
		}
		return nil
	})
	return out, err
}

func (m *Manager) SelectProxy(ctx context.Context, deviceID, group, node string) (*SelectResult, error) {
	var out *SelectResult
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.SelectProxy(ctx, group, node)
		if err == nil {
			out = &SelectResult{Group: r.Group, Now: r.Now}
		}
		return err
	})
	return out, err
}

func (m *Manager) UnfixProxy(ctx context.Context, deviceID, group string) (*SelectResult, error) {
	var out *SelectResult
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.UnfixProxy(ctx, group)
		if err == nil {
			out = &SelectResult{Group: r.Group, Now: r.Now}
		}
		return err
	})
	return out, err
}

// TestDelay tests one node, or the whole group when node is "".
func (m *Manager) TestDelay(ctx context.Context, deviceID, group, node string) ([]DelayView, error) {
	out := []DelayView{}
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.TestDelay(ctx, protocol.DelayRequest{Group: group, Node: node})
		if err != nil {
			return err
		}
		for _, d := range r.Delays {
			out = append(out, DelayView{Name: d.Name, Delay: d.Delay})
		}
		return nil
	})
	return out, err
}

func (m *Manager) Mode(ctx context.Context, deviceID string) (*ModeView, error) {
	var out *ModeView
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.Mode(ctx)
		if err == nil {
			modes := r.Modes
			if modes == nil {
				modes = []string{}
			}
			out = &ModeView{Mode: r.Mode, Modes: modes}
		}
		return err
	})
	return out, err
}

func (m *Manager) SetMode(ctx context.Context, deviceID, mode string) (string, error) {
	var out string
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.SetMode(ctx, mode)
		if err == nil {
			out = r.Mode
		}
		return err
	})
	return out, err
}

func (m *Manager) Profiles(ctx context.Context, deviceID string) (*ProfilesView, error) {
	var out *ProfilesView
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.Profiles(ctx)
		if err != nil {
			return err
		}
		out = &ProfilesView{Active: r.Active, Profiles: []ProfileView{}}
		for _, p := range r.Profiles {
			out.Profiles = append(out.Profiles, ProfileView{ID: p.ID, Name: p.Name, Type: p.Type, UpdatedAt: p.UpdatedAt, Usage: usageView(p.Usage)})
		}
		return nil
	})
	return out, err
}

func (m *Manager) SelectProfile(ctx context.Context, deviceID, id string) (string, error) {
	var out string
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.SelectProfile(ctx, id)
		if err == nil {
			out = r.Active
		}
		return err
	})
	return out, err
}

// UpdateProfile refreshes one remote profile, or all of them when id is "".
func (m *Manager) UpdateProfile(ctx context.Context, deviceID, id string) error {
	return m.withClient(ctx, deviceID, func(c *controller.Client) error { return c.UpdateProfile(ctx, id) })
}

func (m *Manager) DeleteProfile(ctx context.Context, deviceID, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("profile id is required")
	}
	return m.withClient(ctx, deviceID, func(c *controller.Client) error { return c.DeleteProfile(ctx, id) })
}

func (m *Manager) Providers(ctx context.Context, deviceID string) ([]ProviderView, error) {
	out := []ProviderView{}
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.Providers(ctx)
		if err != nil {
			return err
		}
		for _, p := range r.Providers {
			out = append(out, ProviderView{Name: p.Name, Kind: p.Kind, Vehicle: p.Vehicle, Count: p.Count, UpdatedAt: p.UpdatedAt, Usage: usageView(p.Usage)})
		}
		return nil
	})
	return out, err
}

func (m *Manager) UpdateProvider(ctx context.Context, deviceID, kind, name string) error {
	return m.withClient(ctx, deviceID, func(c *controller.Client) error { return c.UpdateProvider(ctx, kind, name) })
}

func (m *Manager) HealthCheckProvider(ctx context.Context, deviceID, name string) error {
	return m.withClient(ctx, deviceID, func(c *controller.Client) error { return c.HealthCheckProvider(ctx, name) })
}

func (m *Manager) Connections(ctx context.Context, deviceID string) (*ConnectionsView, error) {
	var out *ConnectionsView
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.Connections(ctx)
		if err != nil {
			return err
		}
		out = &ConnectionsView{UploadTotal: r.UploadTotal, DownloadTotal: r.DownloadTotal, Connections: []ConnectionView{}}
		for _, cn := range r.Connections {
			chains := cn.Chains
			if chains == nil {
				chains = []string{}
			}
			out.Connections = append(out.Connections, ConnectionView{
				ID: cn.ID, Host: cn.Host, Network: cn.Network, Process: cn.Process, Rule: cn.Rule,
				Chains: chains, Upload: cn.Upload, Download: cn.Download, Start: cn.Start,
			})
		}
		return nil
	})
	return out, err
}

// CloseConnections closes one connection, or all when id is "", and returns how
// many were closed.
func (m *Manager) CloseConnections(ctx context.Context, deviceID, id string) (int, error) {
	var out int
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.CloseConnections(ctx, id)
		if err == nil {
			out = r.Closed
		}
		return err
	})
	return out, err
}

func (m *Manager) Traffic(ctx context.Context, deviceID string) (*TrafficView, error) {
	var out *TrafficView
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.Traffic(ctx)
		if err == nil {
			v := trafficView(*r)
			out = &v
		}
		return err
	})
	return out, err
}

func (m *Manager) Logs(ctx context.Context, deviceID, level string, limit int) ([]LogLineView, error) {
	out := []LogLineView{}
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.Logs(ctx, level, limit)
		if err != nil {
			return err
		}
		for _, l := range r.Lines {
			out = append(out, logLineView(l))
		}
		return nil
	})
	return out, err
}

func (m *Manager) Settings(ctx context.Context, deviceID string) ([]SettingView, error) {
	out := []SettingView{}
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.Settings(ctx)
		if err != nil {
			return err
		}
		for _, s := range r.Settings {
			out = append(out, settingView(s))
		}
		return nil
	})
	return out, err
}

// SetSetting sends a bool or a string; anything else is refused here, before
// the agent sees it (Setting.value is bool | string, PROTOCOL.md §8.18).
func (m *Manager) SetSetting(ctx context.Context, deviceID, key string, value any) (*SettingView, error) {
	switch value.(type) {
	case bool, string:
	default:
		return nil, fmt.Errorf("setting %q: value must be a bool or a string, got %T", key, value)
	}
	var out *SettingView
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.SetSetting(ctx, key, value)
		if err == nil {
			v := settingView(r.Setting)
			out = &v
		}
		return err
	})
	return out, err
}

func (m *Manager) Actions(ctx context.Context, deviceID string) ([]ActionView, error) {
	out := []ActionView{}
	err := m.withClient(ctx, deviceID, func(c *controller.Client) error {
		r, err := c.Actions(ctx)
		if err != nil {
			return err
		}
		for _, a := range r.Actions {
			out = append(out, ActionView{ID: a.ID, Label: a.Label})
		}
		return nil
	})
	return out, err
}

func (m *Manager) RunAction(ctx context.Context, deviceID, id string) error {
	return m.withClient(ctx, deviceID, func(c *controller.Client) error { return c.RunAction(ctx, id) })
}

// --- events ---

// Event kinds a Watch emits. status / changed / traffic / log mirror the agent's
// events (PROTOCOL.md §9.2); open and down are the stream's own state.
const (
	EventOpen         = "open"         // stream (re)connected: refetch everything shown
	EventDown         = "down"         // stream lost; Error says why, a reconnect follows
	EventUnauthorized = "unauthorized" // token revoked: the watch stops
)

// EventView is one event for the UI.
type EventView struct {
	DeviceID string       `json:"deviceId"`
	Type     string       `json:"type"`
	Resource string       `json:"resource,omitempty"` // changed
	Status   *StatusView  `json:"status,omitempty"`   // status
	Traffic  *TrafficView `json:"traffic,omitempty"`  // traffic
	Log      *LogLineView `json:"log,omitempty"`      // log
	Error    string       `json:"error,omitempty"`    // down / unauthorized
}

// WatchOptions selects the optional topics of a Watch.
type WatchOptions struct {
	Traffic  bool
	Logs     bool
	LogLevel string
}

// watchBackoff bounds the reconnect delay of a Watch.
var watchBackoff = struct{ min, max time.Duration }{time.Second, 30 * time.Second}

// Watch streams the agent's events into emit until ctx is cancelled or the
// agent rejects the token. A lost stream is reconnected with exponential
// backoff; an open event follows every (re)connect, because events missed while
// down are not replayed and the UI has to refetch. Unknown event types are
// dropped (PROTOCOL.md §9.2: controllers ignore what they do not know).
func (m *Manager) Watch(ctx context.Context, deviceID string, opts WatchOptions, emit func(EventView)) {
	delay := watchBackoff.min
	for ctx.Err() == nil {
		c, err := m.clientFor(ctx, deviceID)
		if err == nil {
			opened := false
			err = c.Events(ctx, controller.EventOptions{Traffic: opts.Traffic, Logs: opts.Logs, LogLevel: opts.LogLevel}, func(e controller.Event) {
				if !opened {
					opened = true
					delay = watchBackoff.min
					emit(EventView{DeviceID: deviceID, Type: EventOpen})
				}
				if v, ok := eventView(deviceID, e); ok {
					emit(v)
				}
			})
		}
		if ctx.Err() != nil {
			return
		}
		// Revoked by the agent, or forgotten here (unpaired, token gone): no
		// reconnect can succeed.
		if IsUnauthorized(err) || errors.Is(err, ErrNotPaired) || errors.Is(err, ErrTokenNotFound) {
			emit(EventView{DeviceID: deviceID, Type: EventUnauthorized, Error: remoteErr(err).Error()})
			return
		}
		msg := "stream closed"
		if err != nil {
			msg = remoteErr(err).Error()
		}
		emit(EventView{DeviceID: deviceID, Type: EventDown, Error: msg})
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, watchBackoff.max)
	}
}

func eventView(deviceID string, e controller.Event) (EventView, bool) {
	v := EventView{DeviceID: deviceID, Type: e.Type}
	switch e.Type {
	case protocol.EventStatus:
		st, err := e.Status()
		if err != nil {
			return v, false
		}
		v.Status = statusView(st)
	case protocol.EventChanged:
		ch, err := e.Changed()
		if err != nil || ch.Resource == "" {
			return v, false
		}
		v.Resource = ch.Resource
	case protocol.EventTraffic:
		t, err := e.Traffic()
		if err != nil {
			return v, false
		}
		tv := trafficView(t)
		v.Traffic = &tv
	case protocol.EventLog:
		l, err := e.Log()
		if err != nil {
			return v, false
		}
		lv := logLineView(l)
		v.Log = &lv
	default:
		return v, false
	}
	return v, true
}
