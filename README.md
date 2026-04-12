# WireGuard VPN ACAP

A WireGuard VPN client that runs directly on Axis cameras as an ACAP application.

Current version: **1.1.0**

Download the pre-built `.eap` for your camera's architecture from the [latest release](https://github.com/Mo3he/Axis_Cam_WireGuard/releases/latest) and install via the camera's web interface under **Apps → Add app**.

## Overview

Adding a VPN client directly to the camera allows secure remote access without requiring any other equipment or network configuration. WireGuard achieves this in a secure, simple, and lightweight way.

Version 1.1.0 is a full rewrite of the networking layer. The app now runs entirely in userspace using [wireguard-go](https://github.com/WireGuard/wireguard-go) + [gVisor netstack](https://gvisor.dev/), which means:

- **No root required** — runs as the standard unprivileged `sdk` ACAP user
- **Compatible with Axis OS 11 and 12** — OS 12 blocked root ACAP apps; this version works on both
- **No kernel TUN device** — all networking is handled inside the process

## How it works

Once connected, the camera is reachable from the WireGuard network via:

- **Direct port forwarding** — ports 80 (HTTP), 443 (HTTPS), and 554 (RTSP) on the WireGuard IP are transparently forwarded to the camera's local services. Point your browser or RTSP client directly at the WireGuard IP.
- **SOCKS5 proxy on port 1080** — configure any SOCKS5-aware client to use `<wireguard-ip>:1080` for access to any camera port.

## Compatibility

Works on Axis cameras with ARM or aarch64 SoCs running **Axis OS 10, 11, or 12**.

To check your camera's architecture:

```sh
curl --digest -u <username>:<password> \
  http://<device-ip>/axis-cgi/param.cgi?action=list&group=Properties.System.Architecture
```

## Installing

Download the pre-built `.eap` for your camera's architecture from the [latest release](https://github.com/Mo3he/Axis_Cam_WireGuard/releases/latest) and install via the camera's web interface under **Apps → Add app**.

> **Note:** EAP files are not included in the repository. Always download from the [Releases](https://github.com/Mo3he/Axis_Cam_WireGuard/releases) page.

| Architecture | File |
|---|---|
| aarch64 (most cameras 2019+) | `WireGuard_VPN_1_1_0_aarch64.eap` |
| armv7hf (older cameras) | `WireGuard_VPN_1_1_0_armv7hf.eap` |

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

## Building from source

Requires Docker.

```sh
# aarch64
cd aarch64
docker build -t wireguard-acap-aarch64 .
docker cp $(docker create wireguard-acap-aarch64):/opt/app/WireGuard_VPN_1_1_0_aarch64.eap .

# armv7hf
cd armv7hf
docker build -t wireguard-acap-armv7hf .
docker cp $(docker create wireguard-acap-armv7hf):/opt/app/WireGuard_VPN_1_1_0_armv7hf.eap .
```

## Links

- https://www.wireguard.com/
- https://www.axis.com/
