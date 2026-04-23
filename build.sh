#!/usr/bin/env sh
set -eu

REPO_ROOT=$(cd -P "$(dirname "$0")" && pwd)

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

# Remove any previously built .eap files so only the current build remains
echo '==> Cleaning old .eap files...'
rm -f "${REPO_ROOT}"/*.eap

# build_arch <arch>
# For docker: uses BuildKit --output to extract the .eap directly.
# For podman (remote mode): builds with a tag, copies the .eap out via
# a temporary container, then removes the image.
build_arch() {
	ARCH=$1
	echo "==> Building .eap package for ${ARCH}..."
	if [ "$RUNTIME" = "podman" ]; then
		TAG="wireguard-acap-build-${ARCH}-$$"
		DOCKER_BUILDKIT=1 "$RUNTIME" build --build-arg ARCH="$ARCH" -t "$TAG" "$REPO_ROOT"
		CID=$("$RUNTIME" create "$TAG")
		"$RUNTIME" cp "${CID}":/ "${REPO_ROOT}/"
		"$RUNTIME" rm -f "$CID"  >/dev/null 2>&1 || true
		"$RUNTIME" rmi -f "$TAG" >/dev/null 2>&1 || true
	else
		DOCKER_BUILDKIT=1 "$RUNTIME" build \
			--build-arg ARCH="$ARCH" \
			-o type=local,dest="${REPO_ROOT}" \
			"$REPO_ROOT"
	fi
}

for ARCH in aarch64 armv7hf; do
	build_arch "$ARCH" &
done
wait

echo '==> Done!'
ls -lh "$REPO_ROOT"/*.eap
