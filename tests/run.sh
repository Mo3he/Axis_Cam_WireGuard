#!/usr/bin/env sh
# Unit tests for wireguard-userspace: local forward parsing and camera dial.
# Run by CI (build.yml) and by acap-build.sh before every release.
set -eu
cd "$(dirname "$0")/../app/wireguard"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "not gofmt-clean:" >&2
	echo "$unformatted" >&2
	exit 1
fi
go vet ./...
go test -race -count=1 ./...
