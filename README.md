# WireGuard VPN ACAP

A WireGuard VPN client that runs directly on Axis cameras as an ACAP application.

Current version: **1.2.6**

Download the pre-built `.eap` for your camera's architecture from the [latest release](https://github.com/Mo3he/Axis_Cam_WireGuard/releases/latest) and install via the camera's web interface under **Apps → Add app**.

> **Disclaimer:** This is an independent, community-developed ACAP package and is not an official Axis Communications product. It is not affiliated with, endorsed by, or supported by Axis Communications AB. Use it at your own risk. For official Axis software, visit axis.com 

> **WireGuard Notice:** WireGuard is a registered trademark of Jason A. Donenfeld. This package independently redistributes [wireguard-go](https://github.com/WireGuard/wireguard-go) and related components under the MIT License and is not affiliated with, endorsed by, or supported by the WireGuard project or Jason A. Donenfeld. For the official WireGuard project, visit [wireguard.com](https://www.wireguard.com).

## Overview

Adding a VPN client directly to the camera allows secure remote access without requiring any other equipment or network configuration. WireGuard achieves this in a secure, simple, and lightweight way.

The app runs entirely in userspace using [wireguard-go](https://github.com/WireGuard/wireguard-go) + [gVisor netstack](https://gvisor.dev/), which means:

- **No root required** — runs as the standard unprivileged `sdk` ACAP user (ACAP 4 builds)
- **Compatible with Axis OS 9.x through 12** — see the Compatibility section below
- **No kernel TUN device** — all networking is handled inside the process

## Compatibility

| SDK | Axis OS | Architecture | File |
|---|---|---|---|
| ACAP 4 native SDK | 11.11+ (incl. OS 12) | aarch64 | `WireGuard_VPN_1_2_6_aarch64.eap` |
| ACAP 4 native SDK | 11.11+ (incl. OS 12) | armv7hf | `WireGuard_VPN_1_2_6_armv7hf.eap` |
| ACAP 3 SDK | 9.x – 10.x | armv7hf | `WireGuard_VPN_1_2_6_armv7hf_acap3.eap` |

The ACAP 3 build targets older cameras running Axis OS 9.x or 10.x (`EmbeddedDevelopment.Version=2.x`).

To check your camera's OS version and architecture:

```sh
curl --digest -u <username>:<password> \
  'http://<device-ip>/axis-cgi/param.cgi?action=list&group=Properties.Firmware.Version'

curl --digest -u <username>:<password> \
  'http://<device-ip>/axis-cgi/param.cgi?action=list&group=Properties.System.Architecture'
```

## Installing

Download the `.eap` for your camera from the [latest release](https://github.com/Mo3he/Axis_Cam_WireGuard/releases/latest) and install via the camera's web interface under **Apps → Add app**.

> **Note:** EAP files are not included in the repository. Always download from the [Releases](https://github.com/Mo3he/Axis_Cam_WireGuard/releases) page.

| SDK | Architecture | File |
|---|---|---|
| ACAP 4 (OS 11.11+) | aarch64 | `WireGuard_VPN_<version>_aarch64.eap` |
| ACAP 4 (OS 11.11+) | armv7hf | `WireGuard_VPN_<version>_armv7hf.eap` |
| ACAP 3 (OS 9.x–10.x) | armv7hf | `WireGuard_VPN_<version>_armv7hf_acap3.eap` |

## Configuration

Open the app's settings page in the camera web UI and fill in:

| Parameter | Description |
|---|---|
| **Private Key** | Your WireGuard private key for this camera (keep secret) |
| **Listen Port** | Local UDP port (leave blank for random, or use 51820) |
| **Endpoint** | WireGuard server address and port — e.g. `vpn.example.com:51820` |
| **Peer Public Key** | Public key of your WireGuard server |
| **Allowed IPs** | Routes to send through the VPN (default: `0.0.0.0/0` for all traffic) |
| **Client IP** | This camera's IP address on the VPN network — e.g. `10.0.0.2/24` |

The Private Key field will appear blank when you revisit the settings — this is expected behaviour for password-type parameters. The key is saved securely.

> **Tip:** Click **Import .conf** on the settings page to load all fields from a standard WireGuard `.conf` file at once.

### Generating keys

```sh
# Generate a private key for the camera
wg genkey | tee camera-private.key | wg pubkey > camera-public.key

# The private key goes into the ACAP settings
# The public key goes into your server's peer config
```

### Server-side peer config (example)

```ini
[Peer]
PublicKey = <camera-public.key contents>
AllowedIPs = 10.0.0.2/32
```

## How it works

Once connected, three proxy services start on the camera:

| Service | Default address | Purpose |
|---|---|---|
| **HTTP CONNECT proxy** | `http://127.0.0.1:8080` | Routes outbound HTTP/HTTPS camera traffic through WireGuard |
| **Outbound SOCKS5** | `127.0.0.1:1080` | Routes outbound TCP from camera services (e.g. MQTT) through WireGuard |
| **Inbound SOCKS5** | `<wireguard-ip>:1080` | Allows WireGuard peers to reach any camera port |

> **Port conflict handling:** If `8080` or `1080` are already in use by another application (e.g. another VPN ACAP), the app automatically tries the next port (`8081`, `8082`… or `1081`, `1082`…). The actual bound addresses are shown in the connection details panel in the UI.

### Saving config

Click **Save** in the UI — the app stops the tunnel, applies the new config, and restarts within a few seconds. All six fields are saved as a single operation so only one restart occurs.

### Routing outbound camera traffic through WireGuard

**Global HTTP/HTTPS proxy** — set in **System → Network → Global proxies**:
```
HTTP proxy:  http://127.0.0.1:8080
HTTPS proxy: http://127.0.0.1:8080
```

**Built-in MQTT client** — set in **System → MQTT → Broker**:
```
HTTP proxy:  http://127.0.0.1:8080
HTTPS proxy: http://127.0.0.1:8080
```

**ACAP apps / services using SOCKS5** — set their proxy to `127.0.0.1:1080`.

## Config API

The app exposes its configuration entirely through the standard VAPIX `param.cgi` endpoint — no custom API server required. Any HTTP client with camera credentials can read or write the config.

### Read current config

```sh
curl --digest -u admin:password \
  'http://<camera-ip>/axis-cgi/param.cgi?action=list&group=root.wireguardconfig'
```

Example response:
```
root.wireguardconfig.AllowedIPs=0.0.0.0/0
root.wireguardconfig.ClientIP=10.0.0.2/24
root.wireguardconfig.Endpoint=vpn.example.com:51820
root.wireguardconfig.ListenPort=51820
root.wireguardconfig.PeerPublicKey=<base64>
root.wireguardconfig.PrivateKey=<base64>
```

### Push a new config

```sh
curl --digest -u admin:password \
  --data-urlencode 'action=update' \
  --data-urlencode 'root.wireguardconfig.PrivateKey=<base64-private-key>' \
  --data-urlencode 'root.wireguardconfig.PeerPublicKey=<base64-peer-pubkey>' \
  --data-urlencode 'root.wireguardconfig.Endpoint=vpn.example.com:51820' \
  --data-urlencode 'root.wireguardconfig.ClientIP=10.0.0.2/24' \
  --data-urlencode 'root.wireguardconfig.AllowedIPs=0.0.0.0/0' \
  --data-urlencode 'root.wireguardconfig.ListenPort=51820' \
  'http://<camera-ip>/axis-cgi/param.cgi'
```

The app watches for parameter changes and automatically applies the new config — no restart required. A successful update returns `OK`.

### Push config from a `.conf` file (shell helper)

```sh
#!/bin/sh
# Usage: ./push-config.sh vpn.conf <camera-ip> <username> <password>
CONF=$1; CAM=$2; USER=$3; PASS=$4

get() { grep -m1 "^\s*$1" "$CONF" | sed 's/.*= *//' | tr -d '\r'; }

curl --digest -u "$USER:$PASS" \
  --data-urlencode "action=update" \
  --data-urlencode "root.wireguardconfig.PrivateKey=$(get PrivateKey)" \
  --data-urlencode "root.wireguardconfig.PeerPublicKey=$(get PublicKey)" \
  --data-urlencode "root.wireguardconfig.Endpoint=$(get Endpoint)" \
  --data-urlencode "root.wireguardconfig.ClientIP=$(get Address)" \
  --data-urlencode "root.wireguardconfig.AllowedIPs=$(get AllowedIPs)" \
  --data-urlencode "root.wireguardconfig.ListenPort=$(get ListenPort)" \
  "http://$CAM/axis-cgi/param.cgi"
```

## Building from source

Requires Docker. Two separate build scripts cover the two SDK generations.

**ACAP 4 native SDK** (Axis OS 11.11+, aarch64 + armv7hf):

```sh
./build.sh
```

**ACAP 3 SDK** (Axis OS 9.x – 10.x, armv7hf only):

```sh
cd acap3 && ./build.sh
```

## Links

- https://www.wireguard.com/
- https://www.axis.com/
