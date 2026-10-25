# Desync

Desync connects selected services directly using automatically tested strategies in the embedded mihomo core. Other traffic follows the subscription. The feature is off by default.

## Use

Open **Settings → Desync**, enable it and select YouTube or Discord. The Discord control enables its web and voice paths together in one settings update; their health is still reported separately. Connect in Rule mode. Desktop Discord voice additionally requires Windows and TUN. Manual TTL, repeat and method fields are no longer exposed. Previously saved partial selections remain readable; toggling Discord off/on, or enabling Desync with Discord selected, enables the combined behavior.

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

**Copy diagnostics** copies a report through the native clipboard. It includes app/core build identifiers, a controller revision, an opaque network digest, enabled services, candidate attempts and error categories, probe response counts and anonymized application traffic counters. UDP flow summaries distinguish generated Desync routes from profile routes without exposing subscription node names. Targets are hashed consistently between attempts and flow observations. It excludes subscription URLs/configuration/names, credentials, raw logs, local/public IP addresses, file paths and audio. Copying does not upload or send anything.

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
