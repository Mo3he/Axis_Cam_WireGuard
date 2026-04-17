#!/usr/bin/env sh
set -eu

REPO_ROOT=$(realpath "${0%/*}")

for ARCH in aarch64 armv7hf; do
    echo "==> Building ${ARCH}..."
    docker build --build-arg ARCH="${ARCH}" --tag "wireguard-${ARCH}" "$REPO_ROOT"

    echo "==> Extracting ${ARCH} .eap package..."
    CID=$(docker create "wireguard-${ARCH}")
    docker cp "${CID}":/opt/app/ "/tmp/${ARCH}-out"
    cp "/tmp/${ARCH}-out"/*.eap "$REPO_ROOT/"
    docker rm "${CID}" >/dev/null
    rm -rf "/tmp/${ARCH}-out"
done

echo '==> Done!'
ls -lh "$REPO_ROOT"/*.eap
