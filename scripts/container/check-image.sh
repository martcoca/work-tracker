#!/usr/bin/env bash
set -euo pipefail

IMAGE="${1:-work-tracker:check}"

FROM_COUNT="$(grep -Ec '^FROM .+@sha256:[0-9a-f]{64}( AS [a-z]+)?$' Dockerfile)"
[ "$FROM_COUNT" -eq 3 ] || {
  echo "error: every Dockerfile stage must use a sha256-pinned base (found $FROM_COUNT of 3)" >&2
  exit 1
}

USER_VALUE="$(docker image inspect --format '{{.Config.User}}' "$IMAGE")"
case "$USER_VALUE" in
  ''|0|0:0|root|root:root)
    echo "error: final image runs as root ($USER_VALUE)" >&2
    exit 1
    ;;
esac

docker run --rm --entrypoint sh "$IMAGE" -c '
  test ! -e /usr/local/go
  test ! -e /src
  test ! -e /go/pkg/mod
  test ! -e /root/.cache/go-build
'

go run ./internal/imagescan "$IMAGE"
go run ./internal/imagescan --self-test "$IMAGE"

SIZE="$(docker image inspect --format '{{.Size}}' "$IMAGE")"
echo "PASS: image structure (user=$USER_VALUE size_bytes=$SIZE pinned_stages=$FROM_COUNT)"
