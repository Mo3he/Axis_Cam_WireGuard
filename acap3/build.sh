#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(cd -P "$(dirname "$0")" && pwd) # acap3/
REPO_ROOT=$(cd -P "${SCRIPT_DIR}/.." && pwd) # repo root
RUNTIME=${RUNTIME:-docker}

echo '==> Building ACAP 3 armv7hf .eap package (for Axis OS 9.x / 10.x cameras)...'
DOCKER_BUILDKIT=1 "${RUNTIME}" build \
	--file "${SCRIPT_DIR}/Dockerfile" \
	-o type=local,dest="${REPO_ROOT}" \
	"${REPO_ROOT}"

echo '==> Done!'
ls -lh "${REPO_ROOT}"/WireGuard_VPN_*.eap
