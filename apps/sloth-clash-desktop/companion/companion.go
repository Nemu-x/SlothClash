package companion

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Nemu-x/clash-companion/go/controller"
	"github.com/Nemu-x/clash-companion/go/discovery"
	"github.com/Nemu-x/clash-companion/go/pairing"
	"github.com/Nemu-x/clash-companion/go/protocol"
)

// controllerName is how this controller identifies itself to agents during
// PIN pairing (shown on the agent side).
const controllerName = "SlothClash"

// resolveTimeout bounds the mDNS re-resolution done before control calls so an
// agent that moved ports is found, without making every call wait on a full
// browse.
const resolveTimeout = 1500 * time.Millisecond

// AgentInfo is the controller-facing, JSON-friendly view of an agent (paired
// and/or currently discovered).
type AgentInfo struct {
	DeviceID  string `json:"deviceId"`
	Name      string `json:"name"`
	App       string `json:"app"`
	Ver       int    `json:"ver"`
	FP        string `json:"fp"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Paired    bool   `json:"paired"`
	Reachable bool   `json:"reachable"` // seen on the LAN in the latest discovery
	Supported bool   `json:"supported"` // protocol major version matches ours
}

// StatusView is the controller-facing view of an agent's status (JSON-friendly,
// no external types leak into the Wails bindings).
type StatusView struct {
	DeviceID     string   `json:"deviceId"`
	Name         string   `json:"name"`
	App          string   `json:"app"`
	Ver          int      `json:"ver"`
	Power        string   `json:"power"`
	Capabilities []string `json:"capabilities"`
}

// PowerResult is the result of a power action.
type PowerResult struct {
	OK    bool   `json:"ok"`
	Power string `json:"power"`
}

// Manager owns discovery, pairing, the paired-agent store, and control. It is a
// thin wrapper over clash-companion/go.
type Manager struct {
	mu       sync.Mutex
	store    *Store
	tokens   TokenStore
	lastSeen map[string]discovery.Entry // deviceId -> latest discovery entry
}

// NewManager loads the agent store from stateDir and wires the token store.
func NewManager(stateDir string, tokens TokenStore) (*Manager, error) {
	st, err := LoadStore(stateDir)
	if err != nil {
		return nil, err
	}
	if tokens == nil {
		tokens = NewKeyringTokenStore()
	}
	return &Manager{store: st, tokens: tokens, lastSeen: map[string]discovery.Entry{}}, nil
}

// DeviceID is this controller's stable identifier.
func (m *Manager) DeviceID() string { return m.store.DeviceID() }

// Discover browses the LAN for agents and returns the merged paired+discovered
// list. It also refreshes each paired agent's last-known address.
func (m *Manager) Discover(ctx context.Context) ([]AgentInfo, error) {
	entries, err := discovery.Browse(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.lastSeen = map[string]discovery.Entry{}
	for _, e := range entries {
		if e.ID == "" {
			continue
		}
		m.lastSeen[e.ID] = e
		// keep the persisted address fresh for paired agents
		if a, ok := m.store.Get(e.ID); ok && e.Host != "" && e.Port != 0 &&
			(a.LastHost != e.Host || a.LastPort != e.Port) {
			a.LastHost, a.LastPort = e.Host, e.Port
			a.Ver = e.Ver
			_ = m.store.Upsert(a)
		}
	}
	m.mu.Unlock()
	return m.snapshot(), nil
}

// ListAgents returns the merged view from the store + the latest discovery
// (without re-browsing).
func (m *Manager) ListAgents() []AgentInfo { return m.snapshot() }

func (m *Manager) snapshot() []AgentInfo {
	m.mu.Lock()
	seen := make(map[string]discovery.Entry, len(m.lastSeen))
	for k, v := range m.lastSeen {
		seen[k] = v
	}
	m.mu.Unlock()

	out := []AgentInfo{}
	added := map[string]bool{}
	for _, a := range m.store.List() {
		info := AgentInfo{
			DeviceID: a.DeviceID, Name: a.Name, App: a.App, Ver: a.Ver, FP: a.FP,
			Host: a.LastHost, Port: a.LastPort, Paired: true, Supported: true,
		}
		if e, ok := seen[a.DeviceID]; ok {
			info.Reachable = true
			info.Host, info.Port, info.Ver = e.Host, e.Port, e.Ver
			info.Supported = !e.MajorMismatch()
		}
		out = append(out, info)
		added[a.DeviceID] = true
	}
	for id, e := range seen {
		if added[id] {
			continue
		}
		out = append(out, AgentInfo{
			DeviceID: id, Name: e.Name, App: e.App, Ver: e.Ver, FP: e.FP,
			Host: e.Host, Port: e.Port, Reachable: true, Supported: !e.MajorMismatch(),
		})
	}
	return out
}

// PairByString pairs from a pasted clashctl-pair://… string (carries
// ip+port+fp+token; works without mDNS).
func (m *Manager) PairByString(s string) (AgentInfo, error) {
	p, err := pairing.Decode(strings.TrimSpace(s))
	if err != nil {
		return AgentInfo{}, fmt.Errorf("invalid pairing string: %w", err)
	}
	if p.ID == "" || p.Token == "" || p.FP == "" {
		return AgentInfo{}, errors.New("pairing string is missing required fields")
	}
	a := Agent{DeviceID: p.ID, Name: p.Name, App: p.App, FP: p.FP, LastHost: p.IP, LastPort: p.Port}
	m.mu.Lock()
	if e, ok := m.lastSeen[p.ID]; ok {
		a.Ver = e.Ver
	}
	m.mu.Unlock()
	if err := m.tokens.Set(p.ID, p.Token); err != nil {
		return AgentInfo{}, fmt.Errorf("store token: %w", err)
	}
	if err := m.store.Upsert(a); err != nil {
		return AgentInfo{}, err
	}
	return m.agentInfo(a), nil
}

// PairByPin pairs with a discovered agent using the 6-digit PIN it displays.
func (m *Manager) PairByPin(ctx context.Context, deviceID, pin string) (AgentInfo, error) {
	e, ok := m.entryFor(ctx, deviceID)
	if !ok {
		return AgentInfo{}, fmt.Errorf("agent %s not found on the network", deviceID)
	}
	token, err := controller.PairWithPIN(ctx, e.Host, e.Port, e.FP, strings.TrimSpace(pin), m.store.DeviceID(), controllerName)
	if err != nil {
		return AgentInfo{}, err
	}
	a := Agent{DeviceID: e.ID, Name: e.Name, App: e.App, Ver: e.Ver, FP: e.FP, LastHost: e.Host, LastPort: e.Port}
	if err := m.tokens.Set(e.ID, token); err != nil {
		return AgentInfo{}, fmt.Errorf("store token: %w", err)
	}
	if err := m.store.Upsert(a); err != nil {
		return AgentInfo{}, err
	}
	return m.agentInfo(a), nil
}

// Status / Power / ShareSubscription / Rename / Unpair — control a paired agent.

func (m *Manager) Status(ctx context.Context, deviceID string) (*StatusView, error) {
	c, err := m.clientFor(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	st, err := c.Status(ctx)
	if err != nil {
		return nil, err
	}
	return &StatusView{
		DeviceID: st.ID, Name: st.Name, App: st.App, Ver: st.Ver,
		Power: st.Power, Capabilities: st.Capabilities,
	}, nil
}

func (m *Manager) Power(ctx context.Context, deviceID, action string) (*PowerResult, error) {
	switch action {
	case protocol.PowerOn, protocol.PowerOff, "toggle":
	default:
		return nil, fmt.Errorf("invalid power action %q", action)
	}
	c, err := m.clientFor(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	pr, err := c.Power(ctx, action)
	if err != nil {
		return nil, err
	}
	return &PowerResult{OK: pr.OK, Power: pr.Power}, nil
}

func (m *Manager) ShareSubscription(ctx context.Context, deviceID, url, name string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return errors.New("no active subscription URL to share")
	}
	c, err := m.clientFor(ctx, deviceID)
	if err != nil {
		return err
	}
	return c.ImportSubscription(ctx, protocol.SubscriptionRequest{URL: url, Name: name})
}

func (m *Manager) Rename(ctx context.Context, deviceID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name must not be empty")
	}
	c, err := m.clientFor(ctx, deviceID)
	if err != nil {
		return err
	}
	if _, err := c.Rename(ctx, name); err != nil {
		return err
	}
	if a, ok := m.store.Get(deviceID); ok {
		a.Name = name
		_ = m.store.Upsert(a)
	}
	return nil
}

// Unpair removes the agent and deletes its token from the keychain.
func (m *Manager) Unpair(deviceID string) error {
	if err := m.tokens.Delete(deviceID); err != nil {
		return err
	}
	return m.store.Delete(deviceID)
}

// clientFor builds a pinned-TLS client for a paired agent, re-resolving its
// address by deviceId first (ports can change across agent restarts).
func (m *Manager) clientFor(ctx context.Context, deviceID string) (*controller.Client, error) {
	a, ok := m.store.Get(deviceID)
	if !ok {
		return nil, fmt.Errorf("agent %s is not paired", deviceID)
	}
	if e, ok := m.entryFor(ctx, deviceID); ok && e.Host != "" && e.Port != 0 &&
		(e.Host != a.LastHost || e.Port != a.LastPort) {
		a.LastHost, a.LastPort = e.Host, e.Port
		_ = m.store.Upsert(a)
	}
	if a.LastHost == "" || a.LastPort == 0 {
		return nil, fmt.Errorf("agent %s has no known address; discover it first", deviceID)
	}
	token, err := m.tokens.Get(deviceID)
	if err != nil {
		return nil, err
	}
	return controller.New(a.LastHost, a.LastPort, a.FP, token)
}

// entryFor returns the agent's discovery entry, from the latest browse cache or
// a short fresh lookup by deviceId.
func (m *Manager) entryFor(ctx context.Context, deviceID string) (discovery.Entry, bool) {
	m.mu.Lock()
	e, ok := m.lastSeen[deviceID]
	m.mu.Unlock()
	if ok {
		return e, true
	}
	rctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	e, found, err := discovery.FindByDeviceID(rctx, deviceID)
	if err != nil || !found {
		return discovery.Entry{}, false
	}
	m.mu.Lock()
	m.lastSeen[deviceID] = e
	m.mu.Unlock()
	return e, true
}

func (m *Manager) agentInfo(a Agent) AgentInfo {
	info := AgentInfo{
		DeviceID: a.DeviceID, Name: a.Name, App: a.App, Ver: a.Ver, FP: a.FP,
		Host: a.LastHost, Port: a.LastPort, Paired: true, Supported: true,
	}
	m.mu.Lock()
	if e, ok := m.lastSeen[a.DeviceID]; ok {
		info.Reachable = true
		info.Supported = !e.MajorMismatch()
	}
	m.mu.Unlock()
	return info
}
