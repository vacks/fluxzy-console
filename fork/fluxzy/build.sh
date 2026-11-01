#!/bin/sh
set -eu

BASE="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
. "$BASE/upstream.env"
VERSION="$FLUXZY_VERSION"
COMMIT="$FLUXZY_COMMIT"
UPSTREAM="https://github.com/haga-rak/fluxzy.core.git"
SDK_IMAGE="${DOTNET_SDK_IMAGE:-mcr.microsoft.com/dotnet/sdk:10.0}"
NUGET_VOLUME="${FLUXZY_NUGET_VOLUME:-fluxzy-nuget-cache}"

ARCH="${1:-arm64}"
case "$ARCH" in
  arm64) RID="linux-arm64" ; SELF_CONTAINED="true" ; SINGLE_FILE="true" ;;
  # Production uses the official Alpine runtime image; musl apphost is required.
  amd64) RID="linux-musl-x64" ; SELF_CONTAINED="true" ; SINGLE_FILE="true" ;;
  *) echo "usage: $0 [arm64|amd64]" >&2; exit 2 ;;
esac

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
WORK="$ROOT/fork/fluxzy/.work"
OUT="$ROOT/fork/fluxzy/out/linux-$ARCH"
PATCHES="$ROOT/fork/fluxzy/patches"

mkdir -p "$ROOT/fork/fluxzy/.work" "$OUT"

if [ ! -d "$WORK/.git" ]; then
  echo "==> cloning fluxzy.core $VERSION"
  git clone --branch "v$VERSION" "$UPSTREAM" "$WORK"
fi

cd "$WORK"
git fetch --tags origin
git checkout --force "$COMMIT"
git reset --hard "$COMMIT"
git clean -fdx -e .nuget >/dev/null

for patch in "$PATCHES"/*.patch; do
  echo "==> applying $(basename "$patch")"
  git apply --check "$patch"
  git apply "$patch"
done

echo "==> publishing $RID"
docker run --rm \
  -v "$WORK":/src \
  -v "$OUT":/out \
  -v "$NUGET_VOLUME":/root/.nuget/packages \
  -w /src \
  "$SDK_IMAGE" \
  dotnet publish src/Fluxzy/Fluxzy.csproj \
    -c Release -r "$RID" --self-contained "$SELF_CONTAINED" \
    -p:PublishSingleFile="$SINGLE_FILE" -p:DebugType=None \
    -o /out

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$OUT" && sha256sum fluxzy > fluxzy.sha256)
else
  (cd "$OUT" && shasum -a 256 fluxzy > fluxzy.sha256)
fi
cat > "$OUT/fluxzy-fork.json" <<JSON
{
  "upstream": "$UPSTREAM",
  "version": "$VERSION",
  "commit": "$COMMIT",
  "runtime": "$RID",
  "patches": [
    "$(basename "$(ls "$PATCHES"/*.patch | sed -n '1p')")",
    "$(basename "$(ls "$PATCHES"/*.patch | sed -n '2p')")"
  ]
}
JSON
echo "==> built $OUT/fluxzy"
