# Desync

SlothClash applies direct TCP desynchronization to selected services and provides a separate experimental Discord desktop UDP mode. Other traffic retains the subscription's routes. The library is linked into the embedded mihomo core through patches `0017-direct-desync.patch` and `0019-desync-dns-udp.patch`.

## Controls

Open **Settings → Desync**. The feature is off by default.

- **YouTube** uses TLS record fragmentation at `3+s`.
- **Discord** uses TLS record fragmentation at `1+s` and a Windows low-TTL fake prefix. The initial preset is TTL 4; the advanced field accepts 1–255.
- **Discord UDP (experimental)** routes UDP belonging to `Discord.exe` directly through `SLOTH-DESYNC-DISCORD-UDP`. It requires Windows and TUN. It is off by default, including after updating an existing installation. The initial preset sends one low-TTL fake datagram before each of the first three application datagrams to a peer, with TTL 4. Originals are unchanged. There is no automatic fallback to subscription nodes; effectiveness must be tested on the actual network. Existing explicit TTL/repeat settings are retained when updating.
- **Advanced → Apply parameters** saves TCP TTL and UDP TTL/repeats together. UDP repeats accept 1–16. TTL values are examples, not a route-independent guarantee.
- **Test HTTPS** sends fixed HTTPS checks through the named TCP outbounds in the running core. It requires a connected client in Rule mode. These checks do not log in, send messages or test voice.
- **Reset Discord UDP connections** closes only current Discord UDP flows so new traffic can select the configured route. Leave and rejoin the voice channel to renegotiate its public address. Browser UDP, TCP/Gateway connections and other applications are retained.
- **Disable Desync** removes the generated service routes and reloads the normal runtime configuration. Existing application connections retain their handshake; restart the application being compared.

The subscription body is unchanged. Generated TCP rules take priority over its normal service rules. The YouTube list includes `yt3.ggpht.com` thumbnails/avatars as well as the video/site domains. Targeted UDP/443 is rejected for TCP fallback, except for desktop Discord UDP when the experimental route is explicitly enabled. Other UDP retains the profile's routing. Corporate VPN coexistence rules retain their existing priority. Browser voice is outside the desktop process rule.

Settings and diagnostic outcomes are included in the existing runtime diagnostics. The Connections screen identifies selected connections as `SLOTH-DESYNC-YOUTUBE` or `SLOTH-DESYNC-DISCORD`.

When UDP mode is enabled, its settings panel polls actual connection chains and aggregate native counters. **Desync flows** means the dedicated UDP route; **subscription rules** means an existing route (including a subscription's own DIRECT rule). Fake datagrams sent and original packet counters verify that the wrapper ran, while hearing voice and receiving a stream verify application success. No voice payload or Discord credentials are collected by this status view.

The current client uses fixed presets; it does not automatically try or switch strategies. Failed HTTPS checks show the core's error detail and write a warning containing the outbound name to **Logs**. An API `503` denotes a failed core URL test, not necessarily a `503` response from the target website. A successful browser connection can coexist with a failed probe because the checks use a separate HEAD request and connection.

## Direct DNS fallback

A named Desync outbound can retry resolution once through the profile's normal resolver when its direct resolver supplies no usable IP (`couldn't find ip` or an empty answer). This applies to TCP and hostname-based UDP and is enabled by the generated `dns-fallback: true` option. Ordinary DIRECT and all subscription DNS settings remain unchanged. Connection/TLS errors and canceled requests do not trigger this DNS retry. Successful fallback is recorded as `[desync DNS]` in Logs and as `desyncDNSFallbacks` on the named proxy's controller JSON. This addresses the observed empty direct-DNS result without changing the proven TLS preset or hard-coding a cached YouTube IP.

## Live verification with a production subscription

1. Back up `%APPDATA%\SlothClash\prefs.json` and `profiles.json`. Keep the previous installer for rollback.
2. Close the running client and install the test build as a normal update. The Windows installer preserves user data and refreshes an existing service's core hash pin. If service startup still reports an old pin, use the existing reinstall-service action.
3. Select the existing subscription, choose **Rule** mode and connect using the normal Proxy or TUN mode. Verify the subscription works with Desync disabled.
4. Enable Desync for the service being measured. Restart its browser or desktop application to create fresh connections.
5. Run the in-client HTTPS checks and inspect Connections for the expected named outbound.
6. For YouTube, play a video, seek, and compare buffering. For Discord, check full UI, sign-in, messages, attachments and Gateway reconnect. Assess voice separately and record its actual route.
7. Disable bypass, restart the same applications and repeat the comparison. Other subscription traffic should keep its original route throughout.

For a cold YouTube comparison, restart the client and browser and record the first HTTPS check/page load without repeatedly refreshing. If it fails, retain the complete new error and the `[desync DNS]` log lines.

For direct UDP: use Rule + TUN mode, enable **Discord UDP (experimental)**, leave the voice channel, reset Discord UDP connections, and rejoin it. Confirm `SLOTH-DESYNC-DISCORD-UDP` in Connections, a nonzero fake count, increasing original counters and traffic in both directions, audible voice and working streams. Fake counts stop increasing after the initial three-datagram window. Initial discovery replies or a normal initial ping do not establish sustained media delivery. Larger fake bursts can disrupt the exchange on some paths; start with UDP TTL 4 and **UDP fake repeats = 1**, save the parameters and restart Discord. These are separate fields. If media fails, turn off only the UDP option, reset its flows and rejoin; verify the subscription route returns. A working call through a subscription node is not a successful direct UDP test.

Do not treat an HTTPS probe or a title-only Discord page as a complete application success. Authentication and real usage are owner-run checks. If the client is in Global or Direct mode, the service-specific rules are inactive.

## Core configuration

The optional configuration belongs to a named `direct` proxy. Plain `DIRECT` is unchanged:

```yaml
proxies:
  - name: SLOTH-DESYNC-YOUTUBE
    type: direct
    desync:
      tlsrec: 3+s
      dns-fallback: true
  - name: SLOTH-DESYNC-DISCORD
    type: direct
    desync:
      tlsrec: 1+s
      dns-fallback: true
      fake:
        ttl: 4
        prefix: -1
  - name: SLOTH-DESYNC-DISCORD-UDP
    type: direct
    desync:
      dns-fallback: true
      udp:
        ttl: 4
        repeats: 1
        packets: 3
```

The standalone dependency is pinned to `github.com/Nemu-x/desync v0.0.0-20261024221341-3d471713bd2e` with Go module checksums in patch 0019. Native TCP/UDP fake platforms and options are validated before configuration is accepted. TFO and MPTCP are rejected for a configured Desync outbound. UDP uses the owned application's UDP socket, restores its hop limit before originals and requires no packet driver or remote proxy. See the [library API and platform details](https://github.com/Nemu-x/desync/tree/feat/udp-fake).

The core build tests the patch packages on both Linux and Windows, runs the library's native Windows tests on Windows, and retains the existing six-target cross-compilation matrix.
