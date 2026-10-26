# Desync

Desync connects selected services and custom domains directly using automatically tested strategies in the embedded mihomo core. Other traffic follows the subscription. The feature is off by default.

## Use

Open **Settings → Desync**, enable it and select YouTube or Discord. The Discord control enables its web and voice paths together in one settings update; their health is still reported separately. Connect in Rule mode. Desktop Discord voice additionally requires Windows and TUN. Manual TTL, repeat and method fields are no longer exposed. Previously saved partial selections remain readable; toggling Discord off/on, or enabling Desync with Discord selected, enables the combined behavior. Other sites can be added under **Custom domains** (see below).

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
- **Using subscription reserve**: new connections follow the original subscription rules after a remote route passed the service checks. Voice stays on this route during active traffic.
- **Subscription reserve unavailable**: the selected reserve's latest check failed; background recovery continues without bouncing existing connections.
- **Desync and reserve unavailable**: the bounded strategy search and reserve verification failed; the controller backs off and tries again. **Check again** restarts the controller.

**Copy diagnostics** copies a report through the native clipboard. It includes app/core build identifiers, a controller revision, an opaque network digest, enabled services, candidate attempts and error categories, probe response counts and anonymized application traffic counters. UDP flow summaries distinguish generated Desync routes from profile routes without exposing subscription node names. Probe targets, Discord voice endpoints and custom domains appear only as a keyed digest: HMAC-SHA256 truncated to 12 hex characters, keyed by a random secret generated once per install and stored in the local prefs file. The secret is never included in the report and never returned to the UI, so a reader cannot test a guessed site against a digest. Digests stay the same across reports from the same install, so attempts match flow observations and each custom domain (listed with its random entry ID and on/off state) matches its attempts. It excludes subscription URLs/configuration/names, credentials, raw logs, local/public IP addresses, file paths and audio. Copying does not upload or send anything.

## Custom domains

**Settings → Desync → Custom domains** takes any other site. Type a domain (`example.com`; a pasted URL or `host:port` is reduced to its host) and press **Add**. The entry covers the domain and all of its subdomains. Up to 32 entries; each can be switched off or removed. Internationalized names are stored in punycode (`пример.рф` → `xn--e1afmkfd.xn--p1ai`). IP addresses, wildcards (`*.`/`+.`, which are unnecessary since subdomains are included), public suffixes (`com`, `co.uk`, `github.io`), duplicates and domains already covered by the YouTube or Discord switch are rejected. Desync can be enabled with only custom domains selected; switching off or removing the last selected route turns Desync off.

The app runs the existing strategies against the site and keeps the one that works; no strategy needs to be chosen. Each entry shows one of these states:

- **Waiting for a check** / **Finding a strategy · n/N**: the entry is queued or being tested.
- **Not blocked · Desync not needed**: plain direct access already works, so the site is routed directly without any transformation.
- **HTTPS available · automatic**: a strategy passed and is applied to this site.
- **Desync can't unblock this site**: no strategy worked. The site most likely is blocked by its IP address, which no DPI desync can bypass. Its traffic follows the profile's normal rules (a VPN subscription still covers it), and the check is repeated later.

How it works:

- Every enabled entry has its own hidden selector `SLOTH-DESYNC-CUSTOM-<id>`. The ID is random and fixed for the entry, so adding, removing or reordering entries never moves another entry's selection. The group contains the custom candidates, `DIRECT` and `PASS`. Its rules (TCP, plus UDP/443 so candidates can reject QUIC and the browser falls back to TCP) follow the built-in service rules and precede the subscription's rules; a more specific entry (`media.example.org`) is placed before a broader one (`example.org`). Corporate VPN routing keeps its priority.
- Candidates are the YouTube set (TLS record fragmentation `r3`, `r1`, `r2`, `r4`, `r8`, then `split`) followed, on Windows, by the Discord TCP fake-prefix set (TTL 4, 5, 3, … through 1–32). A strategy whose transformation equals an earlier one is tried once (Discord `record` is YouTube `r1`). The cached winner is tried first.
- The probe is `https://<domain>/` through the candidate. It passes on any HTTP response, provided TLS completed with a certificate valid for that host: the status says nothing about DPI on an arbitrary site, while a verified certificate proves the connection reached the real server instead of a block page. YouTube and Discord keep the strict successful-status check. This uses the core delay API with `expected=100-599`; measured on the patched core, it accepts a 404 and rejects a wrong-host, self-signed or expired certificate and plain HTTP on the TLS port. Redirects are not followed, so the redirect target is not tested.
- Before searching, the site is probed twice through plain `DIRECT`. If that works, the entry is **not needed** and selects `DIRECT`. Otherwise the candidates are tried two rounds each, in 30-second budgets that resume where the previous budget stopped. After the first failed budget the group selects `PASS`, so the site follows the profile instead of a failing strategy while the search continues. If the whole set fails, `PASS` stays selected and the entry is searched again after 1 minute, doubling up to 30 minutes.
- Custom entries are calibrated after the built-in services' initial search, one entry at a time. Adding, removing or switching an entry regenerates the configuration but does not restart the YouTube/Discord calibration; only the new entry is searched. Working entries are re-checked every 90 seconds; a failure confirmed by two follow-up checks starts again from the baseline. A long search does not hold the others back: between two 30-second budgets of one entry, re-checks of other entries that are due run first. A network or connection change re-checks every entry.
- Winners (a strategy or "not needed") are cached per network under `custom:<domain>` with the same 24-hour expiry and network/build digest as the built-in services.

