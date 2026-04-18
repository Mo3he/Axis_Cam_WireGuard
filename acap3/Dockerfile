ARG UBUNTU_VERSION=20.04
ARG GO_VERSION=1.22.4

# Build go binary
FROM docker.io/golang:${GO_VERSION} AS gobuilder

# Copy ACAP 3-specific app files (package.conf, param.conf, Makefile)
COPY acap3/app /opt/app/
# Reuse the shared sources from the main app directory
COPY app/config_updater.c /opt/app/config_updater.c
COPY app/html /opt/app/html/
COPY app/wireguard /opt/app/wireguard/
COPY LICENSE /opt/app/LICENSE

WORKDIR /opt/app
# Cross-compile the Go WireGuard binary for armv7hf (fully static, no CGO)
RUN mkdir -p lib
WORKDIR /opt/app/wireguard
RUN GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 \
    go build -ldflags='-s -w' -o ../lib/wireguard-userspace . && \
    chmod 755 ../lib/wireguard-userspace

FROM docker.io/axisecp/acap-sdk:3.5-armv7hf-ubuntu${UBUNTU_VERSION}
COPY --from=gobuilder /opt/app /opt/app
WORKDIR /opt/app

# Patch the version placeholder in package.conf
ARG ACAP_VERSION=1.2.8
RUN sed -i "s/VERSION=BUILDVER/VERSION=${ACAP_VERSION}/" package.conf

# ACAP 3 firmware expects the settings page at the app root, not in html/
# hadolint ignore=DL3059
RUN cp html/index.html index.html

# Build the C parameter bridge and package everything as an ACAP .eap
RUN . /opt/axis/acapsdk/environment-setup* && \
    make && \
    create-package.sh .
