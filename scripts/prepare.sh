#!/bin/sh
# Prepare a patched RomM source tree.
#
#   scripts/prepare.sh <romm-tag> <dest-dir>
#
# Clones upstream at the tag, applies patches/*.patch in order (3-way, so a
# patch that only drifted in context still lands), and copies the extensions
# into frontend/src/ext/ and their locale files into frontend/src/locales/.
# Exits non-zero, naming the patch, as soon as one does not apply.
set -eu

tag="$1"
dest="$2"
here="$(cd "$(dirname "$0")/.." && pwd)"

rm -rf "$dest"
git clone --quiet --depth 1 --branch "$tag" https://github.com/rommapp/romm.git "$dest"

cd "$dest"
git -c user.name=overlay -c user.email=overlay@localhost commit --quiet --allow-empty -m base
for patch in "$here"/patches/*.patch; do
  [ -e "$patch" ] || continue
  if ! git apply --3way --whitespace=nowarn "$patch"; then
    echo "PATCH FAILED: $(basename "$patch") does not apply to RomM $tag" >&2
    exit 3
  fi
  echo "applied $(basename "$patch")"
done

mkdir -p frontend/src/ext
for dir in "$here"/ext/*/; do
  name="$(basename "$dir")"
  mkdir -p "frontend/src/ext/$name"
  # Locales go where upstream's loader finds them; everything else stays put.
  (cd "$dir" && tar cf - --exclude=./locales .) | (cd "frontend/src/ext/$name" && tar xf -)
  if [ -d "$dir/locales" ]; then
    for localeDir in "$dir"/locales/*/; do
      locale="$(basename "$localeDir")"
      mkdir -p "frontend/src/locales/$locale"
      cp "$localeDir"/*.json "frontend/src/locales/$locale/"
    done
  fi
done
echo "prepared RomM $tag with $(ls "$here"/patches/*.patch 2>/dev/null | wc -l) patch(es)"
