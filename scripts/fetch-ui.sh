#!/bin/sh
# Fetch and verify the pinned web panel release for builds using -tags ui.
set -eu

VERSION="v1.273.1"
SHA256="a178e00b67acabcda2dcef00afa90be6a7bb261e466a67dad58c8478d9553603"
URL="https://github.com/MetaCubeX/metacubexd/releases/download/${VERSION}/compressed-dist.tgz"

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DEST="$ROOT/internal/ui/dist"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

curl -fL --retry 3 --connect-timeout 15 -o "$TMP/compressed-dist.tgz" "$URL"

if command -v sha256sum >/dev/null 2>&1; then
  printf '%s  %s\n' "$SHA256" "$TMP/compressed-dist.tgz" | sha256sum -c -
elif command -v shasum >/dev/null 2>&1; then
  printf '%s  %s\n' "$SHA256" "$TMP/compressed-dist.tgz" | shasum -a 256 -c -
else
  echo "fetch-ui.sh: sha256sum or shasum is required" >&2
  exit 1
fi

rm -rf "$DEST"
mkdir -p "$DEST"
tar -xzf "$TMP/compressed-dist.tgz" -C "$DEST"

if [ ! -f "$DEST/index.html" ]; then
  echo "fetch-ui.sh: extracted panel is missing index.html" >&2
  exit 1
fi

echo "fetched metacubexd ${VERSION} into ${DEST}"
