#!/usr/bin/env bash
set -euo pipefail

sudo apt-get update

# Common Wails/Linux desktop dependencies (GTK + WebKit).
sudo apt-get install -y --no-install-recommends \
  build-essential \
  pkg-config \
  libgtk-3-dev \
  libwebkit2gtk-4.1-dev

corepack enable

if [[ -f "pnpm-lock.yaml" ]]; then
  pnpm install --frozen-lockfile
else
  pnpm install
fi
