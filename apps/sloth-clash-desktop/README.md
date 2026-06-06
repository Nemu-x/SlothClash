# Sloth Clash — desktop shell

This is the **Wails (Go + React/TS)** desktop application for **Sloth Clash**, a GUI around **Mihomo** (Clash Meta).

> Full project docs — overview, screenshots, downloads, build, and contributing — live in the **repository root**:
> **[`../../README.md`](../../README.md)** · [Русский](../../docs/README_ru.md) · [简体中文](../../docs/README_zh.md)

## Layout

- `app*.go`, `core_manager.go`, `*_windows.go` / `*_darwin.go` / `*_stub.go` — Go backend (connect/disconnect, core lifecycle, system proxy, TUN, updates, IPC), platform-split by build tags.
- `frontend/` — React + TypeScript UI (Vite). Wails bindings are generated under `frontend/wailsjs/`.
- `build/` — generated resources (mihomo sidecar, geo DBs, service binaries, icons, NSIS template). **gitignored**; produced by `pnpm run desktop:resources`.
- `packaging/windows/project.nsi` — NSIS installer template synced into the Wails build tree.

## Quick start

Run these from the **repo root** (not this folder):

```bash
pnpm install
pnpm run desktop:resources   # populate build/ (sidecar, geo, service, icons)
pnpm run wails:dev           # live dev; or pnpm run wails:build
```

Production-like builds: `pnpm run desktop:build:windows` (NSIS, Windows only), `:darwin:arm64`, `:darwin:amd64`, `:linux:amd64`.

## Versioning

Keep `version.go` (`AppVersion`) and `wails.json` (`info.productVersion`) in sync — the in-app updater compares `AppVersion` against the latest GitHub release tag. See [`../../docs/UPDATES.md`](../../docs/UPDATES.md) for the release & signing flow.

## Config reference

Wails project settings: `wails.json` (see https://wails.io/docs/reference/project-config).
