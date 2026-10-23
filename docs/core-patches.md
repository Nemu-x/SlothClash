# Core patch series

SlothClash ships [mihomo](https://github.com/MetaCubeX/mihomo) built from an unmodified
MetaCubeX release tag plus a short series of patches kept in
[`core/patches/mihomo/`](../core/patches/mihomo). There is no SlothClash fork of mihomo, and
there must not be one: a patch that is no longer small enough to rebase in minutes is a sign to
get it into mihomo or drop it.

Settings → Info shows which core a build carries (`mihomo v1.19.33 + 16 SlothClash patches`)
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

`constant.Version` stays the plain tag (`v1.19.33`). The core's `/version`, the User-Agent it
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
  series touches, on every PR that touches the core and on every release.
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
26.4.13, 26.7.11, 26.7.28, 26.9.9, 26.9.30, 26.10.21, each plain, with `minClientVer: 25.1.1`
and with `maxClientVer: 2.0.0`) and
probes every combination of key share, fingerprint, policy and client version through the core
under test. The expected result of each cell comes from a model of the server generations, so a
regression in either direction fails the test. "Core build" runs it against the freshly built
core and the stock core of the same tag, on every core PR and on every release.

```bash
cd tests/reality-lab
XRAY_LAB=1 SLOTH_MIHOMO_BIN=<patched core> SLOTH_MIHOMO_STOCK_BIN=<stock core> go test -v -count=1 .
```

## Current series

### 0001 REALITY: configurable client version

mihomo sends the constant `1.8.2` as the client version in the REALITY session id. Xray servers
configured with `minClientVer` compare it with their threshold and, when it is lower, silently
hand the connection to the cover site: the client sees a timeout. The patch turns the constant
into `tlsC.RealityClientVersion`, default `26.9.9` (the Xray release the core was validated
against). Only `minClientVer` / `maxClientVer` depend on this value.

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

Xray generations disagree about the post-quantum key share: servers before 25.x silently drop a
hybrid ClientHello (5 s timeout), 26.9.8+ servers (XTLS/REALITY `8cdf7bf`) reject a classic one,
25.x and 26.4 accept both, and no server tells which it is. The patch adds
`tlsC.SetRealityMLKEMPolicy` (Auto / On / Off). In Auto the outcome is remembered per server
address and public key: classic first, flip after one failure, pin after one success, flip again
after three failures of a pinned verdict (servers get upgraded). A per-proxy
`support-x25519mlkem768: true` still forces the hybrid share.

Only the chrome specs in `metacubex/utls` 1.8.8 carry the hybrid share; firefox, safari, ios and
edge cannot offer it. When a hybrid handshake is needed and the configured fingerprint cannot
produce one, the patch uses chrome for that handshake, because the alternative is a node that
can never reach a 26.9.8+ server. Moving to a utls release that gives the other specs a hybrid
share is a one-line dependency change once MetaCubeX tags one.

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
reality-mlkem: auto              # auto | on | off; absent = auto
reality-client-version: "26.9.9" # x.y.z, each part 0..255; absent = core default
```

Invalid values fail the config with a message naming the key. On load the core logs
`REALITY policy: client version 26.9.9, X25519MLKEM768 auto`. An unpatched mihomo ignores
unknown top-level keys, so the same config still starts on a stock core.

The app writes them from Settings → REALITY ("ML-KEM key share": Auto / Always / Never, "Client
version": empty = the subscription's value if it sets one, else the core default). Saving reloads
the running core's config.

### 0004-0015 Citrus client compatibility

[Citrus](https://github.com/getmikan/Citrus) is a mihomo fork some panels build configs for. These
twelve patches make the client side of its config surface work on this core without a fork: they are
the series [ClashFest](https://github.com/Nemu-x/ClashFest) ships as its 0003-0014, applied in the same
order after our 0003 (so ClashFest's number is ours minus one). Ten files are byte-identical to
ClashFest's, and their code comments keep ClashFest's numbering; syncing a later fix is a copy. The
two that differ:

- **0005** drops ClashFest's own `ParseRealityClientVersion` and uses the one from our 0003, which
  also takes a leading `v` (both reject anything that is not `x.y.z` with parts 0..255).
- **0007** adds `github.com/metacubex/mldsa v0.1.1` to mihomo's own `go.mod` / `go.sum`: the core is
  built from mihomo's module here, while ClashFest pins the dependency in its outer module.

| Patch | What a config gains | UA token |
|---|---|---|
| 0004 VLESS over XDrive | `network: xdrive` + `xdrive-opts` (Google Drive or a templated HTTP storage), client side only | `xdrive` |
| 0005 REALITY per-proxy client version | `reality-opts.client-version` overrides the global one from 0001/0003 | |
| 0006 chain groups | `type: chain`: hops in `proxies`, each dialed through the one before (`dialer-proxy` wiring at load), health check names the first broken hop | `chain` |
| 0007 REALITY mldsa65-verify | `reality-opts.mldsa65-verify` (Xray `mldsa65Verify`, share link `pqv`); without the patch the key is ignored and the client only believes it pins the server | `mldsa65` |
| 0008 Xray fingerprint names | `client-fingerprint: hellochrome_131`, `randomizednoalpn`, ... map to the nearest utls parrot instead of falling back to Go's own hello | |
| 0009 pin and name lists | `fingerprint` and `name-cert-verify` take comma-separated lists | `pin-list` |
| 0010 ECH Xray forms | `ech-opts.config` takes the `ECH CONFIGS` PEM; `ech-opts.query-server` | `ech-xray` |
| 0011 OpenVPN server identity | `verify-x509-name` / `-type`, `verify-server-name`, `tls-version-min` are enforced (vanilla ignores them: any certificate of the CA is trusted) | |
| 0012 QUIC disable-mtu-discovery | the option on hysteria2 and masque | |
| 0013 tcp-connect-timeout | top-level `tcp-connect-timeout` (ms, 100..600000, default 5000) | |
| 0014 sniffer.skip-threshold | failed sniffs before a destination is skipped (was the constant 5) | |
| 0015 dns.serve-expired | `serve-expired`, `serve-expired-ttl`, `serve-expired-client-timeout` (RFC 8767) | |

Every patch carries its tests; the core build runs them (it vets and tests every package a patch
touches). Origin, design notes and the condition under which each patch goes away are in ClashFest's
`docs/core-patches.md`, sections 0003-0014. Not taken, by decision: DNSCrypt `sdns://`, the `PROTOCOL`
rule (it comes with a mihomo release), Citrus' XHTTP defaults, ML-KEM on by default, the protocols
Citrus removed, library forks.

