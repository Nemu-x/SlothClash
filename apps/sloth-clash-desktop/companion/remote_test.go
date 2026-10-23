package companion

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Nemu-x/clash-companion/go/agent"
	"github.com/Nemu-x/clash-companion/go/controller"
	"github.com/Nemu-x/clash-companion/go/discovery"
	"github.com/Nemu-x/clash-companion/go/pairing"
	"github.com/Nemu-x/clash-companion/go/protocol"
)

// The Manager against a real agent from clash-companion: pinned TLS on an
// ephemeral loopback port, fake hooks, no mDNS.

func noMDNS(context.Context, string) (discovery.Entry, bool, error) {
	return discovery.Entry{}, false, nil
}

// closedPort returns a loopback port nothing listens on.
func closedPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// remoteHooks implements every remote-control hook over a small in-memory model.
type remoteHooks struct {
	mu       sync.Mutex
	power    string
	groups   []protocol.ProxyGroup
	mode     string
	profiles protocol.ProfileList
	updated  []string
	deleted  []string
	closed   []string
	settings []protocol.Setting
	ran      []string
	health   []string
}

func newRemoteHooks() *remoteHooks {
	return &remoteHooks{
		power: protocol.PowerOff,
		groups: []protocol.ProxyGroup{
			{Name: "Proxy", Type: "Selector", Now: "HK-01", Selectable: true, Nodes: []protocol.ProxyNode{
				{Name: "HK-01", Type: "Vless", Delay: 80}, {Name: "JP-01", Type: "Trojan"},
			}},
			{Name: "Auto", Type: "URLTest", Now: "HK-01", Fixed: "JP-01", Selectable: true},
		},
		mode: protocol.ModeRule,
		profiles: protocol.ProfileList{Active: "p-1", Profiles: []protocol.Profile{
			{ID: "p-1", Name: "Main", Type: "remote", Usage: &protocol.Usage{Total: 100}}, {ID: "p-2", Name: "Spare", Type: "local"},
		}},
		settings: []protocol.Setting{
			{Key: protocol.SettingTUN, Type: protocol.SettingBool, Value: false},
			{Key: protocol.SettingLogLevel, Type: protocol.SettingEnum, Value: "info", Options: []string{"error", "info", "debug"}},
		},
	}
}

func (h *remoteHooks) PowerState(context.Context) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.power, nil
}

func (h *remoteHooks) Power(_ context.Context, action string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch action {
	case protocol.PowerOn, protocol.PowerOff:
		h.power = action
	case "toggle":
		if h.power == protocol.PowerOn {
			h.power = protocol.PowerOff
		} else {
			h.power = protocol.PowerOn
		}
	}
	return h.power, nil
}

func (h *remoteHooks) ImportSubscription(context.Context, protocol.SubscriptionRequest) error {
	return nil
}

func (h *remoteHooks) ProxyGroups(context.Context) ([]protocol.ProxyGroup, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.groups), nil
}

func (h *remoteHooks) group(name string) (*protocol.ProxyGroup, error) {
	for i := range h.groups {
		if h.groups[i].Name == name {
			return &h.groups[i], nil
		}
	}
	return nil, fmt.Errorf("group %q: %w", name, agent.ErrNotFound)
}

func (h *remoteHooks) SelectProxy(_ context.Context, group, node string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	g, err := h.group(group)
	if err != nil {
		return "", err
	}
	if !slices.ContainsFunc(g.Nodes, func(n protocol.ProxyNode) bool { return n.Name == node }) {
		return "", fmt.Errorf("node %q: %w", node, agent.ErrNotFound)
	}
	g.Now = node
	return node, nil
}

func (h *remoteHooks) UnfixProxy(_ context.Context, group string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	g, err := h.group(group)
	if err != nil {
		return "", err
	}
	g.Fixed = ""
	return g.Now, nil
}

