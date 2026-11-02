package citruslab

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fixtures are the keys every case of a run shares.
type fixtures struct {
	reality  x25519Keys
	idA, idB string
	pw       string
	ss       string
	mldsa    mldsaKeys
	ech      echKeys
	echOther echKeys
}

var labFixtures = &lazy[*fixtures]{f: func() (*fixtures, error) {
	f := &fixtures{reality: genX25519(), idA: genUUID(), idB: genUUID(), pw: token(), ss: randB64(16)}
	var err error
	if f.mldsa, err = readMLDSA(); err != nil {
		return nil, err
	}
	if f.ech, err = genECH(echPublic); err != nil {
		return nil, err
	}
	if f.echOther, err = genECH(echPublic); err != nil {
		return nil, err
	}
	return f, nil
}}

func (f *fixtures) realityServer(extra obj) obj {
	rc := obj{
		"dest": targetAddr, "private-key": f.reality.priv, "short-id": []string{shortID},
		"server-names": []string{realitySNI},
	}
	return obj{
		"type":           "vless",
		"users":          []obj{{"username": "alice", "uuid": f.idA, "flow": "xtls-rprx-vision"}},
		"reality-config": merge(rc, extra),
	}
}

func (f *fixtures) realityClient(opts obj) obj {
	return obj{
		"type": "vless", "uuid": f.idA, "flow": "xtls-rprx-vision", "network": "tcp", "udp": true,
		"tls": true, "servername": realitySNI, "client-fingerprint": "chrome",
		"reality-opts": merge(obj{"public-key": f.reality.pub, "short-id": shortID}, opts),
	}
}

// tlsServer is a Citrus inbound with the lab's self-signed interop.test leaf.
func tlsServer(o obj) obj {
	return merge(obj{"certificate": string(tlsCertPEM), "private-key": string(tlsKeyPEM)}, o)
}

// tlsClient pins that leaf.
func tlsClient(o obj) obj {
	return merge(obj{"tls": true, "servername": realitySNI, "fingerprint": tlsCertSHA256}, o)
}

// single is a case with one Citrus listener and one client proxy.
func single(server func() obj, client func(sport int) obj, top obj) func(c *cellCtx) (obj, error) {
	return func(c *cellCtx) (obj, error) {
		sport, err := c.server(server())
		if err != nil {
			return nil, err
		}
		return merge(top, obj{"proxies": []obj{proxy("p", sport, client(sport))}}), nil
	}
}

