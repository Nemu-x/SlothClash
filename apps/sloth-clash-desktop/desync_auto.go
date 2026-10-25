package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"SlothClashDesktop/internal/desync"
)

type DesyncAutoService struct {
	Route     string `json:"route,omitempty"`
	Phase     string `json:"phase"`
	Candidate string `json:"candidate,omitempty"`
	Attempt   int    `json:"attempt"`
	Total     int    `json:"total"`
	Code      string `json:"code,omitempty"`
	Updated   int64  `json:"updated"`
}
type DesyncAutoAttempt struct {
	At         int64  `json:"at"`
	Network    string `json:"network,omitempty"`
	Service    string `json:"service"`
	Candidate  string `json:"candidate"`
	Target     string `json:"target,omitempty"`
	OK         bool   `json:"ok"`
	Code       string `json:"code,omitempty"`
	DurationMS int64  `json:"durationMS"`
	Sent       int    `json:"sent,omitempty"`
	Received   int    `json:"received,omitempty"`
}
type DesyncAutoStatus struct {
	Running  bool                `json:"running"`
	Network  string              `json:"network,omitempty"`
	YouTube  DesyncAutoService   `json:"youtube"`
	Discord  DesyncAutoService   `json:"discord"`
	Voice    DesyncAutoService   `json:"voice"`
	Attempts []DesyncAutoAttempt `json:"attempts"`
}

// desyncController is the automatic calibration state. Every field is guarded
// by mu; epoch is the session lease that makes a superseded session's writes
// no-ops. App-wide inputs (status, core endpoint, connect generation) are
// passed in by the App methods that drive it.
type desyncController struct {
	mu         sync.Mutex
	epoch      uint64
	generation uint64
	key        string
	cancel     context.CancelFunc
	status     DesyncAutoStatus
	nextTCP    map[string]int
}

func (c *desyncController) snapshot() DesyncAutoStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.status
	out.Attempts = append([]DesyncAutoAttempt{}, out.Attempts...)
	return out
}
func (c *desyncController) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.epoch++
	c.key = ""
	c.status.Running = false
}

// active reports whether a calibration session is (or was last) keyed.
func (c *desyncController) active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.key != ""
}

// begin starts a new session for key under parent, cancelling the previous
// one. It returns ok=false when key is already the running session.
func (c *desyncController) begin(parent context.Context, key string, generation uint64, network string) (session context.Context, lease uint64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == key {
		return nil, 0, false
	}
	if c.cancel != nil {
		c.cancel()
	}
	session, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.key = key
	c.epoch++
	c.generation = generation
	c.nextTCP = map[string]int{}
	lease = c.epoch
	history := c.status.Attempts
	c.status = DesyncAutoStatus{Running: true, Network: network, Attempts: history}
	return session, lease, true
}

func (a *App) GetDesyncAutoStatus() DesyncAutoStatus {
	return a.desyncCtl.snapshot()
}
func (a *App) RetryDesyncAuto() DesyncAutoStatus {
	a.desyncCtl.invalidate()
	return a.GetDesyncAutoStatus()
}
func (c *desyncController) setService(lease uint64, service string, state DesyncAutoService) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != lease {
		return
	}
	state.Updated = time.Now().Unix()
	if state.Route == "" && state.Phase != "disabled" && state.Phase != "needs_tun" {
		state.Route = "desync"
		if state.Phase == "profile" || state.Phase == "profile_failed" {
			state.Route = "profile"
		}
	}
	switch service {
	case "youtube":
		c.status.YouTube = state
	case "discord":
		c.status.Discord = state
	case "voice":
		c.status.Voice = state
	}
}

