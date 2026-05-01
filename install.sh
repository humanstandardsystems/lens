#!/usr/bin/env bash
set -euo pipefail

SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="/usr/local/bin"
BINARY="lens"

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

if [ "$OS" != "darwin" ]; then
  echo "lens currently supports macOS only. Linux support coming soon."
  exit 1
fi

if [ "$ARCH" = "arm64" ]; then
  SRC="$SOURCE_DIR/bin/lens-darwin-arm64"
elif [ "$ARCH" = "x86_64" ]; then
  SRC="$SOURCE_DIR/bin/lens-darwin-amd64"
else
  echo "Unsupported architecture: $ARCH"
  exit 1
fi

if [ ! -f "$SRC" ]; then
  echo "Binary not found at $SRC"
  echo "Try: git pull && bash install.sh"
  exit 1
fi

echo "Installing lens..."

if [ -w "$INSTALL_DIR" ]; then
  cp "$SRC" "$INSTALL_DIR/$BINARY"
  chmod +x "$INSTALL_DIR/$BINARY"
else
  echo "Installing to $INSTALL_DIR (sudo required)..."
  sudo cp "$SRC" "$INSTALL_DIR/$BINARY"
  sudo chmod +x "$INSTALL_DIR/$BINARY"
fi

echo ""
echo "lens installed. Run 'lens init' to set up tracking."
echo "Then restart Claude Code to activate the hook."
