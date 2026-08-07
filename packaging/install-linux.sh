#!/bin/bash
set -e

BINARY_NAME="locksmith"
INSTALL_DIR="/usr/local/bin"
SERVICE_DIR="/etc/systemd/system"
REPO="milentijev1c/locksmith"

echo "🔐 Locksmith Installer"
echo "======================"
echo ""

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)  ASSET="locksmith-linux-amd64" ;;
    aarch64|arm64) ASSET="locksmith-linux-arm64" ;;
    *) echo "❌ Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Check for pcscd
if ! command -v pcscd &>/dev/null && ! dpkg -l | grep -q libpcsclite1 2>/dev/null; then
    echo "⚠️  PC/SC dependencies not found. Installing..."
    if command -v apt-get &>/dev/null; then
        sudo apt-get update && sudo apt-get install -y libpcsclite1
    elif command -v dnf &>/dev/null; then
        sudo dnf install -y pcsc-lite-libs
    elif command -v pacman &>/dev/null; then
        sudo pacman -S --noconfirm pcsclite
    else
        echo "❌ Could not detect package manager. Install libpcsclite1 manually."
        exit 1
    fi
fi

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

# Install systemd service
if [ -d "$SERVICE_DIR" ]; then
    echo "⚙️  Installing systemd service..."
    sudo tee "${SERVICE_DIR}/locksmith.service" > /dev/null << 'EOF'
[Unit]
Description=Locksmith - Serbian ID Card Middleware
After=pcscd.service
Requires=pcscd.service

[Service]
Type=simple
ExecStart=/usr/local/bin/locksmith
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
    sudo systemctl daemon-reload
    echo "✅ Service installed. Start with: sudo systemctl start locksmith"
    echo "   Enable on boot:    sudo systemctl enable locksmith"
else
    echo "⚠️  systemd not found. Run manually: ${INSTALL_DIR}/${BINARY_NAME}"
fi

# Ensure pcscd is running
if command -v systemctl &>/dev/null; then
    sudo systemctl start pcscd 2>/dev/null || true
fi

echo ""
echo "✅ Locksmith installed successfully!"
echo ""
echo "Usage:"
echo "  1. Insert your Serbian ID card into a card reader"
echo "  2. Open http://127.0.0.1:19711/ in your browser"
echo "  3. Upload a PDF, enter your PIN, and sign"
echo ""
echo "To start now: sudo systemctl start locksmith"
echo "  or run:      locksmith"