// recordAttempt appends attempt to the session history (stamped and with its
// target hashed) and returns the stored copy; ok=false for a stale lease.
func (c *desyncController) recordAttempt(lease uint64, attempt DesyncAutoAttempt) (DesyncAutoAttempt, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != lease {
		return attempt, false
	}
	attempt.At = time.Now().Unix()
	attempt.Network = c.status.Network
	if attempt.Target != "" {
		sum := sha256.Sum256([]byte(attempt.Network + attempt.Target))
		attempt.Target = hex.EncodeToString(sum[:6])
	}
	history := append(c.status.Attempts, attempt)
	if len(history) > 128 {
		history = history[len(history)-128:]
	}
	c.status.Attempts = history
	return attempt, true
}
func (a *App) desyncAttempt(lease uint64, attempt DesyncAutoAttempt) {
	attempt, ok := a.desyncCtl.recordAttempt(lease, attempt)
	if !ok {
		return
	}
	a.appendRuntimeDiag("desync.auto", fmt.Sprintf("service=%s candidate=%s ok=%t code=%s duration_ms=%d sent=%d received=%d", attempt.Service, attempt.Candidate, attempt.OK, attempt.Code, attempt.DurationMS, attempt.Sent, attempt.Received))
}

func (a *App) runDesyncAutoLoop(ctx context.Context) {
	timer := time.NewTicker(2 * time.Second)
	defer timer.Stop()
	defer a.desyncCtl.invalidate()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		status := a.GetDesyncStatus()
		if !status.Settings.Enabled || !status.Connected || !strings.EqualFold(status.Mode, "rule") {
			if a.desyncCtl.active() {
				a.desyncCtl.invalidate()
			}
			continue
		}
		network := desyncNetworkKey()
		a.mu.RLock()
		endpoint, secret := a.effectiveCoreEndpointLocked(), a.core.secret
		generation := a.connectGen.Load()
		a.mu.RUnlock()
		key := fmt.Sprintf("%d:%s:%s:%s:%t:%t:%t", generation, endpoint, network, status.Traffic, status.Settings.YouTube, status.Settings.Discord, status.Settings.DiscordUDP)
		session, lease, started := a.desyncCtl.begin(ctx, key, generation, network)
		if !started {
			continue
		}
		go a.runDesyncAutoSession(session, lease, network, endpoint, secret, status)
	}
}

func desyncErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var controller *desyncControllerError
	if errors.As(err, &controller) && controller.Code != "" {
		return controller.Code
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "canceled"):
		return "canceled"
	case strings.Contains(s, "dns") || strings.Contains(s, "resolve") || strings.Contains(s, "find ip"):
		return "dns"
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline"):
		return "timeout"
	case strings.Contains(s, "tls") || strings.Contains(s, "certificate") || strings.Contains(s, "eof"):
		return "tls"
	case strings.Contains(s, "expected status"):
		return "http_status"
	case strings.Contains(s, "no candidate") || strings.Contains(s, "no udp candidate"):
		return "exhausted"
	default:
		return "transport"
	}
}

type desyncControllerError struct {
	Status int
	Code   string
}

func (e *desyncControllerError) Error() string {
	return fmt.Sprintf("core HTTP %d %s", e.Status, e.Code)
}
func autoCoreJSON(ctx context.Context, endpoint, secret, method, route string, payload any, dest any) error {
	var reader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	requestCtx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	response, err := coreDoWithEndpoint(requestCtx, endpoint, secret, method, route, reader)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&failure)
		code := ""
		switch {
		case strings.Contains(failure.Message, "UDP candidate socket failed"):
			code = "native_socket"
		case strings.Contains(failure.Message, "UDP probe canceled or socket failed"):
			code = "probe_socket"
		case strings.Contains(failure.Message, "active public Discord"):
			code = "endpoint_changed"
		case response.StatusCode == 429:
			code = "busy"
		}
		return &desyncControllerError{Status: response.StatusCode, Code: code}
	}
	if dest == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil
	}
	// /connections is polled here too; a busy TUN session outgrows 4 MiB.
	return decodeBoundedJSON(response.Body, method+" "+route, coreJSONLimitForRoute(route), dest)
}
func (a *App) applyDesyncCandidate(ctx context.Context, lease uint64, endpoint, secret, route, name string) error {
	// Serialize the selector mutation with cancellation. A late successful probe
	// cannot change a newer session's route. This does not reload the core.
	return a.desyncCtl.applyCandidate(ctx, lease, a.connectGen.Load, a.GetDesyncStatus(), desyncNetworkKey(), endpoint, secret, route, name)
}

