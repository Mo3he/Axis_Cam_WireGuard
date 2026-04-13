ARG ARCH=aarch64
ARG VERSION=1.15.1
ARG UBUNTU_VERSION=22.04
ARG REPO=axisecp
ARG SDK=acap-native-sdk

FROM ${REPO}/${SDK}:${VERSION}-${ARCH}-ubuntu${UBUNTU_VERSION}

# Install Go (amd64 build host, cross-compiling for target camera arch)
ARG GO_VERSION=1.22.4
RUN apt-get update -qq && apt-get install -y --no-install-recommends wget ca-certificates && \
    wget -q https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz && \
    rm go${GO_VERSION}.linux-amd64.tar.gz && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

ENV PATH="/usr/local/go/bin:${PATH}"

COPY ./app /opt/app/
WORKDIR /opt/app

# Patch the architecture placeholder in manifest.json
ARG ARCH
RUN sed -i "s/\"BUILDARCH\"/\"${ARCH}\"/" manifest.json

# Cross-compile the Go binary for the target architecture
RUN cd wireguard && \
    if [ "$ARCH" = "aarch64" ]; then \
        export GOARCH=arm64; \
    else \
        export GOARCH=arm GOARM=7; \
    fi && \
    GOOS=linux CGO_ENABLED=0 \
    go build -ldflags="-s -w" -o ../lib/wireguard-userspace . && \
    chmod 755 ../lib/wireguard-userspace

# Build the ACAP package (compiles the C binary and packages everything)
RUN . /opt/axis/acapsdk/environment-setup* && acap-build .