### 0016 XDrive: local storage only in tests

XDrive's `service: local` keeps the storage in a folder whose path comes from the config. ClashFest
refuses it on Android only; here the core can run under the privileged helper service (SYSTEM on
Windows, root on macOS and Linux), where a subscription-chosen folder would be a privileged file read
and write. The patch turns `xdrive.AllowLocalService` into `testing.Testing()`: the series' own tests
keep using local storage, a shipped binary never does. Such a node still loads, so one bad node does
not fail a subscription, but it creates nothing and every dial through it fails with `xdrive service
local (a folder on the device) is disabled in this build`. The gate sits in the core, so it also covers
proxy-providers the core fetches itself. `xdrive_hardening_test.go` checks it on the built core: the
folder of a local node must not exist after the config is loaded (a build with the gate open creates
it).

Delete together with 0004, or when XDrive drops local storage.

## What the app does with the Citrus surface

- **User-Agent.** A patch may declare a `feature` in the manifest. prebuild writes the tokens of the
  series it actually built into `core-build.json`, and the subscription User-Agent appends them as a
  comment: `clash.meta/v1.19.33 SlothClash/0.9.5 (xdrive; chain; mldsa65; pin-list; ech-xray)`. The
  `clash.meta/` prefix and the version stay first, so panels keep selecting the format and the core
  version as before. A stock core (`MIHOMO_CORE_VERSION`) or a `SLOTH_CORE_FILE` binary has no tokens
  and no comment: a token is only ever sent for a patch that is in the binary.
- **XDrive hardening.** The config pipeline drops `listeners` entries with `xdrive-config` (an XDrive
  inbound polls cloud storage: a reverse tunnel into this machine that no bind address contains) and
  turns `service: local` nodes, in `proxies` and inline provider payloads, into `type: reject` with the
  same name. Keys are matched the way the core's decoder matches them (`Xdrive_Opts` counts). Both run
  again after a profile's JavaScript override, so a script cannot bring them back.
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

Without a core that carries the series the test skips. Outcomes at Citrus `ecaa9466` (2026-10-23):
145 cases, 140 accept, 4 unsupported (`PROTOCOL` rule, DNSCrypt, BitTorrent sniffer, server log
format), 1 deferred (TrustTunnel timers); `skip-host-key-check` is ignored and harmless. Running it the
first time found two app bugs, fixed with it: a subscription's own url-test group named `Auto` that
another group uses was pruned as if it were the app's synthetic fallback group, and `SUB-RULE` targets
were checked against proxies instead of `sub-rules`.

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
