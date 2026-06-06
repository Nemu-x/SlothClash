# In-app updates — signing & release guide

SlothClash ships a **fail-closed, signed** in-app updater. This document is for
maintainers cutting releases and managing the signing key.

## Trust model

There are **two independent** signing concerns — don't conflate them:

| Concern | Mechanism | Needed for | Cert authority |
| --- | --- | --- | --- |
| **Update authenticity** | self-generated **minisign** (ed25519) key | in-app updates (this doc) | none — we generate it |
| **OS install-trust** | Authenticode (Win) / notarization (mac) | removing the first-install SmartScreen / Gatekeeper warning | paid CA / Apple — **out of scope** |

The updater downloads the installer + a `SHA256SUMS` file + its minisign
signature `SHA256SUMS.minisig`, **verifies the signature against the public key
embedded in the binary**, then verifies the installer's SHA-256 against the
(now-authenticated) checksums, and only then launches it. Anything unsigned or
tampered is **refused** (closes audit findings **F1 / F8**).

## One-time: generate the signing key

```bash
# install minisign: winget install jedisct1.minisign | brew install minisign | apt install minisign
minisign -G -p slothclash-updates.pub -s slothclash-updates.key
```

- **`slothclash-updates.pub`** — public, embed in the app (see below). Not secret.
- **`slothclash-updates.key`** — **private, password-encrypted**. Store the
  offline backup in a password manager / offline media. **Never commit it.**

### Embed the public key

The trusted keys live in `apps/sloth-clash-desktop/app_update_verify.go`:

```go
var trustedUpdateKeys = []string{
    "RWQ...AprY", // the second line of slothclash-updates.pub
}
```

It is an **array for rotation** (see below).

### CI secrets

Add two **repository secrets** (Settings → Secrets and variables → Actions):

- `MINISIGN_SECRET_KEY` — the **full contents** of `slothclash-updates.key`
  (both lines: the `untrusted comment:` line + the base64 key line).
- `MINISIGN_SIGN_PASSWORD` — the password that encrypts the key.

The release job (`.github/workflows/desktop-artifacts.yml`) uses them to sign
`SHA256SUMS` → `SHA256SUMS.minisig`. The signing step is guarded by
`if: env.MINISIGN_SECRET_KEY != ''`, so forks without the secret still build.

## Cutting a release

1. Bump the version (`apps/sloth-clash-desktop/version.go` + desktop version).
2. Push a tag `vX.Y.Z`. The **Desktop artifacts** workflow:
   - builds Windows installer `.exe` (exposed as a raw release asset),
     macOS `.dmg`, Linux `.tar.gz`;
   - generates `SHA256SUMS` over the assets (by basename);
   - **minisign-signs** it → `SHA256SUMS.minisig`;
   - publishes everything to the GitHub release.
3. The release must be a **normal release, not a prerelease** — the updater
   queries `releases/latest`, which GitHub excludes prereleases from.

> First time / sanity check: sign a sample file locally with the key and run the
> verifier (`go test ./... -run TestVerifyMinisign` covers the parser; for an
> end-to-end check, install the previous version and use **Check for updates**).

## Platform support

| OS | In-app apply | Notes |
| --- | --- | --- |
| **Windows** | ✅ download → verify → run installer → restart | full path |
| **macOS** (`.dmg`) | ❌ → opens the **release page** | Gatekeeper needs notarization we don't have |
| **Linux** (`.tar.gz`) | ❌ → opens the **release page** | not an AppImage; no in-place self-replace defined |

On non-Windows the Settings UI shows a note and an **Open release page** button.

## Key rotation

`trustedUpdateKeys` is an array so a key can be rotated without bricking
auto-update:

1. Generate a new keypair.
2. Ship an app build that trusts **both** old and new public keys.
3. Once that build is widely adopted, switch CI to sign with the new key.
4. In a later build, drop the old public key.

## Escape hatch (local testing only)

`SLOTH_ALLOW_UNVERIFIED_UPDATE=1` downgrades to best-effort (verify if present,
else allow). **Never set this in production** — an *invalid* signature is refused
regardless.

## Out of scope: OS install-trust

Authenticode (Windows SmartScreen "unknown publisher") and Apple notarization
(macOS Gatekeeper) require paid certificates and only affect **first-install**
UX, not update integrity. They are tracked separately (see SignPath application).
Until then, first-install users may see an OS warning and must choose
"Run anyway" / right-click → Open.
