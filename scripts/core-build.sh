#!/usr/bin/env bash
# Builds the patched mihomo core described by core/mihomo/manifest.json:
# upstream tag (commit-verified) + core/patches/mihomo/*.patch, cross-compiled
# for every desktop target with the release flags upstream uses.
#
# usage: scripts/core-build.sh <out-dir> [goos-goarch ...]   (default: all manifest assets)
# env:   MIHOMO_UPSTREAM  clone source override (e.g. a local mirror); the commit check still applies
#        CORE_SKIP_TESTS=1  skip the patched packages' go tests (local iteration only)
#
# Output: one raw binary per target, SHA256SUMS, BUILDINFO.txt. Exits non-zero —
# naming the patch — when a patch no longer applies, so a core bump can never
# ship an unpatched core.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MANIFEST="$ROOT/core/mihomo/manifest.json"
PATCH_DIR="$ROOT/core/patches/mihomo"
[ $# -ge 1 ] || { echo "usage: $0 <out-dir> [goos-goarch ...]" >&2; exit 2; }
mkdir -p "$1"; OUT="$(cd "$1" && pwd)"; shift
# Never leave binaries of an earlier run next to a failed one.
rm -f "$OUT"/mihomo-* "$OUT/SHA256SUMS" "$OUT/BUILDINFO.txt"

# m <js-expr>: evaluate an expression over the manifest object `m`; arrays print one item per line.
m() { node -e "const m=require(process.argv[1]); const v=($1); process.stdout.write(Array.isArray(v)?v.join(String.fromCharCode(10)):String(v))" "$MANIFEST"; }
UPSTREAM="${MIHOMO_UPSTREAM:-$(m 'm.upstream')}"
TAG="$(m 'm.tag')"
COMMIT="$(m 'm.commit')"
RELEASE="core-$TAG-sloth.$(m 'm.series')"
TARGETS=("$@")
[ ${#TARGETS[@]} -gt 0 ] || mapfile -t TARGETS < <(m 'Object.keys(m.assets)')

fail() { echo "::error::$*" >&2; echo "ERROR: $*" >&2; exit 1; }
sha() { if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi; }

# The patch directory and the manifest must list exactly the same series, and
# every patch must hash to what the manifest records (the shipped binaries are
# built from these bytes).
mapfile -t LISTED < <(m 'm.patches.map(p=>p.file)')
mapfile -t ONDISK < <(cd "$PATCH_DIR" && ls -1 *.patch | sort)
[ "$(printf '%s\n' "${LISTED[@]}")" = "$(printf '%s\n' "${ONDISK[@]}")" ] ||
  fail "core/patches/mihomo does not match the manifest patch list (manifest: ${LISTED[*]}; on disk: ${ONDISK[*]})"
for i in "${!LISTED[@]}"; do
  want="$(m "m.patches[$i].sha256")"; got="$(sha "$PATCH_DIR/${LISTED[$i]}")"
  [ "$want" = "$got" ] || fail "patch ${LISTED[$i]}: sha256 $got does not match manifest $want (update core/mihomo/manifest.json)"
done

WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
SRC="$WORK/mihomo"
echo "==> clone $UPSTREAM @ $TAG"
git -c advice.detachedHead=false clone --quiet --depth 1 --branch "$TAG" "$UPSTREAM" "$SRC"
HEAD_COMMIT="$(git -C "$SRC" rev-parse HEAD)"
[ "$HEAD_COMMIT" = "$COMMIT" ] || fail "tag $TAG resolves to $HEAD_COMMIT, manifest pins $COMMIT (tag moved?)"

for p in "${LISTED[@]}"; do
  if ! git -C "$SRC" apply --check "$PATCH_DIR/$p" 2>"$WORK/apply.err"; then
    cat "$WORK/apply.err" >&2
    fail "patch $p does not apply to mihomo $TAG — rebase it before bumping the core"
  fi
  git -C "$SRC" apply "$PATCH_DIR/$p"
  echo "==> applied $p"
done

cd "$SRC"
export CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-mod=readonly
if [ "${CORE_SKIP_TESTS:-0}" != "1" ]; then
  echo "==> go vet + tests of the patched packages"
  go vet ./component/tls/ ./config/ ./hub/executor/
  go test -count=1 ./component/tls/ ./config/
fi

# Same flags as upstream release builds, except BuildTime: the tag's commit time
# instead of `date`, so a rebuild with the same Go version is byte-identical.
BUILDTIME="$(TZ=UTC git log -1 --format=%cd --date=format-local:'%a %b %d %H:%M:%S UTC %Y')"
LDFLAGS="-extldflags --static -X 'github.com/metacubex/mihomo/constant.Version=$TAG' -X 'github.com/metacubex/mihomo/constant.BuildTime=$BUILDTIME' -w -s -buildid="

: > "$OUT/SHA256SUMS"
for t in "${TARGETS[@]}"; do
  file="$(m "(m.assets['$t']||{}).file||''")"
  [ -n "$file" ] || fail "target $t is not in the manifest assets"
  goos="${t%-*}"; goarch="${t#*-}"
  goamd64=""; [ "$goarch" = amd64 ] && goamd64=v2   # matches the -amd64-v2 builds shipped before
  echo "==> build $t -> $file"
  GOOS="$goos" GOARCH="$goarch" GOAMD64="$goamd64" \
    go build -tags with_gvisor -trimpath -ldflags "$LDFLAGS" -o "$OUT/$file" .
  (cd "$OUT" && echo "$(sha "$file")  $file" >> SHA256SUMS)
done

{
  echo "release:   $RELEASE"
  echo "upstream:  $UPSTREAM"
  echo "tag:       $TAG"
  echo "commit:    $COMMIT"
  echo "go:        $(go version)"
  echo "flags:     CGO_ENABLED=0 GOAMD64=v2(amd64) -tags with_gvisor -trimpath"
  echo "ldflags:   $LDFLAGS"
  echo "patches:"
  for p in "${LISTED[@]}"; do echo "  $(sha "$PATCH_DIR/$p")  $p"; done
} > "$OUT/BUILDINFO.txt"
echo "==> done: $RELEASE"
cat "$OUT/SHA256SUMS"