// applyCandidate selects name in route while holding mu, so the lease and
// liveGeneration checks and the selector write cannot interleave with a
// newer session. status and network are sampled by the caller beforehand.
func (c *desyncController) applyCandidate(ctx context.Context, lease uint64, liveGeneration func() uint64, status DesyncStatus, network, endpoint, secret, route, name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != lease || ctx.Err() != nil || c.generation != liveGeneration() || !status.Connected || !status.Settings.Enabled || !strings.EqualFold(status.Mode, "rule") || c.status.Network != network {
		return context.Canceled
	}
	applyCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return autoCoreJSON(applyCtx, endpoint, secret, http.MethodPut, "/proxies/"+url.PathEscape(route), map[string]string{"name": name}, nil)
}

// tcpStart is the rotation offset for service's next TCP trial over n candidates.
func (c *desyncController) tcpStart(service string, n int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nextTCP[service] % n
}

// advanceTCP moves service's rotation past the i-th tried candidate (1-based
// from startAt), keeping a passing one for confirmation. Stale leases are ignored.
func (c *desyncController) advanceTCP(lease uint64, service string, startAt, i, n int, passed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch == lease {
		if c.nextTCP == nil {
			c.nextTCP = map[string]int{}
		}
		next := startAt + i
		if passed {
			next--
		} // keep a passing background candidate for confirmation
		c.nextTCP[service] = next % n
	}
}

// resetTCP restarts service's rotation after a candidate was applied.
func (c *desyncController) resetTCP(lease uint64, service string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch == lease {
		c.nextTCP[service] = 0
	}
}
func waitDesyncGroup(ctx context.Context, endpoint, secret, route string) error {
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		var group struct {
			Type string   `json:"type"`
			All  []string `json:"all"`
		}
		err := autoCoreJSON(waitCtx, endpoint, secret, http.MethodGet, "/proxies/"+url.PathEscape(route), nil, &group)
		if err == nil && group.Type == "Selector" && len(group.All) > 0 {
			return nil
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return waitCtx.Err()
		case <-timer.C:
		}
	}
}
func desyncTargets(service string) []string {
	if service == "youtube" {
		return []string{"https://www.youtube.com/generate_204"}
	}
	return []string{"https://discord.com/api/v10/gateway", "https://cdn.discordapp.com/embed/avatars/0.png"}
}

