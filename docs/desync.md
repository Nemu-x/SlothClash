# DPI bypass with desync

SlothClash can apply direct TCP desynchronization to selected services while keeping the subscription's routes for other traffic. The implementation is linked into the embedded mihomo core through patch `0017-direct-desync.patch`; it is not a separate background proxy.

## Controls

Open **Settings → DPI bypass**. The feature is off by default.

- **YouTube** uses TLS record fragmentation at `3+s`.
- **Discord** uses TLS record fragmentation at `1+s` and a Windows low-TTL fake prefix. The initial preset is TTL 4; the advanced field accepts 1–255.
- **Test selected services** sends fixed HTTPS checks through the named outbounds in the running core. It requires a connected client in Rule mode. These checks do not log in, send messages or test voice.
- **Disable bypass** removes the generated service routes and reloads the normal runtime configuration. Existing application connections retain their handshake; restart the application being compared.

The subscription body is unchanged. Generated TCP rules take priority over its normal service rules. Targeted UDP/443 is rejected so browsers can fall back to TCP. Other UDP retains the profile's routing; this feature does not implement a UDP voice bypass. Corporate VPN coexistence rules retain their existing priority.

Settings and diagnostic outcomes are included in the existing runtime diagnostics. The Connections screen identifies selected connections as `SLOTH-DESYNC-YOUTUBE` or `SLOTH-DESYNC-DISCORD`.

## Live verification with a production subscription

1. Back up `%APPDATA%\SlothClash\prefs.json` and `profiles.json`. Keep the previous installer for rollback.
2. Close the running client and install the test build as a normal update. The Windows installer preserves user data and refreshes an existing service's core hash pin. If service startup still reports an old pin, use the existing reinstall-service action.
3. Select the existing subscription, choose **Rule** mode and connect using the normal Proxy or TUN mode. Verify the subscription works with DPI bypass disabled.
4. Enable DPI bypass for the service being measured. Restart its browser or desktop application to create fresh connections.
5. Run the in-client HTTPS checks and inspect Connections for the expected named outbound.
6. For YouTube, play a video, seek, and compare buffering. For Discord, check full UI, sign-in, messages, attachments and Gateway reconnect. Assess voice separately and record its actual route.
7. Disable bypass, restart the same applications and repeat the comparison. Other subscription traffic should keep its original route throughout.

Do not treat an HTTPS probe or a title-only Discord page as a complete application success. Authentication and real usage are owner-run checks. If the client is in Global or Direct mode, the service-specific rules are inactive.

## Core configuration

The optional configuration belongs to a named `direct` proxy. Plain `DIRECT` is unchanged:

```yaml
proxies:
  - name: SLOTH-DESYNC-YOUTUBE
    type: direct
    desync:
      tlsrec: 3+s
  - name: SLOTH-DESYNC-DISCORD
    type: direct
    desync:
      tlsrec: 1+s
      fake:
        ttl: 4
        prefix: -1
```

The standalone dependency is pinned to `github.com/Nemu-x/desync v0.0.0-20261024204049-3fa5a43f2e74` with Go module checksums in the core patch. Unsupported fake platforms and invalid options fail configuration validation. TFO and MPTCP are rejected for a configured desync outbound.

The core build tests the patch packages on both Linux and Windows, runs the library's native Windows tests on Windows, and retains the existing six-target cross-compilation matrix.
