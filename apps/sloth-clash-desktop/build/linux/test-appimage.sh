#!/usr/bin/env bash
# Smoke-tests a built AppImage the way the AppImage catalog does: on a machine
# WITHOUT webkit2gtk, every bundled library must load and the GUI must stay
# alive past WebKit's helper-process spawn instead of crashing at startup.
#
# Usage: apps/sloth-clash-desktop/build/linux/test-appimage.sh <path/to/App.AppImage> [appdir]
# Meant for the release container (Ubuntu 22.04, root): it REMOVES the host
# webkit2gtk packages first. Do not run it on a workstation.
set -euo pipefail

AI="${1:?AppImage path}"
APPDIR="${2:-}"
[ -x "$AI" ] || { echo "::error::$AI missing or not executable"; exit 1; }
export APPIMAGE_EXTRACT_AND_RUN=1

echo "== removing host WebKitGTK so only the bundled copy can satisfy the loader"
apt-get remove -y --purge 'libwebkit2gtk-4.1-*' 'libjavascriptcoregtk-4.1-*' >/dev/null
! ldconfig -p | grep -q libwebkit2gtk || { echo "::error::host webkit still present"; exit 1; }

echo "== GUI under Xvfb: must still be running after 20 s (timeout exit 124)"
# The catalog runs with the C locale and no network; mirror both.
set +e
LANG=C LC_ALL=C xvfb-run -a --server-args='-screen 0 1280x800x24' timeout 20s "$AI" >gui.log 2>&1
rc=$?
set -e
cat gui.log || true
[ "$rc" -eq 124 ] || { echo "::error::GUI exited early with code $rc"; exit 1; }
if grep -Eiq 'error while loading shared|Unable to spawn|Failed to fully launch|cannot open shared object' gui.log; then
  echo "::error::startup log shows a bundling problem"; exit 1
fi

if [ -n "$APPDIR" ] && command -v desktop-file-validate >/dev/null; then
  echo "== desktop file"
  desktop-file-validate "$APPDIR/usr/share/applications/sloth-clash.desktop"
fi
if [ -n "$APPDIR" ] && command -v appstreamcli >/dev/null; then
  echo "== AppStream metainfo (warnings allowed, errors fail)"
  appstreamcli validate --no-net "$APPDIR"/usr/share/metainfo/*.xml
fi
echo "AppImage smoke test passed."
