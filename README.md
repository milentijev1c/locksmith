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

- 🪪 **Read card data** — name, JMBG, address, document info, photo
- ✍️ **Digital signing** — sign arbitrary payloads using the card's private key (PKCS#11)
- 📡 **Real-time events** — WebSocket notifications for card insert/remove
- 🔒 **Local-only** — binds to `127.0.0.1`, no external exposure
- ⚙️ **Configurable** — YAML config with sane defaults

---

## Prerequisites

| Dependency | Notes |
|---|---|
| **Go 1.26+** | [go.dev](https://go.dev/dl/) |
| **pcscd** | PC/SC daemon — `sudo apt install pcscd` (Linux) |
| **libpcsclite-dev** | `sudo apt install libpcsclite-dev` (Linux) |
| **srb-id-pkcs11** | [Download](https://github.com/ubavic/srb-id-pkcs11/releases) — required for signing |
| **Smart card reader** | Any PC/SC compatible reader |
| **Serbian ID card** | The physical card with PIN |

---

## Quick Start

```bash
git clone https://github.com/milentijev1c/locksmith.git
cd locksmith

# Start the PC/SC daemon (Linux)
sudo systemctl start pcscd

# Fetch dependencies and run
go mod download
go run main.go
```

The service starts at **http://127.0.0.1:19711**.

---

## Configuration

Copy and edit `config.yaml.example`:

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

# List readers
curl http://127.0.0.1:19711/readers

# Read card data
curl http://127.0.0.1:19711/card/read

# Get signing certificate
curl http://127.0.0.1:19711/card/certificate
```

```javascript
// JavaScript — listen for card events
const ws = new WebSocket('ws://127.0.0.1:19711/ws')
ws.onmessage = e => console.log(JSON.parse(e.data))
```

---

## ✍️ Signing a PDF with Locksmith

This walkthrough shows how to **digitally sign a blank PDF** using the Serbian ID card through Locksmith.

### Step 1 — Start the middleware

```bash
# Make sure pcscd is running and the PKCS#11 module is installed
sudo systemctl start pcscd

# Start Locksmith
go run main.go
```

Insert your Serbian ID card into the reader when prompted.

### Step 2 — Create a blank PDF

Create a minimal blank PDF (or use any existing PDF):

```bash
# Using Python
python3 -c "
import struct
pdf = b'%PDF-1.0\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj 2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj 3 0 obj<</Type/Page/MediaBox[0 0 612 792]/Parent 2 0 R>>endobj\nxref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \ntrailer<</Size 4/Root 1 0 R>>\nstartxref\n190\n%%EOF'
with open('document.pdf', 'wb') as f:
    f.write(pdf)
"
```

### Step 3 — Hash the PDF

Calculate the SHA-256 digest of the PDF file. This is the payload that gets signed:

```bash
sha256sum document.pdf
# Example output: a]3f8b2c...  document.pdf
```

Or in JavaScript:

```javascript
const crypto = require('crypto');
const fs = require('fs');
const pdfBytes = fs.readFileSync('document.pdf');
const hash = crypto.createHash('sha256').update(pdfBytes).digest();
const payloadBase64 = hash.toString('base64');
```

### Step 4 — Send the signing request

```bash
# Base64-encode the raw PDF bytes (the middleware hashes internally)
PAYLOAD=$(base64 -w 0 document.pdf)

# Sign with your ID card PIN
curl -X POST http://127.0.0.1:19711/card/sign \
  -H "Content-Type: application/json" \
  -d "{
    \"payload_base64\": \"$PAYLOAD\",
    \"pin\": \"YOUR_PIN_HERE\",
    \"algorithm\": \"SHA256withRSA\"
  }"
```

<details>
<summary><strong>Or sign just the hash (smaller payload)</strong></summary>

If you prefer to hash the PDF yourself and send only the 32-byte digest:

```bash
# Hash the PDF, base64-encode the hash
HASH=$(sha256sum -b document.pdf | cut -d' ' -f1 | xxd -r -p | base64)

curl -X POST http://127.0.0.1:19711/card/sign \
  -H "Content-Type: application/json" \
  -d "{
    \"payload_base64\": \"$HASH\",
    \"pin\": \"YOUR_PIN_HERE\",
    \"algorithm\": \"SHA256withRSA\"
  }"
```

</details>

### Step 5 — Response

The middleware returns a JSON response:

```json
{
  "signature_base64": "MEUCIQD...base64-encoded RSA signature...",
  "certificate_base64": "MIIC...DER-encoded X.509 certificate..."
}
```

| Field | Description |
|---|---|
| `signature_base64` | The RSA PKCS#1 v1.5 signature over the payload |
| `certificate_base64` | The card's signing certificate (DER → base64) |

### Step 6 — Verify the signature (optional)

```bash
# Save the certificate
echo "$CERT_BASE64" | base64 -d > signer.crt

# Save the signature
echo "$SIG_BASE64" | base64 -d > document.sig

# Verify (OpenSSL)
openssl dgst -sha256 -verify signer.crt -signature document.sig document.pdf
# Expected output: Verified OK
```

### Full JavaScript Example

```javascript
const crypto = require('crypto');
const fs = require('fs');

async function signPDF(pdfPath, pin) {
  // Read and hash the PDF
  const pdfBytes = fs.readFileSync(pdfPath);
  const hash = crypto.createHash('sha256').update(pdfBytes).digest('base64');

  // Send signing request to Locksmith
  const res = await fetch('http://127.0.0.1:19711/card/sign', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      payload_base64: hash,
      pin: pin,
      algorithm: 'SHA256withRSA',
    }),
  });

  const { signature_base64, certificate_base64 } = await res.json();

  // Save outputs
  fs.writeFileSync('document.sig', Buffer.from(signature_base64, 'base64'));
  fs.writeFileSync('signer.crt', Buffer.from(certificate_base64, 'base64'));

  console.log('✅ PDF signed successfully');
  console.log('   Signature: document.sig');
  console.log('   Certificate: signer.crt');
}

signPDF('document.pdf', 'YOUR_PIN');
```

---

## Project Layout

```
├── main.go              # Application entry point
├── config.yaml.example  # Example configuration
├── config/
│   └── config.go        # Configuration loader (YAML + defaults)
├── card/
│   ├── models.go        # Data models (CardData, SignRequest/Response)
│   ├── reader.go        # PC/SC smart card reader manager
│   ├── idcard.go        # Serbian ID card TLV parser
│   ├── service.go       # Card read service (polling, events)
│   └── sign.go          # PKCS#11 signing service
└── server/
    ├── server.go        # HTTP server & REST handlers
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