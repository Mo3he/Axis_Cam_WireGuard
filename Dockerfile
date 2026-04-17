ARG ARCH=aarch64
ARG VERSION=1.15.1
ARG UBUNTU_VERSION=22.04
ARG REPO=docker.io/axisecp
ARG SDK=acap-native-sdk
ARG GO_VERSION=1.22.4

# Build go binary
FROM docker.io/golang:${GO_VERSION} AS gobuilder-aarch64
ENV GOARCH=arm64

FROM docker.io/golang:${GO_VERSION} AS gobuilder-armv7hf
ENV GOARCH=arm
ENV GOARM=7

FROM gobuilder-${ARCH} AS gobuilder
ARG ARCH

COPY ./app /opt/app/
WORKDIR /opt/app/wireguard
RUN GOOS=linux CGO_ENABLED=0 \
    go build -ldflags='-s -w' -o ../lib/wireguard-userspace . && \
    chmod 755 ../lib/wireguard-userspace

# Create ACAP package
FROM ${REPO}/${SDK}:${VERSION}-${ARCH}-ubuntu${UBUNTU_VERSION}
ARG ARCH
COPY --from=gobuilder /opt/app /opt/app
WORKDIR /opt/app

# Patch the architecture placeholder in manifest.json
RUN sed -i "s/\"BUILDARCH\"/\"${ARCH}\"/" manifest.json

# Build the ACAP package (compiles the C binary and packages everything)
RUN . /opt/axis/acapsdk/environment-setup* && acap-build .
