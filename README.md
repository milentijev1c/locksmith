# Locksmith — Serbian ID Card Middleware

Lightweight local middleware that reads Serbian national ID cards and
exposes a simple HTTP and WebSocket API on localhost.

This repository contains a minimal, self-contained daemon intended for
integration with local web applications. The service listens on
127.0.0.1:19711 by default.

## Quick start

Prerequisites:
- Go 1.22 or newer
- pcscd / libpcsclite installed on Linux (e.g. `libpcsclite-dev`)

Run:

```bash
git clone https://github.com/milentijev1c/locksmith
cd locksmith

# Start PC/SC daemon on Linux if needed
sudo systemctl start pcscd

# Fetch dependencies and run
go mod download
go run main.go
```

The service will be available at http://127.0.0.1:19711

## API

REST endpoints:
- `GET /status`  — health and current reader/card status
- `GET /readers` — list available smart card readers
- `GET /card/read` — read data from inserted ID card (JSON)
- `GET /card/photo` — return card photo (base64)
- `POST /card/sign` — sign payload using card (requires PKCS#11 module)

WebSocket (`/ws`) emits simple events:
- `reader.connected`, `reader.disconnected`
- `card.inserted`, `card.removed`

## Security notes

- The server binds to `127.0.0.1` only and is not exposed externally.
- CORS is restricted via the `config.yaml` origin whitelist.
- PINs and sensitive data are not persisted.

## Project layout

Top-level files and directories:

```
main.go            # application entry
config/            # configuration loader
card/              # card reader, TLV parser, models
server/            # HTTP and WebSocket server
build/             # helper build scripts
```

## Build

Build a local binary:

```bash
go build -o locksmith .
```

Cross-build scripts are available in `build/`.

## Example (JavaScript)

```javascript
// check service
fetch('http://127.0.0.1:19711/status').then(r => r.json()).then(console.log)

// read card
fetch('http://127.0.0.1:19711/card/read').then(r => r.json()).then(console.log)

// websocket
const ws = new WebSocket('ws://127.0.0.1:19711/ws')
ws.onmessage = e => console.log(JSON.parse(e.data))
```

## References

- https://github.com/ubavic/bas-celik — reference implementation and TLV layout
- https://github.com/ebfe/scard — PC/SC Go binding
- https://github.com/gorilla/websocket — WebSocket library
