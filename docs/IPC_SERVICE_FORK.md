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

- `scripts/prebuild.mjs`: download or copy **your** release assets instead of upstream `clash-verge-service-*` names (or rename after download).
- `apps/sloth-clash-desktop/app.go` — `findServiceInstaller` should match the new installer basename.
- Re-test **Install service** + TUN with Verge’s service **stopped** / not registered under the same name.

This document is a checklist; exact strings depend on what you choose for branding (`sloth_*` vs `io.github.*` bundle ids for macOS).
