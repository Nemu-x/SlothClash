#!/usr/bin/env bash
# Builds a self-contained AppImage of Sloth Clash.
#
# Usage: apps/sloth-clash-desktop/build/linux/build-appimage.sh <x86_64|aarch64> <version> <out-dir>
#   Expects the Wails binary at apps/sloth-clash-desktop/build/bin/sloth-clash-desktop
#   and must run on a Debian/Ubuntu host that has libgtk-3 and libwebkit2gtk-4.1
#   installed — that is where the bundled copies come from. Run it from the
#   repository root. CI runs it inside ubuntu:22.04 so the result needs no
#   newer glibc than the oldest supported Ubuntu LTS.
#
# Why this is not just "appimagetool AppDir": the app links WebKitGTK, and an
# AppImage that relies on the host's WebKit is not self-contained (the AppImage
# catalog test rejects it, and users without webkit2gtk-4.1 get "error while
# loading shared libraries"). Bundling WebKitGTK has two traps, both handled here
# the way Tauri does it:
#   1. WebKit spawns helper processes (WebKitNetworkProcess, WebKitWebProcess,
#      WebKitGPUProcess) and loads an injected bundle from absolute paths
#      compiled into libwebkit2gtk-4.1.so.0 (/usr/libexec/webkit2gtk-4.1 or
#      /usr/lib/<triplet>/webkit2gtk-4.1, depending on the distro build). The
#      script reads those paths out of the library, copies the directories into
#      the AppDir keeping the layout under usr/, and patches each string to a
#      relative path of the SAME length; AppRun chdir's into usr/ so it
#      resolves to the bundled files on any distro.
#   2. GTK needs its schemas, pixbuf loaders, IM modules and GIO modules (TLS).
#      linuxdeploy-plugin-gtk (Tauri's fork, which also bundles GIO modules)
#      collects them and writes an apprun-hooks/ script that AppRun sources.
set -euo pipefail

ARCH="${1:?arch (x86_64|aarch64)}"
VERSION="${2:?version, e.g. 0.9.4}"
OUT_DIR="${3:?output directory}"

APP=apps/sloth-clash-desktop
LINUX="$APP/build/linux"
BIN="$APP/build/bin/sloth-clash-desktop"
[ -x "$BIN" ] || { echo "::error::$BIN not found — run the wails build first"; exit 1; }
LIBDIR="$(dpkg-architecture -qDEB_HOST_MULTIARCH)" # x86_64-linux-gnu / aarch64-linux-gnu
HOST_WEBKIT="/usr/lib/$LIBDIR/libwebkit2gtk-4.1.so.0"
[ -f "$HOST_WEBKIT" ] || { echo "::error::$HOST_WEBKIT missing — install libwebkit2gtk-4.1-0"; exit 1; }

WORK="$(mktemp -d)"
APPDIR="$WORK/AppDir"
TOOLS="$WORK/tools"
mkdir -p "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/512x512/apps" \
  "$APPDIR/usr/share/metainfo" "$TOOLS" "$OUT_DIR"
export APPIMAGE_EXTRACT_AND_RUN=1 # the tools are AppImages themselves; no FUSE in containers

# webkit_paths prints every NUL-terminated string in a library that looks like
# an absolute /usr/... path mentioning webkit2gtk-4.1 — the helper directory
# and the injected bundle, whatever this distro's build chose.
webkit_paths() {
  perl -0777 -ne 'while (/(\/usr\/[\x21-\x7e]*?webkit2gtk-4\.1(?:\/[\x21-\x7e]*)?)\0/g) { print "$1\n" }' "$1" | sort -u
}

# --- 1. Application files -------------------------------------------------
# The binary is handed to linuxdeploy under its final name (Exec= in the
# .desktop file); linuxdeploy copies it into usr/bin and sets its rpath.
cp "$BIN" "$WORK/sloth-clash-desktop"
cp "$LINUX/sloth-clash.desktop" "$APPDIR/usr/share/applications/"
cp "$APP/build/appicon.png" "$APPDIR/usr/share/icons/hicolor/512x512/apps/sloth-clash.png"
sed -e "s/@VERSION@/$VERSION/" -e "s/@DATE@/$(date -u +%Y-%m-%d)/" \
  "$LINUX/sloth-clash.metainfo.xml" > "$APPDIR/usr/share/metainfo/io.github.nemu_x.SlothClash.metainfo.xml"
# Our own AppRun: linuxdeploy leaves an existing AppRun alone (and would
# otherwise symlink the binary, skipping the GTK hooks and the chdir).
install -m 0755 "$LINUX/AppRun" "$APPDIR/AppRun"

