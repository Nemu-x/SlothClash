# Security Policy

## Reporting a vulnerability

**Please do not open public issues for security vulnerabilities.**

Report privately via GitHub's **"Report a vulnerability"** (repository → **Security** tab → *Report a vulnerability*), which opens a private security advisory with the maintainers. Include:

- a description of the issue and its impact,
- steps to reproduce (a proof-of-concept if you have one),
- affected version(s) / OS,
- any suggested remediation.

We aim to acknowledge reports within a few days and will coordinate a fix and disclosure timeline with you. Please give us reasonable time to ship a fix before any public disclosure.

## Supported versions

Security fixes target the **latest released version**. Please reproduce on the most recent release before reporting.

## Security model

SlothClash is a desktop proxy/VPN client (a Wails fork of clash-verge-rev) that manages the **mihomo** (Clash.Meta) core. Key security properties:

- **Signed, fail-closed updates.** In-app updates download a `SHA256SUMS` file and its **minisign (ed25519)** signature; the app verifies the signature against a **public key embedded in the binary**, then verifies the installer's SHA-256 against the (now-authenticated) checksums, and only then launches it. Anything unsigned, tampered, or mismatched is **refused**. (See `docs/UPDATES.md`.)
- **Dependency scanning in CI.** Every PR/push runs `govulncheck` (Go) and `pnpm audit` (frontend prod deps); the build fails on known-exploitable vulnerabilities (`.github/workflows/security-audit.yml`).
- **Reproducible core provisioning.** The embedded mihomo core is pinned to an explicit version (not a moving `latest`), so a given commit always ships the same core.
- **Privileged helper service.** A separate, open-source helper service (`sloth-clash-service`) manages the mihomo core and the TUN adapter. Installing it requires a one-time elevation (UAC / `sudo` / `pkexec`); it then lets the unprivileged app start/stop the core without further elevation. The service and its hardening are tracked in its own repository.
- **Privacy.** No analytics/telemetry are sent to the SlothClash project. Subscription requests include client metadata headers (e.g. a hardware id) that the provider uses for rate-limiting/classification; the hardware-id header is **user-toggleable** in Settings.
- **Open source.** Licensed GPL-3.0; the full source is auditable.

## Out of scope

- **OS first-install trust** (Windows SmartScreen "unknown publisher" / macOS Gatekeeper) requires paid code-signing / notarization and is tracked separately. Until a code-signing certificate is in place, first-install users may see an OS warning and must choose "Run anyway" / right-click → Open. This does **not** affect update integrity, which is cryptographically verified as described above.
- Vulnerabilities in upstream dependencies (mihomo, Wails, OS components) should be reported to their respective projects; if they affect SlothClash users, we'll help coordinate.
