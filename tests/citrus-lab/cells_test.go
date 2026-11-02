package citruslab

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// outcome is what a cell is expected to (or did) do: the traffic reaches the
// echo server through the Citrus inbound, or the client is refused.
type outcome string

const (
	pass    outcome = "pass"
	refused outcome = "refused"
)

// column is the core a cell runs: the patched core SlothClash ships, or the
// stock core of the same tag (the comparison column).
type column string

const (
	colPatched column = "patched"
	colStock   column = "stock"
)

func (col column) bin() string {
	if col == colStock {
		return stockCore
	}
	return patchedCore
}

func columns() []column {
	if stockCore != "" {
		return []column{colPatched, colStock}
	}
	return []column{colPatched}
}

// cellCtx is one attempt of one cell: its directory, its port band and the
// processes it started.
type cellCtx struct {
	col     column
	dir     string
	band    *portBand
	procs   []*proc
	servers []*proc
	sports  []int
	state   any // per-case data between build and verify
}

func (c *cellCtx) port() int {
	p, err := c.band.take()
	if err != nil {
		panic(err)
	}
	return p
}

// server starts a Citrus server with one listener (name, listen and port are
// filled in) and waits until it runs. It returns the listener's port.
func (c *cellCtx) server(listener obj) (int, error) {
	i := len(c.servers)
	sport, ctl := c.port(), c.port()
	listener = merge(obj{"name": fmt.Sprintf("in%d", i), "listen": "127.0.0.1", "port": sport}, listener)
	cfg := obj{
		"log-level": "debug",
		"ipv6":      false,
		"listeners": []obj{listener},
		"rules":     []string{"MATCH,DIRECT"},
		// the echo servers and the target live on the loopback, which the
		// egress guard keeps server users away from by default
		"egress-guard":        obj{"allow-ips": []string{"127.0.0.0/8"}},
		"external-controller": fmt.Sprintf("127.0.0.1:%d", ctl),
		"secret":              "citrus-lab",
	}
	p, err := spawn(c.dir, fmt.Sprintf("citrus-%d", i), citrusBin, cfg)
	if err != nil {
		return 0, err
	}
	c.procs = append(c.procs, p)
	// the listener may be UDP only: wait on the controller
	if err := p.waitReady(ctl); err != nil {
		return 0, err
	}
	c.servers = append(c.servers, p)
	c.sports = append(c.sports, sport)
	return sport, nil
}

func (c *cellCtx) stopAll() {
	for i := len(c.procs) - 1; i >= 0; i-- {
		c.procs[i].stop()
	}
	c.procs = nil
}

func (c *cellCtx) lastServer() *proc { return c.servers[len(c.servers)-1] }

// proxy is a client proxy aimed at a server port.
func proxy(name string, sport int, o obj) obj {
	return merge(o, obj{"name": name, "server": "127.0.0.1", "port": sport})
}

// startCore runs the core under test with a mixed port and the client part of
// a case: "proxies", optional "proxy-groups" and "rules" (default: MATCH to
// the first proxy) and any top-level keys. errExited means the core refused
// the config.
func (c *cellCtx) startCore(client obj) (*proc, int, error) {
	mp := c.port()
	cfg := obj{
		"log-level":  "debug",
		"ipv6":       false,
		"allow-lan":  false,
		"mode":       "rule",
		"mixed-port": mp,
	}
	for k, v := range client {
		cfg[k] = v
	}
	if _, ok := cfg["rules"]; !ok {
		first := client["proxies"].([]obj)[0]["name"].(string)
		cfg["rules"] = []string{"MATCH," + first}
	}
	p, err := spawn(c.dir, "core", c.col.bin(), cfg)
	if err != nil {
		return nil, 0, err
	}
	c.procs = append(c.procs, p)
	return p, mp, p.waitReady(mp)
}

// evidence is what the processes of the attempt logged, for a failing cell.
func (c *cellCtx) evidence() string {
	var b strings.Builder
	for _, p := range c.procs {
		if p.log == "" {
			continue
		}
		fmt.Fprintf(&b, "--- %s (%s), last lines:\n%s\n", p.role, p.log, p.tail(25))
	}
	return b.String()
}

// cellResult is one attempt of a cell.
type cellResult struct {
	got      outcome
	detail   string
	err      error // a broken invariant, whatever the outcome
	evidence string
	took     time.Duration
}

// labCase is one row of the matrix.
type labCase struct {
	name    string
	feature string // Citrus feature the case needs (detectFeatures)
	patched outcome
	stock   outcome // "" = no stock column
	why     string  // the reason for the expectations, shown on a mismatch
	udp     bool    // a passing cell also echoes UDP (16/512/1200 bytes)
	// build starts the servers and returns the client part of the core config
	build func(c *cellCtx) (obj, error)
	// verify checks more than the traffic (both chain hops saw it, storage
	// untouched, ...); runs whatever the outcome
	verify func(c *cellCtx, got outcome) error
}

func (lc labCase) want(col column) outcome {
	if col == colStock {
		return lc.stock
	}
	return lc.patched
}