# --- 2. WebKit helper processes + injected bundle, layout preserved under usr/ -
mapfile -t EMBEDDED < <(webkit_paths "$HOST_WEBKIT")
[ "${#EMBEDDED[@]}" -gt 0 ] || { echo "::error::no /usr/...webkit2gtk-4.1 path found inside $HOST_WEBKIT"; exit 1; }
echo "Paths compiled into libwebkit2gtk-4.1.so.0:"; printf '  %s\n' "${EMBEDDED[@]}"
declare -A COPIED=()
for p in "${EMBEDDED[@]}"; do
  d="$p"; [ -d "$d" ] || d="$(dirname "$p")"
  [ -d "$d" ] || { echo "  (skip $p — not present on this host)"; continue; }
  [ -n "${COPIED[$d]:-}" ] && continue
  mkdir -p "$APPDIR$(dirname "$d")"
  cp -a "$d" "$APPDIR$d"
  COPIED[$d]=1
  echo "  copied $d"
done
[ "${#COPIED[@]}" -gt 0 ] || { echo "::error::no WebKit helper directory could be copied"; exit 1; }
HELPERS=()
for d in "${!COPIED[@]}"; do
  while IFS= read -r -d '' f; do HELPERS+=("$f"); done \
    < <(find "$APPDIR$d" -type f \( -perm -u+x -o -name '*.so' \) -print0)
done
[ "${#HELPERS[@]}" -gt 0 ] || { echo "::error::no WebKit helpers found under ${!COPIED[*]}"; exit 1; }
echo "WebKit helpers: ${HELPERS[*]#"$APPDIR"}"

# --- 3. linuxdeploy + Tauri's GTK plugin: libraries, schemas, loaders, GIO ---
curl -fsSL --retry 6 --retry-all-errors --retry-delay 10 -o "$TOOLS/linuxdeploy" \
  "https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-$ARCH.AppImage"
curl -fsSL --retry 6 --retry-all-errors --retry-delay 10 -o "$TOOLS/linuxdeploy-plugin-gtk.sh" \
  "https://raw.githubusercontent.com/tauri-apps/linuxdeploy-plugin-gtk/master/linuxdeploy-plugin-gtk.sh"
chmod +x "$TOOLS/linuxdeploy" "$TOOLS/linuxdeploy-plugin-gtk.sh"
DEPS_ARGS=()
for h in "${HELPERS[@]}"; do DEPS_ARGS+=(--deploy-deps-only "$h"); done
# Helpers: dependencies only — they must stay where WebKit expects them.
PATH="$TOOLS:$PATH" DEPLOY_GTK_VERSION=3 DISABLE_COPYRIGHT_FILES_DEPLOYMENT=1 \
  "$TOOLS/linuxdeploy" --appdir "$APPDIR" \
    --executable "$WORK/sloth-clash-desktop" \
    "${DEPS_ARGS[@]}" \
    --plugin gtk
# linuxdeploy only fixed the rpath of what it copied itself; the helpers sit
# elsewhere under usr/, so point them at usr/lib explicitly.
for h in "${HELPERS[@]}"; do
  rel="$(realpath --relative-to="$(dirname "$h")" "$APPDIR/usr/lib")"
  patchelf --set-rpath "\$ORIGIN/$rel" "$h"
done
[ -x "$APPDIR/usr/bin/sloth-clash-desktop" ] || { echo "::error::linuxdeploy did not place usr/bin/sloth-clash-desktop"; exit 1; }

# --- 3b. Integrity + a pristine WebKit ---------------------------------------
# In plugin mode linuxdeploy "copies" libraries it reaches through
# usr/bin/../lib onto themselves; make sure nothing came out empty, and take
# the two WebKit libraries from the host again so the strings patched below
# are exactly what the host build contains, whatever happened to the copies.
bad=0
while IFS= read -r -d '' so; do
  if ! perl -e 'read(STDIN, $m, 4); exit($m eq "\x7fELF" ? 0 : 1)' < "$so"; then
    echo "::error::corrupt or empty library ${so#"$APPDIR"/} ($(stat -c %s "$so") bytes)"; bad=1
  fi
done < <(find "$APPDIR/usr/lib" -maxdepth 1 -type f -name '*.so*' -print0)
[ "$bad" -eq 0 ] || exit 1
echo "usr/lib holds $(find "$APPDIR/usr/lib" -maxdepth 1 -type f -name '*.so*' | wc -l) libraries, all ELF"
for name in libwebkit2gtk-4.1.so.0 libjavascriptcoregtk-4.1.so.0; do
  [ -f "$APPDIR/usr/lib/$name" ] || { echo "::error::$name was not bundled"; exit 1; }
  cp -f "/usr/lib/$LIBDIR/$name" "$APPDIR/usr/lib/$name"
  patchelf --set-rpath '$ORIGIN' "$APPDIR/usr/lib/$name"