// Stop at the first candidate which passes two independent rounds, rather than
// rewarding a single warm connection or a filtered site's error page.
func chooseDesyncTCP(ctx context.Context, service string, candidates []desync.Candidate, probe func(context.Context, desync.Candidate, string) error, record func(desync.Candidate, int, error, time.Duration)) (desync.Candidate, error) {
	for i, c := range candidates {
		if ctx.Err() != nil {
			return desync.Candidate{}, ctx.Err()
		}
		start := time.Now()
		var err error
		for round := 0; round < 2 && err == nil; round++ {
			for _, target := range desyncTargets(service) {
				one, cancel := context.WithTimeout(ctx, 5*time.Second)
				err = probe(one, c, target)
				cancel()
				if err != nil {
					break
				}
			}
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		record(c, i+1, err, time.Since(start))
		if ctx.Err() != nil {
			return desync.Candidate{}, ctx.Err()
		}
		if err == nil {
			return c, nil
		}
	}
	return desync.Candidate{}, fmt.Errorf("no candidate passed")
}
func (a *App) findDesyncTCP(ctx context.Context, lease uint64, network, endpoint, secret, service, route string, background bool) (desync.Candidate, error) {
	candidates := desync.Ordered(service, desyncCachedSelection(network, service))
	startAt := a.desyncCtl.tcpStart(service, len(candidates))
	candidates = append(append([]desync.Candidate{}, candidates[startAt:]...), candidates[:startAt]...)
	if !background {
		a.desyncCtl.setService(lease, service, DesyncAutoService{Phase: "testing", Total: len(candidates)})
	}
	if err := waitDesyncGroup(ctx, endpoint, secret, route); err != nil {
		if !background {
			a.desyncCtl.setService(lease, service, DesyncAutoService{Phase: "failed", Code: desyncErrorCode(err)})
		}
		return desync.Candidate{}, err
	}
	trial, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	winner, err := chooseDesyncTCP(trial, service, candidates, func(one context.Context, c desync.Candidate, target string) error {
		_, e := probeDesyncOutbound(one, endpoint, secret, c.Name(route), target)
		return e
	}, func(c desync.Candidate, i int, e error, d time.Duration) {
		a.desyncCtl.advanceTCP(lease, service, startAt, i, len(candidates), e == nil)
		if !background {
			a.desyncCtl.setService(lease, service, DesyncAutoService{Phase: "testing", Candidate: c.ID, Attempt: i, Total: len(candidates), Code: desyncErrorCode(e)})
		}
		a.desyncAttempt(lease, DesyncAutoAttempt{Service: service, Candidate: c.ID, OK: e == nil, Code: desyncErrorCode(e), DurationMS: d.Milliseconds()})
	})
	if err != nil {
		if !background {
			a.desyncCtl.setService(lease, service, DesyncAutoService{Phase: "failed", Code: desyncErrorCode(err)})
		}
		return desync.Candidate{}, err
	}
	return winner, nil
}

func (a *App) calibrateDesyncTCP(ctx context.Context, lease uint64, network, endpoint, secret, service, route string) error {
	winner, err := a.findDesyncTCP(ctx, lease, network, endpoint, secret, service, route, false)
	if err != nil {
		return err
	}
	if err = a.applyDesyncCandidate(ctx, lease, endpoint, secret, route, winner.Name(route)); err != nil {
		a.desyncCtl.setService(lease, service, DesyncAutoService{Phase: "failed", Code: desyncErrorCode(err)})
		return err
	}
	saveDesyncSelection(network, service, winner.ID)
	a.desyncCtl.resetTCP(lease, service)
	a.desyncCtl.setService(lease, service, DesyncAutoService{Phase: "ready", Candidate: winner.ID})
	return nil
}

type desyncUDPActivity struct {
	Candidate   string `json:"-"`
	Endpoint    string `json:"endpoint"`
	FirstMS     int64  `json:"firstMS"`
	LastTXMS    int64  `json:"lastTXMS"`
	LastRXMS    int64  `json:"lastRXMS"`
	TXPackets   uint64 `json:"txPackets"`
	RXPackets   uint64 `json:"rxPackets"`
	TXBytes     uint64 `json:"txBytes"`
	RXBytes     uint64 `json:"rxBytes"`
	DiscoveryTX uint64 `json:"discoveryTX"`
	DiscoveryRX uint64 `json:"discoveryRX"`
	MediaTX     uint64 `json:"mediaTX"`
	MediaRX     uint64 `json:"mediaRX"`
}
type desyncUDPProxy struct {
	Now string `json:"now"`
	UDP *struct {
		Counters DesyncUDPCounters         `json:"counters"`
		Activity []desyncUDPActivity       `json:"activity"`
		Binding  *DesyncUDPBindingCounters `json:"binding,omitempty"`
	} `json:"desyncUDP"`
}

// Explicit numeric allowlist: no endpoint, SSRC, local address or process path.
type DesyncUDPBindingCounters struct {
	Enabled        bool   `json:"enabled"`
	NativeSockets  uint64 `json:"nativeSockets"`
	ActiveSockets  int    `json:"activeSockets"`
	Sessions       uint64 `json:"sessions"`
	ActiveSessions int    `json:"activeSessions"`
	Reuses         uint64 `json:"reuses"`
	Handovers      uint64 `json:"handovers"`
	Dropped        uint64 `json:"dropped"`
	Errors         uint64 `json:"errors"`
	QueuedBytes    int    `json:"queuedBytes"`
}
type desyncUDPProbe struct {
	Sent               int   `json:"sent"`
	Received           int   `json:"received"`
	MaxConsecutiveLoss int   `json:"maxConsecutiveLoss"`
	DurationMS         int64 `json:"durationMS"`
}

func (p desyncUDPProbe) passed() bool {
	return p.Sent == 12 && p.Received >= 10 && p.MaxConsecutiveLoss <= 1 && p.DurationMS >= 1500
}
func desyncVoiceTarget(connections []ConnectionItem) string {
	var picked ConnectionItem
	found := false
	for _, c := range connections {
		if !isDiscordUDPConnection(c) || c.Metadata.DestinationPort == "443" {
			continue
		}
		direct := false
		for _, chain := range c.Chains {
			if chain == desync.DiscordUDPRoute {
				direct = true
			}
		}
		addr, err := netip.ParseAddrPort(netipEndpoint(c.Metadata.DestinationIP, c.Metadata.DestinationPort))
		if err != nil || !addr.Addr().IsGlobalUnicast() || addr.Addr().IsPrivate() || addr.Port() < 1024 || !direct {
			continue
		}
		if !found || c.Start > picked.Start || (c.Start == picked.Start && c.Upload+c.Download > picked.Upload+picked.Download) {
			picked = c
			found = true
		}
	}
	if !found {
		return ""
	}
	return netipEndpoint(picked.Metadata.DestinationIP, picked.Metadata.DestinationPort)
}
func netipEndpoint(ip, port string) string {
	if strings.Contains(ip, ":") {
		return "[" + ip + "]:" + port
	}
	return ip + ":" + port
}

// No incoming media can mean a silent channel. Reselect only when all inbound
// UDP progress is absent despite ongoing sends, after the reconnect grace.
func desyncVoiceHealth(now time.Time, p desyncUDPActivity, stableSamples, mediaSamples int, graceUntil time.Time) string {
	millis := now.UnixMilli()
	if millis-p.LastTXMS < 10000 && p.TXPackets >= 5 && millis-p.FirstMS > 22000 && millis-p.LastRXMS > 20000 && now.After(graceUntil) {
		return "stalled"
	}
	if mediaSamples >= 3 && millis-p.LastRXMS < 10000 && p.MediaTX >= 3 && p.MediaRX >= 3 && p.TXBytes >= 512 && p.RXBytes >= 512 {
		return "media"
	}
	if stableSamples >= 3 && millis-p.LastRXMS < 10000 && p.RXPackets > p.DiscoveryRX+2 {
		return "transport"
	}
	return "observing"
}
func (a *App) calibrateDesyncUDP(ctx context.Context, lease uint64, network, endpoint, secret, target string, rejected map[string]bool) (string, bool, error) {
	candidates := desync.Ordered("voice", desyncCachedSelection(network, "voice"))
	var group desyncUDPProxy
	if err := autoCoreJSON(ctx, endpoint, secret, http.MethodGet, "/proxies/"+desync.DiscordUDPRoute, nil, &group); err != nil {
		return "", false, err
	}
	trial, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for i, c := range candidates {
		if rejected[c.ID] {
			continue
		}
		if trial.Err() != nil {
			return "", false, trial.Err()
		}
		a.desyncCtl.setService(lease, "voice", DesyncAutoService{Phase: "testing", Candidate: c.ID, Attempt: i + 1, Total: len(candidates)})
		var result desyncUDPProbe
		start := time.Now()
		err := autoCoreJSON(trial, endpoint, secret, http.MethodPost, "/proxies/"+c.Name(desync.DiscordUDPRoute)+"/desync/udp-probe", map[string]string{"target": target}, &result)
		passed := err == nil && result.passed()
		code := desyncErrorCode(err)
		if err == nil && !passed {
			code = "udp_loss"
		}
		a.desyncAttempt(lease, DesyncAutoAttempt{Service: "voice", Candidate: c.ID, Target: target, OK: passed, Code: code, DurationMS: time.Since(start).Milliseconds(), Sent: result.Sent, Received: result.Received})
		if trial.Err() != nil {
			return "", false, trial.Err()
		}
		if err != nil {
			var controller *desyncControllerError
			if errors.As(err, &controller) && (controller.Status == 400 || controller.Code == "busy" || controller.Code == "native_socket" || controller.Code == "probe_socket") {
				return "", false, err
			}
		}
		if !passed {
			rejected[c.ID] = true
			continue
		}
		// Application traffic may have recovered during the bounded trial.
		// Never replace a live call/stream because a synthetic probe passed.
		var current desyncUDPProxy
		if err := autoCoreJSON(ctx, endpoint, secret, http.MethodGet, "/proxies/"+url.PathEscape(group.Now), nil, &current); err != nil {
			return "", false, err
		}
		if current.UDP != nil {
			for _, p := range current.UDP.Activity {
				if time.Now().UnixMilli()-p.LastRXMS < 10000 && p.RXPackets > p.DiscoveryRX+2 {
					return group.Now, false, nil
				}
			}
		}
		if err = a.applyDesyncCandidate(ctx, lease, endpoint, secret, desync.DiscordUDPRoute, c.Name(desync.DiscordUDPRoute)); err != nil {
			return "", false, err
		}
		return c.Name(desync.DiscordUDPRoute), group.Now != c.Name(desync.DiscordUDPRoute), nil
	}
	return "", false, fmt.Errorf("no UDP candidate passed")
}
func closeStaleDesyncVoice(ctx context.Context, endpoint, secret, target, selected string, connections []ConnectionItem) {
	for _, c := range connections {
		if !isDiscordUDPConnection(c) || netipEndpoint(c.Metadata.DestinationIP, c.Metadata.DestinationPort) != target {
			continue
		}
		current := false
		owned := false
		for _, chain := range c.Chains {
			if chain == selected {
				current = true
			}
			if chain == desync.DiscordUDPRoute {
				owned = true
			}
		}
		if !owned || current || c.ID == "" {
			continue
		}
		one, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = autoCoreJSON(one, endpoint, secret, http.MethodDelete, "/connections/"+url.PathEscape(c.ID), nil, nil)
		cancel()
	}
}
func (a *App) runDesyncAutoSession(ctx context.Context, lease uint64, network, endpoint, secret string, status DesyncStatus) {
	for _, service := range []struct {
		key, route string
		enabled    bool
	}{{"youtube", desync.YouTubeRoute, status.Settings.YouTube}, {"discord", desync.DiscordRoute, status.Settings.Discord}} {
		if service.enabled {
			go a.runDesyncTCPService(ctx, lease, network, endpoint, secret, service.key, service.route)
		} else {
			a.desyncCtl.setService(lease, service.key, DesyncAutoService{Phase: "disabled"})
		}
	}
	if !status.Settings.DiscordUDP {
		a.desyncCtl.setService(lease, "voice", DesyncAutoService{Phase: "disabled"})
	} else if status.Traffic != "tun" {
		a.desyncCtl.setService(lease, "voice", DesyncAutoService{Phase: "needs_tun"})
	}
	if status.Settings.DiscordUDP && status.Traffic == "tun" {
		a.runDesyncVoiceService(ctx, lease, network, endpoint, secret)
	} else {
		<-ctx.Done()
	}
}

// A separate allowlist makes the copied report safe to send to trial testers.
// It intentionally does not serialize AppState, profiles, raw errors or logs.
func (a *App) GetDesyncDiagnosticReport() string {
	status := a.GetDesyncStatus()
	auto := a.GetDesyncAutoStatus()
	info := coreBuildInfoFromFS(a.bundle)
	coreSHA := ""
	if data, e := a.bundle.ReadFile(embeddedCoreBuildFile); e == nil {
		var metadata struct {
			SHA string `json:"sha256"`
		}
		if json.Unmarshal(data, &metadata) == nil {
			if decoded, e := hex.DecodeString(metadata.SHA); e == nil && len(decoded) == 32 {
				coreSHA = metadata.SHA
			}
		}
	}
	report := map[string]any{"format": "sloth-desync-1", "at": time.Now().UTC().Format(time.RFC3339), "appVersion": AppVersion, "platform": runtime.GOOS, "arch": runtime.GOARCH, "coreVersion": info.Version, "coreSHA256": coreSHA, "features": info.Features, "connected": status.Connected, "mode": status.Mode, "traffic": status.Traffic, "services": map[string]bool{"enabled": status.Settings.Enabled, "youtube": status.Settings.YouTube, "discord": status.Settings.Discord, "voice": status.Settings.DiscordUDP}, "automatic": auto}
	report["controllerRevision"] = 3
	if build, ok := debug.ReadBuildInfo(); ok {
		report["goVersion"] = build.GoVersion
		for _, s := range build.Settings {
			if s.Key == "vcs.revision" {
				if decoded, e := hex.DecodeString(s.Value); e == nil && len(decoded) == 20 {
					report["clientRevision"] = s.Value
				}
			}
		}
	}
	a.mu.RLock()
	endpoint, secret := a.effectiveCoreEndpointLocked(), a.core.secret
	a.mu.RUnlock()
	if status.Connected && status.Settings.DiscordUDP {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		var overview struct {
			Connections []ConnectionItem `json:"connections"`
		}
		if autoCoreJSON(ctx, endpoint, secret, http.MethodGet, "/connections", nil, &overview) == nil {
			flows := []map[string]any{}
			for _, c := range overview.Connections {
				if !isDiscordUDPConnection(c) || len(flows) >= 64 {
					continue
				}
				sum := sha256.Sum256([]byte(auto.Network + netipEndpoint(c.Metadata.DestinationIP, c.Metadata.DestinationPort)))
				route, candidate := "profile", ""
				for _, name := range c.Chains {
					if name == desync.DiscordUDPRoute {
						route = "desync"
					}
					if knownDesyncVoiceCandidate(name) {
						candidate = strings.TrimPrefix(name, desync.DiscordUDPRoute+"-")
					}
				}
				flows = append(flows, map[string]any{"endpoint": hex.EncodeToString(sum[:6]), "route": route, "candidate": candidate, "upload": c.Upload, "download": c.Download})
			}
			report["udpFlows"] = flows
		}
		var group desyncUDPProxy
		if autoCoreJSON(ctx, endpoint, secret, http.MethodGet, "/proxies/"+desync.DiscordUDPRoute, nil, &group) == nil && strings.HasPrefix(group.Now, desync.DiscordUDPRoute+"-") {
			var proxy desyncUDPProxy
			if autoCoreJSON(ctx, endpoint, secret, http.MethodGet, "/proxies/"+url.PathEscape(group.Now), nil, &proxy) == nil && proxy.UDP != nil {
				for i := range proxy.UDP.Activity {
					sum := sha256.Sum256([]byte(auto.Network + proxy.UDP.Activity[i].Endpoint))
					proxy.UDP.Activity[i].Endpoint = hex.EncodeToString(sum[:6])
				}
				report["udp"] = proxy.UDP
			}
		}
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "Desync report unavailable"
	}
	return string(raw)
}
