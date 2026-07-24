# WireGuard ACAP for Axis Cameras

[![Release](https://img.shields.io/github/v/release/Mo3he/Axis_Cam_WireGuard?style=flat)](https://github.com/Mo3he/Axis_Cam_WireGuard/releases)
[![License](https://img.shields.io/github/license/Mo3he/Axis_Cam_WireGuard?style=flat)](LICENSE)
[![Build](https://github.com/Mo3he/Axis_Cam_WireGuard/actions/workflows/build.yml/badge.svg)](https://github.com/Mo3he/Axis_Cam_WireGuard/actions/workflows/build.yml)
[![Super-Linter](https://github.com/Mo3he/Axis_Cam_WireGuard/actions/workflows/super-linter.yml/badge.svg)](https://github.com/Mo3he/Axis_Cam_WireGuard/actions/workflows/super-linter.yml)
[![Sponsor](https://img.shields.io/badge/Sponsor%20My%20Work-EA4AAA?style=flat&logo=github&logoColor=white)](https://github.com/sponsors/Mo3he)
[![Buy Me A Coffee](https://img.shields.io/badge/Buy%20Me%20A%20Coffee-FFDD00?style=flat&logo=buy-me-a-coffee&logoColor=black)](https://www.buymeacoffee.com/mo3he)

A WireGuard VPN client that runs directly on Axis cameras as an ACAP
application, enabling secure remote access without requiring any other equipment
or network configuration. WireGuard achieves this in a secure, simple, and
lightweight way.

> **Disclaimer:** Independent, community-developed ACAP package. Not an official
> Axis product and not affiliated with, endorsed by, or supported by Axis
> Communications AB or the WireGuard project. Use at your own risk.

> **WireGuard Notice:** WireGuard is a registered trademark of Jason A.
> Donenfeld. This package independently redistributes
> [wireguard-go](https://github.com/WireGuard/wireguard-go) and related
> components under the MIT License and is not affiliated with, endorsed by, or
> supported by the WireGuard project or Jason A. Donenfeld. For the official
> WireGuard project, visit [wireguard.com](https://www.wireguard.com).

## Table of Contents

- [Overview](#overview)
- [Compatibility](#compatibility)
- [Installation](#installation)
- [Configuration](#configuration)
- [Config API](#config-api)
- [Ports & security](#ports--security)
- [How it works](#how-it-works)
- [Build from source](#build-from-source)
- [Roadmap](#roadmap)
- [Links](#links)
- [License](#license)

## Overview

The app runs entirely in userspace using
[wireguard-go](https://github.com/WireGuard/wireguard-go) +
[gVisor netstack](https://gvisor.dev/), which means:

- **No root required:** runs as the standard unprivileged `sdk` ACAP user (ACAP
  4 builds).
- **Compatible with AXIS OS 9.x through 13:** see the Compatibility section
  below.
- **No kernel TUN device:** all networking is handled inside the process.

## Compatibility

| Build | AXIS OS | Architecture | Notes |
|---|---|---|---|
| ACAP 4 (native SDK) | 10.x – 13 | aarch64 | Standard build |
| ACAP 4 (native SDK) | 10.x – 13 | armv7hf | Standard build |
| ACAP 3 (legacy SDK) | 9.x – 10.x | armv7hf | Legacy cameras (`EmbeddedDevelopment.Version=2.x`) |

> Most cameras use the **ACAP 4** build. Use the **ACAP 3** build only on legacy
> cameras that don't support ACAP 4 (typically AXIS OS 9–10).

**Verified on AXIS OS 13** (13.0.0, aarch64).

## Installation

> **Signed packages:** Release `.eap` files are signed with the Axis ACAP
> signing service and install normally on AXIS OS 12.10 and later.
>
> **Upgrading from an earlier version?** The signing vendor changed, so
> installing over a previously installed unsigned build can fail with
> **"Couldn't install: app"** (device log: *"Vendor ID in manifest does not
> match the vendor ID of the previous version"*). To upgrade: back up your app
> configuration, **uninstall** the old version, then install the signed one.

Download the `.eap` for your camera from the
[latest release](https://github.com/Mo3he/Axis_Cam_WireGuard/releases/latest) and
install via the camera's web interface under **Apps -> Add app**.

> [!NOTE]
> EAP files are not included in the repository. Always download from the
> [Releases](https://github.com/Mo3he/Axis_Cam_WireGuard/releases) page.

| SDK | Architecture | File |
|---|---|---|
| ACAP 4 (OS 10.x – 13) | aarch64 | `WireGuard_VPN_<version>_aarch64.eap` |
| ACAP 4 (OS 10.x – 13) | armv7hf | `WireGuard_VPN_<version>_armv7hf.eap` |
| ACAP 3 (OS 9.x – 10.x) | armv7hf | `WireGuard_VPN_<version>_armv7hf_acap3.eap` |

## Configuration

Open the app's settings page in the camera web UI and fill in:

| Parameter | Description |
|---|---|
| **Private Key** | Your WireGuard private key for this camera (keep secret) |
| **Listen Port** | Local UDP port (leave blank for random, or use 51820) |
| **Endpoint** | WireGuard server address and port, e.g. `vpn.example.com:51820` |
| **Peer Public Key** | Public key of your WireGuard server |
| **Allowed IPs** | Routes to send through the VPN (default: `0.0.0.0/0` for all traffic) |
| **Client IP** | This camera's IP address on the VPN network, e.g. `10.0.0.2/24` |
| **HTTP Proxy Port** | Port for the HTTP CONNECT proxy on localhost (default: `8080`) |
| **Outbound SOCKS5 Port** | Port for the outbound SOCKS5 proxy on localhost (default: `1080`) |

The Private Key field will appear blank when you revisit the settings, this is
expected behavior for password-type parameters. The key is saved securely.

> [!TIP]
> Click **Import .conf** on the settings page to load all fields from a standard
> WireGuard `.conf` file at once.

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

## Config API

The app exposes its configuration entirely through the standard VAPIX
`param.cgi` endpoint, with no custom API server required. Any HTTP client with camera
credentials can read or write the config.

### Read current config

```sh
curl --digest -u admin:password \
  'https://<camera-ip>/axis-cgi/param.cgi?action=list&group=root.wireguardconfig'
```

Example response:

```ini
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
  'https://<camera-ip>/axis-cgi/param.cgi'
```

The app watches for parameter changes and automatically applies the new config,
no restart required. A successful update returns `OK`.

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
  "https://$CAM/axis-cgi/param.cgi"
```

## Ports & security

| Service | Default address | Purpose |
|---|---|---|
| **HTTP CONNECT proxy** | `http://127.0.0.1:<HTTPProxyPort>` | Routes outbound HTTP/HTTPS camera traffic through WireGuard |
| **Outbound SOCKS5** | `127.0.0.1:<OutboundSOCKS5Port>` | Routes outbound TCP from camera services (e.g. MQTT) through WireGuard |
| **Inbound SOCKS5** | `<wireguard-ip>:1080` | Allows WireGuard peers to reach any camera port |

> **Security:** the inbound SOCKS5 proxy is bound to the tunnel IP and is
> therefore reachable by any WireGuard peer. Restrict access with your VPN's own
> peer allow-lists, and keep the camera behind its normal authentication.

## How it works

Once connected, three proxy services start on the camera (see the ports table
above). The HTTP proxy and outbound SOCKS5 ports are user-configurable in the
settings (defaults: `8080` and `1080`). If the configured port is already in use
by another application, the proxy will fail to start and log an error; change
the port in settings to resolve the conflict. The actual bound addresses are
shown in the connection details panel in the UI.

### Saving config

Click **Save** in the UI; the app stops the tunnel, applies the new config, and
restarts within a few seconds. All six fields are saved as a single operation so
only one restart occurs.

### Routing outbound camera traffic through WireGuard

**Global HTTP/HTTPS proxy:** set in **System -> Network -> Global proxies**:

```ini
HTTP proxy:  http://127.0.0.1:<HTTPProxyPort>
HTTPS proxy: http://127.0.0.1:<HTTPProxyPort>
```

**Built-in MQTT client:** set in **System -> MQTT -> Broker**:

```ini
HTTP proxy:  http://127.0.0.1:<HTTPProxyPort>
HTTPS proxy: http://127.0.0.1:<HTTPProxyPort>
```

**ACAP apps / services using SOCKS5:** set their proxy to
`127.0.0.1:<OutboundSOCKS5Port>`.

## Build from source

Requires Docker. Two separate build scripts cover the two SDK generations.

**ACAP 4 native SDK** (AXIS OS 10.x through 13, aarch64 + armv7hf):

```sh
./build.sh
```

**ACAP 3 SDK** (AXIS OS 9.x – 10.x, armv7hf only):

```sh
cd acap3 && ./build.sh
```

## Roadmap

### AXIS OS 13 preparation

AXIS OS 13 introduces breaking changes that affect ACAP applications. Current
status for this project:

- [x] **Recompile for 64-bit time (Y2038)** - ACAP 4 builds use Native SDK
  `12.10.0` on Ubuntu `24.04`.
- [x] **Migrate to Manifest Schema v2** - `manifest.json` uses Schema v2 with
  `compatibleOsVersions` and an OS 13 max (`13`).
- [x] **Audit for executable stack** - Packaged `wireguardconfig` and
  `lib/wireguard-userspace` were checked for both architectures; all report
  non-executable GNU_STACK (`RW`).
- [x] **HTTPS-only UI check** - Web UI API calls use relative paths and no
  hardcoded remote `http://` or `ws://` endpoints.
- [ ] **Sign via ACAP Portal** - Still required for production install on OS 13.

Note: `compatibleOsVersions` is only enforced from AXIS OS 12.10 onward. Setting
`max: 13` (with the SDK's auto `min: 12.10.68`) therefore does not raise the floor
for older firmware, devices below 12.10 ignore the field entirely, so the
package still installs down to OS 10.x.

For the full list of OS 13 changes see
[AXIS OS 13 breaking changes](https://www.axis.com/for-developers/news/AXIS-OS-13-breaking-changes)
and [Changes in AXIS OS 13](https://help.axis.com/en-us/axis-os#changes-in-axis-os-13).

## Links

- [WireGuard](https://www.wireguard.com/)
- [wireguard-go](https://github.com/WireGuard/wireguard-go)
- [Axis Communications](https://www.axis.com/)

## License

The packaging code in this repository is licensed under BSD 3-Clause (see
[LICENSE](LICENSE)). Bundled upstream components (wireguard-go, gVisor,
golang.org/x, btree) are listed in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
