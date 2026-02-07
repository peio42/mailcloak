#!/bin/sh
set -eu

TARGET="$1"
TIMEOUT="${2:-60}"

start=$(date +%s)

is_ready() {
  case "$TARGET" in
    http://*|https://*)
      curl -fsS "$TARGET" >/dev/null 2>&1
      ;;
    tcp://*)
      hostport=$(echo "$TARGET" | sed 's|tcp://||')
      host=$(echo "$hostport" | cut -d: -f1)
      port=$(echo "$hostport" | cut -d: -f2)
      nc -z "$host" "$port" >/dev/null 2>&1
      ;;
    *)
      [ -S "$TARGET" ]
      ;;
  esac
}

while ! is_ready; do
  now=$(date +%s)
  if [ $((now - start)) -ge "$TIMEOUT" ]; then
    echo "timeout waiting for $TARGET" >&2
    exit 1
  fi
  sleep 1
done

exit 0
