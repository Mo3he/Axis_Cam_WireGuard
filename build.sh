#!/usr/bin/env sh
set -eu

REPO_ROOT=$(cd -P "$(dirname "$0")" && pwd)
RUNTIME=${RUNTIME:-docker}

for ARCH in aarch64 armv7hf; do
	echo "==> Building .eap package for ${ARCH}..."
	DOCKER_BUILDKIT=1 "${RUNTIME}" build --build-arg ARCH="${ARCH}" -o type=local,dest="${REPO_ROOT}" "${REPO_ROOT}" &
done
wait

echo '==> Done!'
ls -lh "$REPO_ROOT"/*.eap
