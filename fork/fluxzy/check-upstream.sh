#!/bin/sh
set -eu

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "usage: $0 <official-version> [--full]" >&2
  echo "example: $0 1.39.52.12345" >&2
  exit 2
fi
FULL=0
[ "${2:-}" = "--full" ] && FULL=1

UPSTREAM="https://github.com/haga-rak/fluxzy.core.git"
SDK_IMAGE="${DOTNET_SDK_IMAGE:-mcr.microsoft.com/dotnet/sdk:10.0}"
NUGET_VOLUME="${FLUXZY_NUGET_VOLUME:-fluxzy-nuget-cache}"
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)"
BASE="$ROOT/fork/fluxzy"
CACHE="$BASE/.upstream"
CHECK="$BASE/.check-$VERSION"
OUT="$BASE/out/check-$VERSION"
PATCHES="$BASE/patches"

HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in
  arm64|aarch64) CHECK_RID="linux-arm64" ;;
  x86_64|amd64) CHECK_RID="linux-x64" ;;
  *) echo "unsupported host architecture: $HOST_ARCH" >&2; exit 2 ;;
esac

rm -rf "$CHECK"
mkdir -p "$OUT"

if [ ! -d "$CACHE/.git" ]; then
  echo "==> cloning upstream"
  git clone "$UPSTREAM" "$CACHE"
fi
cd "$CACHE"
git fetch --tags origin
TAG="v$VERSION"
if ! git rev-parse --verify "$TAG^{commit}" >/dev/null 2>&1; then
  echo "FAIL: upstream tag $TAG not found" >&2
  exit 1
fi
COMMIT="$(git rev-parse "$TAG^{commit}")"
git clone --local --no-checkout "$CACHE" "$CHECK"
cd "$CHECK"
git checkout --force "$COMMIT"
PATCH_OK=true
for patch in "$PATCHES"/*.patch; do
  echo "==> checking $(basename "$patch")"
  if ! git apply --check "$patch"; then
    echo "FAIL: patch does not apply: $patch" >&2
    PATCH_OK=false
    break
  fi
  git apply "$patch"
done

if [ "$PATCH_OK" != true ]; then
  cat > "$OUT/report.json" <<JSON
{"version":"$VERSION","commit":"$COMMIT","patchApplied":false,"build":false,"decision":"reject"}
JSON
  exit 1
fi

echo "==> building $CHECK_RID"
docker run --rm \
  -v "$CHECK":/src \
  -v "$OUT":/out \
  -v "$NUGET_VOLUME":/root/.nuget/packages \
  -w /src \
  "$SDK_IMAGE" \
  dotnet publish src/Fluxzy/Fluxzy.csproj \
    -c Release -r "$CHECK_RID" --self-contained true \
    -p:PublishSingleFile=true -p:DebugType=None \
    -o /out

VERSION_OUTPUT="$(docker run --rm -v "$OUT":/out --entrypoint /out/fluxzy "$SDK_IMAGE" --version)"
echo "==> built fluxzy $VERSION_OUTPUT"

echo "==> running fork e2e"
docker run --rm \
  -v "$OUT":/out \
  -v "$BASE":/fork \
  --entrypoint sh \
  "$SDK_IMAGE" /fork/e2e.sh /out/fluxzy

TEST_RESULT="skipped"
TEST_FAILED=false
if [ "$FULL" = 1 ]; then
  echo "==> running upstream rule tests"
  if docker run --rm \
    -v "$CHECK":/src \
    -v "$NUGET_VOLUME":/root/.nuget/packages \
    -w /src \
    "$SDK_IMAGE" \
    dotnet test test/Fluxzy.Tests/Fluxzy.Tests.csproj -c Release \
      --filter "FullyQualifiedName~Rules"; then
    TEST_RESULT="passed"
  else
    TEST_RESULT="failed"
    TEST_FAILED=true
  fi
fi

cat > "$OUT/report.json" <<JSON
{
  "version": "$VERSION",
  "commit": "$COMMIT",
  "runtime": "$CHECK_RID",
  "patchApplied": true,
  "build": true,
  "fluxzyVersion": "$VERSION_OUTPUT",
  "rulesTests": "$TEST_RESULT",
  "decision": "manual-review-required"
}
JSON
echo "==> compatibility report: $OUT/report.json"
if [ "$TEST_FAILED" = true ]; then
  echo "FAIL: upstream rule tests failed; review $OUT/report.json" >&2
  exit 1
fi
