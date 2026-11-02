package citruslab

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The UDP block: what a voice call (Discord: DTLS + RTP over UDP) needs from a
// tunnel, on every inbound a Citrus subscription hands out for UDP. Sizes up
// to 1400 bytes (DTLS handshake records are large), a fresh association per
// call, several calls at once, a call that goes quiet for 35 s and resumes,
// and a burst of small packets with bounded loss. The relay must happen as
// UDP on the server (`[UDP]` in its log), not over a TCP fallback.

type udpProto struct {
	name   string
	server func(f *fixtures) obj
	client func(f *fixtures) obj
}

func udpProtos() []udpProto {
	pinned := func(o obj) obj { return merge(obj{"sni": realitySNI, "fingerprint": tlsCertSHA256, "udp": true}, o) }
	return []udpProto{
		{"vless-reality-vision",
			func(f *fixtures) obj { return f.realityServer(nil) },
			func(f *fixtures) obj { return f.realityClient(nil) }},
		{"vless-xhttp-stream-one-tls",
			func(f *fixtures) obj {
				return tlsServer(obj{"type": "vless", "users": []obj{{"username": "alice", "uuid": f.idA}},
					"xhttp-config": obj{"path": "/xh", "mode": "auto"}})
			},
			func(f *fixtures) obj {
				return tlsClient(obj{"type": "vless", "uuid": f.idA, "network": "xhttp", "alpn": []string{"h2"}, "udp": true,
					"xhttp-opts": obj{"path": "/xh", "mode": "stream-one"}})
			}},
		{"hysteria2",
			func(f *fixtures) obj { return tlsServer(obj{"type": "hysteria2", "users": obj{"alice": f.pw}}) },
			func(f *fixtures) obj { return pinned(obj{"type": "hysteria2", "password": f.pw}) }},
		{"tuic-v5",
			func(f *fixtures) obj {
				return tlsServer(obj{"type": "tuic", "users": obj{f.idA: f.pw}, "alpn": []string{"h3"}})
			},
			func(f *fixtures) obj {
				return pinned(obj{"type": "tuic", "uuid": f.idA, "password": f.pw, "alpn": []string{"h3"}})
			}},
		{"mieru-udp",
			func(f *fixtures) obj {
				return obj{"type": "mieru", "transport": "UDP", "users": obj{"alice": f.pw}}
			},
			func(f *fixtures) obj {
				return obj{"type": "mieru", "transport": "UDP", "username": "alice", "password": f.pw, "udp": true}
			}},
		{"anytls",
			func(f *fixtures) obj { return tlsServer(obj{"type": "anytls", "users": obj{"alice": f.pw}}) },
			func(f *fixtures) obj { return pinned(obj{"type": "anytls", "password": f.pw}) }},
	}
}

// udpCell starts the Citrus server and the core for one protocol and column.
func udpCell(f *fixtures, p udpProto, col column, band int, group string) (*cellCtx, int, error) {
	c := &cellCtx{
		col:  col,
		dir:  filepath.Join(logDir, group, safeName(p.name), string(col)),
		band: newBand(band),
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return c, 0, err
	}
	sport, err := c.server(p.server(f))
	if err != nil {
		return c, 0, fmt.Errorf("server setup: %w", err)
	}
	_, mp, err := c.startCore(obj{"proxies": []obj{proxy("p", sport, p.client(f))}})
	if err != nil {
		return c, 0, fmt.Errorf("core setup: %w", err)
	}
	return c, mp, nil
}

