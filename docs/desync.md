# Desync

Desync connects selected services directly using automatically tested strategies in the embedded mihomo core. Other traffic follows the subscription. The feature is off by default.

## Use

Open **Settings → Desync**, enable it and select YouTube, Discord, or Discord voice. Connect in Rule mode. Desktop Discord voice additionally requires Windows and TUN. Manual TTL, repeat and method fields are no longer exposed.

Selection runs in the background even when Settings is closed. The panel shows progress and one of these outcomes:

- **HTTPS available**: the service passed two independent rounds of HTTPS checks with a successful HTTP status.
- **Waiting for a Discord voice connection**: no current voice endpoint is available to test. Join a channel.
- **Checking voice traffic**: the current route is being observed; sustained application traffic is not established yet. Rejoining the channel can accelerate renegotiation after a strategy change.
- **UDP exchange detected**: the actual application has sustained incoming and outgoing UDP traffic.
- **Media traffic in both directions**: RTP-shaped packets are progressing in both directions across multiple observations. This does not inspect, decrypt or certify audible audio.
- **No working strategy found**: the bounded search failed; the controller backs off and tries again. **Check again** restarts calibration immediately.

**Copy diagnostics** copies a report through the native clipboard. It includes app/core build identifiers, an opaque network digest, enabled services, candidate attempts and error categories, probe response counts and anonymized application traffic counters. It excludes subscription URLs/configuration/names, credentials, raw logs, local/public IP addresses, file paths and audio. Copying does not upload or send anything.

## Selection and recovery

Generated hidden selector groups keep the familiar `SLOTH-DESYNC-YOUTUBE`, `SLOTH-DESYNC-DISCORD` and `SLOTH-DESYNC-DISCORD-UDP` connection routes. Candidates are independent named direct outbounds. The controller tests candidates before changing a selector; candidate changes do not reload the whole core or edit the subscription.

The initial YouTube candidate uses TLS record fragmentation at `3+s`. Discord TCP starts with `1+s` plus a Windows fake prefix at TTL 4. Voice starts at TTL 4 with one fake before each of the first three application datagrams. Searches vary TLS record/write boundaries or fake TTL independently for each service. UDP searches use conservative one- or two-fake bursts rather than assuming that more fakes are better.

Successful candidate IDs are cached for up to six network/build digests, expire after 24 hours, and are revalidated on connection/network changes. HTTPS services are checked independently and periodically. TTL candidates cover 1–32, with the proven presets first. Searches have deadlines and retry backoff, and resume across a failed search budget instead of repeatedly testing only the first candidates. Disabling Desync, disconnecting, changing mode/network or selecting different services cancels superseded work; a late result cannot apply to a newer connection generation.

Voice calibration waits for an endpoint actually used by `Discord.exe`. An authenticated core API probe sends at most twelve independent IP-discovery requests through a candidate, validates the reply address/type/length/SSRC, and rejects sequences that only answer the first few requests. No cached voice server, account credentials or captured call packets are used. These diagnostic requests are excluded from the real application activity counters.

Actual application traffic takes priority: an already working call is observed and retained rather than being invalidated by a separate discovery socket. Initial discovery or one good ping never establishes media success. Ongoing sends with no incoming progress trigger candidate probing and reselection after a grace period. An idle or silent channel is not itself a failure. If a selector changes, only stale Desync voice UDP flows for that endpoint are closed; other applications, Discord TCP and subscription selections are retained. Discord may need time to renegotiate, or the user can rejoin the voice channel.

There is no automatic fallback to remote subscription nodes. Disable only the voice service and rejoin to return to the subscription's normal voice rule. A call working through a subscription node does not validate direct Desync voice.

## Routing and DNS

The YouTube overlay includes the site/API, `googlevideo.com`, `ytimg.com` and `yt3.ggpht.com`. Selected service TCP rules precede subscription service rules; targeted UDP/443 is rejected for TCP fallback, except desktop Discord UDP when explicitly enabled. Corporate VPN coexistence retains its priority. Browser voice is outside the desktop process rule.

Named Desync outbounds retry empty direct DNS resolution once through the profile's normal resolver. Ordinary DIRECT, subscription DNS configuration, canceled requests and TLS/connect errors retain their behavior. The core reports resolution fallbacks and exact probe failures; HTTP test error pages do not count as successful availability.

## Trial checklist

1. Keep the previous installer and back up `%APPDATA%\SlothClash\prefs.json` and `profiles.json`.
2. Close SlothClash and install the trial as a normal update. The installer preserves data and refreshes the existing service's core hash pin.
3. Connect the existing subscription in Rule mode. Use TUN for voice. Enable the desired Desync services; no strategy values need to be entered.
4. Restart the browser/Discord, verify YouTube playback and seeking, and test a two-way Discord call and a stream separately. Inspect connection chains to distinguish direct Desync from subscription routing.
5. Include cold startup, reconnect, a longer call and a different network where available. If something fails, press **Copy diagnostics** and send the report with what failed and when. **Check again** restarts the search.
6. To compare subscription behavior, disable the relevant service and restart the application. Other subscription routes should retain their behavior.

The library remains pinned to `github.com/Nemu-x/desync v0.0.0-20261024221341-3d471713bd2e`. Core patches 0017/0019 provide native transformations and DNS fallback; 0020 adds scoped UDP probes, application-only activity telemetry, cancellation and strict HTTP probe status handling. Build validation applies and tests the entire patch series. Local mechanics and automated checks do not replace testing on each tester's network.
