#!/usr/bin/env sh
# Installs the observa CLI from the GitHub releases.
#   curl -fsSL https://raw.githubusercontent.com/root-lr/observaboard/main/install.sh | sh
# Variables: OBSERVA_VERSION (default: latest release), OBSERVA_REPO, INSTALL_DIR.
set -eu

REPO=${OBSERVA_REPO:-root-lr/observaboard}
INSTALL_DIR=${INSTALL_DIR:-/usr/local/bin}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) echo "Système non pris en charge par ce script : $os (sous Windows, téléchargez observa-windows-amd64.exe depuis les releases)" >&2; exit 1 ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "Architecture non prise en charge : $arch" >&2; exit 1 ;;
esac

if [ -n "${OBSERVA_VERSION:-}" ]; then
  base="https://github.com/$REPO/releases/download/$OBSERVA_VERSION"
else
  base="https://github.com/$REPO/releases/latest/download"
fi
asset="observa-$os-$arch"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Téléchargement de $asset…"
curl -fsSL "$base/$asset" -o "$tmp/observa"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
expected=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/observa" | cut -d' ' -f1)
else
  actual=$(shasum -a 256 "$tmp/observa" | cut -d' ' -f1)
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "Somme de contrôle invalide pour $asset" >&2
  exit 1
fi
chmod +x "$tmp/observa"

if [ -w "$INSTALL_DIR" ]; then
  mv "$tmp/observa" "$INSTALL_DIR/observa"
else
  echo "Installation dans $INSTALL_DIR (sudo requis)…"
  sudo mv "$tmp/observa" "$INSTALL_DIR/observa"
fi
echo "observa installé : $("$INSTALL_DIR/observa" version | head -1)"
