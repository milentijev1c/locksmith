#!/bin/bash
set -e

BINARY_NAME="locksmith"
INSTALL_DIR="/usr/local/bin"
REPO="milentijev1c/locksmith"

echo "🔐 Locksmith Installer (macOS)"
echo "==============================="
echo ""

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  ASSET="locksmith-darwin-amd64" ;;
    arm64)   ASSET="locksmith-darwin-arm64" ;;
    *) echo "❌ Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Download release
echo "📥 Downloading ${ASSET}.tar.gz..."
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

VERSION="${VERSION:-v1.0.0}"
curl -sL "https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}.tar.gz" -o "${TMPDIR}/${ASSET}.tar.gz"

# Extract
echo "📦 Extracting..."
tar xzf "${TMPDIR}/${ASSET}.tar.gz" -C "$TMPDIR"

# Install binary
echo "📁 Installing to ${INSTALL_DIR}/${BINARY_NAME}..."
sudo install -m 755 "${TMPDIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"

# Remove quarantine attribute (for unsigned binaries)
if command -v xattr &>/dev/null; then
    sudo xattr -d com.apple.quarantine "${INSTALL_DIR}/${BINARY_NAME}" 2>/dev/null || true
fi

echo ""
echo "✅ Locksmith installed successfully!"
echo ""
echo "Usage:"
echo "  1. Connect a smart card reader and insert your Serbian ID card"
echo "  2. Open http://127.0.0.1:19711/ in your browser"
echo "  3. Upload a PDF, enter your PIN, and sign"
echo ""
echo "Run: ${BINARY_NAME}"