func matrixCases(f *fixtures) []labCase {
	reality := func(serverExtra, clientOpts, top obj) func(c *cellCtx) (obj, error) {
		return single(func() obj { return f.realityServer(serverExtra) }, func(int) obj { return f.realityClient(clientOpts) }, top)
	}
	mlkemRequired := obj{"require-x25519mlkem768": true}
	cases := []labCase{
		{name: "reality/control", patched: pass, stock: pass,
			why:   "a plain VLESS-REALITY-vision inbound",
			build: reality(nil, nil, nil)},

		// --- ML-DSA-65 (Xray mldsa65Verify, core patch 0007) ---
		{name: "reality/mldsa65-verify-right", patched: pass, stock: pass,
			why:   "the right verify key passes; the stock core ignores the key",
			build: reality(obj{"mldsa65-seed": f.mldsa.Seed}, obj{"mldsa65-verify": f.mldsa.Verify}, nil)},
		{name: "reality/mldsa65-verify-wrong", patched: refused, stock: pass,
			why:   "the patched core must reject a server whose ML-DSA-65 signature does not verify; the stock core does not check it at all",
			build: reality(obj{"mldsa65-seed": f.mldsa.Seed}, obj{"mldsa65-verify": f.mldsa.OtherVerify}, nil)},

		// --- client version (core patches 0001/0003/0005) ---
		{name: "reality/min-client-ver/default", patched: pass, stock: refused,
			why:   "the patched default client version (26.x) clears min-client-ver 26.0.0; stock sends 1.8.2",
			build: reality(obj{"min-client-ver": "26.0.0"}, nil, nil)},
		{name: "reality/min-client-ver/per-proxy-low", patched: refused, stock: refused,
			why:   "reality-opts.client-version 1.9.0 is below min-client-ver 26.0.0",
			build: reality(obj{"min-client-ver": "26.0.0"}, obj{"client-version": "1.9.0"}, nil)},
		{name: "reality/min-client-ver/per-proxy-overrides-global", patched: pass, stock: refused,
			why:   "a per-proxy client-version 26.10.29 wins over a global reality-client-version 1.9.0",
			build: reality(obj{"min-client-ver": "26.0.0"}, obj{"client-version": "26.10.29"}, obj{"reality-client-version": "1.9.0"})},
		{name: "reality/max-client-ver/default", feature: "max-client-ver", patched: refused, stock: pass,
			why:   "the patched default client version is above max-client-ver 2.0.0; stock's 1.8.2 is below",
			build: reality(obj{"max-client-ver": "2.0.0"}, nil, nil)},
		{name: "reality/max-client-ver/per-proxy-low", feature: "max-client-ver", patched: pass, stock: pass,
			why:   "reality-opts.client-version 1.9.0 fits under max-client-ver 2.0.0",
			build: reality(obj{"max-client-ver": "2.0.0"}, obj{"client-version": "1.9.0"}, nil)},

		// --- X25519MLKEM768 policy against a server that requires it (patches 0002/0003) ---
		{name: "reality/require-mlkem/auto", patched: pass, stock: refused,
			why:   "auto learns from the first refused classic hello and offers the hybrid share; stock offers it only when forced",
			build: reality(mlkemRequired, nil, obj{"reality-mlkem": "auto"})},
		{name: "reality/require-mlkem/on", patched: pass, stock: refused,
			why:   "on always offers the hybrid share",
			build: reality(mlkemRequired, nil, obj{"reality-mlkem": "on"})},
		{name: "reality/require-mlkem/off", patched: refused, stock: refused,
			why:   "a classic hello against a server that requires X25519MLKEM768 must be refused",
			build: reality(mlkemRequired, nil, obj{"reality-mlkem": "off"})},
		{name: "reality/require-mlkem/forced", patched: pass, stock: pass,
			why:   "support-x25519mlkem768: true forces the hybrid share in both cores",
			build: reality(mlkemRequired, obj{"support-x25519mlkem768": true}, nil)},

		// --- negative controls ---
		{name: "reality/wrong-uuid", patched: refused, stock: refused,
			why: "a UUID the inbound does not know",
			build: single(func() obj { return f.realityServer(nil) }, func(int) obj {
				return merge(f.realityClient(nil), obj{"uuid": f.idB})
			}, nil)},
		{name: "reality/wrong-short-id", patched: refused, stock: refused,
			why:   "a short-id the inbound does not list",
			build: reality(nil, obj{"short-id": "fedcba9876543210"}, nil)},

		// --- XHTTP ---
		{name: "xhttp/reality-stream-up", patched: pass, stock: pass,
			why: "XHTTP stream-up over REALITY",
			build: single(func() obj {
				return merge(f.realityServer(nil), obj{"users": []obj{{"username": "alice", "uuid": f.idA}}, "xhttp-config": obj{"path": "/xh", "mode": "auto"}})
			}, func(int) obj {
				return merge(f.realityClient(nil), obj{"flow": "", "network": "xhttp", "alpn": []string{"h2"}, "udp": false,
					"xhttp-opts": obj{"path": "/xh", "mode": "stream-up"}})
			}, nil)},
		{name: "xhttp/reality-packet-up-GET-header", patched: pass, stock: pass,
			why:   "XHTTP packet-up with the uplink carried in GET headers under the key Xray and Citrus use by default (X-Data)",
			build: xhttpGET(f, "X-Data")},
		// Stock mihomo sends header uplink data under "<uplink-data-key>-<n>"
		// and leaves the key empty when the config sets none ("-0", "-1", ...),
		// where Xray and Citrus default to X-Data; share links leave the key
		// out when it is the default. Patch 0028 fills in X-Data (x_data for
		// cookies), so the patched core passes and the stock core does not.
		{name: "xhttp/reality-packet-up-GET-header-default-key", patched: pass, stock: refused,
			why:   "no uplink-data-key: patch 0028 defaults it to X-Data; stock sends header \"-0\" instead of \"X-Data-0\"",
			build: xhttpGET(f, "")},
	}
	for _, mode := range []string{"stream-one", "packet-up", "stream-up"} {
		cases = append(cases, labCase{name: "xhttp/h3-" + mode, feature: "xhttp-h3", patched: pass, stock: pass,
			why: "XHTTP over HTTP/3 (the inbound listens on UDP only)",
			build: single(func() obj {
				return tlsServer(obj{"type": "vless", "users": []obj{{"username": "alice", "uuid": f.idA}},
					"alpn": []string{"h3"}, "xhttp-config": obj{"path": "/xh"}})
			}, func(int) obj {
				return tlsClient(obj{"type": "vless", "uuid": f.idA, "network": "xhttp", "udp": false, "alpn": []string{"h3"},
					"xhttp-opts": obj{"path": "/xh", "mode": mode}})
			}, nil)})
	}

	// --- ECH in the forms Xray configs carry (core patch 0010) ---
	ech := func(config string) func(c *cellCtx) (obj, error) {
		return single(func() obj {
			return tlsServer(obj{"type": "vless", "users": []obj{{"username": "alice", "uuid": f.idA}}, "ech-key": f.ech.keyPEM})
		}, func(int) obj {
			return tlsClient(obj{"type": "vless", "uuid": f.idA, "network": "tcp", "udp": false,
				"ech-opts": obj{"enable": true, "config": config}})
		}, nil)
	}
	cases = append(cases,
		labCase{name: "ech/base64", patched: pass, stock: pass,
			why: "the base64 ECHConfigList both cores read", build: ech(f.ech.configList)},
		labCase{name: "ech/xray-pem", patched: pass, stock: refused,
			why: "the ECH CONFIGS PEM of `xray tls ech --pem`; stock cannot parse it", build: ech(echPEM(f.ech.configList))},
		labCase{name: "ech/wrapped-base64", patched: pass, stock: pass,
			why: "a base64 list wrapped over several lines (Go's base64 decoder skips newlines, so stock reads it too)", build: ech(echWrapped(f.ech.configList))},
		labCase{name: "ech/wrong-config", patched: refused, stock: refused,
			why: "a config list the server has no key for: ECH is rejected and the outer name does not verify", build: ech(f.echOther.configList)},
	)

	// --- chain group across two Citrus inbounds (core patch 0006) ---
	cases = append(cases, labCase{name: "chain/reality-then-ss2022", patched: pass, stock: refused,
		why: "type: chain dials the second hop through the first; stock has no chain group and refuses the config",
		build: func(c *cellCtx) (obj, error) {
			entry, err := c.server(f.realityServer(nil))
			if err != nil {
				return nil, err
			}
			exit, err := c.server(obj{"type": "shadowsocks", "cipher": "2022-blake3-aes-128-gcm", "password": f.ss, "udp": true})
			if err != nil {
				return nil, err
			}
			return obj{
				"proxies": []obj{
					proxy("hop-entry", entry, f.realityClient(nil)),
					proxy("hop-exit", exit, obj{"type": "ss", "cipher": "2022-blake3-aes-128-gcm", "password": f.ss, "udp": true}),
				},
				"proxy-groups": []obj{{"name": "cascade", "type": "chain", "proxies": []string{"hop-entry", "hop-exit"}}},
				"rules":        []string{"MATCH,cascade"},
			}, nil
		},
		verify: func(c *cellCtx, got outcome) error {
			if got != pass {
				return nil
			}
			entry := c.servers[0]
			if !entry.has(fmt.Sprintf("--> 127.0.0.1:%d", c.sports[1])) {
				return fmt.Errorf("the entry hop never dialed the exit hop (127.0.0.1:%d)", c.sports[1])
			}
			if entry.has("--> " + echoAddr) {
				return fmt.Errorf("the entry hop reached the echo server itself: the exit hop was skipped")
			}
			return nil
		}})

	// --- ShadowQUIC ---
	cases = append(cases, labCase{name: "shadowquic", feature: "shadowquic", patched: pass, stock: pass, udp: true,
		why: "ShadowQUIC (JLS upstream: the local TLS target)",
		build: single(func() obj {
			return obj{"type": "shadowquic", "users": []obj{{"username": "alice", "password": f.pw}},
				"jls-upstream": obj{"addr": targetAddr, "sni": realitySNI}, "alpn": []string{"h3"}}
		}, func(int) obj {
			return obj{"type": "shadowquic", "username": "alice", "password": f.pw, "sni": realitySNI,
				"alpn": []string{"h3"}, "udp": true}
		}, nil)})

	// --- XDrive: the shipped core refuses local and template storage ---
	for _, backend := range []string{"local", "template"} {
		cases = append(cases, labCase{name: "xdrive/" + backend + "-refused", feature: "xdrive", patched: refused, stock: refused,
			why:    "a subscription must not make the shipped core read or write a local folder or call arbitrary HTTP endpoints (local: core patch 0016; template: refused since the template change)",
			build:  xdriveBuild(f, backend),
			verify: xdriveVerify})
	}
	return cases
}

