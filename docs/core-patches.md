# Core patch series

SlothClash ships [mihomo](https://github.com/MetaCubeX/mihomo) built from an unmodified
MetaCubeX release tag plus a short series of patches kept in
[`core/patches/mihomo/`](../core/patches/mihomo). There is no SlothClash fork of mihomo, and
there must not be one: a patch that is no longer small enough to rebase in minutes is a sign to
get it into mihomo or drop it.

Settings → Info shows which core a build carries (`mihomo v1.19.32 + 3 SlothClash patches`)
with links to the patches and to this page.

## Where the core comes from

| Piece | Role |
|---|---|
| [`core/mihomo/manifest.json`](../core/mihomo/manifest.json) | MetaCubeX tag + its commit, series number, every patch with its SHA-256, every built binary with its SHA-256 |
| [`scripts/core-build.sh`](../scripts/core-build.sh) | clones the tag, checks the commit, applies the series, runs the patch tests, cross-compiles all targets |
| [`.github/workflows/core-build.yml`](../.github/workflows/core-build.yml) | runs the script, runs the REALITY lab, publishes release `core-<tag>-sloth.<series>` |
| [`scripts/core-manifest.mjs`](../scripts/core-manifest.mjs) | copies the published hashes into the manifest after checking the minisign signature |
| [`scripts/prebuild.mjs`](../scripts/prebuild.mjs) | downloads the binary for the build target and refuses any bytes whose SHA-256 differs from the manifest |

The desktop build never compiles the core. Every app release of one series embeds byte-identical
cores, so the privileged service's SHA-256 pin only changes when the core really changes (the
in-app "reinstall service" banner appears once per core change, not once per app release).

Core releases are prereleases and never marked latest. The in-app updater, the download site and
the package repositories only look at app releases (`v*` tags).

### Build flags

The same as MetaCubeX release builds: `CGO_ENABLED=0`, `-tags with_gvisor`, `-trimpath`,
`-ldflags "-extldflags --static -X constant.Version=<tag> -X constant.BuildTime=… -w -s -buildid="`,
`GOAMD64=v2` on amd64. Two differences, both deliberate:

- **Toolchain:** stock Go (the version is recorded in `BUILDINFO.txt` of each release) for every
  target. MetaCubeX builds with its own Go fork and ships extra builds for old Windows and macOS;
  the app itself needs Windows 10 / macOS 12 or later, so those builds buy nothing here.
- **BuildTime:** the tag's commit time instead of the wall clock, so the build is reproducible.

`constant.Version` stays the plain tag (`v1.19.32`). The core's `/version`, the User-Agent it
uses for providers and everything panels match on are identical to an unpatched core.

### Reproducing a core release

```bash
# Go version from BUILDINFO.txt of the release
bash scripts/core-build.sh out
cat out/SHA256SUMS   # equals SHA256SUMS of the release and the hashes in the manifest
```

`SHA256SUMS` of a release is signed with the SlothClash release key (the same key the in-app
updater trusts):

```bash
minisign -Vm SHA256SUMS -P RWQaeqnvGRd4kI2tkOi4JGfiD5IM5gXw3X5ZJ8cohs0dWAmEoav+AprY
```

## Rules for a patch

- One concern per patch, numbered, with a comment in the code explaining why it exists.
- Behaviour that users may need to change is read from the config, not baked in (see 0003).
- Logic ships with a Go test inside the patch; `core-build.sh` runs it on every build.
- Record the related mihomo issue or PR below, and the condition under which the patch goes away.
- Never edit a patch without updating its hash in the manifest; the build refuses the mismatch.

## Bumping the core or changing the series

1. Edit `core/mihomo/manifest.json`: `tag` and `commit` (for a bump), increase `series`, update
   patch hashes if a patch changed. Clear the `assets[*].sha256` values.
2. Open the PR. The "Core build" workflow builds every target and runs the REALITY lab. If a
   patch no longer applies it stops with `patch <name> does not apply to mihomo <tag>`; no core is
   produced.
3. To fix a patch: `git apply --3way core/patches/mihomo/<patch>` on a checkout of the new tag
   with the earlier patches applied, resolve, then regenerate it with `git diff` against a tree
   that has only the earlier patches. If mihomo fixed the problem, delete the patch and its entry
   below.
4. After merge, run "Core build" on `main` with `publish` checked. It creates
   `core-<tag>-sloth.<series>` and refuses to overwrite an existing release.
5. `node scripts/core-manifest.mjs` fills the asset hashes (verifying the signature and every
   file), commit the manifest. App builds now pick up the new core.

The tag is pinned; nothing forces a bump. If an area a patch touches was rewritten, stay on the
previous tag until the patch is redone.

Local escape hatches, never used by release CI: `SLOTH_CORE_FILE=<path> pnpm run prebuild` uses a
locally built core; `MIHOMO_CORE_VERSION=<tag> pnpm run prebuild` uses the stock MetaCubeX build
of that tag (Settings → Info then says "stock").

## Verifying behaviour

`tests/reality-lab` runs real Xray servers of every generation in Docker (24.12.31, 25.7.25,
26.4.13, 26.9.9, each plain, with `minClientVer: 25.1.1` and with `maxClientVer: 2.0.0`) and
probes every combination of key share, fingerprint, policy and client version through the core
under test. The expected result of each cell comes from a model of the server generations, so a
regression in either direction fails the test. "Core build" runs it against the freshly built
core and the stock core of the same tag.

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

Lab: servers with `minClientVer: 25.1.1` fail on the stock core and pass with the patch. A
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