func (h *remoteHooks) TestDelay(_ context.Context, req protocol.DelayRequest) ([]protocol.Delay, error) {
	if req.Node != "" {
		return []protocol.Delay{{Name: req.Node, Delay: 42}}, nil
	}
	return []protocol.Delay{{Name: "HK-01", Delay: 42}, {Name: "JP-01", Delay: 0}}, nil
}

func (h *remoteHooks) Mode(context.Context) (protocol.ModeState, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return protocol.ModeState{Mode: h.mode, Modes: []string{protocol.ModeRule, protocol.ModeGlobal, protocol.ModeDirect}}, nil
}

func (h *remoteHooks) SetMode(_ context.Context, mode string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.mode = mode
	return mode, nil
}

func (h *remoteHooks) Profiles(context.Context) (protocol.ProfileList, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.profiles, nil
}

func (h *remoteHooks) SelectProfile(_ context.Context, id string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.profiles.Active = id
	return id, nil
}

func (h *remoteHooks) UpdateProfile(_ context.Context, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updated = append(h.updated, id)
	return nil
}

func (h *remoteHooks) DeleteProfile(_ context.Context, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.deleted = append(h.deleted, id)
	return nil
}

func (h *remoteHooks) Providers(context.Context) ([]protocol.Provider, error) {
	return []protocol.Provider{{Name: "sub", Kind: protocol.ProviderProxy, Vehicle: "http", Count: 3}}, nil
}

func (h *remoteHooks) UpdateProvider(_ context.Context, kind, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updated = append(h.updated, kind+"/"+name)
	return nil
}

func (h *remoteHooks) HealthCheckProvider(_ context.Context, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.health = append(h.health, name)
	return nil
}

func (h *remoteHooks) Connections(context.Context) (protocol.ConnectionList, error) {
	return protocol.ConnectionList{UploadTotal: 10, DownloadTotal: 20, Connections: []protocol.Connection{
		{ID: "c1", Host: "example.com:443", Network: "tcp", Chains: []string{"HK-01", "Proxy"}},
	}}, nil
}

func (h *remoteHooks) CloseConnections(_ context.Context, id string) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = append(h.closed, id)
	if id == "" {
		return 1, nil
	}
	return 1, nil
}

func (h *remoteHooks) Traffic(context.Context) (protocol.Traffic, error) {
	return protocol.Traffic{Up: 1, Down: 2, UpTotal: 3, DownTotal: 4}, nil
}

func (h *remoteHooks) Logs(_ context.Context, level string, limit int) ([]protocol.LogLine, error) {
	return []protocol.LogLine{{Time: 1, Level: "info", Msg: fmt.Sprintf("level=%s limit=%d", level, limit)}}, nil
}

func (h *remoteHooks) Settings(context.Context) ([]protocol.Setting, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.settings), nil
}

func (h *remoteHooks) SetSetting(_ context.Context, key string, value any) (protocol.Setting, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.settings {
		if h.settings[i].Key == key {
			h.settings[i].Value = value
			return h.settings[i], nil
		}
	}
	return protocol.Setting{}, fmt.Errorf("setting %q: %w", key, agent.ErrNotFound)
}

func (h *remoteHooks) Actions(context.Context) ([]protocol.Action, error) {
	return []protocol.Action{{ID: protocol.ActionFlushDNS}, {ID: "custom", Label: "Custom"}}, nil
}

func (h *remoteHooks) RunAction(_ context.Context, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ran = append(h.ran, id)
	return nil
}

func (h *remoteHooks) Summary(context.Context) (protocol.Summary, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return protocol.Summary{Mode: h.mode, Profile: "Main", Proxy: "HK-01", AppVersion: "1.2.3", CoreVersion: "v1.19.33"}, nil
}

type remoteFixture struct {
	m        *Manager
	agent    *agent.Agent
	hooks    *remoteHooks
	agentID  string
	fp, host string
	port     int
	token    string
}