func xhttpGET(f *fixtures, key string) func(c *cellCtx) (obj, error) {
	return single(func() obj {
		return merge(f.realityServer(nil), obj{"users": []obj{{"username": "alice", "uuid": f.idA}}, "xhttp-config": obj{"path": "/xh", "mode": "packet-up"}})
	}, func(int) obj {
		opts := obj{"path": "/xh", "mode": "packet-up", "uplink-http-method": "GET", "uplink-data-placement": "header"}
		if key != "" {
			opts["uplink-data-key"] = key
		}
		return merge(f.realityClient(nil), obj{"flow": "", "network": "xhttp", "alpn": []string{"h2"}, "udp": false, "xhttp-opts": opts})
	}, nil)
}

// xdriveState is the storage of an XDrive cell.
type xdriveState struct {
	folder string
	before []string
	puts   *atomic.Int64
	putsAt int64
}

func xdriveBuild(f *fixtures, backend string) func(c *cellCtx) (obj, error) {
	return func(c *cellCtx) (obj, error) {
		keys := genX25519()
		st := &xdriveState{folder: filepath.Join(c.dir, "xdrive-folder"), puts: &atomic.Int64{}}
		if err := os.MkdirAll(st.folder, 0o755); err != nil {
			return nil, err
		}
		opts := obj{"service": backend, "remote-folder": st.folder, "segment-bytes": 65536,
			"flush-interval-ms": 5, "poll-interval-ms": 5, "max-poll-interval-ms": 20}
		if backend == "template" {
			srv := templateStorage(st.puts)
			c.procs = append(c.procs, srv.proc)
			opts["template"] = obj{"flatten": true,
				"put":    obj{"method": "PUT", "url": srv.url + "/objects/{name}"},
				"get":    obj{"method": "GET", "url": srv.url + "/objects/{name}"},
				"delete": obj{"method": "DELETE", "url": srv.url + "/objects/{name}"},
				"list":   obj{"method": "PROPFIND", "url": srv.url + "/objects/", "namesRegex": "<key>([^<]+)</key>"}}
		}
		sport, err := c.server(obj{"type": "vless", "users": []obj{{"username": "alice", "uuid": f.idA}},
			"decryption": "mlkem768x25519plus.native.600s." + keys.priv, "xdrive-config": opts})
		if err != nil {
			return nil, err
		}
		st.before = listDir(st.folder)
		st.putsAt = st.puts.Load()
		c.state = st
		return obj{"proxies": []obj{proxy("p", sport, obj{"type": "vless", "uuid": f.idA, "udp": true, "network": "xdrive",
			"encryption": "mlkem768x25519plus.native.0rtt." + keys.pub, "xdrive-opts": opts})}}, nil
	}
}

