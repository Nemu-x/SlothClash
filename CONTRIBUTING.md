# Contributing to Sloth Clash

Thanks for helping improve **Sloth Clash** — a **Wails + Go + React** desktop client around **Mihomo** (Clash Meta), hosted under `apps/sloth-clash-desktop/`.

## Internationalization (i18n)

UI strings live in flat JSON files per language. For where to edit and how to keep locales aligned, see **[docs/CONTRIBUTING_i18n.md](docs/CONTRIBUTING_i18n.md)** (short guide).

## What you need

| Tool | Notes |
| --- | --- |
| **Go** | 1.25+ ([go.dev/dl](https://go.dev/dl)); on `PATH` for `wails` / CI. Matches `go.mod` (`go 1.25`, toolchain 1.26.x). |
| **Node.js** | 20+ |
| **pnpm** | `corepack enable` then use repo `packageManager` (see `package.json`). |
| **Wails v2** | Invoked via `node scripts/wails.mjs` (downloads the CLI with `go run`); global install optional. |
| **NSIS** | Windows only, for **`-nsis`** installer builds (`choco install nsis` or [nsis.sourceforge.io](https://nsis.sourceforge.io/Download)). |

This repository does **not** use Tauri or the old `src-tauri` workspace — ignore upstream Verge docs that refer to them.

## Clone and install

```bash
pnpm install --frozen-lockfile
```

## Desktop resources (Mihomo, geo DBs, service binaries, icons)

```bash
pnpm run desktop:resources
```

This runs `prebuild`, Wails asset prep, Windows icon generation, and copies `packaging/windows/project.nsi` into the Wails build tree. Output goes under `apps/sloth-clash-desktop/build/` (gitignored).

**Windows service binaries** are downloaded from [sloth-clash-service-ipc releases](https://github.com/Nemu-x/sloth-clash-service-ipc/releases); override tag with `SLOTH_SERVICE_RELEASE_TAG` if needed (see `scripts/prebuild.mjs`).

**Mihomo core** is pinned to a specific release in `scripts/prebuild.mjs` (the `META_VERSION_PINNED` constant) for reproducible builds. To bump it: edit that constant, then run `pnpm run prebuild --force` to refresh the embedded sidecar. For a one-off build against a candidate version without editing the file, set `MIHOMO_CORE_VERSION` (e.g. `MIHOMO_CORE_VERSION=v1.19.27 pnpm run prebuild --force`).

## Development

```bash
pnpm run wails:dev
```

Runs the Wails v2 dev server for `apps/sloth-clash-desktop` (frontend + Go backend).

## Production-like builds

```bash
# Windows installer (NSIS) — run on Windows with NSIS installed
pnpm run desktop:build:windows

# Other platforms (no NSIS)
pnpm run desktop:build:darwin:arm64
pnpm run desktop:build:darwin:amd64
pnpm run desktop:build:linux:amd64
```

## Linux (local dev)

Install GTK + WebKit2GTK dev packages (names vary by distro). Example for Debian/Ubuntu:

```bash
sudo apt-get update
sudo apt-get install -y build-essential libgtk-3-dev libwebkit2gtk-4.1-dev
```

## Checks before a PR

```bash
pnpm run lint
pnpm run typecheck
pnpm run format:check   # optional; or pnpm run format to write
```

Go code: `cd apps/sloth-clash-desktop && go vet ./...` (and `gofmt` as you prefer).

## Commits and PRs

1. Fork and branch from the default branch.
2. Keep commits focused; write clear messages.
3. Open a PR describing **what** and **why**; screenshots help for UI/i18n.
4. Signed commits are welcome but not required unless maintainers ask.

CI: [.github/workflows/desktop-artifacts.yml](.github/workflows/desktop-artifacts.yml) builds Windows (with NSIS via Chocolatey), macOS, and Linux artifacts on tag `v*` or manual **workflow_dispatch**.

## Releases & signed updates

Cutting a release and managing the in-app update signing key (minisign) is documented in **[docs/UPDATES.md](docs/UPDATES.md)**. In short: bump `apps/sloth-clash-desktop/version.go` **and** `wails.json` `productVersion` together, push a non-prerelease tag `vX.Y.Z`, and CI publishes the installers plus a signed `SHA256SUMS`. The updater is **fail-closed**: unsigned or tampered artifacts are refused.

Thank you for contributing.
