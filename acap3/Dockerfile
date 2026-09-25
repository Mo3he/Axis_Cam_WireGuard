ARG UBUNTU_VERSION=20.04
ARG GO_VERSION=1.27.1

# Build go binary
FROM docker.io/golang:${GO_VERSION} AS gobuilder

COPY acap3/app /opt/app/
# Reuse the shared sources from the main app directory
COPY app/config_updater.c /opt/app/config_updater.c
COPY app/html /opt/app/html/
COPY app/wireguard /opt/app/wireguard/
COPY LICENSE /opt/app/LICENSE

WORKDIR /opt/app
RUN mkdir -p lib
WORKDIR /opt/app/wireguard
RUN GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
    go build -ldflags='-s -w' -o ../lib/wireguard-userspace . && \
    chmod 755 ../lib/wireguard-userspace

FROM docker.io/axisecp/acap-sdk:3.5-armv7hf-ubuntu${UBUNTU_VERSION} AS builder
COPY --from=gobuilder /opt/app /opt/app
WORKDIR /opt/app

ARG APPMAJORVERSION=1
ARG APPMINORVERSION=2
ARG APPMICROVERSION=13
RUN sed -i "s/VER_MAJOR/${APPMAJORVERSION}/g;s/VER_MINOR/${APPMINORVERSION}/g;s/VER_MICRO/${APPMICROVERSION}/g" package.conf

# ACAP 3 firmware expects the settings page at the app root, not in html/
# hadolint ignore=DL3059
RUN cp html/index.html index.html

# The _acap3 suffix keeps it from clashing with the ACAP 4 armv7hf package.
RUN . /opt/axis/acapsdk/environment-setup* && \
    make && \
    create-package.sh . && \
    for f in WireGuard_VPN_*.eap; do mv "$f" "${f%.eap}_acap3.eap"; done

FROM scratch
COPY --from=builder /opt/app/*acap3.eap /