// xdriveVerify: whatever the outcome, the client must not have written to the
// storage (the server only writes in answer to a client).
func xdriveVerify(c *cellCtx, _ outcome) error {
	st, _ := c.state.(*xdriveState)
	if st == nil {
		return nil
	}
	if after := listDir(st.folder); strings.Join(after, "\n") != strings.Join(st.before, "\n") {
		return fmt.Errorf("the storage folder changed: before %v, after %v", st.before, after)
	}
	if n := st.puts.Load() - st.putsAt; n != 0 {
		return fmt.Errorf("the template endpoint got %d PUT requests after the server started", n)
	}
	return nil
}

func listDir(dir string) []string {
	var names []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && p != dir {
			rel, _ := filepath.Rel(dir, p)
			names = append(names, rel)
		}
		return nil
	})
	sort.Strings(names)
	return names
}

// templateStorage is the object store of Citrus' XDrive interop test
// (test/interop/xdrive_test.go at 62d1f567), counting PUTs. It is wrapped in
// a proc so that the cell stops it with its processes.
type httpProc struct {
	*proc
	url string
}

func templateStorage(puts *atomic.Int64) *httpProc {
	var mu sync.Mutex
	objects := map[string][]byte{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/objects/")
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "PUT":
			puts.Add(1)
			b, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			objects[name] = b
			w.WriteHeader(201)
		case "GET":
			b, ok := objects[name]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Write(b)
		case "DELETE":
			for key := range objects {
				if key == name || strings.HasPrefix(key, name+"~") {
					delete(objects, key)
				}
			}
			w.WriteHeader(204)
		case "PROPFIND":
			for key := range objects {
				fmt.Fprintf(w, "<key>%s</key>\n", key)
			}
		default:
			w.WriteHeader(405)
		}
	}))
	return &httpProc{proc: closerProc("template-storage", srv.Close), url: srv.URL}
}

func TestCitrusMatrix(t *testing.T) {
	needLab(t)
	f, err := labFixtures.get()
	if err != nil {
		t.Fatal(err)
	}
	runMatrix(t, matrixCases(f), 0)
}