func TestCitrusUDP(t *testing.T) {
	needLab(t)
	f, err := labFixtures.get()
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		name string
		run  func(mp int) (string, error)
	}{
		{"sizes-74-1200-1400", udpCheckSizes},
		{"sequential-12-sessions", udpCheckSequential},
		{"concurrent-8-sessions", udpCheckConcurrent},
		{"burst-200", func(mp int) (string, error) { return udpCheckBurst(mp, time.Millisecond) }},
	}
	// serial: a UDP verdict must not depend on what else the runner is doing
	for i, p := range udpProtos() {
		for ci, col := range columns() {
			t.Run(p.name+"/"+string(col), func(t *testing.T) {
				c, mp, err := udpCell(f, p, col, 120+i*2+ci, "udp")
				defer c.stopAll()
				if err != nil {
					t.Logf("evidence:\n%s", c.evidence())
					t.Fatal(err)
				}
				for _, ch := range checks {
					stats, err := ch.run(mp)
					mark := "  "
					if err != nil {
						// a failure counts only if it reproduces on the same tunnel
						first := err
						stats, err = ch.run(mp)
						mark = "~~"
						if err != nil {
							mark = "!!"
						}
						t.Logf("%s %s first round: %v", mark, ch.name, first)
					}
					t.Logf("%s %-24s %s", mark, ch.name, stats)
					if err != nil {
						t.Errorf("%s: %v", ch.name, err)
					}
				}
				// measured, not asserted: the same burst with no spacing at all
				// mostly measures socket buffers, but a protocol that loses a
				// large share of it is worth seeing next to the paced one
				stats, err := udpCheckBurst(mp, 0)
				t.Logf("   %-24s %s (not asserted: %v)", "burst-200-unpaced", stats, err)
				if !c.lastServer().has("[UDP] ") || !c.lastServer().has("--> "+udpEchoAddr) {
					t.Errorf("the Citrus server never relayed the datagrams as UDP to %s", udpEchoAddr)
				}
				if t.Failed() {
					t.Logf("evidence:\n%s", c.evidence())
				}
			})
		}
	}
}

func udpCheckSizes(mp int) (string, error) {
	s, err := openUDP(mp)
	if err != nil {
		return "", err
	}
	defer s.close()
	var stats []string
	for _, size := range []int{74, 1200, 1400} {
		payload := make([]byte, size)
		copy(payload, token())
		start := time.Now()
		n, err := s.echo(payload, 8, 1500*time.Millisecond)
		if err != nil {
			return strings.Join(stats, " "), err
		}
		stats = append(stats, fmt.Sprintf("%dB:%d try/%v", size, n, time.Since(start).Round(time.Millisecond)))
	}
	return strings.Join(stats, " "), nil
}

// oneCall is a short call: a fresh association, a few packets, closed.
func oneCall(mp int) (time.Duration, error) {
	s, err := openUDP(mp)
	if err != nil {
		return 0, err
	}
	defer s.close()
	start := time.Now()
	var first time.Duration
	for i := 0; i < 3; i++ {
		payload := make([]byte, 74)
		copy(payload, token())
		if _, err := s.echo(payload, 4, 1500*time.Millisecond); err != nil {
			return 0, fmt.Errorf("packet %d: %w", i+1, err)
		}
		if i == 0 {
			first = time.Since(start)
		}
	}
	return first, nil
}

func udpCheckSequential(mp int) (string, error) {
	var worst time.Duration
	var errs []string
	for i := 0; i < 12; i++ {
		d, err := oneCall(mp)
		if err != nil {
			errs = append(errs, fmt.Sprintf("session %d: %v", i+1, err))
			continue
		}
		worst = max(worst, d)
	}
	stats := fmt.Sprintf("%d/12 ok, slowest first reply %v", 12-len(errs), worst.Round(time.Millisecond))
	if len(errs) > 0 {
		return stats, errors.New(strings.Join(errs, "; "))
	}
	return stats, nil
}

func udpCheckConcurrent(mp int) (string, error) {
	var mu sync.Mutex
	var worst time.Duration
	var errs []string
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := oneCall(mp)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, fmt.Sprintf("session %d: %v", i+1, err))
				return
			}
			worst = max(worst, d)
		}()
	}
	wg.Wait()
	stats := fmt.Sprintf("%d/8 ok, slowest first reply %v", 8-len(errs), worst.Round(time.Millisecond))
	if len(errs) > 0 {
		return stats, errors.New(strings.Join(errs, "; "))
	}
	return stats, nil
}

