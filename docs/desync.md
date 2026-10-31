# Desync

Desync connects selected services and sites directly using automatically tested strategies in the embedded mihomo core, and applies [your own rules](#my-rules) for domains and apps. Other traffic follows the subscription, or goes directly with a [Desync-only profile](#desync-only-profile-no-vpn). The feature is off by default.

Desync is **experimental**: the Desync window and the Settings card carry an *Experimental* label. It may not work on every network, and its options and generated configuration can still change between releases.

## Use

Desync settings belong to a profile: whether it is on and what it covers (YouTube, Discord, popular services, your rules) are stored with each profile, and the active profile's settings apply. Turning Desync off on one profile leaves the others alone. Open **Settings → Desync** (or the Desync chip on Home), enable it and select YouTube or Discord. The Discord control enables its web and voice paths together in one settings update; their health is still reported separately. Connect in Rule mode. Desktop Discord voice additionally requires Windows and TUN. Manual TTL, repeat and method fields are no longer stored. Previously saved partial selections remain readable; toggling Discord off/on, or enabling Desync with Discord selected, enables the combined behavior. Other sites and apps can be added under **My rules** (see below). Turning off the last selected service, rule or popular service turns Desync off; enabling it with nothing selected is refused.

Builds before per-profile settings kept one global selection in `prefs.json` (`"desync"`). On the first start of a newer build it is copied into every profile that has no Desync settings of its own (an untouched default selection is not copied), `profiles.json` gets `"desyncPerProfile": true`, and the global object is dropped from `prefs.json` once the profiles are saved. Entries stored under the old `"preset"` key read as popular services.

Selection runs in the background even when Settings is closed. The panel shows progress and one of these outcomes:

- **HTTPS available**: the service passed two independent rounds of HTTPS checks with a successful HTTP status.
- **Waiting for a Discord voice connection**: no current voice endpoint is available to test. Join a channel.
- **Checking voice traffic**: the current route is being observed; sustained application traffic is not established yet. Rejoining the channel can accelerate renegotiation after a strategy change.
- **UDP exchange detected**: the actual application has sustained incoming and outgoing UDP traffic.
- **Media traffic in both directions**: RTP-shaped packets are progressing in both directions across multiple observations. This does not inspect, decrypt or certify audible audio.
- **Receiving media**: sustained receive-only media is progressing, as expected when watching a stream without sending video.
- **One media connection stalled**: another active connection is still receiving traffic; the controller retains the working strategy rather than treating one stream as a failure of all Discord UDP.
- **UDP traffic interrupted**: repeated observations show missing inbound progress. Established connections receive an additional recovery grace before a whole-service strategy trial.
- **Confirming a connection failure**: fresh HTTPS checks confirm whether a scheduled failure persists before recovery starts.
- **Through VPN (Desync didn't work here)**: Desync found no working strategy, so new connections follow the original subscription rules after a remote route passed the service checks. Voice stays on this route during active traffic. Shown as pending, not as working: the service works, but not through Desync.
- **VPN unavailable too**: the selected reserve's latest check failed; background recovery continues without bouncing existing connections.
- **Desync and reserve unavailable**: the bounded strategy search and reserve verification failed; the controller backs off and tries again. **Check again** restarts the controller.
- **No working strategy** (Desync-only profile): no strategy passed and there is no reserve; the service connects directly while the search is repeated in the background (the status stays put during those retries). **Check again** searches at once. **Desync unavailable** is the same profile's wording for any other failure.

**Copy diagnostics** copies a report through the native clipboard. It includes app/core build identifiers, a controller revision, an opaque network digest, enabled services, candidate attempts and error categories, probe response counts and anonymized application traffic counters. UDP flow summaries distinguish generated Desync routes from profile routes without exposing subscription node names. Probe targets, Discord voice endpoints and custom domains appear only as a keyed digest: HMAC-SHA256 truncated to 12 hex characters, keyed by a random secret generated once per install and stored in the local prefs file. The secret is never included in the report and never returned to the UI, so a reader cannot test a guessed site against a digest. Digests stay the same across reports from the same install, so attempts match flow observations and each custom domain (listed with its random entry ID, kind, route and on/off state) matches its attempts. An app rule's process name is digested the same way (lowercased). It excludes subscription URLs/configuration/names, credentials, raw logs, local/public IP addresses, file paths and audio. Copying does not upload or send anything.

### With a subscription

Desync can be mixed into a subscription profile: its routes (YouTube, Discord, popular services, your rules) come before the subscription's rules, and a service for which no strategy works follows the subscription's rules again (**Through VPN (Desync didn't work here)**). Because this changes how those services leave the machine, it is only ever the user's decision:

- **When adding a subscription**, the dialog offers **Connect Desync to this subscription**. It is unchecked every time, for every provider; nothing suggests it. Checked, the profile starts with Desync on: with the selection of your Desync-only profile (services, popular services and your Direct/Desync rules) if you have one, otherwise the defaults. Changing the URL clears the box.
- **Later**, the master switch in the Desync window turns it on or off for the active profile.

### Provider control

A subscription provider can switch Desync off, or forbid it, for their subscription with a response header. It cannot switch Desync on.

```text
X-Desync-Desktop-Mode: off     # switch Desync off on this subscription's profile (once)
X-Desync-Desktop-Mode: deny    # Desync stays off and cannot be enabled
```

- Desktop only (`X-Desync-Desktop-*`, like `X-Brand-Desktop-*`), honored only when the subscription URL is `https`. The value is case-insensitive; any other value, `on` included, is ignored.
- The app stores the last value it applied per profile and acts only when it changes. After `off` the user may switch Desync on again, and a refresh with the same `off` leaves it on. The Desync window says "Your provider turned Desync off" until the user does.
- Under `deny` the add dialog keeps the box unchecked and disabled (checking it asks the provider first; the app also checks again when adding), the Desync window's switch is disabled with "Disabled by your provider", and enabling it any other way fails with `provider_denied`. Editing the selection stays possible.
- A response without the header clears the stored value: a `deny` is lifted, Desync stays as it is.
- When a refresh switches Desync off on the active profile, the running configuration is regenerated. Local and Desync-only profiles have no provider and ignore it. **Copy diagnostics** includes `"providerMode"` (`none`, `off`, `deny`).

## My rules

**Settings → Desync → My rules** (in Russian «Мои правила») routes any other site or app the way you choose. A rule has a type, a value and a route:

| Type | Value | Generated matcher |
| --- | --- | --- |
| Domain and subdomains | `example.com` | `DOMAIN-SUFFIX` |
| Exact domain | `www.example.com` | `DOMAIN` |
| App | a process name: `Telegram.exe` (Windows), `telegram` (Linux, macOS) | `PROCESS-NAME` |

| Route | Where the traffic goes |
| --- | --- |
| **Desync (auto)** | Through Desync with a strategy found automatically (below). An app gets the catalog's default strategy, see [Apps](#apps). |
| **VPN** | The profile's main proxy group, see [VPN route](#vpn-route). Not offered on a [Desync-only profile](#desync-only-profile-no-vpn), which has no VPN; the backend refuses it there with `no_vpn`. |
| **Direct** | `DIRECT`, without any transformation. |

Choose the type and the route around the value field and press **Add**. **Edit** (✎) puts a rule back into the form; **Save** replaces it in place, keeping its on/off state, and **Cancel** leaves it. Each rule can be switched off or removed. The rules apply while Desync is on; a VPN or direct rule counts as a selection, so Desync can be on with nothing but such rules.

Validation:

- Domains: a pasted URL or `host:port` is reduced to its host, and internationalized names are stored in punycode (`пример.рф` → `xn--e1afmkfd.xn--p1ai`). IP addresses and wildcards (`*.`/`+.`, unnecessary since a suffix includes subdomains) are rejected. A rule through Desync must name a site: public suffixes (`com`, `co.uk`, `github.io`), domains covered by the YouTube or Discord switch and domains of a popular service in the list are rejected. A VPN or direct **suffix** may name a whole zone (`ru`, `co.uk`) and may cover a built-in or popular service's domain: it is the user overriding that service (see [Rule order](#rule-order)). An exact domain must not be a public suffix.
- Process names: a pasted path (`C:\Program Files\Telegram Desktop\Telegram.exe`) is reduced to its file name and surrounding quotes are dropped. Wildcards (`*`, `?`) and the characters a rule cannot carry (`,`, `(`, `)`, `"`, `<`, `>`, `|`, `:`, control characters) are rejected, as are names over 255 bytes. On Windows a name typed without an extension gets `.exe` (`telegram` → `telegram.exe`), because the core matches the executable's file name there. The core compares process names case-insensitively, so `Telegram.exe` and `telegram.EXE` are one rule.
- One rule per type and value, whatever its route (`duplicate`); a domain rule and a suffix rule for the same name are different rules.
- Limits: up to 32 entries through Desync (rules and popular services: each is a hidden group with the whole candidate set, calibrated one at a time) and up to 128 entries in all.

Failures reach the UI as codes (`desync: <code>`) it translates: the list's `empty`, `invalid`, `ip`, `wildcard`, `public_suffix`, `builtin`, `duplicate`, `limit` (32 through Desync), `rule_limit` (128 in all), `not_found`, `service_overlap`, `unknown_service`, `invalid_process`, `invalid_kind`, `invalid_route`, `no_vpn`, and the settings' `unsupported_core`, `unsupported_udp_core`, `no_selection`, `discord_platform`, `no_profile`. `empty`, `wildcard` and `duplicate` are worded for a process name when the rule is an app.

### Sites are rules

The **Sites** list of earlier builds is this list: every stored site is a **Domain and subdomains** rule through **Desync (auto)**, which is what it always routed. Entries stored without a type and a route read that way and are written back with both fields (`"kind": "suffix", "route": "desync"`) on the next save of the list; their ID, hidden group and cached strategy stay, so nothing is searched again. No separate migration step or marker is needed, and popular services are unchanged. An older build reading the list back sees VPN and direct domain rules as sites through Desync and drops app rules (it has no process field).

### Rules through Desync

The app runs the existing strategies against the site and keeps the one that works; no strategy needs to be chosen. Each domain rule through Desync shows one of these states:

- **Waiting for a check** / **Finding a strategy · n/N**: the entry is queued or being tested.
- **Not blocked · Desync not needed**: plain direct access already works, so the site is routed directly without any transformation.
- **HTTPS available · automatic**: a strategy passed and is applied to this site.
- **Desync can't unblock this site**: no strategy worked. The site most likely is blocked by its IP address, which no DPI desync can bypass. Its traffic follows the profile's normal rules (a VPN subscription still covers it), and the check is repeated later.

VPN and direct rules show **Through VPN** / **Directly** once active.

How it works:

- Every enabled entry (a rule of any route, or a popular service) has its own hidden selector `SLOTH-DESYNC-CUSTOM-<id>`. The ID is random and fixed for the entry, so adding, removing or reordering entries never moves another entry's selection. Editing a rule gives it a new ID: its group selection, calibration state and cached winner belonged to what it matched before. A VPN rule's group has one member, the profile's main group; a direct rule's has `DIRECT`; a domain rule through Desync gets the custom candidates, `DIRECT` and `PASS`. The group name is ours, so a later pass (after a user script) recognizes and replaces every generated rule. Rules through Desync match TCP, plus UDP/443 so candidates can reject QUIC and the browser falls back to TCP; VPN and direct rules match every network. Their order is described under [Rule order](#rule-order). Corporate VPN routing keeps its priority.
- Candidates are the TCP strategies of the [catalog](#strategy-catalog) this computer can run, in the catalog's ranked order. The cached winner is tried first.
- The probe is `https://<domain>/` through the candidate. It passes on any HTTP response, provided TLS completed with a certificate valid for that host: the status says nothing about DPI on an arbitrary site, while a verified certificate proves the connection reached the real server instead of a block page. YouTube and Discord keep the strict successful-status check. This uses the core transfer test (patch 0025) with `expected=100-599`: a GET with ALPN `h2` and `http/1.1` that reads up to 64 KB of the page and fails if nothing arrives for 2 seconds. On the patched core the status check accepts a 404 and rejects a wrong-host, self-signed or expired certificate and plain HTTP on the TLS port. Redirects are not followed, so the redirect target is not tested.
- Before searching, the site is probed twice through plain `DIRECT`. If that works, the entry is **not needed** and selects `DIRECT`. Otherwise the candidates are tried two rounds each, in budgets of 30 seconds per probed host (30 seconds for a domain; a popular service, which probes its page and CDN hosts, gets 30 seconds for each) that resume where the previous budget stopped. After the first failed budget the group selects `PASS`, so the site follows the profile instead of a failing strategy while the search continues. If the whole set fails, `PASS` stays selected and the entry is searched again after 1 minute, doubling up to 30 minutes.
- Custom entries are calibrated after the built-in services' initial search, one entry at a time. Adding, removing or switching an entry regenerates the configuration but does not restart the YouTube/Discord calibration; only the new entry is searched. Working entries are re-checked every 90 seconds; a failure confirmed by two follow-up checks starts again from the baseline. A long search does not hold the others back: between two budgets of one entry, re-checks of other entries that are due run first. A network or connection change re-checks every entry.
- Winners (a strategy or "not needed") are cached per network under `custom:<domain>` with the same 24-hour expiry and network/build digest as the built-in services.

Limits: Desync changes how a connection's first packets look to a DPI middlebox; it cannot reach a server whose IP address is blocked or a domain blocked by DNS beyond the resolver fallback. UDP and voice for custom domains are not supported. A site served from several CDN hosts may need different strategies per host: add the affected subdomain as its own entry. A site whose media lives on another registrable domain needs that domain too; Facebook, Instagram and X are [popular services](#popular-services) for this. Only the domain's root URL is tested, not logged-in pages or media playback. The probe reads at most 64 KB of the root page, so a DPI that freezes a connection only after a size threshold is caught when the page is larger than that threshold; a small root page can still show a site as **not needed** while larger page loads stall. If a site shows **not needed** but does not load, keep the entry (and add the host that actually stalls, such as a media or CDN subdomain, as its own entry anyway) and press **Check again** while the problem is visible: a strategy is picked only when a probe through plain direct access fails.

### Apps

An app rule matches the connections of one process (`PROCESS-NAME`), whatever their destination. Through Desync it cannot be calibrated: an app has no address to probe, and its connections go to servers the app picks. Its group therefore holds the catalog's first, cheapest TCP strategy (the same first candidate a site tries; on Windows a TLS record split) and `PASS` (which also is all it holds should this OS run no TCP strategy), and its status reads **Default strategy · apps aren't tested** with the strategy line ending in "default". The transform acts on TLS connections; whether it helps depends on how the app connects. TCP and UDP/443 of the app go to the group (QUIC is rejected so the app falls back to TCP); its other UDP traffic follows the profile's rules. Process rules need the core to know the process: they work with System proxy and TUN on desktop, and not when `find-process-mode` is `off` (Settings → Traffic).

### VPN route

A VPN rule goes to the profile's main proxy group, determined when the configuration is generated: the target of the profile's `MATCH` rule when that is one of its proxy groups or proxies; otherwise (a split profile whose catch-all is `DIRECT`) the first proxy group that is not hidden and reaches a server (a member other than `DIRECT`/`REJECT`/`PASS`, or a provider). Without one the group holds `PASS`, so the traffic follows the profile's rules. Changing the node in that group on the Proxies screen applies to the rule at once; the group itself is chosen only when the configuration is regenerated.

### Rule order

The generated rules precede the subscription's (and the rules template's):

1. Domain rules, the user's and the popular services' together: the deepest domain first; at equal depth an exact domain before a suffix and a user's rule before a popular service's domain. So `login.example.org → Direct` wins over `example.org → Desync`, and a VPN rule for `music.youtube.com` or `fbcdn.net` wins over the YouTube switch or the Facebook service.
2. The built-in services: YouTube, Discord, Discord voice.
3. App rules, in list order. A rule for the destination wins over a rule for the app: with `chrome.exe → VPN` and YouTube on, YouTube still goes through Desync.

Corporate VPN routing keeps its priority.

Sample (a subscription profile whose `MATCH` goes to `PROXY`):

```yaml
rules:
  - DOMAIN,login.example.org,SLOTH-DESYNC-CUSTOM-2b3c4d5e          # exact domain → Direct
  - DOMAIN-SUFFIX,music.youtube.com,SLOTH-DESYNC-CUSTOM-3c4d5e6f   # → VPN (group: [PROXY])
  - AND,((NETWORK,udp),(DST-PORT,443),(DOMAIN-SUFFIX,example.org)),SLOTH-DESYNC-CUSTOM-1a2b3c4d
  - AND,((NETWORK,tcp),(DOMAIN-SUFFIX,example.org)),SLOTH-DESYNC-CUSTOM-1a2b3c4d  # → Desync (auto)
  - DOMAIN-SUFFIX,ru,SLOTH-DESYNC-CUSTOM-4d5e6f70                  # zone → VPN
  - AND,((NETWORK,udp),(DST-PORT,443),(DOMAIN-SUFFIX,youtube.com)),SLOTH-DESYNC-YOUTUBE
  # … the rest of YouTube
  - PROCESS-NAME,Telegram.exe,SLOTH-DESYNC-CUSTOM-5e6f7081         # app → VPN
  - AND,((NETWORK,udp),(DST-PORT,443),(PROCESS-NAME,chrome.exe)),SLOTH-DESYNC-CUSTOM-6f708192
  - AND,((NETWORK,tcp),(PROCESS-NAME,chrome.exe)),SLOTH-DESYNC-CUSTOM-6f708192  # app → Desync
  - PROCESS-NAME,steam.exe,SLOTH-DESYNC-CUSTOM-708192a3            # app → Direct
  - MATCH,PROXY
```

The golden snapshots `synthetic/representative-desync-rules` and `synthetic/desync-only-rules` under `apps/sloth-clash-desktop/testdata/golden/runtime-config/` hold the full output for both profile kinds.

## Popular services

A plain entry covers one domain and its subdomains, but large services spread a page over several registrable domains: with only `facebook.com` the page opens and the pictures do not, because they come from `fbcdn.net`. Popular services are curated entries for such services (they were called presets before; "preset" now names a strategy from the desync library's catalog). A popular service is one entry in the custom list (it counts as one toward the 32-entry cap, has one hidden group `SLOTH-DESYNC-CUSTOM-<id>` and can be switched off or removed like any entry); its domains are read from the built-in catalog when the configuration is generated, so an app update that refines a list reaches existing entries.

| Service | Domains | Probes |
| --- | --- | --- |
| Facebook | `facebook.com`, `fbcdn.net`, `facebook.net`, `fbsbx.com`, `fb.com`, `fb.me`, `fb.watch`, `messenger.com`, `m.me` | `www.facebook.com`, `static.xx.fbcdn.net`, `scontent.xx.fbcdn.net` |
| Instagram | `instagram.com`, `cdninstagram.com`, `ig.me`, `instagr.am`, `fbcdn.net` | `www.instagram.com`, `static.cdninstagram.com`, `scontent.cdninstagram.com`, `scontent.xx.fbcdn.net` |
| X (Twitter) | `x.com`, `twitter.com`, `twimg.com`, `t.co`, `pscp.tv` | `x.com`, `abs.twimg.com`, `pbs.twimg.com` |

The lists hold the page, API, CDN/media, short-link and messaging domains each service owns, and nothing shared with other sites (no public clouds or CDN catch-alls). `x.ai` and `grok.com` belong to a different service and are not part of X.

In the Desync window (and in "Unblock without VPN") they are listed under **Popular services** after YouTube and Discord, one switch per catalog entry with a short domain summary (**+N more** shows the full list); your own rules follow under **My rules**. The first switch-on adds the service; after that the switch only turns its entry on and off, so the entry keeps its cached strategy. The Desync window shows each switched-on service's status like any site, and Home and Settings name switched-on services ("Facebook, Instagram") next to YouTube, Discord and the count of your rules ("My rules 3").

- **Probes check the CDN too.** The baseline, every candidate and every periodic re-check must pass all of the service's probe hosts, the page and its static/media CDN. A strategy that opens the page but not the pictures is a failure, as is plain direct access that does. Probe hosts are concrete names with a certificate valid for that exact host (a bare CDN apex such as `twimg.com` has none). The status codes at `/` (204, 302, 400, 404) do not matter for the reachable probe described above.
- **Shared domains.** Much of Instagram's media is served from `fbcdn.net`, so both services list it. A domain is routed by one group only: the first enabled service in catalog order (Facebook, then Instagram, then X), regardless of the list order. With Facebook on, Facebook's group carries `fbcdn.net` and Instagram neither routes nor probes it; with Facebook off or removed, Instagram does both.
- **Rules and popular services.** Adding a service replaces the domain rules through Desync it covers (for example a `facebook.com` site added by hand): the service routes the same names and more. While a service is in the list, switched on or off, adding a domain rule through Desync it covers is rejected with `service_overlap`, naming the service. VPN and direct rules for its domains are kept and allowed: they override the service. Unrelated domains are unaffected.
- **Cache and diagnostics.** Winners are cached per network under `custom:preset:<id>` (the key predates the rename and is kept, so cached winners survive). The diagnostics report lists a service entry by its ID under `"preset"` (the catalog is public), and its attempts carry that ID instead of a target digest; plain domains remain keyed digests.

The DPI limits above still apply: a popular service fixes the missing-domain problem, not an IP-level block. If a service's servers are blocked by address, no strategy passes and the entry follows the profile's rules (through the VPN), as for any site; on a Desync-only profile it stays blocked.

## Desync-only profile (no VPN)

For users without a VPN subscription. **Profiles → Add → Desync only (no VPN)** (or **Unblock YouTube & Discord without VPN** on an empty Home) opens "Unblock without VPN": the services and rules chosen there stay in the dialog until **Create profile**, which creates the profile with them in one step; **Cancel** changes nothing. The profile has no subscription URL and no proxy servers; it is only created by that dialog. Its generated configuration is the Desync routes (YouTube, Discord, Discord voice and your rules, exactly as above) followed by `MATCH,DIRECT`: everything that is not a Desync route connects directly. DNS uses the app's default resolver settings, and both System proxy and TUN work. Discord voice still requires Windows and TUN.

- **No VPN route.** My rules offers **Desync (auto)** and **Direct** only; a VPN rule stored there anyway (a hand-edited file) is not used.
- **Rule mode only.** The routes are rules and there is no server for Global mode, so Global and Direct are not offered while the profile is active (the mode switches back to Rule when it is activated). Switching to a profile with servers again restores exactly the mode the Desync-only profile overrode (Global or Direct), also after a restart; if the mode was already Rule, it stays Rule.
- **Desync is always on.** The profile is nothing but Desync routes, so its Desync window has no master switch and there is no "Desync is off" state to explain. Its settings are edited from the Desync window while it is active (the card's **Set up** link).
- **No reserve.** There is no subscription to fall back to, so the controller never verifies or selects one. When no strategy works, the route's group selects `PASS`, which in this profile ends at `MATCH,DIRECT`, the service reports **No working strategy**, and the search is repeated in the background after 1 minute, doubling up to 30 minutes (as for custom domains). The status stays **No working strategy** during those retries instead of switching to the search and failure states every round; the first strategy that passes is applied and the service shows it as working. **Check again** restarts the controller and searches immediately. A `PASS` left selected from an earlier session is searched past instead of being treated as a reserve. Voice keeps its candidate and reports the same status.
- **Nothing else to edit.** The profile has no subscription refresh, update status or "updated" time, no extend / proxy-groups / rules templates, no script and no editable config file; only its name can be changed. Proxies shows that there are no proxy servers, and Home has no node picker. Home also skips the separate direct WAN lookup: the exit already is the direct address.
- **Diagnostics** include `"desyncOnly": true` and the `no_strategy` attempts.

## Selection and recovery

Generated hidden selector groups keep the familiar `SLOTH-DESYNC-YOUTUBE`, `SLOTH-DESYNC-DISCORD` and `SLOTH-DESYNC-DISCORD-UDP` connection routes. Candidates are independent `type: desync` outbounds (core patch 0023) named `<route>-<preset ID>`. The controller tests candidates before changing a selector; candidate changes do not reload the whole core or edit the subscription.

### Strategy catalog

Candidates come from the presets catalog of the desync library (`github.com/Nemu-x/desync/presets`; the app pins the library version core patch 0023 pins): 22 TCP transformations (TLS record and write splits, random cuts, split waits, disorder, out-of-band byte, low-TTL fakes and combinations) and 9 UDP bursts, each with a stable ID derived from its options. The app generates only what the desync library implements on this OS (`desync.PlatformCapabilities`): macOS, for example, gets no TCP fake and no "sent" split wait, because the core would reject the whole configuration with them. A method the library lists as conditional (decided per socket; only Android has them) is generated and may fail at runtime. Availability is not effectiveness: a strategy this computer can run may still fail on a given network, which is what calibration measures.

- **HTTPS services and sites** (YouTube, Discord, popular services and domain rules through Desync) try every available TCP strategy in the catalog's ranked order (cheapest first: record splits, write splits, waits, disorder/out-of-band, fakes, combinations). YouTube tries `tlsrec-3s` (TLS record split 3 bytes into the server name) first, its proven default. Every TCP candidate sets `reject-quic` and `dns-fallback`.
- **Discord voice** tries the zero-payload UDP bursts with at most six decoys (`udp-zero-ttl5-r2`, then TTL 3, 5 and 8), each with `udp.stable-binding` and `dns-fallback`. The QUIC Initial decoys act only before QUIC Initials and would leave voice untouched; the twelve-decoy burst is left out to keep bursts conservative.
- **Cache**: winners are stored by preset ID. A winner cached by an earlier build under the hand-written list's IDs is kept where the catalog has the same transformation (`r1`/`record` → `tlsrec-1s`, `r3` → `tlsrec-3s`); the others (record split plus a fake, split with a delay, single-decoy UDP bursts) have no catalog equivalent and are ignored, so the search starts from the catalog order.

The Desync window names the strategy in use on each service and site in plain words (its kind and variant, for example "Split the TLS record, variant 2", chosen automatically), and **What this computer can do** lists the methods this build implements on this OS. It is read from the build, not measured, and says nothing about which method works on the current network. **Check network** restarts calibration, **Reconnect voice** closes Discord's voice flows so the client renegotiates, and **Turn on TUN** appears when Discord voice is on without TUN.

Successful candidate IDs are cached for up to six network/build digests, expire after 24 hours, and are revalidated on connection/network changes. HTTPS services are checked independently and periodically. Searches have deadlines and retry backoff, and resume across a failed search budget instead of repeatedly testing only the first candidates. Disabling Desync, disconnecting, changing mode/network or selecting different services cancels superseded work; a late result cannot apply to a newer connection generation.

Voice calibration waits for an endpoint actually used by `Discord.exe`. An authenticated core API probe sends at most twelve independent IP-discovery requests through a candidate, validates the reply address/type/length/SSRC, and rejects sequences that only answer the first few requests. No cached voice server, account credentials or captured call packets are used. These diagnostic requests are excluded from the real application activity counters. Native probe eligibility is preserved through the parsed adapter's lifetime wrapper; ordinary Direct and unrelated adapters remain ineligible. Regression coverage exercises the real parser and HTTP handler together, including a Windows process path.

Actual application traffic takes priority: an already working call is observed and retained rather than being invalidated by a separate discovery socket. Initial discovery or one good ping never establishes media success. Each active endpoint has independent progress history; a new stream endpoint does not reset the voice call's history. Receive-only streams are valid media connections. Ongoing sends with no incoming progress must persist across separated observations; previously established traffic receives an additional 60-second grace. A strategy search starts only when all relevant active endpoints are persistently stalled and none is receiving traffic. A missing observation, idle endpoint or silent channel is not itself a failure. Before applying a successful probe, the controller checks again for recovered application traffic. Synthetic probe success alone is not saved as a confirmed voice strategy.

If a selector changes, only stale Desync voice UDP flows for that endpoint are closed; other applications, Discord TCP and subscription selections are retained. Discord may need time to renegotiate, or the user can rejoin the voice channel. Protecting healthy calls from global strategy churn does not establish the cause of a stopped stream or guarantee its recovery; sustained playback still requires a live test.

Automatic reserve is independent for YouTube HTTPS, Discord HTTPS and Discord UDP. After a confirmed failure and a bounded 30-second candidate search, the client tests the subscription route before selecting it. TCP requires two successful reserve checks; UDP requires a sustained discovery sequence for every relevant observed endpoint, with at most eight endpoints and a 30-second overall deadline. DIRECT, REJECT and a route without UDP support do not qualify as a working VPN reserve. If both paths fail, the controller retains the selection, reports the failure and retries with backoff.

The reserve uses `PASS` in the generated selector, so the full rule engine continues with the original subscription rules and actual connection metadata. It does not choose a random node or edit the subscription. Targeted QUIC rejection belongs to Desync candidates; selecting the reserve also restores the subscription's UDP/443 policy. Corporate routing keeps its existing priority. Prospective TCP checks use fixed service URLs and representative metadata; custom process/source/inbound rules can route an application's real connection differently. HTTPS checks still establish endpoint availability rather than video playback or logged-in application behavior.

A single failed scheduled HTTPS check never changes the route: two fresh follow-up checks must also fail before candidate recovery starts. Desync recovery is tested in the background without changing the working reserve. Restoring it requires at least three separated successes for the same candidate, a minimum three-minute reserve dwell and recent positive evidence. Failed or stale evidence resets the sequence. UDP calls remain on the reserve while traffic is active; candidate probes rotate after failures, and restoration also waits for a 60-second idle boundary. An unobserved/new peer or any recovered peer blocks an automatic UDP fallback. A stalled stream alongside a healthy voice flow remains a partial failure rather than a reason to move the entire call.

Route changes affect new connections. TCP application data is never replayed. Only confirmed failed owned Discord UDP associations are closed after a verified reserve change; healthy and unrelated connections are retained. A failed browser connection or Discord negotiation may still require refresh/rejoin. A subscription call does not validate direct Desync voice.

## Discord UDP binding continuity

Generated Discord UDP candidates enable `udp.stable-binding`. In Windows TUN traffic from `Discord.exe`, the core preserves one native UDP socket when the application replaces its local socket within the same IP-discovery session. This keeps the server-facing UDP binding and the library's initial fake window together. There is no external helper or additional proxy route.

Bindings are isolated by source address, process, inbound, interface policy, server endpoint and discovery SSRC. Different sessions at one server remain separate. Discovery replies return to waiting associations without moving the media recipient. A newer association takes over on application traffic; old associations cannot steal it or close its native socket. Non-discovery starts keep private sockets. Ordinary DIRECT, other processes, private/low-port endpoints and internal calibration probes keep their existing paths.

Logical deadlines and closure are independent. Socket/session counts and queued bytes are bounded; detached bindings expire after 90 seconds and adapter shutdown closes sockets and pending readers. **Copy diagnostics** adds numeric `udp.binding` counters for socket creation/reuse, handovers, queue drops and errors. Session identifiers and native addresses are excluded. A positive discovery probe or a reused socket alone still does not certify playback; validate a sustained stream and two-way voice after installing the build.

## Routing and DNS

The YouTube overlay includes the site/API, `googlevideo.com`, `ytimg.com` and `yt3.ggpht.com`. Selected service TCP rules precede subscription service rules; targeted UDP/443 is rejected for TCP fallback. Desktop Discord voice transforms apply only to high-port UDP from `Discord.exe`, excluding QUIC/CDN on UDP/443 and low-port traffic. Migration removes the exact earlier all-UDP process rule. Corporate VPN coexistence retains its priority. Browser voice is outside the desktop process rule.

Named Desync outbounds retry empty direct DNS resolution once through the profile's normal resolver. Ordinary DIRECT, subscription DNS configuration, canceled requests and TLS/connect errors retain their behavior. The core reports resolution fallbacks and exact probe failures; HTTP test error pages do not count as successful availability.

## Trial checklist

1. Keep the previous installer and back up `%APPDATA%\SlothClash\prefs.json` and `profiles.json`.
2. Close SlothClash and install the trial as a normal update. The installer preserves data and refreshes the existing service's core hash pin.
3. Connect the existing subscription in Rule mode. Use TUN for voice. Enable the desired Desync services; no strategy values need to be entered.
4. Restart the browser/Discord, verify YouTube playback and seeking, and test a two-way Discord call and a stream separately. Inspect connection chains to distinguish direct Desync from subscription routing.
5. Include cold startup, reconnect, a longer call and a different network where available. If something fails, press **Copy diagnostics** and send the report with what failed and when. **Check again** restarts the search.
6. To compare subscription behavior, disable the relevant service and restart the application. Other subscription routes should retain their behavior.

The core and the app pin the same `github.com/Nemu-x/desync` version (`v0.2.1-0.20261029232908-673cbefddb80`, Nemu-x/desync main at 673cbef). Core patches 0017/0019 provide native transformations and DNS fallback; 0020 adds scoped UDP probes, application-only activity telemetry, cancellation and strict HTTP probe status handling. Patch 0021 adds opt-in Discord UDP binding continuity and private aggregate counters. Patch 0022 adds prospective profile probes and conditional QUIC rejection. Patch 0023 adds the `type: desync` outbound every generated candidate uses; a core without it (feature token `desync-outbound`) reports Desync as unsupported. Patch 0025 (feature token `transfer-test`) adds the transfer test every HTTPS probe uses when the core has it: GET with ALPN `h2` and `http/1.1`, up to 64 KB of the body, failure after 2 seconds without data. Some networks let a connection start and then freeze it after the first tens of kilobytes (about 16 KB is the reported threshold, seen on Cloudflare-hosted services); a header-only probe passes there. The YouTube and Discord probe lists therefore end with a page known to be larger (`https://www.youtube.com/`, about 1 MB; `https://discord.com/login`, about 72 KB, measured 2026-10-30), which must deliver at least 32 KB. Calibration, the periodic re-checks and the reserve probes all use it; the diagnostics report such a failure as `stalled`. A freeze keyed to the server's IP address rather than to the TLS handshake is not something a strategy can avoid: then every candidate fails and the service stays on its reserve, which is the honest result. Build validation applies and tests the entire patch series. Local mechanics and automated checks do not replace testing on each tester's network.
