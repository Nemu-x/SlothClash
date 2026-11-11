# Core patch series

SlothClash ships [mihomo](https://github.com/MetaCubeX/mihomo) built from an unmodified
MetaCubeX release tag plus a short series of patches kept in
[`core/patches/mihomo/`](../core/patches/mihomo). There is no SlothClash fork of mihomo, and
there must not be one: a patch that is no longer small enough to rebase in minutes is a sign to
get it into mihomo or drop it.

Settings → Info shows which core a build carries (`mihomo v1.19.34 + 29 SlothClash patches`)
with links to the patches and to this page.

## Where the core comes from

| Piece | Role |
|---|---|
| [`core/mihomo/manifest.json`](../core/mihomo/manifest.json) | MetaCubeX tag + its commit, every patch with its SHA-256, the binary name per target |
| [`core/mihomo/.tool-versions`](../core/mihomo/.tool-versions) | the Go release every core is built with |
| [`scripts/core-build.mjs`](../scripts/core-build.mjs) | clones the tag, checks the commit, applies the series, runs the patch tests, cross-compiles |
| [`.github/workflows/core-build.yml`](../.github/workflows/core-build.yml) | builds every target in parallel, runs the REALITY lab, hands the result on as the `core-dist` artifact |
| [`.github/workflows/desktop-artifacts.yml`](../.github/workflows/desktop-artifacts.yml) | on a `v*` tag: runs the core build first, then builds the app for every OS from that `core-dist` |
| [`scripts/prebuild.mjs`](../scripts/prebuild.mjs) | puts the core for the build target next to the app (see below) |

A release is one tag push. The core is built from source in the same run, before any app build,
and a patch that no longer applies, a failing patch test or a REALITY lab regression stops the
release: no app is built and nothing is published. There are no separate core releases.

The trust anchor is the source, not a binary: the manifest pins the upstream commit and the hash of
every patch, `.tool-versions` pins Go, and the build is reproducible. The same inputs give the same
bytes on any machine, so the privileged service's SHA-256 pin only changes when one of those
inputs changes (the "reinstall service" banner appears once per core change, not once per app
release).

`prebuild` gets the core in one of three ways:

- **Release CI**: `SLOTH_CORE_DIR` points at `core-dist` from the same run. prebuild checks that its
  `BUILDINFO.txt` names the pinned tag, commit and patch hashes and that the binary matches its
  `SHA256SUMS` line. In CI without `SLOTH_CORE_DIR` it fails instead of building an untested core.
- **Local build** (default): builds the host target with `core-build.mjs` (git and Go needed; Go
  fetches the pinned toolchain itself if the installed one differs). It takes a minute once, then
  comes from `node_modules/.verge/core` until the tag, a patch or the Go pin changes. The bytes
  equal the release's.
- **Escape hatches**, never used by release CI: `SLOTH_CORE_FILE=<path>` embeds that binary as-is
  (Settings → Info says "local build"); `MIHOMO_CORE_VERSION=<tag>` embeds the stock MetaCubeX
  build of that tag (Settings → Info says "stock").

### Build flags

The same as MetaCubeX release builds: `CGO_ENABLED=0`, `-tags with_gvisor`, `-trimpath`,
`-ldflags "-extldflags --static -X constant.Version=<tag> -X constant.BuildTime=… -w -s -buildid="`,
`GOAMD64=v2` on amd64. Two differences, both deliberate:

- **Toolchain:** stock Go, the release pinned in `core/mihomo/.tool-versions`, for every target.
  MetaCubeX builds with its own Go fork and ships extra builds for old Windows and macOS; the app
  itself needs Windows 10 / macOS 12 or later, so those builds buy nothing here.
- **BuildTime:** the tag's commit time instead of the wall clock, so the build is reproducible.

`constant.Version` stays the plain tag (`v1.19.34`). The core's `/version`, the User-Agent it
uses for providers and everything panels match on are identical to an unpatched core.

### Reproducing a release core

```bash
node scripts/core-build.mjs out        # all targets; or: node scripts/core-build.mjs out linux-amd64
cat out/SHA256SUMS
```

The hashes equal those in `BUILDINFO.txt`/`SHA256SUMS` of the `core-dist` artifact of the release
run, and the binary embedded in the app.

## Rules for a patch

- One concern per patch, numbered, with a comment in the code explaining why it exists.
- Behaviour that users may need to change is read from the config, not baked in (see 0003).
- Logic ships with a Go test inside the patch; the core build vets and tests every package the
  series touches, on every PR that touches the core and on every release, once plainly and once
  under the race detector on Linux (`--test-only --race`, see 0040).
- Record the related mihomo issue or PR below, and the condition under which the patch goes away.
- Never edit a patch without updating its hash in the manifest; the build refuses the mismatch.

## Bumping the core or changing the series

1. Edit `core/mihomo/manifest.json`: `tag` and `commit` for a bump, the patch hashes if a patch
   changed.
2. Open the PR. The "Core build" workflow builds every target and runs the REALITY lab. If a
   patch no longer applies it stops with `patch <name> does not apply to mihomo <tag>`; no core is
   produced.
3. To fix a patch: `git apply --3way core/patches/mihomo/<patch>` on a checkout of the new tag
   with the earlier patches applied, resolve, then regenerate it with `git diff` against a tree
   that has only the earlier patches. If mihomo fixed the problem, delete the patch and its entry
   below.
4. Merge. The next tag ships it; nothing else to run.

The Go pin moves the same way: Renovate opens a PR for `core/mihomo/.tool-versions` (never
automerged, since it changes the core bytes and the service pin). Take it when the Go release fixes
something the core uses (`crypto/tls`, `net`, …).

The tag is pinned; nothing forces a bump. If an area a patch touches was rewritten, stay on the
previous tag until the patch is redone.

## Verifying behaviour

`tests/reality-lab` runs real Xray servers of every generation in Docker (24.12.31, 25.7.25,
26.4.13, 26.7.11, 26.7.28, 26.9.9, 26.9.30, 26.10.21, 26.10.29, 26.11.9, each plain, with
`minClientVer: 25.1.1` and with `maxClientVer: 2.0.0`) and
probes every combination of key share, fingerprint (chrome, plus firefox, safari, chrome155, Xray's hellofirefox_148 and hellosafari_26_3 and no fingerprint on
one server per generation and every current release), policy and client version through the core
under test. Images are `teddysun/xray`; a release not published there yet (26.11.9 today) is built
from the official `Xray-linux-64.zip`, pinned by its SHA-256 in the test. The expected result of each cell comes from a model of the server generations, so a
regression in either direction fails the test. "Core build" runs it against the freshly built
core and the stock core of the same tag, on every core PR and on every release.

```bash
cd tests/reality-lab
XRAY_LAB=1 SLOTH_MIHOMO_BIN=<patched core> SLOTH_MIHOMO_STOCK_BIN=<stock core> go test -v -count=1 .
```

### Citrus lab

`tests/citrus-lab` runs real [Citrus](https://github.com/getmikan/Citrus) servers on the loopback
against the core under test: a local HTTP echo, a UDP echo and a local TLS 1.3 REALITY target, no
internet past the downloads and no Docker. Which Citrus is pinned in `tests/citrus-lab/citrus.json`:
`release` (a published build, checked against the pinned sha256 and the release's `checksums.txt`) and
`dev` (a pinned commit of the dev branch, built from source). Four stages:

1. **Citrus' own client suite.** `TestMihomoClientCitrusServer` from the Citrus checkout at that
   version (about 30 inbounds with TCP and UDP echo), run as Citrus wrote it, with the patched core as
   the client. Cases the patched core fails by design are listed in `knownDivergences` with the
   reason (today one: a client without `support-x25519mlkem768` is not refused by a
   `require-x25519mlkem768` server, because adaptive X25519MLKEM768 from 0002 retries with the hybrid
   share); a listed case that starts passing fails the test, so the list cannot go stale.
2. **The lab's matrix.** Each case has an expected outcome (traffic reaches the echo through the
   Citrus inbound, or the client is refused) for the patched core and for the stock core of the same
   tag: REALITY `mldsa65-verify` right and wrong, `min-client-ver` / `max-client-ver` against the
   default and the per-proxy `client-version`, `reality-mlkem` auto/on/off/forced against a
   `require-x25519mlkem768` server, wrong UUID and short-id, XHTTP stream-up, packet-up with the
   uplink in GET headers, XHTTP over HTTP/3, ECH in base64, `xray tls ech --pem` and wrapped form plus
   a wrong config, `type: chain` across two Citrus inbounds (the entry hop's log must show the exit
   hop, not the echo), ShadowQUIC, and XDrive `local` / `template`, which the shipped core must refuse
   without writing to the storage. A case the Citrus version does not serve is skipped by name
   (read from its source: `max-client-ver`, XHTTP/3 and XDrive are dev-only today).
3. **UDP block** (`TestCitrusUDP`), on vless-reality-vision, vless-xhttp, hysteria2, tuic-v5,
   mieru-udp and anytls, one protocol at a time: echoes of 74, 1200 and 1400 bytes, 12 calls in a
   row (a fresh UDP ASSOCIATE each, closed after), 8 at once, 200 voice-sized packets 1 ms apart with
   loss under 5 %, and `[UDP]` in the server log. The same burst with no spacing is logged, not
   asserted.
4. **Long idle** (`TestCitrusUDPLongIdle`, skipped under `-short`): one association per protocol for
   90 s, a packet every 2 s, 35 s of silence in the middle; the first packets after the silence must
   come back.

Flake handling follows the REALITY lab: fixed port bands per cell (22000 and up), at most eight
cells at once, three probes per cell, a mismatch counts only if it reproduces when the cell is run
again alone (logged as `~~` otherwise), readiness and bind checks for every process, and the process
logs of a failing cell in the output. "Core build" runs it for both Citrus versions on every core
PR and every release; `citrus-lab-head.yml` runs it weekly against the current dev head of Citrus
without gating anything.

```bash
cd tests/citrus-lab
CITRUS_LAB=1 SLOTH_MIHOMO_BIN=<patched core> SLOTH_MIHOMO_STOCK_BIN=<stock core> \
  CITRUS_LAB_FLAVOR=release go test -v -count=1 -parallel 12 -timeout 15m .
```

`CITRUS_LAB_FLAVOR` is `release`, `dev` or `head`; `CITRUS_BIN` (with `CITRUS_SRC` for its source
tree) uses a local Citrus instead; `CITRUS_LAB_LOG_DIR` keeps every process log and config. To bump
Citrus, change `citrus.json` (the sha256 is the asset's line in the release's `checksums.txt`).

## Current series

### 0001 REALITY: configurable client version

mihomo sends the constant `1.8.2` as the client version in the REALITY session id. Xray servers
configured with `minClientVer` compare it with their threshold and, when it is lower, silently
hand the connection to the cover site: the client sees a timeout. The patch turns the constant
into `tlsC.RealityClientVersion`, default `26.11.9` since 0029 (`26.9.9` before, `26.10.29`
while the former 0024 was in the series; the Xray release the core was validated against). Only `minClientVer` / `maxClientVer` depend on this value.

Xray 26.7.11 and 26.7.28 apply `minClientVer: 26.3.27` when the server config sets none
(XTLS/Xray-core af7eb680; commented out again in 47cfe999, the 26.9.8 change that also requires
the hybrid key share). A stock mihomo cannot reach a default-configured server of those two
releases at all; the patched core can, as long as the configured client version stays at
26.3.27 or above. `0.0.0` or any lower value is harmless elsewhere but fails exactly there.

Lab: servers with `minClientVer: 25.1.1`, and 26.7.11 / 26.7.28 servers without one, fail on the
stock core and pass with the patch (2026-10-23). A
server with `maxClientVer: 2.0.0` now rejects the default; 0003 lets a user who meets one set an
older version.

mihomo: out of scope for MetaCubeX. Delete when mihomo makes the version configurable or tracks
Xray releases.

### 0002 REALITY: adaptive X25519MLKEM768

Xray generations disagree about the post-quantum key share: servers before 25.x do not accept a
hybrid ClientHello, 26.9.8+ servers (XTLS/REALITY `8cdf7bf`) do not accept a classic one, 25.x and
26.4 accept both, and no server tells which it is. A server that does not accept the hello does not
fail the handshake either: it treats the client as unauthenticated and relays it to its cover site,
so the client completes a TLS handshake with the cover site's certificate and REALITY verification
fails (measured 2026-11-02 against 24.12.31 with a hybrid hello and 26.10.29 with a classic one:
`REALITY authentication failed` within 300 ms on loopback). The patch adds
`tlsC.SetRealityMLKEMPolicy` (Auto / On / Off). In Auto the outcome is remembered per server
address and public key: classic first, flip after one failure, pin after one success, flip again
after three failures of a pinned verdict (servers get upgraded). A per-proxy
`support-x25519mlkem768: true` still forces the hybrid share.

Only that signal moves the memo: a handshake counts as failed when the server sent a certificate
chain and it was not the REALITY one. Dial and DNS errors (they happen before the handshake),
resets, EOFs, timeouts, non-TLS answers and cancelled requests leave it alone, so a flaky network
cannot flip a classic-only server to the hybrid share and kill it. The cost: a server whose cover
site is unreachable closes the connection instead of relaying it, and Auto alone never learns its
key share; 0030 lets such failures buy one attempt with the other share.

Since mihomo v1.19.34 (`metacubex/utls` v1.9.0-alpha.1) the chrome (Chrome 133), firefox
(Firefox 148) and safari (Safari 26.3) parrots carry the hybrid share; ios, edge and the randomized
families cannot offer it. The fingerprint is the user's choice and is never swapped: when a hybrid
handshake is wanted and the configured fingerprint cannot produce one, its classic hello goes out
unchanged (logged once per fingerprint at debug level) and that handshake is not reported to the
memo, since it says nothing about the server. With `reality-mlkem: off` and the stock
`support-x25519mlkem768` key the handshake is the stock one.

Tests in the patch run real handshakes on loopback: servers that close, reset, stay silent past the
deadline, answer with plain HTTP, or meet a cancelled request leave the memo untouched; a cover-site
answer flips it; ios and edge keep their classic hello under a hybrid verdict and never report it;
firefox and safari offer the share from their own parrots and strip it for a classic attempt, and
chrome, firefox and safari hybrid hellos pass the XTLS/REALITY 26.9.8+ key-share gate with the
X25519 key the server uses equal to the client's auth key (Firefox one key in both shares, Safari
two).

The core logs each verdict, which is the main diagnostic signal in the Logs screen:

```
REALITY 203.0.113.7:443: handshake with X25519MLKEM768=false failed, next attempt uses true
REALITY 203.0.113.7:443: X25519MLKEM768=true confirmed
```

mihomo: issue MetaCubeX/mihomo#3193 (closed as out of scope for Xray 26.7.11+), PR #2983 ("always
send the hybrid share", closed). Delete when mihomo negotiates this itself.

### 0003 REALITY: policy from the config file

The desktop core runs as a separate process, so the policy reaches it through two top-level keys
of the generated `config.yaml`, applied on every config load (a reload undoes earlier values):

```yaml
reality-mlkem: auto                # auto | on | off; absent = auto
reality-client-version: "26.11.9"  # x.y.z, each part 0..255; absent = core default
```

Invalid values fail the config with a message naming the key. On load the core logs
`REALITY policy: client version 26.11.9, X25519MLKEM768 auto` at debug level. An unpatched mihomo ignores
unknown top-level keys, so the same config still starts on a stock core.

The app writes them from Settings → REALITY ("ML-KEM key share": Auto / Always / Never, "Client
version": empty = the subscription's value if it sets one, else the core default). Saving reloads
the running core's config.

### 0004-0015 Citrus client compatibility

[Citrus](https://github.com/getmikan/Citrus) is a mihomo fork some panels build configs for. These
twelve patches make the client side of its config surface work on this core without a fork: they are
the series [ClashFest](https://github.com/Nemu-x/ClashFest) ships as its 0003-0014, applied in the same
order after our 0003 (so ClashFest's number is ours minus one). Nine files are byte-identical to
ClashFest's, and their code comments keep ClashFest's numbering; syncing a later fix is a copy. The
four that differ:

- **0005** drops ClashFest's own `ParseRealityClientVersion` and uses the one from our 0003, which
  also takes a leading `v` (both reject anything that is not `x.y.z` with parts 0..255).
- **0008** keeps the stock fingerprint names case-sensitive, as in stock mihomo (ClashFest lowercases
  every name). The Xray table is ClashFest's: since mihomo v1.19.34 (metacubex/utls v1.9.0-alpha.1)
  both map `hellofirefox_148` and `hellosafari_26_3` to the real Firefox 148 and Safari 26.3
  parrots instead of Firefox 120 and Safari 16.0. Our test also checks that both carry the
  X25519MLKEM768 share.
- **0007** adds `github.com/metacubex/mldsa v0.1.1` to mihomo's own `go.mod` / `go.sum`: the core is
  built from mihomo's module here, while ClashFest pins the dependency in its outer module.
- **0004** carries later Citrus `dev` fixes of the XDrive client (its header lists them):
  `6f580ef9` + `a2cfd331` (a stream's write deadline or a cancelled dial fails that stream only, not
  the Mux.Cool session and its siblings; End frames are queued without blocking Close), `d447a87a`
  (a session whose conn takes over 30 s to accept one frame fails, so new dials stop queueing behind
  a dead storage), `94f62093` (a session without streams closes after 16 s, as Xray's mux client,
  instead of polling storage for the life of the proxy), `b1835036` (test of the cancelled-dial
  path), and the `transport/xdrive` hunk of `d3a24184` (the default storage TLS client verifies
  against `component/ca`'s roots, so `custom-certifactes` applies). Our own addition: a `template`
  key the decoder does not know is an error naming it, instead of being dropped; Xray's two-step
  `hrefPath` / `hrefMethod` operations (Xray `00c02777`) are not implemented and would otherwise run
  a different storage protocol than the panel configured. Mux.Cool is only used by `network:
  xdrive`; plain VLESS does not touch it.

| Patch | What a config gains | UA token |
|---|---|---|
| 0004 VLESS over XDrive | `network: xdrive` + `xdrive-opts` (Google Drive; the templated HTTP and local storages are refused in shipped builds, see 0016), client side only | `xdrive` |
| 0005 REALITY per-proxy client version | `reality-opts.client-version` overrides the global one from 0001/0003 | |
| 0006 chain groups | `type: chain`: hops in `proxies`, each dialed through the one before (`dialer-proxy` wiring at load), health check names the first broken hop | `chain` |
| 0007 REALITY mldsa65-verify | `reality-opts.mldsa65-verify` (Xray `mldsa65Verify`, share link `pqv`); without the patch the key is ignored and the client only believes it pins the server | `mldsa65` |
| 0008 Xray fingerprint names | `client-fingerprint: hellochrome_131`, `randomizednoalpn`, ... map to the nearest utls parrot instead of falling back to Go's own hello (`hellofirefox_148` and `hellosafari_26_3` to the real Firefox 148 and Safari 26.3). Only these Xray names are case-insensitive; stock names match exactly, as in stock mihomo (`Chrome` stays an unknown name) | |
| 0009 pin and name lists | `fingerprint` and `name-cert-verify` take comma-separated lists | `pin-list` |
| 0010 ECH Xray forms | `ech-opts.config` takes the `ECH CONFIGS` PEM; `ech-opts.query-server` | `ech-xray` |
| 0011 OpenVPN server identity | `verify-x509-name` / `-type`, `verify-server-name`, `tls-version-min` are enforced (vanilla ignores them: any certificate of the CA is trusted); a proxy without them is noted at debug level | |
| 0012 QUIC disable-mtu-discovery | the option on hysteria2 and masque | |
| 0013 tcp-connect-timeout | top-level `tcp-connect-timeout` (ms, 100..600000, default 5000) | |
| 0014 sniffer.skip-threshold | failed sniffs before a destination is skipped (was the constant 5) | |
| 0015 dns.serve-expired | `serve-expired`, `serve-expired-ttl`, `serve-expired-client-timeout` (RFC 8767) | |

Every patch carries its tests; the core build runs them (it vets and tests every package a patch
touches). Origin, design notes and the condition under which each patch goes away are in ClashFest's
`docs/core-patches.md`, sections 0003-0014. Not taken, by decision: DNSCrypt `sdns://`, the `PROTOCOL`
rule (it comes with a mihomo release), Citrus' XHTTP defaults, ML-KEM on by default, the protocols
Citrus removed, library forks.

### 0016 XDrive: local and template storage only in tests

SlothClash ships vanilla XDrive: Google Drive only. Two storages of 0004 let a config reach into
the user's machine and network, and both are refused wherever the node comes from (config.yaml,
http, file and inline proxy-providers, manual and local profiles):

- `service: local` keeps the storage in a folder whose path comes from the config. ClashFest refuses
  it on Android only; here the core can run under the privileged helper service (SYSTEM on Windows,
  root on macOS and Linux), where a config-chosen folder would be a privileged file read and write.
- `service: template` lets the config choose the method, URL, headers and body of every storage
  request. They are sent from the user's machine, to loopback and LAN hosts too, as soon as a health
  check dials the node: a subscription could drive requests against local services. Citrus refuses
  both services in http proxy-providers only; we refuse them everywhere.

The patch turns `xdrive.AllowLocalService` into `testing.Testing()` and adds
`xdrive.AllowTemplateService`, also `testing.Testing()`: the series' own tests keep using both
storages, a shipped binary never does. Such a node still loads, so one bad node does not fail a
subscription, but its storage (`disabledStorage`) touches no file and sends no request: every dial
through it fails with `xdrive service local (a folder on the device) is disabled in this build` or
`xdrive service template is disabled in this build`. The gate sits in the core, so it also covers
proxy-providers the core fetches itself. `TestTemplateServiceGate` checks that a refused template node sends no request to a
test server; `xdrive_hardening_test.go` checks the local gate on the built core: the folder of a local
node must not exist after the config is loaded (a build with the gate open creates it).

A service the build does not implement at all (`s3`, a typo, a different spelling such as `Template`
or `google_drive`, an empty service) is handled the same way. Vanilla 0004 fails validation for it,
and mihomo fails a whole http or file proxy-provider (or the whole config, for a top-level proxy)
when one entry fails to parse; providers the core downloads itself never pass through the app's
checks. Such a node now loads, sends no request and fails every dial with
`xdrive service "s3" is not supported by this build` (`xdrive.UnsupportedServiceError`). A refused
node, unsupported or gated off, is valid whatever its other options, since none of them is used.
Malformed options of a service the build runs are still load errors: a `google-drive` node without
three non-empty secrets, without `remote-folder` or with a bad `fronting` fails as before.
`TestUnsupportedServiceLoadsButNeverDials` checks the no-request dial through a counting HTTP
transport, `TestSupportedServiceStillValidated` the remaining load errors, and
`TestProviderLoadsUnsupportedXDriveService` parses a provider document with one unsupported and one
Google Drive node through the real provider parser: both load, only the first fails when dialed.

Delete together with 0004, or when XDrive drops local and template storage.

### 0024 (removed with mihomo v1.19.34)

0024 carried the Firefox 148 and Safari 26.3 hellos as data so that firefox and safari proxies could
offer X25519MLKEM768 to Xray 26.9.8+ servers while `metacubex/utls` 1.8.8 had no such parrots.
mihomo v1.19.34 moved to `metacubex/utls` v1.9.0-alpha.1, whose `HelloFirefox_Auto` and
`HelloSafari_Auto` are those same parrots (same extensions, groups, key shares, signature schemes
and cipher suites; Firefox reuses one X25519 key in both shares there too), so the patch was
deleted as its own condition said. Behaviour difference for users: with `reality-mlkem: off` and a
forced `support-x25519mlkem768`, firefox and safari now send the hybrid share, exactly as stock
mihomo v1.19.34 does. Its tests moved to 0002 (`TestRealityFirefoxSafariParrotsOfferMLKEM`,
`TestRealityHybridParrotsPassXrayGate`), and the REALITY lab models firefox and safari as hybrid
parrots for both the patched and the stock core.

### 0026 Citrus: root CA policy (`tls.ca-roots`, `tls.ca-denylist`)

Citrus (6183f272, 6b5ce5b9, d3a24184) lets a config choose where the roots of the core's own TLS
clients (proxies, DoH/DoT/DoQ, provider and geodata downloads) come from, and refuses chains through
state interception CAs (the Russian Trusted Root / Sub CAs, Kazakhstan's Qaznet and ISCA roots,
matched by the SHA-256 of their public key). Without the patch the core ignores both keys.

Citrus turns both on by default. Here they are opt-in: without the keys the pool is mihomo's own
(the system roots plus the `ca-certificates.crt` the build embeds, which is empty in mihomo's git and
in this build) and nothing is checked after verification, so a vanilla config behaves exactly as on
a stock core.

| Key | Effect |
|---|---|
| `ca-roots: both` | the system pool plus the embedded Mozilla bundle (`DISABLE_SYSTEM_CA` / `DISABLE_EMBED_CA` still narrow it) |
| `ca-roots: bundle` | the Mozilla bundle only: CAs installed on the device are ignored |
| `ca-roots: system` | the system pool only |
| `ca-denylist: true` | a verified chain through a denylisted CA fails with `certificate chain goes through the distrusted CA`; one clean chain is enough |

`tls.custom-certifactes` is added on top in every mode, and a chain anchored at one of them is exempt
from the denylist. `fingerprint` pinning and `skip-cert-verify` are explicit choices and are not
affected. The denylist is checked in `GetTLSConfig`, the `name-cert-verify` verifier, and the JLS,
Restls and ShadowQUIC clients that use the pool directly; with a policy set, the Sudoku `wss` client
also verifies against the core's pool instead of the platform's, and the DoH HTTP/3 probe keeps the
verifier. The XDrive storage client needs no hunk here: 0004 already builds its TLS config with
`GetTLSConfig`, so it follows the policy like every other client. The Tailscale control client is an exception, as in Citrus.

Most proxies keep the pool they were built with, and mihomo rebuilds it only in `ApplyConfig`, after
the proxies are parsed. With a policy (or when the running config had one), the config's pool is
installed before its proxies are parsed and put back if the config fails to parse; without one,
mihomo's flow is untouched.

The bundle is the file Citrus commits, curl's extract of Mozilla's store (Mozilla data of
2026-10-20), embedded as `component/ca/mozilla-roots.crt` and pinned by its SHA-256
(`796f1389…3e3cbe`) in a test. It adds about 200 KB to every core binary, whether or not a config
uses it.

Tests in the patch: the default policy builds mihomo's pool and installs no `VerifyConnection`;
`bundle` / `system` / `both` pools; a denylisted root is left out of the bundle and rejected from the
system pool; custom certificates win; pinning and `skip-cert-verify` pass; the caller's own
`VerifyConnection` still runs; every denylist entry matches its certificate (`testdata/denylist`);
the config's pool reaches an HTTPS proxy on the first parse and a failed parse restores the previous
one.

No mihomo counterpart. Delete when mihomo gets an equivalent root policy.

### 0027 Citrus: geodata SHA-256 pins (`geox-sha256`)

Citrus (395e8d4b, 85e07053) takes optional SHA-256 pins of the databases `geox-url` downloads:

```yaml
geox-sha256:
  geoip: ""   # 64 hex characters, "" = no pin
  geosite: ""
  mmdb: ""
  asn: ""
```

A pinned database with another SHA-256 is refused: a download at start goes to a temporary file
that replaces the old one only when it matches, a file already on disk that does not match is
replaced, and the updater refuses the new data. An invalid pin is a config error. Without a pin
(absent or empty) nothing is checked and the download path is mihomo's own; `/configs` shows
`geox-sha256` only when a pin is set. The app keeps the key in its place next to `geox-url`.

Tests in the patch: pin validation, per-type pins, an existing file against the current pin, a
mismatched download leaves the old file and no temporary file, an unpinned download is unchanged,
and `geox-sha256` parsing.

No mihomo counterpart. Delete when mihomo verifies geodata itself.

### 0028 XHTTP: default uplink-data-key

With `uplink-data-placement: header` or `cookie` (packet-up, uplink data in the request instead of
the body), mihomo names the chunks `<uplink-data-key>-<n>` (headers) or `<uplink-data-key>_<n>`
(cookies) and uses `uplink-data-key` as written. With the key unset it sends headers named `-0`,
`-1`, ... and cookies `_0`, `_1`, ..., which no Xray-compatible server reads: the upload is
dropped and the connection never carries data. Xray-core fills the key in when it loads the config
(`X-Data` for header and auto, `x_data` for cookie), Citrus does the same, and share links leave
`uplinkDataKey` out when it is the default, so such links did not work on this core.

The patch adds `Config.GetNormalizedUplinkDataKey(placement)`: an explicit key is returned as is,
otherwise `X-Data` for headers and `x_data` for cookies. The client builders
(`GetRequestHeaderWithPayload`, `GetRequestCookiesWithPayload`) and the inbound packet-up reader use
it, so a mihomo server with the key unset accepts uploads from Xray clients too. Only configs
that leave the key unset change, and those sent malformed requests before; an explicit key behaves
as in vanilla. Unlike Xray, a server in `auto` placement looks for cookies under `x_data` (Xray
looks for `X-Data_<n>`), the name a client in `cookie` placement actually sends. This is one
default, not the Citrus XHTTP defaults declined under 0004-0015.

Tests in the patch: default and explicit keys for both placements, a multi-chunk upload in GET
headers carrying `X-Data-0`, `X-Data-1`, ... and no header with an empty prefix, cookies named
`x_data_<n>`, and an upload built by the client read back by the server handler under the default
key for both placements. The Citrus lab cell `xhttp/reality-packet-up-GET-header-default-key`
passes on the patched core (the stock core still sends `-0`).

No upstream issue is claimed. Delete when mihomo defaults the key itself.

### 0029 TLS: Chrome 155 hello (`chrome155`)

Xray-core 26.11.9 sends Chrome 155 as its default fingerprint, and Citrus v1.0.0-beta.3 makes its
`chrome` Chrome 155 too (through its utls fork). `metacubex/utls` v1.9.0-alpha.1 (pinned by mihomo
v1.19.34) still stops at Chrome 133. The patch carries the profile as data in
`component/tls/chrome155.go` and builds the hello through `HelloCustom` + `ApplyPreset`; the
library stays the one mihomo pins. The profile is
refraction-networking/utls `6ebdceb2` (`HelloChrome_155`, checked there against a capture of Chrome
155.0.8059.40), the same one Citrus ships (getmikan/utls `a6dee38a`); the wiring follows
[Rerowros/veles](https://github.com/Rerowros/veles) (`component/tls/chrome155.go`, GPL-3.0). Against
Chrome 133 the hello gains a GREASE value and the ML-DSA-44/65/87 schemes at the head of
`signature_algorithms` (advertised only, as in the browser: nothing here verifies them) and the
`trust_anchors` extension (`0xca34`) with 28 Chrome Root Store IDs.

| Name | Hello |
|---|---|
| `chrome155`, `hellochrome_155` (any case), `hellochrome_auto` (Xray's name, any case) | Chrome 155 |
| `chrome` (stock), `chrome133`, `hellochrome_133` | Chrome 133, as in stock mihomo |

`chrome` keeps its stock meaning: a stock config sends the same hello as on a stock core, and a
Citrus subscription that writes `client-fingerprint: chrome` gets Chrome 133 here. A config that
wants Chrome 155 names it. `hellochrome_auto` is not a stock name (stock mihomo sends Go's own hello
for it), so it follows Xray. The ID is unknown to utls: every client goes through `tlsC.UClient`
(REALITY, JLS and `transport/jls` now included), and a test fails when code outside
`component/tls` calls `utls.UClient` directly. A config with a TLS session cache is handled as utls
handles its own parrots (resumption skipped instead of the HelloCustom panic). REALITY needs no
special case: the hello carries the hybrid share, and `reality-mlkem: off` or a classic Auto
verdict removes it exactly as for chrome.

The default REALITY client version moves from `26.9.9` to `26.11.9`, the Xray release the
series was checked against (an explicit `reality-client-version` still wins).

Tests in the patch: twenty generated hellos per name match the capture
(`testdata/chrome155_clienthello.hex`, from refraction-networking/utls) modulo extension order,
GREASE values, random, key material and the GREASE ECH body; `chrome` and `chrome133` stay Chrome 133
without `trust_anchors`; specs are fresh and the GREASE signature scheme is drawn per hello; a
session cache does not panic and leaves the caller's config alone; a REALITY handshake against
utls' own REALITY server authenticates with the classic and the hybrid share and puts Chrome 155 on
the wire; JLS client and server complete with `chrome155`. The REALITY lab probes `chrome155` against
24.12.31 to 26.11.9, forced and Auto.

mihomo: none claimed. Delete when mihomo moves to a utls release that ships `HelloChrome_155`.

### 0030 REALITY: one probe after silent handshakes

0002 learns a server's key share only from an answer: a cover-site certificate means the hello was
relayed. A server that closes the connection instead (an Xray 26.9.8+ server whose cover site is
unreachable, so it cannot relay a classic hello) never answers the wrong share, and Auto would stay
on it forever. The patch counts handshakes that ended before the server answered (a reset, EOF,
timeout, cancelled request, an alert or a non-TLS answer instead of a certificate chain) per
server and public key. They still never flip or unpin a verdict. After three in a row the next
attempt uses the other share once: success pins it, failure is a stale report and changes nothing,
and the count starts over. Any answer resets the count. Only Auto probes; `on`, `off` and a per-proxy
`support-x25519mlkem768` do not.

The idea is the silent probe of [Rerowros/veles](https://github.com/Rerowros/veles)
(`realityClassify`, GPL-3.0). Not taken from there: counting any failure after a ServerHello as a
refusal (here only a certificate chain that is not the REALITY one counts, the signal measured for
0002) and swapping in Chrome's hello for a fingerprint without the hybrid share (the fingerprint is
the user's choice, see 0002).

Tests in the patch: silent handshakes never move a pinned verdict; a probe after three, at most one
per round, a silent or refused probe is stale and a successful one pins; an answer in between
resets the count; no probe outside Auto; and end to end, a server that closes on a classic hello and
authenticates a hybrid one is learned on the fourth dial and stays learned.

mihomo: none claimed. Delete together with 0002.

### 0031 REALITY: a fingerprint for the hybrid handshakes (`mlkem-client-fingerprint`)

0002 never swaps the configured fingerprint: when a server needs the X25519MLKEM768 share and the
proxy's parrot has none (`ios`, `edge`, `chrome120`, `firefox120`, `safari16`, ...), its classic
hello goes out unchanged and cannot reach an Xray 26.9.8+ server. The patch adds an opt-in key that
names the hello for exactly those handshakes:

```yaml
reality-opts:
  public-key: ...
  mlkem-client-fingerprint: firefox   # any name client-fingerprint takes whose hello has the share
```

The hello is used for every handshake that offers the hybrid share (`support-x25519mlkem768`, policy
`on`, or Auto after the server relayed a classic hello); classic handshakes keep
`client-fingerprint`. A name without the share (`ios`, `chrome120`, ...) or an unknown name fails
the proxy at load time. `random` is one pick per process and may land on `ios`; chrome, one of its
picks, is used then, so the config does not fail on some starts only. Without the key nothing
changes. The choice stays with the config: the core never picks a hello on its own.

The key and its checks are [Rerowros/veles](https://github.com/Rerowros/veles) P7 (GPL-3.0); Veles
also swaps in chrome on its own for a parrot without the share, which is not taken (see 0002).

Tests in the patch: which parrots carry the share (chrome, chrome155, firefox, safari, hellochrome_131
yes; ios, edge, chrome120, firefox120, safari16, hellochrome_120 no); name checks and the `random`
fallback; on loopback, the hello is Firefox only on hybrid handshakes (Auto after a relay, `on`,
forced) and the configured iOS hello otherwise. Old parrots that share the client name of a current
one (`chrome120` is "Chrome" like `chrome`) send their classic hello under a hybrid verdict and never
reach the memo, because 0002 looks at the key shares the hello really carries, not at the client name.

mihomo: none claimed. Delete together with 0002.

### 0032 TLS: ClientHello fragmentation per proxy (`fragment-opts`)

Some networks reset a TCP connection as soon as they can read a whole TLS ClientHello with a
blocked SNI in one record or one segment. Xray's finalmask `fragment` splits the hello so they
cannot; mihomo has nothing comparable for proxies. The patch adds an opt-in block on any proxy:

```yaml
fragment-opts:
  packets: tlshello   # "tlshello" (default) or a range of writes, "1-3"
  length: 10-20       # bytes per piece (default 10-20)
  delay: 0            # ms between pieces (default 0)
  max-split: 0        # at most this many pieces, 0 = no limit
```

The names and values follow Xray finalmask `fragment` (Xray-core v26.10.21, MPL-2.0):

- `tlshello`: a first write that is a whole ClientHello record is re-framed into several TLS records
  of `length` bytes each. With delay 0 they go out in one write (the kernel segments as usual);
  with a delay each record is its own write followed by the pause.
- `N-M`: writes N to M of the connection are cut into raw TCP pieces of `length` bytes, no
  re-framing.

Everything after that passes untouched. Only TCP connections of the proxy's own dialer are wrapped
(UDP and QUIC are not), so the outer TLS or REALITY hello of `vless`, `trojan`, `vmess`, `anytls`,
... is what gets split. An invalid block fails that proxy at load time. A VLESS or VMess share link
with Xray's `fm` parameter (`{"tcp":[{"type":"fragment","settings":{...}}]}`) gets the same block
when the core parses it; other masks are ignored, and a mask that does not parse is dropped, not
the link. Without the block nothing changes: the dialer is the stock one.

Ported from [Rerowros/veles](https://github.com/Rerowros/veles) P19 (`component/fragment`, GPL-3.0).

Tests in the patch: parsing and defaults; `tlshello` merged and delayed (the records below TLS are
valid, re-assemble to the original hello, a TLS server still completes the handshake and echoes up
to 256 KiB, and later writes pass one to one); raw TCP pieces; off by default; a first write that is not a ClientHello passes untouched;
and a model of the middlebox (`dpifilter`, test-only) that resets a connection whose first record,
or first segment, holds a whole ClientHello with SNI: without the block it resets, with the right
mode it passes. The dialer of vless and trojan proxies is wrapped only when the block is set;
`ParseProxy` takes YAML numbers and refuses bad ranges; the `fm` mapping.

mihomo: none claimed. Delete when mihomo has its own ClientHello fragmentation for proxies.

### 0033 gRPC: frame reader without recursion

mihomo's gun (gRPC transport) reader called itself once per frame header, so a peer that sends a
stream of empty frames grows the stack until the process dies (a fatal stack overflow, not an
error), and a protobuf length larger than its gRPC message was taken as is. The patch reads frame
headers in a loop and refuses a payload length beyond its gRPC message (or beyond `math.MaxInt32`)
with `ErrInvalidLength`. Well-formed streams read exactly as before. The reader is shared by the
client and the server side of `grpc` transports.

From [Rerowros/veles](https://github.com/Rerowros/veles) `transport/gun/gun.go` (GPL-3.0). Tests:
200 000 empty frames under a 1 MiB stack limit (the old reader dies), oversized lengths, and a
payload split across reads.

mihomo: none claimed. Delete when mihomo's reader loops.

### 0034 XHTTP: per-direction deadlines that only end blocked operations

mihomo's XHTTP connection keeps one timer for both directions and closes the whole connection when
it fires, busy or idle; `SetReadDeadline` and `SetWriteDeadline` both reset that one timer, so
setting one direction moves (or with a zero time clears) the other. A multiplexer that keeps a
write deadline ahead of its writes (yamux, smux) tears down an idle session that way. The patch
gives each direction its own deadline: an operation that starts after it fails at once with
`os.ErrDeadlineExceeded`, one still blocked when it passes is ended by closing the connection (the
only way to unblock the HTTP body), and an idle connection stays open. `Close` runs once.

From [Rerowros/veles](https://github.com/Rerowros/veles) `transport/xhttp/conn.go`, which took it
from getmikan/Citrus (GPL-3.0). Tests: an idle deadline keeps the connection open and later
operations time out until it is moved, a blocked read ends with a timeout, a cleared deadline has no
effect, and `Close` runs its hooks once.

mihomo: none claimed. Delete when mihomo's XHTTP conn keeps separate deadlines.

### 0035 Share links: no credentials in the skip warning

When the core parses a subscription of share links (a proxy-provider whose body is not YAML) and a
`vless://` or `vmess://` link fails, mihomo logs `error:... line:<the whole link>`: the UUID, the
REALITY keys and any password end up in the log, which users paste into bug reports. The patch logs
`skip vless link to <host:port> (<name>): <error>` instead. Nothing else changes; the link is
skipped as before.

From [Rerowros/veles](https://github.com/Rerowros/veles) `common/convert/converter.go` (GPL-3.0).
Tests: a failing VLESS link with a REALITY key and a failing VMess link are logged without the UUID,
the key or the query, and with the server and the name.

mihomo: none claimed. Delete when mihomo stops logging the link.

### 0036 external-ui: never a directory that holds the core's files

`external-ui` (with `external-ui-name`) is served at `/ui` without the controller secret, and a UI
update (`POST /upgrade/ui`) first empties that directory. mihomo only checks that the path is inside
the home directory or a `SAFE_PATHS` root, so a config, a subscription included, can set
`external-ui: ./`: `/ui/config.yaml` then serves the running config (the controller secret, every
node's credentials) to anything that can reach the controller, a web page through DNS rebinding
included, and a UI update would delete the core's files.

The patch adds `C.Path.HoldsCoreState` (the home directory, the directory of the config file, a
`SAFE_PATHS` root, or an ancestor of one of them) and `config.CheckExternalUI`. Such an
`external-ui` does not fail the config, since one bad key from a subscription should not stop the
core: the UI is turned off with the warning `External UI disabled: external-ui ... would hold the
files of the core`, so `/ui` serves nothing and the updater keeps its default path. The `-ext-ui`
command-line flag with such a directory is a fatal error. A UI in a subdirectory (`ui`,
`ui/zashboard`, the default) is unchanged.

From [Rerowros/veles](https://github.com/Rerowros/veles) `constant/path.go`, `config/config.go` and
`main.go` (GPL-3.0). Tests: `HoldsCoreState` for the home, its parents, a `SAFE_PATHS` root, a
config directory outside the home and their subdirectories; `CheckExternalUI` for the cases above;
a config with `external-ui: .`, `./` or the home directory loads with the UI off and the warning, and
`ui` + `zashboard` is kept.

mihomo: none claimed. Delete when mihomo refuses such a directory itself.

### 0038 Relay: small buffer while idle, batched writes while busy, lazy `BufferedConn` reader

`N.Relay` copies both directions with sing's `bufio.Copy`. For a source that is neither a socket nor
a read waiter, which is every TLS-based outbound (Trojan, VLESS/VMess over TLS, REALITY), that copy
takes a 32 KiB pool buffer before each read: an idle proxied connection holds one on its download
side for as long as it stays open, and during a download every decrypted TLS record goes to the
inbound socket with a `write(2)` of its own. Every inbound connection is also wrapped in a
`BufferedConn` whose 4 KiB reader was made at once and, once the connection had been peeked
(sniffing, the mixed port), kept for the whole relay.

The patch has three parts, each in `common/net`:

- **Small buffer while idle** (`relay_copy.go`): `copyConn` is `bufio.Copy` with another fallback for
  such sources. It reads into a 2 KiB buffer while the connection looks idle and into full pool
  buffers while data keeps coming; a read that fills the small buffer switches up, two short reads
  in a row switch back. Cached data, Vision's replaceable reader and writer, splice and read waiters
  (sockets, Shadowsocks) keep `bufio.Copy`'s own code.
- **Batched writes** (`relay_batch.go`): while a transfer runs on full buffers and the destination
  unwraps to a socket, a reader goroutine decrypts up to 8 buffers ahead and the copy writes
  everything read meanwhile with one `writev(2)`. The reader never waits for data: when the burst
  ends it hands the source back, so an idle connection has no extra goroutine or buffer. Order is
  kept (one reader, a FIFO channel); a read error or EOF is returned only after every buffer read
  before it was written, so half-close works as before; after a write error the reader exits when
  the relay closes the connection. Uploads (socket to proxy) and socket-to-socket copies (splice,
  `DIRECT`) do not go through it.
- **Lazy `BufferedConn` reader** (`bufconn.go`): the reader is made on first use, a read without one
  goes straight to the connection, and `ReaderReplaceable` drops a drained reader when the relay
  unwraps the connection. Buffered data is never dropped.

From getmikan/Citrus (GPL-3.0) `common/net` (commits 6c1d881e, d23b8008, 415e7791, 316b3af7,
8c11680f, b00599ab), in the form [Rerowros/veles](https://github.com/Rerowros/veles) carries it as
its P12. Not taken, measured or judged not worth the risk here: Citrus' wait on a TLS connection
without a buffer and its AnyTLS queue (both need Citrus' own forks), the 64 KiB socket reads of the
upload side (no gain in the measurement below, and they change the write size every outbound
protocol sees), and the scheduler yields of the copy loops.

Measured in Docker (golang:1.26 image, Linux): load generator → SOCKS on the client core (limited to
2 CPUs) → Trojan (TLS) or Shadowsocks 2022 → stock v1.19.34 + series server (4 CPUs) → target, all
on one Docker network; stock series vs series + 0038, interleaved runs, medians. The host ran other
Docker labs at the same time, so single runs vary by up to ±30 %.

| Trojan download, 8 s, 7 runs each | stock series | + 0038 | paired median | wins |
| --- | --- | --- | --- | --- |
| 1 stream | 991 MB/s, 1.07 CPU s/GB | 1251 MB/s, 1.18 CPU s/GB | +18 % MB/s | 6/7 |
| 4 streams | 2276 MB/s, 0.88 CPU s/GB | 2663 MB/s, 0.75 CPU s/GB | +25 % MB/s, −20 % CPU/GB | 6/7 |
| 16 streams | 2100 MB/s, 0.97 CPU s/GB | 2897 MB/s, 0.70 CPU s/GB | +34 % MB/s, −26 % CPU/GB | 7/7 |

The batched writes give all of the throughput gain; the small buffer alone is neutral (paired median
0.91-1.05 of stock). A single stream uses the second core for decryption while the first writes, so
it is faster at slightly more CPU per GB.

Client RSS growth for 2000 idle connections (3 runs each, first round): Trojan 153 MB stock, 134 MB
with the lazy reader alone, 93 MB with the small buffer as well (a second round, stock against the
whole patch: 140 MB → 96 MB, about 22 KB less per idle connection). Shadowsocks 2022 is a read waiter
and holds no idle buffer already: 70 MB → 71 MB. Uploads, Shadowsocks 2022 and `DIRECT` throughput
were unchanged within the noise (5 builds × 3 rounds × 9 tests).

Tests (from Citrus, cut to what is ported): copies of up to 8 MiB through the adaptive and the
batched copy with reads of every size; the exact buffer size of every read through a burst and back
(with and without Vision's replaceable reader); batches gathered while a write is blocked are
bounded by the read-ahead and keep their order; a write error ends the batched copy with that error
and the reader goroutine exits; a relayed TLS connection with a 6 MiB upload, a half-close and a
5 MiB answer; the `BufferedConn` reader made only by a peek, dropped once drained, and dropped while
a relay waits on a peeked connection. `common/net` passes with `-race`.

mihomo: none claimed. Delete when sing's `bufio.Copy` stops holding a pool buffer while it waits on
a TLS source and writes a burst with one call, and `BufferedConn` makes its reader lazily.

### 0040 Listeners: closed flag without a data race

The accept and read loops of 20 inbound listeners in mihomo (socks TCP/UDP, http, mixed, redir,
tproxy TCP/UDP, tunnel TCP/UDP, shadowsocks TCP/UDP, sing_shadowsocks, sing_vmess, sing_vless,
trojan, snell, anytls, sudoku, tuic, shadowquic) check a plain `closed bool` after `Accept` or
`ReadFrom` fails, to tell a closed listener from a transient error, while `Close` sets it from
another goroutine. That is a data race. After a single `Close` the race detector does not see it
(closing the socket happens to order the two accesses), but a second `Close` writes the flag again
with nothing ordering it against the loop's read. The chain-group tests of 0006 close their
listeners twice (once in the test, once in cleanup), so `go test -race ./config` failed on vanilla
v1.19.34 with races in `listener/socks` and `listener/sing_shadowsocks`. The patch makes the flag
an `atomic.Bool` (`Store` in `Close`, `Load` in the loops); nothing else changes. Listeners that
only ever write the flag (sing_hysteria2, hysteria2_realm, sing_tun, trusttunnel) are left alone.

User impact: none observed. A racing `bool` read on amd64/arm64 gives a stale value at worst, and
the loop sees the error again on the next turn; the fix is about correctness under the Go memory
model and a clean `-race` run.

Own code (SlothClash). Test: `listener/close_race_test.go` opens each of the 16 inbound types above
(socks, http and mixed with UDP, shadowsocks with an AEAD and a stream cipher, which take the sing
and the embedded server; tproxy is skipped where it cannot listen: without CAP_NET_ADMIN on Linux,
and on other systems), closes it, waits 100 ms for the loops to read the flag and closes it again.
Without the fix `go test -race` reports 20 races there (every changed file; the shadowquic one only
when `Serve` returns an error, so not on every run); with it, none. Without `-race` it checks that a
second `Close` neither panics nor hangs.

**Race job.** `node scripts/core-build.mjs --test-only --race` runs the tests of every patched
package under the race detector (cgo on, no vet), and the `patches (race)` job of
`core-build.yml` runs it on ubuntu. With this patch every patched package passes: 4 runs in a
golang:1.26 container capped at 4 CPUs, warm build cache, took 83–97 s for the tests (median
84 s), no race and no failure; the CI job, clone and cold race build included, took 3 min 21 s on
its first run. Packages the series does not touch are not run: `listener/inbound` (mihomo's own
inbound tests) still reports races in `transport/vless/vision` and `transport/anytls/session`,
which are out of scope here.

mihomo: none claimed. Delete when mihomo's listeners use an atomic (or `net.ErrClosed`) for this.

### 0041 REALITY: chrome when no `client-fingerprint` is set

REALITY only works over uTLS, and stock mihomo fails every dial of a REALITY proxy without
`client-fingerprint` with `REALITY is based on uTLS, please set a client-fingerprint`. Xray clients
send chrome when a share link has no `fp=`, so such links (and hand-written configs that leave the
key out) worked everywhere but here. The patch dials them with the `chrome` hello. That is Chrome 133,
the stock meaning of `chrome` (0029 leaves it alone); a config that wants Chrome 155 names
`chrome155`.

Only the empty value changes. `client-fingerprint: none` (and Xray's `unsafe` / `hellogolang`, see
0008) still means no uTLS and still fails the dial; an explicit fingerprint is used as written; a
plain TLS proxy without the key keeps Go's own hello. The default is applied in `StreamTLSConn`,
which every REALITY dial goes through (VLESS, VMess and Trojan, every transport, proxy-providers
included), after config parsing, so 0031's `mlkem-client-fingerprint` and 0002's key-share choice
see chrome like an explicit `chrome`.

Ported from [Nemu-x/ClashFest](https://github.com/Nemu-x/ClashFest) core patch 0032 (PR #291,
GPL-3.0), which takes it from [Rerowros/veles](https://github.com/Rerowros/veles) P7 (GPL-3.0).

Tests in the patch (`transport/vmess`, over a pipe, reading the ClientHello the dial sends): without
the key the REALITY hello is chrome's (cipher suites, groups, signature schemes and versions, GREASE
normalised, 32-byte REALITY session id) and carries no ML-DSA scheme, so it is Chrome 133 and not
Chrome 155; `none`, `unsafe` and `HelloGolang` still fail; `firefox`, `safari`, `ios` and `chrome155`
keep their own hellos; plain TLS without the key sends the same hello as with `none`, not chrome's.
The first test fails on the series without this patch. The REALITY lab has a no-fingerprint row (Auto
and forced) on every browser server: the patched core passes it like `chrome`, the stock core fails
it.

mihomo: none claimed. Delete when mihomo picks a default fingerprint for REALITY itself.

### What else Rerowros/veles changes

[Veles](https://github.com/Rerowros/veles) (v1.19.33-veles.3) is a mihomo fork. 0029, 0030,
0031-0036, 0038 and 0041 take parts of it. The rest is not taken:

- **Already here:** client version 26.11.9 (0001/0029), per-proxy client version (0005), Chrome 155
  (0029), the ML-KEM memo with the silent probe (0002/0030), XDrive (0004).
- **Changes stock behaviour:**
  - `chrome` meaning Chrome 155;
  - the automatic swap to chrome for a parrot without the hybrid share;
  - XHTTP packet-up uploads with several requests in flight (P5/P8);
  - the controller refusing a non-loopback address without a secret (P17);
  - redacted `GET /configs`.
- **Server only:**
  - per-user traffic, limits and IP limits (P10/P18);
  - the Xray gRPC API (P11);
  - pre-auth checks of inbounds (P13);
  - fallback pages (P14);
  - `/metrics`, `/healthz`, `/events` (P16);
  - the PROXY protocol;
  - the AnyTLS stream cap;
  - the VLESS addons and XHTTP upload-queue bounds;
  - the Linux GC ballast;
  - `age` CLI key files.
- **Too large for a patch here:** the VLESS `mux.cool` client and server (P15).

## What the app does with the Citrus surface

- **User-Agent.** The subscription User-Agent advertises Citrus client compatibility the way a
  Citrus core writes its own: `clash.meta/v1.19.34 citrus/v1.0.0-beta.2 SlothClash/0.10.1`. The
  `clash.meta/` prefix and the core version stay first, so panels keep selecting the format and the
  core version as before; panels tell a Citrus client from a stock mihomo by `\bcitrus/(\S+)`. The
  citrus tag is the manifest's `citrusCompat`: the latest Citrus release whose client surface the
  compat suite covers, not our core version. It is bumped together with a Citrus release sync, and
  `scripts/core-build.mjs` fails the build on a value that is not a `vX.Y.Z[-pre]` tag. prebuild
  writes it into `core-build.json` only for our own build: a stock core (`MIHOMO_CORE_VERSION`) or a
  `SLOTH_CORE_FILE` binary sends no citrus token, and neither does a manifest without the field.
- **Capability tokens.** A patch may declare a `feature` in the manifest. prebuild writes the tokens
  of the series it actually built into `core-build.json`, and the app checks them before using a
  capability only the patched core has (Desync). They are not sent to panels.
- **XDrive hardening.** The config pipeline drops `listeners` entries with `xdrive-config` (an XDrive
  inbound polls cloud storage: a reverse tunnel into this machine that no bind address contains) and
  turns `service: local` and `service: template` nodes (value trimmed, any case), in `proxies` and
  inline provider payloads, into `type: reject` with the same name. The `XDRIVE-1` debug log counts
  them separately (`localNodesRejected`, `templateNodesRejected`). Keys are matched the way the
  core's decoder matches them (`Xdrive_Opts` counts). Both run again after a profile's JavaScript
  override, so a script cannot bring them back.
- **XDrive services.** The shipped build runs `service: google-drive` (and its alias `Google Drive`)
  only. Any other service, such as `s3` from newer Xray, a typo or a missing one, would make mihomo
  fail the whole config (a top-level proxy that does not parse) or the whole provider, so
  `xdrive_unknown_service.go` turns such a node, in `proxies` and inline provider payloads, into
  `type: reject` with the same name, next to the hardening above and again after the script.
  Providers the core fetches itself are outside the app's reach; there the core's error still fails
  that one provider.
- **Credentials.** `xdrive-opts.secrets`, `template.auth` and credential-looking headers, query values
  and bodies of template operations are shown as `[redacted]` in the script preview; credential query
  values (`token`, `key`, `signature`, ...) are redacted in the log views and the TUN diagnostics. The
  profile config editor keeps the real values, since saving it writes the file back.
- **Chain groups** reach the core with exactly the hops the config lists: the generic group cleanup,
  which drops unknown members and falls back to DIRECT, never touches them (a dropped hop would
  silently shorten the chain). A chain the core would refuse (an unknown, repeated or self hop, fewer
  than two hops, a built-in after the first hop, `use` / `include-all` / `filter`, or a later hop that is
  itself such a chain) becomes `select [REJECT]` with the same name, so the rest of the subscription
  loads and traffic for it fails closed. The Proxies screen shows a chain as hops in dial order, not
  selectable, with the hop the last health check failed at.
- **`tcp-connect-timeout`** has its place in the generated layout next to the other TCP keys.

## Direct TCP desynchronization

Patch **0017** links the separately published `github.com/Nemu-x/desync` library at an exact version and checksum. An optional `desync` mapping on a named `direct` outbound wraps the TCP socket before its first TLS write; the ordinary DIRECT route and UDP behaviour remain unchanged. The parser validates settings, rejects unsupported native fake platforms and disallows TFO/MPTCP for these outbounds. Tests verify configuration decoding and that the core's buffered copy path retains the transformation.

The client injects service-specific routes only when explicitly enabled in Settings. See [Desync](desync.md) for controls, production-subscription testing and rollback. Windows patch tests also execute the standalone library's native tests.

## Citrus compatibility suite

`apps/sloth-clash-desktop/citrus_compat_test.go` runs Citrus' own client-side examples through the
app's config pipeline and then the shipped core's preflight (`mihomo -t`), and reports per case
whether the pipeline kept every Citrus-only key, whether the core accepts the config, and whether it
silently ignores a key Citrus reads (the key is replaced by a mapping and then a sequence; a key the
core decodes fails for one of them). Fixtures and `manifest.json` in `testdata/citrus/` are ClashFest's
(its `docs/core-patches.md` describes them and how to refresh them from a newer Citrus commit). The
test fails on an `accept` case that is rejected, loses a key in the pipeline or has an ignored key not
listed as harmless, and on an `unsupported` / `deferred` case that no longer shows a gap. Fingerprint
names are resolved by the core only when dialing, so they are not probed here; patch 0008 tests its
whole table on every core build.

```bash
cd apps/sloth-clash-desktop
SLOTH_MIHOMO_BIN=<patched core> go test -count=1 -run TestCitrusCompat -v .
```

Without a core that carries the series the test skips. Outcomes at Citrus `22da58f1` (dev, the
v1.0.0-beta.3 release candidate, 2026-11-11): 161 cases, 153 accept, 6 unsupported, 2 deferred (at
`62d1f567`: 156, 149 / 6 / 1; at `ecaa9466`: 145, 140 / 4 / 1).

New client surface between `62d1f567` and beta.3, case by case:

- **accept**: TUN `dns-mode` (`citrus-tun-dns-mode`; a mihomo v1.19.33 key Citrus took over, the
  pipeline keeps it), the names `chrome155`, `chrome133` and `hellochrome_155` in
  `citrus-xray-fingerprints` (0029). Citrus' `chrome` became Chrome 155; ours stays Chrome 133 as in
  stock mihomo (0029 explains why). The REALITY client-version and fingerprint comments of the
  examples follow Citrus (26.11.9, Chrome 155).
- **deferred**: Snell v6 (`citrus-snell-v6`): `version: 6` and its `mode` are not in mihomo
  v1.19.33, so the core rejects such a proxy (and with it a provider or a top-level config). It comes
  with the next mihomo release, from which Citrus took it. That release also accepts `mode:
  unsafe-raw` (no PSK, no encryption), which Citrus refuses: decide on that core bump.
- **not a client key**: `mux-option.max-streams` (sing-mux inbound limit), the Snell v6 and mieru
  `tcp-idle-timeout` listener options, the listener `dns-mode`. Nothing new in `tls.ca-roots`,
  `tls.ca-denylist`, `geox-sha256` (0026, 0027) or the `type: desync` options since `62d1f567`.

- **accept**: everything before, plus `geox-sha256` in the general example (0027),
  `tls.ca-roots` / `tls.ca-denylist` (`citrus-tls-ca-policy`, 0026), the `type: desync` outbound of
  `docs/config.yaml` with every option uncommented and of the desync site page (0023; all 28 option
  paths are read), the `vless-xdrive` Google Drive proxy (0004), and the new interop clients: XHTTP
  over HTTP/3 (`alpn: [h3]`, three modes; vanilla) and the two Citrus clients of the sing-box suite.
- **unsupported**: `PROTOCOL` rule, DNSCrypt, BitTorrent sniffer, server log format, and XDrive
  `service: local` / `service: template` (`citrus-xdrive-local`, `citrus-xdrive-template`; owner
  decision, see 0016). The template case passes because the app turns such nodes into `reject`.
- **deferred**: TrustTunnel timers.
- `skip-host-key-check` is ignored and harmless.

Not copied, by the refresh rules: `egress-guard`, PROXY protocol, `fallbacks` / `fallback-page`,
listener `alpn: [h3]`, `zero-rtt` and KCPTun server keys (server side), `external-ui-sha256`,
`external-ui-allow-insecure` and the WebSocket ticket options (controller). The Windows TUN
forwarding note is a comment and a runtime event, not a key.

Running it the first time found two app bugs, fixed with it: a subscription's own url-test group
named `Auto` that another group uses was pruned as if it were the app's synthetic fallback group, and
`SUB-RULE` targets were checked against proxies instead of `sub-rules`.

## Controller diagnostics

Patch `0019-desync-dns-udp.patch` adds explicit DNS fallback for named Desync outbounds when direct DNS returns no IP, plus a validated Windows UDP fake option with per-outbound counters. The ordinary direct route and subscription resolvers are retained. Tests cover cold empty resolution, retry scope/cancellation, UDP configuration, native datagram integrity and the core wrapper path. Desktop media routing is enabled independently in Settings; live network success remains an owner-run measurement.

Patch `0020-desync-auto-diagnostics.patch` provides the `desync-auto` capability used by automatic client calibration. Its authenticated UDP probe is limited to an owned candidate and an active public Discord UDP endpoint, sends a bounded discovery sequence, and validates complete replies. Separate bounded application-only activity counters distinguish discovery from RTP-shaped traffic without storing payloads; internal probes do not populate them. For delay tests that opt in with `detail=1`, controller request cancellation stops the pending probe and an explicit expected HTTP status range is enforced (`503` on a mismatch); without it `GET /proxies/{name}/delay` keeps the stock timeout and reports the latency of a mismatching response. Tests cover endpoint scope, mismatched replies, initial-only response loss, cancellation, telemetry bounds and error-page rejection. Candidate selection, network caching and recovery policy live in the client rather than the transport library.

Patch `0018-probe-error-details.patch` makes `GET /proxies/{name}/delay?...&detail=1` return the URL-test error message (`504` with the timeout and its cause, `503` with the dial/TLS error) and log the failure with the outbound name at debug level. Without `detail=1` the endpoint answers exactly as stock mihomo (`504 {"message":"Timeout"}`, `503 {"message":"An error occurred in the delay test"}`), so panels and scripts written for stock mihomo see no change. The app's Desync probes send `detail=1` when the core has no transfer test. Tests cover both forms for dial/TLS failures, timeouts, zero-delay failures and successful responses; log assertions wait for the matching event under one fixed deadline and tolerate unrelated asynchronous log events. A controller `503` indicates a failed core test, not necessarily an upstream HTTP status.

Patch `0021-desync-stable-udp-binding.patch` adds the explicit `desync.udp.stable-binding` option. It reuses a native socket only for the same Discord TUN source/process/inbound/interface/server/discovery-session identity, across local port replacement. Discovery replies and logical deadlines remain independent; private socket starts, other traffic and inner probes are excluded from sharing. Buffers, memberships and idle lifetime are bounded, and adapter shutdown owns cleanup. Private diagnostics expose aggregate counts only. Regression coverage includes concurrent sessions, handover/old-close behavior, native library fake-window continuity, deadlines, bounds, shutdown and a 75-second native core PacketConn test. Remove this patch when upstream offers equivalent scoped association continuity and lifecycle guarantees; no upstream issue is claimed.

Patch `0022-desync-profile-reserve.patch` adds bounded authenticated prospective reserve probes for the three generated service selectors. Fixed HTTPS targets or actually observed Discord UDP endpoints are evaluated through the original full rule engine with only generated overlay targets skipped. Probes never change live selectors; results contain availability/error categories without subscription node names. DIRECT/REJECT and unsupported UDP do not qualify as remote reserve. `desync.reject-quic` rejects UDP/443 only inside explicitly configured Desync candidates, allowing selector PASS to restore the profile's QUIC policy as well as TCP rules. Tests cover full rule order, process/corporate precedence, PASS, local HTTPS success/error validation, strict request scope, endpoint ownership and UDP capability. Client hysteresis and sticky recovery remain outside the transport library.

## Desync outbound

Patch `0023-desync-outbound.patch` adds `type: desync`, a first-class outbound for the same client-side
library, and moves the series to `github.com/Nemu-x/desync` `v0.2.1` (commit 673cbef,
Nemu-x/desync#11), exact version and checksum in mihomo's `go.mod` / `go.sum`. It is a DIRECT connection whose first TLS ClientHello leaves the
client's own socket transformed; non-TLS traffic and everything after the ClientHello pass through.
Named `direct` outbounds with a `desync` mapping (0017-0022) keep working unchanged during the
migration; the app generates `type: desync` outbounds from the desync presets catalog
(docs/desync.md, "Strategy catalog").

The keys, values, defaults, validation and the `/proxies` fields under `desync` are those of Citrus'
`type: desync` (getmikan/Citrus#172, `adapter/outbound/desync.go`, `docs/config.yaml`), so a client
generates one config for both cores. Only `dns-fallback`, `reject-quic` and `udp.stable-binding` are
SlothClash's own (Citrus does not have them); Citrus' egress guard has no counterpart in mihomo and
is not ported.

```yaml
proxies:
  - name: SLOTH-DESYNC-YOUTUBE
    type: desync
    tlsrec: "1+s"              # TLS record cuts, comma-separated ("3", "1+s", "-2+s")
    split: "1+s"               # TCP write cuts
    split-delay: 5             # ms between pieces, 0-1000
    split-wait: sent           # sent | acked (absent = no wait; macOS: acked only; Android: sent per connection)
    split-wait-timeout: 2000   # ms per wait (also bounds disorder), 0-60000, default 2000
    disorder: true             # first split piece with hop limit 1 (ByeDPI --disorder)
    oob: false                 # urgent byte after the first split piece; with disorder = disoob
    oob-byte: "a"              # one ASCII character, default "a"
    random:                    # random cuts per connection; count and min-piece default to 1
      tlsrec: [{from: "-8+s", to: "8+s", count: 2, min-piece: 2}]
      split: [{from: 1, to: 120}]
      seed: 0                  # 0 = a new seed per connection
    fake:                      # low-TTL decoy (Windows, Linux, Android per connection)
      ttl: 4                   # with auto-ttl the fallback (0 = no fallback)
      auto-ttl: {delta: 1, min: 3, max: 20}   # {} = these defaults; absent = off; observable on Linux/Android over IPv6
      prefix: -1               # end of the fake piece; with split it is the piece ending here
      timeout: 3000            # ms
      sni: [www.example.com, "cdn-##.example.net"]   # decoy ClientHello
      randomize: true
      from-original: false
      offset: 0
    dns-fallback: true         # SlothClash only (0019)
    reject-quic: true          # SlothClash only (0022)
  - name: SLOTH-DESYNC-DISCORD-UDP-1
    type: desync
    udp:                       # low-TTL burst before the first datagrams per peer (all OSes)
      ttl: 4
      repeats: 6               # 1-16
      packets: 3               # 1-8
      template: quic           # zero (default) | quic
      sni: www.wikipedia.org   # with template: quic
      quic-only: false
      stable-binding: true     # SlothClash only (0021)
```

What an OS cannot do is a configuration error naming the outbound, the method, the OS and the
library's reason (`desync.PlatformCapabilities`), never a silent plain DIRECT: `fake` on macOS and
`split-wait: sent` on macOS. On Android the library lists `split-wait: sent`, `disorder` and `fake`
in `Capabilities.Conditional`: apps may not read `SIOCOUTQNSD`, so unsent bytes come from `TCP_INFO`
`tcpi_notsent_bytes` (Linux 4.6+), decided per socket. As in Citrus, such a method loads and logs once
per outbound that it is verified per connection; a socket without the field fails the connection
before any byte is sent, with the library's `ErrSplitWaitUnsupported` / `ErrDisorderUnsupported` /
`ErrFakeUnsupported`, never a substitute. As in Citrus, `auto-ttl` fails only where it can never derive a TTL
(Windows, macOS, Linux with `ip-version: ipv4`) and no fallback `ttl` is set; with a fallback it
loads and logs that every fake uses it, and on Linux without a fallback it logs that IPv4
connections fail. Options are parsed on every load, validation and snapshot of a
configuration (with proxy auto-selection, many times per load), so each of these notes is logged once
per process: the same outbound name, methods and OS never repeat it. `tfo`, `mptcp` and `dialer-proxy` are refused. The transformed connection hides
the socket and the transformer from relay fast paths on writes (no `NetConn`/`Upstream`/
`SyscallConn`, no replaceable writer); reads go to the socket directly. UDP is Direct's path with the
0019-0022 options, so the burst counters, activity, the stable binding pool and the profile-reserve
classification (a desync outbound counts as `direct`) work as for `direct.desync`.

`/proxies` shows type `Desync` and, under `desync`, Citrus' `strategy` (`static`), `tcp` (`tlsrec`,
`split`, `split-delay`, `split-wait`, `split-wait-timeout`, `disorder`, `oob{byte}`,
`random{tlsrec,split,seed}`, `fake{ttl,auto-ttl{delta,min,max},prefix,timeout,sni,randomize,
from-original,offset,last-ttl{ttl,auto}}`) and `udp` (`ttl`, `repeats`, `packets`, `template`, `sni`,
`quic-only`, `counters`), with defaults filled in; SlothClash adds `dns-fallback`, `reject-quic` and
`udp.stable-binding`. The 0019-0022 diagnostics stay at the top level under the keys of
`direct.desync` (`desyncDNSFallbacks`, `desyncUDP`). The dial takes its options from a strategy seam,
static for now. The feature token is `desync-outbound`.

Remove when the client generates only `type: desync` and mihomo carries an equivalent outbound; no
upstream issue is claimed.

## Transfer test

Patch `0025-proxy-transfer-test.patch` adds `GET /proxies/{name}/transfer`, a delay test that also
reads the response body. The delay test (`adapter.URLTest`) sends one HEAD over HTTP/1.1 and passes
when the headers arrive, a few hundred bytes. Some networks let a TLS connection through and stop
forwarding server data after the first tens of kilobytes (the connection stays open and freezes),
so a desync strategy can pass the delay test and still leave the service unusable.

```text
GET /proxies/{name}/transfer?url=https://discord.com/login&timeout=5000&expected=200-299&min-bytes=32768&max-bytes=65536&stall=2000
200 {"delay":298,"elapsed":343,"bytes":32768,"status":200,"protocol":"HTTP/2.0"}
503 {"message":"transfer stalled after 16384 bytes: nothing arrived for 2000 ms","bytes":16384}
```

The test dials through the named proxy, sends GET with ALPN `h2` and `http/1.1` and
`Accept-Encoding: identity` (so `bytes` is what the network carried), verifies the certificate
against the system roots for the URL's host and does not follow redirects. It passes when
`max-bytes` (1-1048576, default 1048576) arrived, or the body ended after at least `min-bytes`
(default 0), with no pause longer than `stall` ms (100 to `timeout`, default 3000) between reads.
`url` must be `https`; `timeout` is 1-60000 ms; `expected` is the delay test's status filter. A stall
or a short body is `503` with the bytes received, the deadline `504`; failures go to the warning log
with the proxy name, as 0018 does for the delay test. The test records no delay history. The feature
token is `transfer-test`; the app uses the test for every desync HTTPS probe when the token is
present (docs/desync.md).

Tests: HTTP/2 and min-bytes against a local TLS server, a connection frozen after 16 KB reported as
a stall with its byte count well before the deadline, a short body, an error status, the deadline,
and query validation. Checked live through DIRECT: discord.com/login and www.youtube.com/ deliver
32 KB over HTTP/2, the small Discord and YouTube targets pass with their whole bodies.

Remove when the client no longer needs transfer-level probes; no upstream issue is claimed.

## Lab results (2026-10-04, Windows, same host and containers for every run)

Stock v1.19.32 against the patched core, `chrome` fingerprint; "forced" = per-proxy
`support-x25519mlkem768: true`; patched columns in Auto.

| Server | stock classic | stock forced | patched classic | patched forced |
|---|---|---|---|---|
| 24.12.31 | OK | FAIL | OK | FAIL (expected) |
| 25.7.25, 26.4.13 | OK | OK | OK | OK |
| 26.9.9 | FAIL | OK | OK | OK |
| any, `minClientVer: 25.1.1` | FAIL | FAIL | OK | as the row of its version |
| any, `maxClientVer: 2.0.0` | as the row of its version | as the row of its version | FAIL (expected: 26.9.9 > 2.0.0) | FAIL (expected) |

- firefox / safari / ios / edge against 26.9.9: stock 0/10, patched 20/20 over two runs.
- XHTTP + REALITY against 26.9.9: stock classic FAIL, patched OK. (24.12.31 rejects mihomo's
  XHTTP on any core, `invalid x_padding length:0`; a server-age limitation.)
- 0003 keys: `off` fails 26.9.9 classic, `on` fails 24.12.31, `auto` passes everything above,
  `"1.9.0"` revives `maxClientVer: 2.0.0` and fails `minClientVer: 25.1.1`; the stock core with
  the keys behaves exactly like the stock core without them.
- A real 34-node subscription (tcp+vision and xhttp, chrome and firefox, older Xray): stock 34/34,
  patched 34/34, every node `X25519MLKEM768=false confirmed` on the first attempt.
