#!/bin/sh
set -eu

BIN="${1:-/out/fluxzy}"
WORK="${2:-/tmp/fluxzy-fork-e2e}"
rm -rf "$WORK"
mkdir -p "$WORK/up/data" "$WORK/up/home" "$WORK/proxy/data" "$WORK/proxy/home"

cat > "$WORK/up/rules.yaml" <<'YAML'
rules:
  - name: upstream mock
    filter:
      typeKind: AnyFilter
    actions:
      - typeKind: MockedResponseAction
        response:
          statusCode: 200
          headers:
            - name: content-type
              value: text/plain
          body:
            origin: FromString
            type: Text
            text: hello
YAML

cat > "$WORK/proxy/rules.yaml" <<'YAML'
rules:
  - name: transform response
    filter:
      typeKind: PathFilter
      pattern: /probe
      operation: StartsWith
    actions:
      - typeKind: ConsoleTransformResponseBodyAction
        operation: Set
        content: patched
        contentType: text/plain
YAML

HOME="$WORK/up/home" "$BIN" start -l 127.0.0.1:19080 --mode ReversePlain --mode-reverse-port 9 \
  -d "$WORK/up/data" -r "$WORK/up/rules.yaml" > "$WORK/up.log" 2>&1 &
UP_PID=$!
HOME="$WORK/proxy/home" FLUXZY_CONSOLE_RULE_HOT_RELOAD=1 \
  FLUXZY_CONSOLE_RULE_STATUS_FILE="$WORK/proxy/status.json" \
  "$BIN" start -l 127.0.0.1:19081 --mode ReversePlain --mode-reverse-port 19080 \
  -d "$WORK/proxy/data" -r "$WORK/proxy/rules.yaml" > "$WORK/proxy.log" 2>&1 &
PROXY_PID=$!
trap 'kill "$PROXY_PID" "$UP_PID" 2>/dev/null || true; wait 2>/dev/null || true' EXIT INT TERM

for i in $(seq 1 80); do
  if wget -qO- --timeout=1 http://127.0.0.1:19081/probe > "$WORK/out1" 2>/dev/null; then
    break
  fi
  sleep 0.25
done

[ "$(cat "$WORK/out1")" = "patched" ] || {
  echo "FAIL: initial body transform output: $(cat "$WORK/out1" 2>/dev/null)" >&2
  cat "$WORK/proxy.log" >&2
  exit 1
}

old_hash="$(sed -n 's/.*"hash":"\([^"]*\)".*/\1/p' "$WORK/proxy/status.json" 2>/dev/null || true)"

sed 's/content: patched/content: hot2/' "$WORK/proxy/rules.yaml" > "$WORK/proxy/rules.new"
mv "$WORK/proxy/rules.new" "$WORK/proxy/rules.yaml"

new_hash=""
for i in $(seq 1 80); do
  new_hash="$(sed -n 's/.*"hash":"\([^"]*\)".*/\1/p' "$WORK/proxy/status.json" 2>/dev/null || true)"
  if [ -n "$new_hash" ] && [ "$new_hash" != "$old_hash" ]; then
    break
  fi
  sleep 0.1
done

[ -n "$new_hash" ] && [ "$new_hash" != "$old_hash" ] || {
  echo "FAIL: hot reload did not report a new hash" >&2
  cat "$WORK/proxy/status.json" >&2
  exit 1
}

for i in $(seq 1 40); do
  if wget -qO- --timeout=1 http://127.0.0.1:19081/probe > "$WORK/out2" 2>/dev/null; then
    break
  fi
  sleep 0.25
done

[ "$(cat "$WORK/out2")" = "hot2" ] || {
  echo "FAIL: hot-reloaded body transform output: $(cat "$WORK/out2" 2>/dev/null)" >&2
  cat "$WORK/proxy.log" >&2
  exit 1
}

kill -0 "$PROXY_PID" 2>/dev/null || {
  echo "FAIL: proxy restarted during hot reload" >&2
  exit 1
}

echo "E2E PASS: body transform + hot reload without restart"