// runCell runs one attempt of a cell with fresh processes.
func runCell(lc labCase, col column, band int, attempt int) (res cellResult) {
	start := time.Now()
	c := &cellCtx{
		col:  col,
		dir:  filepath.Join(logDir, "matrix", safeName(lc.name), string(col), fmt.Sprintf("attempt-%d", attempt)),
		band: newBand(band),
	}
	defer func() {
		if r := recover(); r != nil {
			res.err = fmt.Errorf("panic: %v", r)
		}
		if res.err != nil || res.got != lc.want(col) {
			res.evidence = c.evidence()
		}
		c.stopAll()
		res.took = time.Since(start).Round(100 * time.Millisecond)
	}()
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		res.err = err
		return
	}
	client, err := lc.build(c)
	if err != nil {
		res.err = fmt.Errorf("server setup: %w", err)
		return
	}
	_, mp, err := c.startCore(client)
	switch {
	case errors.Is(err, errExited):
		res.got, res.detail = refused, "the core refused the config: "+lastLine(err.Error())
	case err != nil:
		res.err = fmt.Errorf("core setup: %w", err)
		return
	default:
		ok, detail := probeTCP(mp, 3, 8*time.Second)
		res.detail = detail
		res.got = refused
		if ok {
			res.got = pass
		}
		relayed := c.lastServer().has("--> " + echoAddr)
		switch {
		case ok && !relayed:
			res.err = errors.New("the traffic passed but the last Citrus server never relayed it: it went around the tunnel")
		case !ok && relayed:
			res.err = errors.New("the Citrus server relayed traffic the client never got back")
		}
		if res.err == nil && ok && lc.udp {
			if err := udpSizes(mp, []int{16, 512, 1200}); err != nil {
				res.err = fmt.Errorf("UDP: %w", err)
			} else if !c.lastServer().has("[UDP] ") {
				res.err = errors.New("UDP echoed but the Citrus server never relayed it as UDP")
			}
		}
	}
	if res.err == nil && lc.verify != nil {
		res.err = lc.verify(c, res.got)
	}
	return
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func udpSizes(mp int, sizes []int) error {
	s, err := openUDP(mp)
	if err != nil {
		return err
	}
	defer s.close()
	for _, size := range sizes {
		payload := make([]byte, size)
		copy(payload, token())
		if _, err := s.echo(payload, 8, 1500*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

// cellParallel bounds how many cells run at once: every cell runs one to
// three processes and fresh handshakes, and a small CI runner starts timing
// out for reasons that have nothing to do with the protocols.
const cellParallel = 8

// runMatrix runs every cell once in parallel, then rechecks each mismatch
// alone: a mismatch counts only if it reproduces. Both rounds are logged.
// The result is one subtest per cell.
func runMatrix(t *testing.T, cases []labCase, bandOffset int) {
	type cell struct {
		lc    labCase
		col   column
		band  int
		first cellResult
		final cellResult
		mark  string
		skip  string
	}
	var cells []*cell
	for i, lc := range cases {
		for ci, col := range columns() {
			if lc.want(col) == "" {
				continue
			}
			cl := &cell{lc: lc, col: col, band: bandOffset + i*2 + ci}
			if lc.feature != "" && !features[lc.feature] {
				cl.skip = "this Citrus does not serve " + lc.feature
			}
			cells = append(cells, cl)
		}
	}
	sem := make(chan struct{}, cellParallel)
	var wg sync.WaitGroup
	for _, cl := range cells {
		if cl.skip != "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			cl.first = runCell(cl.lc, cl.col, cl.band, 1)
		}()
	}
	wg.Wait()
	for _, cl := range cells {
		if cl.skip != "" {
			continue
		}
		cl.final, cl.mark = cl.first, "  "
		if cl.first.err != nil || cl.first.got != cl.lc.want(cl.col) {
			again := runCell(cl.lc, cl.col, cl.band+bandsRecheck, 2)
			if again.err == nil && again.got == cl.lc.want(cl.col) {
				cl.mark = "~~"
			} else {
				cl.mark = "!!"
			}
			cl.final = again
		}
	}
	for _, cl := range cells {
		t.Run(cl.lc.name+"/"+string(cl.col), func(t *testing.T) {
			if cl.skip != "" {
				t.Skip(cl.skip)
			}
			want := cl.lc.want(cl.col)
			t.Logf("%s %-48s got=%-7s want=%-7s %v [%s]", cl.mark, cl.lc.name+"/"+string(cl.col), cl.final.got, want, cl.final.took, cl.final.detail)
			switch cl.mark {
			case "~~":
				t.Logf("~~ flaky: first round got=%s err=%v [%s]; recheck alone matched", cl.first.got, cl.first.err, cl.first.detail)
			case "!!":
				t.Logf("first round: got=%s err=%v [%s]", cl.first.got, cl.first.err, cl.first.detail)
				t.Logf("evidence (recheck):\n%s", cl.final.evidence)
				if cl.final.err != nil {
					t.Errorf("%v (got=%s, want=%s; %s)", cl.final.err, cl.final.got, want, cl.lc.why)
				} else {
					t.Errorf("got %s, want %s: %s", cl.final.got, want, cl.lc.why)
				}
			}
		})
	}
}

// bandsRecheck separates the recheck's port bands from the first round's.
const bandsRecheck = 300
