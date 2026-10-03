#!/bin/sh
# Build the overlay image for a RomM release.
#
#   scripts/build.sh [romm-tag] [image-name]
#
# The tag defaults to the ROMM_VERSION in the Dockerfile (what Renovate bumps).
# The Node version is read from upstream's Dockerfile at that tag.
set -eu
here="$(cd "$(dirname "$0")/.." && pwd)"
tag="${1:-$(sed -n 's/^ARG ROMM_VERSION=//p' "$here/Dockerfile")}"
image="${2:-romm-overlay}"

node="$(curl -fsSL "https://raw.githubusercontent.com/rommapp/romm/${tag}/docker/Dockerfile" \
  | sed -n 's/^ARG NODE_VERSION=//p')"
[ -n "$node" ] || { echo "cannot read NODE_VERSION for RomM $tag" >&2; exit 2; }

echo "building $image:$tag (node $node)"
docker build \
  --build-arg "ROMM_VERSION=$tag" \
  --build-arg "NODE_VERSION=$node" \
  -t "$image:$tag" "$here"