Limits: Desync changes how a connection's first packets look to a DPI middlebox; it cannot reach a server whose IP address is blocked or a domain blocked by DNS beyond the resolver fallback. UDP and voice for custom domains are not supported. A site served from several CDN hosts may need different strategies per host: add the affected subdomain as its own entry. Only the domain's root URL is tested, not logged-in pages or media playback.

## Selection and recovery

Generated hidden selector groups keep the familiar `SLOTH-DESYNC-YOUTUBE`, `SLOTH-DESYNC-DISCORD` and `SLOTH-DESYNC-DISCORD-UDP` connection routes. Candidates are independent named direct outbounds. The controller tests candidates before changing a selector; candidate changes do not reload the whole core or edit the subscription.

The initial YouTube candidate uses TLS record fragmentation at `3+s`. Discord TCP starts with `1+s` plus a Windows fake prefix at TTL 4. Voice starts at TTL 4 with one fake before each of the first three application datagrams. Searches vary TLS record/write boundaries or fake TTL independently for each service. UDP searches use conservative one- or two-fake bursts rather than assuming that more fakes are better.

Successful candidate IDs are cached for up to six network/build digests, expire after 24 hours, and are revalidated on connection/network changes. HTTPS services are checked independently and periodically. TTL candidates cover 1–32, with the proven presets first. Searches have deadlines and retry backoff, and resume across a failed search budget instead of repeatedly testing only the first candidates. Disabling Desync, disconnecting, changing mode/network or selecting different services cancels superseded work; a late result cannot apply to a newer connection generation.

Voice calibration waits for an endpoint actually used by `Discord.exe`. An authenticated core API probe sends at most twelve independent IP-discovery requests through a candidate, validates the reply address/type/length/SSRC, and rejects sequences that only answer the first few requests. No cached voice server, account credentials or captured call packets are used. These diagnostic requests are excluded from the real application activity counters. Native probe eligibility is preserved through the parsed adapter's lifetime wrapper; ordinary Direct and unrelated adapters remain ineligible. Regression coverage exercises the real parser and HTTP handler together, including a Windows process path.

Actual application traffic takes priority: an already working call is observed and retained rather than being invalidated by a separate discovery socket. Initial discovery or one good ping never establishes media success. Each active endpoint has independent progress history; a new stream endpoint does not reset the voice call's history. Receive-only streams are valid media connections. Ongoing sends with no incoming progress must persist across separated observations; previously established traffic receives an additional 60-second grace. A strategy search starts only when all relevant active endpoints are persistently stalled and none is receiving traffic. A missing observation, idle endpoint or silent channel is not itself a failure. Before applying a successful probe, the controller checks again for recovered application traffic. Synthetic probe success alone is not saved as a confirmed voice strategy.

If a selector changes, only stale Desync voice UDP flows for that endpoint are closed; other applications, Discord TCP and subscription selections are retained. Discord may need time to renegotiate, or the user can rejoin the voice channel. Protecting healthy calls from global strategy churn does not establish the cause of a stopped stream or guarantee its recovery; sustained playback still requires a live test.

Automatic reserve is independent for YouTube HTTPS, Discord HTTPS and Discord UDP. After a confirmed failure and a bounded 30-second candidate search, the client tests the subscription route before selecting it. TCP requires two successful reserve checks; UDP requires a sustained discovery sequence for every relevant observed endpoint, with at most eight endpoints and a 30-second overall deadline. DIRECT, REJECT and a route without UDP support do not qualify as a working VPN reserve. If both paths fail, the controller retains the selection, reports the failure and retries with backoff.

The reserve uses `PASS` in the generated selector, so the full rule engine continues with the original subscription rules and actual connection metadata. It does not choose a random node or edit the subscription. Targeted QUIC rejection belongs to Desync candidates; selecting the reserve also restores the subscription's UDP/443 policy. Corporate routing keeps its existing priority. Prospective TCP checks use fixed service URLs and representative metadata; custom process/source/inbound rules can route an application's real connection differently. HTTPS checks still establish endpoint availability rather than video playback or logged-in application behavior.

A single failed scheduled HTTPS check never changes the route: two fresh follow-up checks must also fail before candidate recovery starts. Desync recovery is tested in the background without changing the working reserve. Restoring it requires at least three separated successes for the same candidate, a minimum three-minute reserve dwell and recent positive evidence. Failed or stale evidence resets the sequence. UDP calls remain on the reserve while traffic is active; candidate probes rotate after failures, and restoration also waits for a 60-second idle boundary. An unobserved/new peer or any recovered peer blocks an automatic UDP fallback. A stalled stream alongside a healthy voice flow remains a partial failure rather than a reason to move the entire call.

Route changes affect new connections. TCP application data is never replayed. Only confirmed failed owned Discord UDP associations are closed after a verified reserve change; healthy and unrelated connections are retained. A failed browser connection or Discord negotiation may still require refresh/rejoin. A subscription call does not validate direct Desync voice.

## Discord UDP binding continuity

Generated Discord UDP candidates enable `desync.udp.stable-binding`. In Windows TUN traffic from `Discord.exe`, the core preserves one native UDP socket when the application replaces its local socket within the same IP-discovery session. This keeps the server-facing UDP binding and the library's initial fake window together. There is no external helper or additional proxy route.

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

The library remains pinned to `github.com/Nemu-x/desync v0.0.0-20261024221341-3d471713bd2e`. Core patches 0017/0019 provide native transformations and DNS fallback; 0020 adds scoped UDP probes, application-only activity telemetry, cancellation and strict HTTP probe status handling. Patch 0021 adds opt-in Discord UDP binding continuity and private aggregate counters. Patch 0022 adds prospective profile probes and conditional QUIC rejection. Build validation applies and tests the entire patch series. Local mechanics and automated checks do not replace testing on each tester's network.