// udpCheckBurst sends 200 voice-sized (160 B) packets, gap apart (1 ms:
// twenty times the packet rate of a voice stream), on one warmed-up
// association and counts the echoes: loss must stay under 5%.
func udpCheckBurst(mp int, gap time.Duration) (string, error) {
	const n, size, maxLoss = 200, 160, 0.05
	s, err := openUDP(mp)
	if err != nil {
		return "", err
	}
	defer s.close()
	warm := make([]byte, size)
	copy(warm, "warm"+token())
	if _, err := s.echo(warm, 8, 1500*time.Millisecond); err != nil {
		return "", fmt.Errorf("warm-up: %w", err)
	}
	tag := []byte(token())
	got := make([]bool, n)
	done := make(chan int)
	go func() {
		count := 0
		for {
			body, err := s.recv(time.Now().Add(3 * time.Second))
			if err != nil {
				done <- count
				return
			}
			if len(body) == size && string(body[4:4+len(tag)]) == string(tag) {
				if i := binary.BigEndian.Uint32(body); int(i) < n && !got[i] {
					got[i] = true
					count++
				}
			}
		}
	}()
	start := time.Now()
	for i := 0; i < n; i++ {
		payload := make([]byte, size)
		binary.BigEndian.PutUint32(payload, uint32(i))
		copy(payload[4:], tag)
		if err := s.send(payload); err != nil {
			return "", err
		}
		if gap > 0 {
			for next := start.Add(time.Duration(i+1) * gap); time.Now().Before(next); {
				time.Sleep(next.Sub(time.Now()) / 2)
			}
		}
	}
	sent := time.Since(start)
	count := <-done
	loss := 1 - float64(count)/n
	stats := fmt.Sprintf("%d/%d echoed (loss %.1f%%), sent in %v", count, n, loss*100, sent.Round(time.Millisecond))
	if loss >= maxLoss {
		return stats, fmt.Errorf("loss %.1f%% >= %.0f%%", loss*100, maxLoss*100)
	}
	return stats, nil
}

// TestCitrusUDPLongIdle keeps one association per protocol and column for
// 90 s: a packet every 2 s, then 35 s of silence (a muted call), then a
// packet every 2 s again. Every packet waits for its own echo. The sessions
// run side by side: at one packet per 2 s they do not compete, and serially
// they would take 9 minutes per column.
func TestCitrusUDPLongIdle(t *testing.T) {
	needLab(t)
	if testing.Short() {
		t.Skip("90 s per session; skipped under -short")
	}
	f, err := labFixtures.get()
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range udpProtos() {
		for ci, col := range columns() {
			t.Run(p.name+"/"+string(col), func(t *testing.T) {
				t.Parallel()
				c, mp, err := udpCell(f, p, col, 160+i*2+ci, "udp-long-idle")
				defer c.stopAll()
				if err != nil {
					t.Logf("evidence:\n%s", c.evidence())
					t.Fatal(err)
				}
				stats, err := longIdleSession(mp)
				t.Logf("%s", stats)
				if err != nil {
					t.Logf("evidence:\n%s", c.evidence())
					t.Error(err)
				}
			})
		}
	}
}

func longIdleSession(mp int) (string, error) {
	const (
		total    = 90 * time.Second
		every    = 2 * time.Second
		idleFrom = 26 * time.Second
		idle     = 35 * time.Second
	)
	s, err := openUDP(mp)
	if err != nil {
		return "", err
	}
	defer s.close()
	tag := token()
	type phase struct{ sent, lost int }
	var before, after phase
	var afterGap []bool // the echoes of the first packets after the silence
	start := time.Now()
	seq := 0
	for time.Since(start) < total {
		elapsed := time.Since(start)
		if elapsed >= idleFrom && elapsed < idleFrom+idle {
			time.Sleep(idleFrom + idle - elapsed)
			continue
		}
		tick := time.Now()
		seq++
		payload := []byte(fmt.Sprintf("%s-%04d-", tag, seq))
		payload = append(payload, make([]byte, 74-len(payload))...)
		ok := false
		if err := s.send(payload); err == nil {
			deadline := time.Now().Add(1500 * time.Millisecond)
			for !ok {
				body, err := s.recv(deadline)
				if err != nil {
					break
				}
				ok = string(body) == string(payload)
			}
		}
		ph := &before
		if elapsed >= idleFrom {
			ph = &after
			if len(afterGap) < 3 {
				afterGap = append(afterGap, ok)
			}
		}
		ph.sent++
		if !ok {
			ph.lost++
		}
		time.Sleep(every - time.Since(tick))
	}
	stats := fmt.Sprintf("before the silence %d/%d echoed, after it %d/%d (first three after it: %v)",
		before.sent-before.lost, before.sent, after.sent-after.lost, after.sent, afterGap)
	var errs []string
	if before.lost > 1 {
		errs = append(errs, fmt.Sprintf("%d of %d packets lost before the silence", before.lost, before.sent))
	}
	if len(afterGap) == 0 || !(afterGap[0] || len(afterGap) > 1 && afterGap[1]) {
		errs = append(errs, "the association did not survive 35 s of silence: no echo of the first two packets after it")
	}
	if after.lost > 1 {
		errs = append(errs, fmt.Sprintf("%d of %d packets lost after the silence", after.lost, after.sent))
	}
	if len(errs) > 0 {
		return stats, errors.New(strings.Join(errs, "; "))
	}
	return stats, nil
}
