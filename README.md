<div align="center">

# 🔐 Locksmith

**Serbian ID Card Middleware**

Lightweight local daemon that reads Serbian national ID cards and exposes a
simple HTTP + WebSocket API on `localhost`.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)]()

</div>

---

## Features

| Feature | Description |
|---------|-------------|
| 🪪 **Read card data** | Name, JMBG, address, document info, photo |
| ✍️ **PDF signing** | Sign PDFs with visible signature + CMS/PKCS#7 embedded |
| 📡 **Real-time events** | WebSocket notifications for card insert/remove |
| 🔒 **Local-only** | Binds to `127.0.0.1`, no external exposure |
| ⚙️ **Configurable** | YAML config with sane defaults |

---

## Prerequisites

| Dependency | Notes |
|---|---|
| **Go 1.26+** | [go.dev](https://go.dev/dl/) |
| **pcscd** | PC/SC daemon — `sudo systemctl start pcscd` |
| **libpcsclite-dev** | `sudo apt install libpcsclite-dev` |
| **srb-id-pkcs11** | [Download](https://github.com/ubavic/srb-id-pkcs11/releases) |
| **Smart card reader** | Any PC/SC compatible reader |
| **Serbian ID card** | The physical card with PIN |

---

## Quick Start

```bash
git clone https://github.com/milentijev1c/locksmith.git
cd locksmith

# Start the PC/SC daemon
sudo systemctl start pcscd

# Build and run
go mod download
go run main.go
```

The service starts at **http://127.0.0.1:19711**.

---

## Configuration

```bash
cp config.yaml.example config.yaml
```

| Key | Default | Description |
|---|---|---|
| `port` | `19711` | HTTP server port |
| `bind_address` | `127.0.0.1` | Bind address (never expose externally) |
| `allowed_origins` | `["http://localhost:3000"]` | CORS origin whitelist |
| `pkcs11_module` | `/usr/lib/srb-id-pkcs11.so` | Path to the PKCS#11 module |
| `card_poll_interval_ms` | `500` | Card presence polling interval |
| `sign_timeout_seconds` | `30` | Signing operation timeout |

<details>
<summary><strong>PKCS#11 module paths by platform</strong></summary>

| Platform | Path |
|---|---|
| Linux | `/usr/lib/srb-id-pkcs11.so` or `~/lib/libsrb-id-pkcs11.so` |
| macOS | `/usr/local/lib/libsrb-id-pkcs11.dylib` |
| Windows | `C:\Program Files\srb-id-pkcs11.dll` |

</details>

---

## 🖥️ Web UI — Signing a PDF

Open **http://127.0.0.1:19711/** in your browser to access the built-in signing page.

### How it works

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│  Upload PDF  │ ──▶ │  Enter PIN   │ ──▶ │  Signed PDF  │
│  (drag &     │     │  (card auth) │     │  (download)  │
│   drop)      │     │              │     │              │
└──────────────┘     └──────────────┘     └──────────────┘
```

1. **Upload a PDF** — drag & drop or click to select any PDF file
2. **Insert your ID card** — the Web UI detects it in real time via WebSocket
3. **Enter your PIN** — the card authenticates with the chip
4. **Choose algorithm** — SHA256 (default), SHA384, or SHA512
5. **Download the signed PDF** — the result is a standard PDF with an embedded visible signature

The signed PDF contains:
- A **visible signature box** at the bottom-right corner with the signer's name and date
- A **CMS/PKCS#7 detached signature** embedded in the PDF structure
- A **ByteRange** reference for cryptographic verification

### What the signature looks like

```
┌─────────────────────────────────┐
│  Digitally signed by            │
│  ─────────────────────────────  │
│  ALEKSANDAR MILENTIJEVIC        │
│  2026-01-01                     │
│  ▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬▬   │
└─────────────────────────────────┘
```

> **Note:** Cyrillic names from the ID card are automatically transliterated to Latin characters.

---

## API Reference

### REST Endpoints

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/status` | Health check — reader & card status |
| `GET` | `/readers` | List available smart card readers |
| `GET` | `/card/read` | Read data from the inserted ID card |
| `GET` | `/card/photo` | Get the card photo as base64 |
| `GET` | `/card/certificate` | Retrieve the signing certificate (DER, base64) |
| `POST` | `/card/sign` | Sign a payload with the card's private key |

### WebSocket (`/ws`)

Real-time events pushed to connected clients:

| Event | Description |
|---|---|
| `ws.connected` | Connection established (includes `version`) |
| `reader.connected` | A card reader was detected |
| `reader.disconnected` | Card reader removed |
| `card.inserted` | ID card inserted into reader |
| `card.removed` | ID card removed from reader |

### Examples

```bash
# Health check
curl http://127.0.0.1:19711/status

# Read card data
curl http://127.0.0.1:19711/card/read

# Get signing certificate
curl http://127.0.0.1:19711/card/certificate
```

```javascript
// Listen for card events via WebSocket
const ws = new WebSocket('ws://127.0.0.1:19711/ws')
ws.onmessage = e => console.log(JSON.parse(e.data))
```

### REST API Signing (Raw Signature)

For programmatic signing without the Web UI:

```bash
# Sign a PDF via the REST API
PAYLOAD=$(base64 -w 0 document.pdf)

curl -X POST http://127.0.0.1:19711/card/sign \
  -H "Content-Type: application/json" \
  -d "{
    \"payload_base64\": \"$PAYLOAD\",
    \"pin\": \"YOUR_PIN\",
    \"algorithm\": \"SHA256withRSA\"
  }"
```

The response contains `signature_base64` and `certificate_base64` for independent verification:

```bash
echo "$CERT_BASE64" | base64 -d > signer.crt
echo "$SIG_BASE64" | base64 -d > document.sig
openssl dgst -sha256 -verify signer.crt -signature document.sig document.pdf
# Expected: Verified OK
```

---

## Signing Walkthrough — Step by Step

### Step 1 — Start the middleware

```bash
sudo systemctl start pcscd
go run main.go
```

### Step 2 — Open the Web UI

Navigate to **http://127.0.0.1:19711/** in your browser.

### Step 3 — Upload a PDF

Click the upload area or drag a PDF file onto it. Any PDF works — blank or content.

### Step 4 — Insert your ID card

Insert your Serbian ID card into the connected card reader. The Web UI will show a real-time "Card detected" status via WebSocket.

### Step 5 — Enter your PIN

Type your card PIN into the input field and click **Sign**.

### Step 6 — Download the signed PDF

The signed PDF is returned automatically. It contains:

| Component | Description |
|---|---|
| **Visible signature** | Bottom-right corner box with name + date |
| **CMS signature** | Embedded PKCS#7/CMS detached signature |
| **ByteRange** | Cryptographic integrity reference |

### Step 7 — Verify (optional)

```bash
# Extract the signature from a signed PDF and verify
openssl cms -verify -in signed.pdf -CAfile signer.crt -out /dev/null
```

---

## Project Layout

```
├── main.go              # Application entry point
├── config.yaml.example  # Example configuration
├── web/
│   └── index.html       # Built-in signing web UI
├── config/
│   └── config.go        # Configuration loader (YAML + defaults)
├── card/
│   ├── models.go        # Data models (CardData, SignRequest/Response)
│   ├── reader.go        # PC/SC smart card reader manager
│   ├── idcard.go        # Serbian ID card TLV parser
│   ├── service.go       # Card read service (polling, events)
│   ├── sign.go          # PKCS#11 signing service
│   └── pdfsign.go       # PDF signature embedding (visible + CMS)
└── server/
    ├── server.go        # HTTP server & REST handlers
    ├── static.go        # Static file server (web UI)
    └── websocket.go     # WebSocket hub & client management
```

---

## Build

```bash
# Local build
go build -o locksmith .

# Run tests
go test ./...

# Lint
golangci-lint run
```

---

## Security

- 🔒 Server binds to `127.0.0.1` only — **never exposed externally**
- 🛡️ CORS restricted via `allowed_origins` in config
- 🚫 PINs and sensitive data are **never persisted**
- 🔐 Signing happens on-card — private key never leaves the chip

---

## References

- [ubavic/bas-celik](https://github.com/ubavic/bas-celik) — Reference implementation & TLV layout
- [ubavic/srb-id-pkcs11](https://github.com/ubavic/srb-id-pkcs11/releases) — PKCS#11 module download
- [ebfe/scard](https://github.com/ebfe/scard) — PC/SC Go binding
- [gorilla/websocket](https://github.com/gorilla/websocket) — WebSocket library
- [miekg/pkcs11](https://github.com/miekg/pkcs11) — PKCS#11 Go binding

---

<div align="center">

Made with ❤️ for Serbia

</div>