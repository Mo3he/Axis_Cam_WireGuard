#!/bin/bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"

echo "==> Building aarch64..."
docker build --tag wireguard-aarch64 "$REPO_ROOT/aarch64/"

echo "==> Building armv7hf..."
docker build --tag wireguard-armv7hf "$REPO_ROOT/armv7hf/"

echo "==> Extracting .eap packages..."
CID_AARCH64=$(docker create wireguard-aarch64)
CID_ARMV7HF=$(docker create wireguard-armv7hf)

docker cp "$CID_AARCH64":/opt/app/ /tmp/aarch64-out
docker cp "$CID_ARMV7HF":/opt/app/ /tmp/armv7hf-out

cp /tmp/aarch64-out/*.eap "$REPO_ROOT/"
cp /tmp/armv7hf-out/*.eap "$REPO_ROOT/"

echo "==> Cleaning up..."
docker rm "$CID_AARCH64" "$CID_ARMV7HF" >/dev/null
rm -rf /tmp/aarch64-out /tmp/armv7hf-out

echo "==> Done!"
ls -lh "$REPO_ROOT"/*.eap
