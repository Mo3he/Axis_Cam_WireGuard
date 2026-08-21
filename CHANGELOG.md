# Changelog

All notable changes to this project are documented here. Each version
links to its full release notes on GitHub.

The format is based on [Keep a Changelog](https://keepachangelog.com/).

## 1.2.15 - 2026-08-21

- Update to upstream v0.0.0-20260522210424-ecfc5a8d5446.

## 1.2.14 - 2026-08-21

- Update to upstream v0.0.0-20260522210424-ecfc5a8d5446.

## [1.2.13] - 2026-08-19 - Configurable forwarded ports

- The directly forwarded ports are now configurable instead of fixed at 80, 443,
  and 554. Add port 22 to reach SSH over WireGuard, or any other camera service.
  Set them under **Forwarded Ports** in Settings, or through the `ForwardPorts`
  parameter. Up to 16 ports; clearing the field restores the default.

## [1.2.12] - 2026-07-24 - Save settings on recorder / access-control devices

- Fix: settings can now be saved on Axis devices that do not expose
  `/axis-cgi/param.cgi`, such as recorder/NVR products (e.g. S3008) and
  access-control controllers (e.g. A1610, A1710, A1810). The web UI previously
  appeared unable to persist configuration on those devices.
- The app now exposes a small settings endpoint at
  `/local/wireguardconfig/api/settings` through a manifest reverse-proxy. The
  web UI uses `param.cgi` when available and transparently falls back to this
  endpoint when it is not, writing configuration through the ACAP parameter
  store. The embedded server listens on port 2203 to stay clear of the other
  VPN ACAPs' settings servers.

## [1.2.11-Signed] - 2026-07-21 - WireGuard VPN 1.2.11 (Signed)

- Packages are now signed with the Axis ACAP signing service and install
  normally on AXIS OS 12.10 and later.
- Vendor updated to `moshe@mohome.net` with the registered vendor ID.
- The `acap3` variant remains unsigned (manifest schema v1.x).
- Upgrading from an earlier unsigned version can fail with "Couldn't
  install: app" (device log: "Vendor ID in manifest does not match the
  vendor ID of the previous version"). Back up your config, uninstall the
  old version, then install this one.

## [1.2.11] - 2026-07-07 - AXIS OS 13 ready

## [1.2.10] - 2026-05-11

## [1.2.9] - 2026-04-23

## [1.2.8] - 2026-04-17 - WireGuard VPN v1.2.8

## [1.2.7] - 2026-04-17 - User-configurable proxy ports

## [1.2.6] - 2026-04-17

## [1.2.5] - 2026-04-16

## [1.2.4] - 2026-04-14

## [1.2.1] - 2026-04-14 - WireGuard VPN v1.2.1

## [1.2.0] - 2026-04-13 - ZeroTier-style web UI

## [1.1.0] - 2026-04-12 - Axis OS 12 support (no root)

## [1.0.0] - 2025-04-17

[1.2.11]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/v1.2.11
[1.2.10]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/v1.2.10
[1.2.9]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/v1.2.9
[1.2.8]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/v1.2.8
[1.2.7]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/v1.2.7
[1.2.6]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/v1.2.6
[1.2.5]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/v1.2.5
[1.2.4]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/v1.2.4
[1.2.1]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/v1.2.1
[1.2.0]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/v1.2.0
[1.1.0]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/v1.1.0
[1.0.0]: https://github.com/Mo3he/Axis_Cam_WireGuard/releases/tag/V1.0.0