done
[ -f "$APPDIR/AppRun" ] && [ ! -L "$APPDIR/AppRun" ] || { echo "::error::linuxdeploy replaced AppRun"; exit 1; }
[ -d "$APPDIR/apprun-hooks" ] || { echo "::error::linuxdeploy-plugin-gtk installed no apprun hook"; exit 1; }
# Top-level desktop file, icon and .DirIcon are what appimagetool reads.
ln -sf usr/share/applications/sloth-clash.desktop "$APPDIR/sloth-clash.desktop"
ln -sf usr/share/icons/hicolor/512x512/apps/sloth-clash.png "$APPDIR/sloth-clash.png"
ln -sf sloth-clash.png "$APPDIR/.DirIcon"

# --- 4. Patch the compiled-in paths (same length, fail-closed) ---------------
# "/usr/<rest>" becomes "././/<rest>": four bytes for four bytes, and with
# cwd=usr/ (AppRun) it resolves to $APPDIR/usr/<rest>, where step 2 put the files.
patched=0
for lib in "$APPDIR"/usr/lib/libwebkit2gtk-4.1.so*; do
  [ -f "$lib" ] && [ ! -L "$lib" ] || continue
  before="$(webkit_paths "$lib" | wc -l)"
  [ "$before" -gt 0 ] || { echo "::error::$lib contains no /usr/...webkit2gtk-4.1 path — refusing to ship"; exit 1; }
  size_before="$(stat -c %s "$lib")"
  perl -pi -e 's{/usr/([\x21-\x7e]*?webkit2gtk-4\.1(?:/[\x21-\x7e]*)?)\0}{././/$1\0}g' "$lib"
  after="$(webkit_paths "$lib" | wc -l)"
  [ "$after" -eq 0 ] || { echo "::error::$lib still contains absolute WebKit paths after patching"; exit 1; }
  [ "$(stat -c %s "$lib")" -eq "$size_before" ] || { echo "::error::$lib changed size while patching"; exit 1; }
  echo "Patched $before WebKit path string(s) in ${lib#"$APPDIR"/}:"
  perl -0777 -ne 'while (/(\.\/\.\/\/[\x21-\x7e]*?webkit2gtk-4\.1(?:\/[\x21-\x7e]*)?)\0/g) { print "  $1\n" }' "$lib" | sort -u
  patched=$((patched + 1))
done
[ "$patched" -gt 0 ] || { echo "::error::libwebkit2gtk-4.1.so was not bundled"; exit 1; }
# Every patched path must now exist inside the AppDir.
while IFS= read -r rel; do
  target="$APPDIR/usr/${rel#././/}"
  [ -e "$target" ] || { echo "::error::patched path $rel has no file at $target"; exit 1; }
done < <(perl -0777 -ne 'while (/(\.\/\.\/\/[\x21-\x7e]*?webkit2gtk-4\.1(?:\/[\x21-\x7e]*)?)\0/g) { print "$1\n" }' "$APPDIR"/usr/lib/libwebkit2gtk-4.1.so.0 | sort -u)

# --- 5. Pack --------------------------------------------------------------
curl -fsSL --retry 6 --retry-all-errors --retry-delay 10 -o "$TOOLS/appimagetool" \
  "https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-$ARCH.AppImage"
chmod +x "$TOOLS/appimagetool"
# Catalog nomenclature: "<RepoName>-<arch>.AppImage", no "linux" (every
# AppImage is for Linux), no version (the download URL stays stable).
OUT="$OUT_DIR/SlothClash-$ARCH.AppImage"
# Update information: AppImageUpdate (and the catalog) can then fetch deltas of
# the newest release via the .zsync file that is published next to the AppImage.
UPDATE_INFO="gh-releases-zsync|Nemu-x|SlothClash|latest|SlothClash-$ARCH.AppImage.zsync"
# VERSION is embedded as X-AppImage-Version in the desktop file by appimagetool.
( cd "$OUT_DIR" && ARCH="$ARCH" VERSION="$VERSION" "$TOOLS/appimagetool" --no-appstream -u "$UPDATE_INFO" "$APPDIR" "$(basename "$OUT")" )
chmod +x "$OUT"
[ -s "$OUT.zsync" ] || { echo "::error::appimagetool did not write $OUT.zsync"; exit 1; }
echo "Built $OUT ($(du -h "$OUT" | cut -f1))"
echo "appdir=$APPDIR" >> "${GITHUB_OUTPUT:-/dev/null}"
