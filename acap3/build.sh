#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"   # acap3/
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"   # repo root

VERSION="1.2.8"

echo "==> Building ACAP 3 armv7hf (for Axis OS 9.x / 10.x cameras)..."
docker build \
    --build-arg ACAP_VERSION="${VERSION}" \
    --tag "wireguard-vpn-acap3-armv7hf" \
    --file "${SCRIPT_DIR}/Dockerfile" \
    "${REPO_ROOT}"

echo "==> Extracting .eap package..."
CID=$(docker create "wireguard-vpn-acap3-armv7hf")
docker cp "${CID}":/opt/app/ "/tmp/acap3-out"

find "/tmp/acap3-out" -name "*.eap" -exec cp {} \
    "${REPO_ROOT}/WireGuard_VPN_${VERSION//./_}_armv7hf_acap3.eap" \;

docker rm "${CID}" >/dev/null
rm -rf "/tmp/acap3-out"

echo "==> Done!"
ls -lh "${REPO_ROOT}"/WireGuard_VPN_*.eap
