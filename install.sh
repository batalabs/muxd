#!/bin/sh
# muxd installer — curl -fsSL https://raw.githubusercontent.com/rui-batalabs/muxd/main/install.sh | sh
set -e

REPO="batalabs/muxd"
INSTALL_DIR="${MUXD_INSTALL_DIR:-/usr/local/bin}"

# Detect OS
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux)  OS="linux" ;;
  darwin) OS="darwin" ;;
  mingw*|msys*|cygwin*) OS="windows" ;;
  *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac

# Detect arch
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64)  ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

# Determine binary name
BIN="muxd-${OS}-${ARCH}"
if [ "$OS" = "windows" ]; then
  BIN="${BIN}.exe"
fi

# Get latest version if not specified
VERSION="${MUXD_VERSION:-latest}"
if [ "$VERSION" = "latest" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)"
  if [ -z "$VERSION" ]; then
    echo "Failed to fetch latest version" >&2
    exit 1
  fi
fi

URL="https://github.com/${REPO}/releases/download/${VERSION}/${BIN}"

echo "Installing muxd ${VERSION} (${OS}/${ARCH})..."
echo "  from: ${URL}"
echo "  to:   ${INSTALL_DIR}/muxd"

# Download
TMP="$(mktemp)"
curl -fsSL "$URL" -o "$TMP"
chmod +x "$TMP"

# Install (may need sudo)
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "${INSTALL_DIR}/muxd"
else
  echo "Need elevated permissions to install to ${INSTALL_DIR}"
  sudo mv "$TMP" "${INSTALL_DIR}/muxd"
fi

echo "muxd ${VERSION} installed successfully!"
echo "Run 'muxd' to get started."
