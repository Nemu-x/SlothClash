# Required PR Tests

This folder contains the required PR merge test gate.

## Local run

From repo root:

`pnpm run test:required`

## What it checks

- backend tests: `go test ./...` in `apps/sloth-clash-desktop`
- backend build: `go build ./...` in `apps/sloth-clash-desktop`
- frontend type safety: `pnpm tsc --noEmit` in `apps/sloth-clash-desktop/frontend`
- config corpus validation: `TestMihomoCorpus*`
- runtime pipeline smoke validation: `TestRuntimeSmoke*`
- runtime E2E scenarios: `TestE2ERuntime*`
- optional local stress sample: `TestLocalStressYamlFromDownloadsIfPresent` (skip if absent)
- optional real-core preflight: `TestIntegrationPreflightWithRealCoreBinary` (skip if `SLOTH_MIHOMO_BIN` is not set)

These are intentionally fast smoke checks to block obviously broken runtime/config changes.

## Optional real-core integration

You can run preflight integration tests against a real mihomo binary:

`SLOTH_MIHOMO_BIN=/absolute/path/to/mihomo go test ./...`

These tests are auto-skipped when `SLOTH_MIHOMO_BIN` is not set.

For CI across all target OS, use manual workflow:

- `.github/workflows/integration-preflight.yml`
- Stable-only mode (no nightly schedule): run manually for a chosen stable release.
- You can provide either:
  - `mihomo_version` (for example `v1.19.3`) and workflow will resolve official MetaCubeX stable assets, or
  - direct per-OS URLs via workflow inputs (`mihomo_url_windows`, `mihomo_url_macos`, `mihomo_url_linux`)
- Optional repository variables for default stable URLs:
  - `MIHOMO_URL_WINDOWS`
  - `MIHOMO_URL_MACOS`
  - `MIHOMO_URL_LINUX`

## CI enforcement

- Workflow: `.github/workflows/required-tests.yml`
- Trigger: all pull requests
- Matrix: Ubuntu + Windows + macOS

Set this workflow as a required status check in branch protection, so PRs cannot merge until it passes.
