#!/bin/sh
set -e

REPO="fastclaw-ai/weclaw"
BINARY="weclaw"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  darwin|linux) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "Detected: ${OS}/${ARCH}"

# Get latest version
echo "Fetching latest release..."
VERSION=$(curl -fsSI "https://github.com/${REPO}/releases/latest" | grep -i '^location:' | sed 's|.*/tag/||' | tr -d '\r')

if [ -z "$VERSION" ]; then
  echo "Error: could not determine latest version. Is there a release on GitHub?"
  exit 1
fi

echo "Latest version: ${VERSION}"

# Download
FILENAME="${BINARY}_${OS}_${ARCH}"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

echo "Downloading ${URL}..."
TMP=$(mktemp)
curl -fsSL -o "$TMP" "$URL"

# Install
chmod +x "$TMP"
if [ -d "$INSTALL_DIR" ] && [ -w "$INSTALL_DIR" ]; then
  mv "$TMP" "${INSTALL_DIR}/${BINARY}"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo mkdir -p "$INSTALL_DIR"
  sudo mv "$TMP" "${INSTALL_DIR}/${BINARY}"
fi

# Clear macOS quarantine attributes
if [ "$OS" = "darwin" ]; then
  xattr -d com.apple.quarantine "${INSTALL_DIR}/${BINARY}" 2>/dev/null || true
  xattr -d com.apple.provenance "${INSTALL_DIR}/${BINARY}" 2>/dev/null || true
fi

echo ""
echo "weclaw ${VERSION} installed to ${INSTALL_DIR}/${BINARY}"
echo ""
echo "Get started:"
echo "  weclaw start"
