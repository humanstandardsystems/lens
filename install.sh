#!/usr/bin/env bash
set -euo pipefail

REPO="humanstandardsystems/lens"
VERSION="v1.0.0"
INSTALL_DIR="/usr/local/bin"
BINARY="lens"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

if [ "$OS" != "darwin" ]; then
  echo "lens currently supports macOS only. Linux support coming soon."
  exit 1
fi

if [ "$ARCH" = "arm64" ]; then
  ASSET="lens-darwin-arm64"
elif [ "$ARCH" = "x86_64" ]; then
  ASSET="lens-darwin-amd64"
else
  echo "Unsupported architecture: $ARCH"
  exit 1
fi

URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"

echo "Installing lens $VERSION..."

TMP=$(mktemp)
if ! curl -fsSL "$URL" -o "$TMP"; then
  echo "Download failed. Check your connection or visit:"
  echo "  https://github.com/$REPO/releases"
  rm -f "$TMP"
  exit 1
fi
chmod +x "$TMP"

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "$INSTALL_DIR/$BINARY"
else
  echo "Installing to $INSTALL_DIR (sudo required)..."
  sudo mv "$TMP" "$INSTALL_DIR/$BINARY"
fi

echo ""
echo "lens installed. Run 'lens init' to set up tracking."
echo "Then restart Claude Code to activate the hook."