// newRemoteFixture pairs a Manager with a live agent.
func newRemoteFixture(t *testing.T, mut func(*agent.Config)) *remoteFixture {
	t.Helper()
	id, err := pairing.NewIdentity("clashctl-agent", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	hooks := newRemoteHooks()
	store := pairing.NewStore()
	cfg := agent.Config{App: "clashfest", DeviceID: "agent-1", Name: "Phone", Identity: id, Store: store, Hooks: hooks}
	if mut != nil {
		mut(&cfg)
	}
	a, err := agent.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(a.Handler())
	if srv.TLS, err = a.TLSConfig(); err != nil {
		t.Fatal(err)
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	m, err := NewManager(t.TempDir(), newFakeTokens())
	if err != nil {
		t.Fatal(err)
	}
	m.findByID = noMDNS
	token, err := store.Pair(m.DeviceID(), "SlothClash")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.tokens.Set(cfg.DeviceID, token); err != nil {
		t.Fatal(err)
	}
	if err := m.store.Upsert(Agent{DeviceID: cfg.DeviceID, Name: "Phone", App: "clashfest", FP: id.FP, LastHost: "127.0.0.1", LastPort: port}); err != nil {
		t.Fatal(err)
	}
	return &remoteFixture{m: m, agent: a, hooks: hooks, agentID: cfg.DeviceID, fp: id.FP, host: "127.0.0.1", port: port, token: token}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var re *RemoteError
	if !errors.As(err, &re) || re.Code != code {
		t.Fatalf("want a RemoteError with code %s, got %v", code, err)
	}
}

func TestRemoteButtons(t *testing.T) {
	f := newRemoteFixture(t, nil)
	m, id, ctx := f.m, f.agentID, t.Context()

	st, err := m.Status(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{protocol.CapProxies, protocol.CapSelect, protocol.CapUnfix, protocol.CapDelay, protocol.CapMode,
		protocol.CapProfiles, protocol.CapProfileUpdate, protocol.CapProfileDelete, protocol.CapProviders,
		protocol.CapConnections, protocol.CapTraffic, protocol.CapLogs, protocol.CapSettings, protocol.CapActions, protocol.CapEvents} {
		if !slices.Contains(st.Capabilities, c) {
			t.Errorf("capability %s not advertised: %v", c, st.Capabilities)
		}
	}
	if st.Summary == nil || st.Summary.Profile != "Main" || st.Summary.CoreVersion != "v1.19.33" {
		t.Errorf("summary = %+v", st.Summary)
	}

	groups, err := m.Proxies(ctx, id)
	if err != nil || len(groups) != 2 || groups[0].Nodes[0].Delay != 80 || groups[1].Fixed != "JP-01" || groups[1].Nodes == nil {
		t.Fatalf("proxies = %+v, %v", groups, err)
	}
	if r, err := m.SelectProxy(ctx, id, "Proxy", "JP-01"); err != nil || r.Now != "JP-01" {
		t.Fatalf("select = %+v, %v", r, err)
	}
	_, err = m.SelectProxy(ctx, id, "Proxy", "nope")
	wantCode(t, err, protocol.CodeNotFound)
	if r, err := m.UnfixProxy(ctx, id, "Auto"); err != nil || r.Group != "Auto" {
		t.Fatalf("unfix = %+v, %v", r, err)
	}
	if d, err := m.TestDelay(ctx, id, "Proxy", ""); err != nil || len(d) != 2 || d[1].Delay != 0 {
		t.Fatalf("group delay = %+v, %v", d, err)
	}

	if mv, err := m.Mode(ctx, id); err != nil || mv.Mode != protocol.ModeRule || len(mv.Modes) != 3 {
		t.Fatalf("mode = %+v, %v", mv, err)
	}
	if got, err := m.SetMode(ctx, id, protocol.ModeGlobal); err != nil || got != protocol.ModeGlobal {
		t.Fatalf("set mode = %q, %v", got, err)
	}
	_, err = m.SetMode(ctx, id, "sideways")
	wantCode(t, err, protocol.CodeBadRequest)

	pv, err := m.Profiles(ctx, id)
	if err != nil || pv.Active != "p-1" || len(pv.Profiles) != 2 || pv.Profiles[0].Usage == nil || pv.Profiles[1].Usage != nil {
		t.Fatalf("profiles = %+v, %v", pv, err)
	}
	if got, err := m.SelectProfile(ctx, id, "p-2"); err != nil || got != "p-2" {
		t.Fatalf("select profile = %q, %v", got, err)
	}
	if err := m.UpdateProfile(ctx, id, ""); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteProfile(ctx, id, "p-2"); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteProfile(ctx, id, " "); err == nil {
		t.Fatal("deleting an empty profile id must be refused before the agent")
	}

	if pr, err := m.Providers(ctx, id); err != nil || len(pr) != 1 || pr[0].Count != 3 {
		t.Fatalf("providers = %+v, %v", pr, err)
	}
	if err := m.UpdateProvider(ctx, id, protocol.ProviderProxy, "sub"); err != nil {
		t.Fatal(err)
	}
	if err := m.HealthCheckProvider(ctx, id, "sub"); err != nil {
		t.Fatal(err)
	}

	cv, err := m.Connections(ctx, id)
	if err != nil || len(cv.Connections) != 1 || cv.Connections[0].Chains[0] != "HK-01" || cv.DownloadTotal != 20 {
		t.Fatalf("connections = %+v, %v", cv, err)
	}
	if n, err := m.CloseConnections(ctx, id, ""); err != nil || n != 1 {
		t.Fatalf("close all = %d, %v", n, err)
	}
	if tv, err := m.Traffic(ctx, id); err != nil || tv.DownTotal != 4 {
		t.Fatalf("traffic = %+v, %v", tv, err)
	}
	if lv, err := m.Logs(ctx, id, "warning", 5); err != nil || len(lv) != 1 || lv[0].Msg != "level=warning limit=5" {
		t.Fatalf("logs = %+v, %v", lv, err)
	}

	sv, err := m.Settings(ctx, id)
	if err != nil || len(sv) != 2 || sv[1].Options[2] != "debug" || sv[0].Options == nil {
		t.Fatalf("settings = %+v, %v", sv, err)
	}
	if s, err := m.SetSetting(ctx, id, protocol.SettingTUN, true); err != nil || s.Value != true {
		t.Fatalf("set bool = %+v, %v", s, err)
	}
	if s, err := m.SetSetting(ctx, id, protocol.SettingLogLevel, "debug"); err != nil || s.Value != "debug" {
		t.Fatalf("set enum = %+v, %v", s, err)
	}
	_, err = m.SetSetting(ctx, id, protocol.SettingLogLevel, "loud")
	wantCode(t, err, protocol.CodeBadRequest)
	if _, err := m.SetSetting(ctx, id, protocol.SettingTUN, 1.0); err == nil {
		t.Fatal("a value that is neither bool nor string must be refused before the agent")
	}

	if av, err := m.Actions(ctx, id); err != nil || len(av) != 2 || av[1].Label != "Custom" {
		t.Fatalf("actions = %+v, %v", av, err)
	}
	if err := m.RunAction(ctx, id, protocol.ActionFlushDNS); err != nil {
		t.Fatal(err)
	}

	f.hooks.mu.Lock()
	defer f.hooks.mu.Unlock()
	if f.hooks.mode != protocol.ModeGlobal || f.hooks.profiles.Active != "p-2" || !slices.Equal(f.hooks.deleted, []string{"p-2"}) ||
		!slices.Equal(f.hooks.ran, []string{protocol.ActionFlushDNS}) || !slices.Equal(f.hooks.health, []string{"sub"}) {
		t.Fatalf("hooks did not see the buttons: %+v", f.hooks)
	}
}

// A capability the agent does not advertise answers not_found after auth.
func TestRemoteUnadvertisedCapability(t *testing.T) {
	f := newRemoteFixture(t, func(c *agent.Config) {
		c.DisableCapabilities = []string{protocol.CapMode, protocol.CapSettings}
	})
	st, err := f.m.Status(t.Context(), f.agentID)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(st.Capabilities, protocol.CapMode) {
		t.Fatalf("mode still advertised: %v", st.Capabilities)
	}
	_, err = f.m.Mode(t.Context(), f.agentID)
	wantCode(t, err, protocol.CodeNotFound)
	_, err = f.m.SetSetting(t.Context(), f.agentID, protocol.SettingTUN, true)
	wantCode(t, err, protocol.CodeNotFound)
}

func TestRemotePowerIsExplicit(t *testing.T) {
	f := newRemoteFixture(t, nil)
	if _, err := f.m.Power(t.Context(), f.agentID, "toggle"); err == nil {
		t.Fatal("toggle must not be sent")
	}
	if r, err := f.m.Power(t.Context(), f.agentID, protocol.PowerOn); err != nil || r.Power != protocol.PowerOn {
		t.Fatalf("power on = %+v, %v", r, err)
	}
}

func TestRemoteUnpairRevokesOnAgent(t *testing.T) {
	f := newRemoteFixture(t, nil)
	agentErr, err := f.m.Unpair(t.Context(), f.agentID)
	if err != nil || agentErr != nil {
		t.Fatalf("unpair: local %v, agent %v", err, agentErr)
	}
	// The old token is dead on the agent.
	c, err := controller.New(f.host, f.port, f.fp, f.token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Status(t.Context()); !IsUnauthorized(err) {
		t.Fatalf("revoked token still works: %v", err)
	}
	if _, err := f.m.Status(t.Context(), f.agentID); !errors.Is(err, ErrNotPaired) {
		t.Fatalf("forgotten agent: want ErrNotPaired, got %v", err)
	}
}

// Watch: open + status first, changed / log events, and a stop on a revoked
// token after the agent drops the stream.
func TestRemoteWatch(t *testing.T) {
	saved := watchBackoff
	watchBackoff.min, watchBackoff.max = 10*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { watchBackoff = saved })

	f := newRemoteFixture(t, nil)
	events := make(chan EventView, 64)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		defer close(done)
		f.m.Watch(ctx, f.agentID, WatchOptions{Logs: true}, func(e EventView) { events <- e })
	}()

	next := func(want string) EventView {
		t.Helper()
		for {
			select {
			case e := <-events:
				if e.Type == want {
					return e
				}
				// A drop is expected only on the way to unauthorized.
				if e.Type == EventUnauthorized || (e.Type == EventDown && want != EventUnauthorized) {
					t.Fatalf("unexpected %s while waiting for %s: %s", e.Type, want, e.Error)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("no %s event", want)
			}
		}
	}

	next(EventOpen)
	if st := next(protocol.EventStatus); st.Status == nil || st.Status.DeviceID != f.agentID {
		t.Fatalf("first status = %+v", st)
	}
	f.agent.Notify(protocol.ResourceProxies)
	if ch := next(protocol.EventChanged); ch.Resource != protocol.ResourceProxies {
		t.Fatalf("changed = %+v", ch)
	}
	f.agent.PublishLog(protocol.LogLine{Time: 7, Level: "info", Msg: "hello"})
	if l := next(protocol.EventLog); l.Log == nil || l.Log.Msg != "hello" {
		t.Fatalf("log = %+v", l)
	}

	// Revoke on the agent (as its own unpair does): the stream is dropped, the
	// reconnect is refused, and the watch ends.
	c, err := controller.New(f.host, f.port, f.fp, f.token)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Unpair(t.Context()); err != nil {
		t.Fatal(err)
	}
	next(EventUnauthorized)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watch kept running after the token was revoked")
	}
}

func TestRemoteErrorText(t *testing.T) {
	err := remoteErr(&controller.APIError{Status: 404, Code: protocol.CodeNotFound, Message: "no such group"})
	if err.Error() != "[not_found] no such group" {
		t.Fatalf("got %q", err.Error())
	}
	if !IsUnauthorized(remoteErr(&controller.APIError{Status: 401, Code: protocol.CodeUnauthorized})) {
		t.Fatal("unauthorized not recognised")
	}
	plain := errors.New("dial tcp: refused")
	if remoteErr(plain) != plain {
		t.Fatal("non-API errors pass through")
	}
}
