#!/bin/sh
# Start a throwaway RomM from the overlay image, run the browser checks against
# it, and tear it down whatever happened.
#
#   checks/run.sh <image:tag>
#
# SCREENSHOTS=<dir> also saves screenshots of the pages it visits there.
#
# The checks run in the official Playwright image, on the stack's network, so
# the host needs nothing but Docker.
set -eu
image="$1"
here="$(cd "$(dirname "$0")" && pwd)"
export OVERLAY_IMAGE="$image" CHECK_PORT="${CHECK_PORT:-18081}"
project="romm-overlay-check-$$"
# RomM rewrites config.yml when a setting is saved: give it a throwaway copy.
if [ -z "${CHECK_CONFIG:-}" ]; then
  CHECK_CONFIG="$(mktemp)"
  cp "$here/config.yml" "$CHECK_CONFIG"
  chmod 666 "$CHECK_CONFIG"
  own_config=1
fi
export CHECK_CONFIG

cleanup() {
  docker compose -p "$project" -f "$here/compose.yml" down -v >/dev/null 2>&1 || true
  [ -z "${own_config:-}" ] || rm -f "$CHECK_CONFIG"
}
trap cleanup EXIT INT TERM

docker compose -p "$project" -f "$here/compose.yml" up -d --quiet-pull
for _ in $(seq 1 60); do
  docker compose -p "$project" -f "$here/compose.yml" exec -T romm \
    wget -q -O /dev/null http://127.0.0.1:8080/api/heartbeat 2>/dev/null && break
  sleep 3
done

playwright="mcr.microsoft.com/playwright:v1.63.0-noble"
shots=""
[ -z "${SCREENSHOTS:-}" ] || { mkdir -p "$SCREENSHOTS"; shots="-v $SCREENSHOTS:/shots -e SCREENSHOTS=/shots"; }
# shellcheck disable=SC2086 # $shots is two options or nothing
docker run --rm --network "${project}_default" -v "$here:/checks:ro" $shots -w /tmp \
  -e ROMM_URL=http://romm:8080 "$playwright" \
  sh -c 'npm i -s --no-audit --no-fund playwright-core@1.63.0 >/dev/null && cp /checks/*.mjs . && node nav-check.mjs'
