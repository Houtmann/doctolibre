#!/bin/sh
# Télécharge l'exécutable autonome Tailwind CSS dans bin/ et vérifie sa somme sha256 épinglée.
# Usage : tools/fetch-tailwind.sh   (idempotent ; TAILWIND_SHA256_FILE surchargeable pour les tests)
set -eu
cd "$(dirname "$0")/.."
VERSION=4.3.3
SUMS="${TAILWIND_SHA256_FILE:-tools/tailwind.sha256}"
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) ASSET=tailwindcss-macos-arm64 ;;
  Darwin-x86_64) ASSET=tailwindcss-macos-x64 ;;
  Linux-x86_64) ASSET=tailwindcss-linux-x64 ;;
  Linux-aarch64|Linux-arm64) ASSET=tailwindcss-linux-arm64 ;;
  *) echo "plateforme non prise en charge : $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac
WANT="$(grep -v '^#' "$SUMS" | awk -v a="$ASSET" '$2 == a { print $1 }')"
[ -n "$WANT" ] || { echo "aucune somme sha256 pour $ASSET dans $SUMS" >&2; exit 1; }
sum() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi
}
DEST=bin/tailwindcss
if [ -x "$DEST" ] && [ "$(sum "$DEST")" = "$WANT" ]; then
  echo "tailwindcss $VERSION déjà présent"
  exit 0
fi
mkdir -p bin
TMP="$(mktemp bin/tailwindcss.XXXXXX)"
trap 'rm -f "$TMP"' EXIT
curl -fsSL -o "$TMP" "https://github.com/tailwindlabs/tailwindcss/releases/download/v$VERSION/$ASSET"
GOT="$(sum "$TMP")"
if [ "$GOT" != "$WANT" ]; then
  echo "somme sha256 invalide pour $ASSET : attendu $WANT, obtenu $GOT" >&2
  exit 1
fi
chmod +x "$TMP"
mv "$TMP" "$DEST"
trap - EXIT
echo "tailwindcss $VERSION installé dans $DEST"
