#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(cd -P "$(dirname "$0")" && pwd) # acap3/
REPO_ROOT=$(cd -P "${SCRIPT_DIR}/.." && pwd) # repo root
# Auto-detect container runtime.
# Prefer docker when the daemon is reachable; fall back to podman.
if [ -z "${RUNTIME:-}" ]; then
	if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
		RUNTIME=docker
	elif command -v podman >/dev/null 2>&1; then
		RUNTIME=podman
	elif command -v docker >/dev/null 2>&1; then
		# docker exists but daemon not running — let it fail with a clear error
		RUNTIME=docker
	else
		echo 'Error: neither docker nor podman found in PATH' >&2
		exit 1
	fi
fi
echo "==> Using container runtime: ${RUNTIME}"

# Remove any previously built acap3 .eap files so only the current build remains
echo '==> Cleaning old acap3 .eap files...'
rm -f "${REPO_ROOT}"/*_acap3.eap

echo '==> Building ACAP 3 armv7hf .eap package (for Axis OS 9.x / 10.x cameras)...'
# On macOS podman runs in remote mode and does not support --output,
# so we build with a tag, copy the .eap out via a temporary container, then clean up.
if [ "$RUNTIME" = "podman" ] && [ "$(uname -s)" = "Darwin" ]; then
	TAG="wireguard-acap-build-acap3-$$"
	DOCKER_BUILDKIT=1 "$RUNTIME" build \
		--file "${SCRIPT_DIR}/Dockerfile" \
		-t "$TAG" \
		"$REPO_ROOT"
	CID=$("$RUNTIME" create "$TAG")
	"$RUNTIME" cp "${CID}":/ "${REPO_ROOT}/"
	"$RUNTIME" rm -f "$CID" >/dev/null 2>&1 || true
	"$RUNTIME" rmi -f "$TAG" >/dev/null 2>&1 || true
else
	DOCKER_BUILDKIT=1 "$RUNTIME" build \
		--file "${SCRIPT_DIR}/Dockerfile" \
		-o type=local,dest="${REPO_ROOT}" \
		"$REPO_ROOT"
fi

echo '==> Done!'
ls -lh "${REPO_ROOT}"/WireGuard_VPN_*.eap
