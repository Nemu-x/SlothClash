# Forking the Clash Verge IPC service for Sloth (no SCM / pipe conflicts)

Goal: **Verge** keeps `clash_verge_service` + `\\.\pipe\clash-verge-service`; **Sloth** uses **new** Windows service name, display name, and named pipe so both installers can coexist and you can stop Verge TUN and start Sloth TUN without sharing one SCM slot.

Reference tree: your fork (e.g. `sloth-clash-service-ipc`) cloned from [clash-verge-rev/clash-verge-service-ipc](https://github.com/clash-verge-rev/clash-verge-service-ipc).

## Windows (highest priority)

| What | Where to change | Example Sloth value |
|------|-----------------|---------------------|
| SCM internal name | `src/bin/install_service.rs` — `open_service("…")`, `ServiceInfo { name: … }` | `sloth_clash_service` |
| Display name / description | same file — `display_name`, `set_description` | `Sloth Clash Service` |
| Named pipe (IPC) | `src/lib.rs` — `IPC_PATH` | `\\.\pipe\sloth-clash-service` |
| Service EXE on disk | installer looks for `clash-verge-service.exe` next to install exe | build/copy as `sloth-clash-service.exe` and adjust `with_file_name(...)` |
| NSIS bundle | `resources/installer.nsi` — `OutFile`, `InstallDir`, `ExecShell` target names | `SlothClashServiceInstaller.exe`, `SlothClashService`, run `sloth-clash-service-install.exe` |

Also grep the repo for `clash_verge`, `clash-verge-service`, `Clash Verge`, and `clash.verge` strings and update tests under `tests/` and `.github/workflows/*.yml` that hardcode the old names.

**Important:** Sloth Desktop must talk to the **same** pipe name and service binary names that your fork installs (if you ever wire GUI ↔ service IPC beyond raw mihomo).

## macOS

- `resources/info.plist.tmpl` — bundle id / label (`io.github.clash-verge-rev…`).
- `resources/launchd.plist.tmpl`, paths in `install_service.rs` / `uninstall_service.rs` (`/Library/...`).

## Linux

- `SERVICE_NAME` / unit file path in `install_service.rs` and `uninstall_service.rs` (`clash-verge-service.service`).

## Cargo artifact names (optional but clearer)

In `Cargo.toml`, `[[bin]]` `name` values drive output filenames. You can keep crate package name and only rename bins to `sloth-clash-service`, `sloth-clash-service-install`, `sloth-clash-service-uninstall`, then update CI and NSIS placeholders to match.

## SlothClash repo follow-up

- `scripts/prebuild.mjs`: pulls **`sloth-clash-service*.exe`** from **`https://github.com/Nemu-x/sloth-clash-service-ipc/releases/download/<tag>/`**. Default `<tag>` is the Rust host triple; on **Windows GNU** toolchains the tag is mapped to **`…-pc-windows-msvc`** so the MSVC artifacts resolve. Override: `SLOTH_SERVICE_RELEASE_TAG=my-tag pnpm run prebuild`. You must publish a GitHub **Release** whose tag matches that string and attach the three binaries.
- `apps/sloth-clash-desktop/app.go` — `findServiceInstaller` should match the new installer basename.
- Re-test **Install service** + TUN with Verge’s service **stopped** / not registered under the same name.

This document is a checklist; exact strings depend on what you choose for branding (`sloth_*` vs `io.github.*` bundle ids for macOS).

## Relationship to the v0.3 reload model

Since Sloth Desktop `0.3.0` the GUI talks to Mihomo through the **reload model** (Clash Verge Rev): a single long-lived `mihomo` process per active profile, with `Connect` / `Disconnect` / traffic-mode flips / YAML hot reloads driven over Mihomo's external controller API (`PATCH /configs`, `PUT /configs?force=true`).

This does **not** change what the IPC service (`sloth_clash_service` / `\\.\pipe\sloth-clash-service`) has to do — its job is still:

1. Spawn the Mihomo binary under LocalSystem with the requested config dir and arguments.
2. Expose Mihomo's named pipe (`slothMihomoIPCPath`) to the unprivileged GUI so the desktop can hit `/configs` and `/proxies` without elevation.
3. Kill the Mihomo process when the GUI asks for shutdown.

All the reload-model traffic (TUN toggle, hot reload, mode change) rides **on top of** that existing pipe — no new SCM surface, no extra privileged endpoint, no new installer artifacts. If you are porting a Verge fork to a new branding you can follow the checklist above without touching any Sloth-specific reload logic.
