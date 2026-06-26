// Package companion is SlothClash's controller side of the clashctl LAN
// companion protocol: discover LAN agents (e.g. ClashFest on a TV), pair with
// them, and control them. It is a thin wrapper over
// github.com/Nemu-x/clash-companion/go — it does NOT reimplement the protocol,
// TLS pinning, or the encodings; interop is guaranteed by depending on that
// module (vector-tested upstream). See architecture/lan-companion-controller.md.
package companion

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/Nemu-x/clash-companion/go/pairing"
	"github.com/zalando/go-keyring"
)

// Agent is the persisted, non-secret metadata for a paired agent. The bearer
// token is NEVER stored here — it lives in the OS keychain (see TokenStore).
type Agent struct {
	DeviceID string `json:"deviceId"`
	Name     string `json:"name"`
	App      string `json:"app"`
	Ver      int    `json:"ver"`
	FP       string `json:"fp"`
	LastHost string `json:"lastHost"`
	LastPort int    `json:"lastPort"`
}

// Store persists the controller's own deviceId and the set of paired agents to
// a JSON file. Concurrency-safe.
type Store struct {
	mu       sync.Mutex
	path     string
	deviceID string
	agents   map[string]Agent
}

type storeFile struct {
	DeviceID string  `json:"deviceId"`
	Agents   []Agent `json:"agents"`
}

// LoadStore reads (or initialises) the agent store at <stateDir>/companion.json
// and ensures a stable controller deviceId exists.
func LoadStore(stateDir string) (*Store, error) {
	s := &Store{
		path:   filepath.Join(stateDir, "companion.json"),
		agents: map[string]Agent{},
	}
	b, err := os.ReadFile(s.path)
	if err == nil {
		var f storeFile
		if jerr := json.Unmarshal(b, &f); jerr == nil {
			s.deviceID = f.DeviceID
			for _, a := range f.Agents {
				if a.DeviceID != "" {
					s.agents[a.DeviceID] = a
				}
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if s.deviceID == "" {
		id, gerr := pairing.NewDeviceID()
		if gerr != nil {
			return nil, gerr
		}
		s.deviceID = id
		if serr := s.saveLocked(); serr != nil {
			return nil, serr
		}
	}
	return s, nil
}

// DeviceID returns the controller's own stable identifier.
func (s *Store) DeviceID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deviceID
}

// Upsert inserts or updates a paired agent.
func (s *Store) Upsert(a Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[a.DeviceID] = a
	return s.saveLocked()
}

// Get returns a paired agent by deviceId.
func (s *Store) Get(deviceID string) (Agent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[deviceID]
	return a, ok
}

// List returns all paired agents.
func (s *Store) List() []Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Agent, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, a)
	}
	return out
}

// Delete removes a paired agent.
func (s *Store) Delete(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.agents, deviceID)
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	f := storeFile{DeviceID: s.deviceID}
	for _, a := range s.agents {
		f.Agents = append(f.Agents, a)
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// TokenStore stores per-agent bearer tokens. Tokens MUST NOT be written to logs
// or to the plaintext agent store; the production impl uses the OS keychain.
type TokenStore interface {
	Get(deviceID string) (string, error)
	Set(deviceID, token string) error
	Delete(deviceID string) error
}

// ErrTokenNotFound is returned when no token is stored for a deviceId.
var ErrTokenNotFound = errors.New("companion: token not found")

const keyringService = "SlothClash.companion"

// keyringTokenStore stores tokens in the OS keychain (Windows Credential
// Manager / macOS Keychain / Linux Secret Service) via go-keyring.
type keyringTokenStore struct{}

// NewKeyringTokenStore returns the OS-keychain-backed token store.
func NewKeyringTokenStore() TokenStore { return keyringTokenStore{} }

func (keyringTokenStore) Get(deviceID string) (string, error) {
	t, err := keyring.Get(keyringService, deviceID)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrTokenNotFound
	}
	return t, err
}

func (keyringTokenStore) Set(deviceID, token string) error {
	return keyring.Set(keyringService, deviceID, token)
}

func (keyringTokenStore) Delete(deviceID string) error {
	err := keyring.Delete(keyringService, deviceID)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
