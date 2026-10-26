FROM rust:1.99.1-slim-bookworm AS rust
RUN rustup target add wasm32-wasip1 && \
    mv "$(rustc --print sysroot)" /opt/rust && \
    rm -rf /opt/rust/share

FROM node:24.22.1-bookworm-slim AS assemblyscript
WORKDIR /opt/assemblyscript
RUN npm install --omit=dev --ignore-scripts --no-audit --no-fund assemblyscript@0.28.20 @assemblyscript/wasi-shim@0.1.0

FROM debian:bookworm-slim AS wasi-sdk
ARG WASI_SDK_VERSION=34
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl && rm -rf /var/lib/apt/lists/*
RUN arch="$(dpkg --print-architecture | sed 's/amd64/x86_64/')" && \
    curl --proto '=https' --tlsv1.2 -fsSL "https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-${WASI_SDK_VERSION}/wasi-sdk-${WASI_SDK_VERSION}.0-${arch}-linux.tar.gz" | tar -xz -C /opt && \
    mv /opt/wasi-sdk-${WASI_SDK_VERSION}.0-${arch}-linux /opt/wasi-sdk && \
    cd /opt/wasi-sdk && \
    find bin -mindepth 1 ! -name clang ! -name 'clang-[0-9]*' ! -name wasm-ld ! -name lld ! -name '*.cfg' -delete && \
    find share -mindepth 1 -maxdepth 1 ! -name wasi-sysroot -exec rm -rf {} + && \
    find share/wasi-sysroot/include share/wasi-sysroot/lib -mindepth 1 -maxdepth 1 ! -name wasm32-wasip1 -exec rm -rf {} + && \
    rm -f lib/liblldb* lib/libclang.so*

FROM golang:1.25-bookworm AS build
ARG TINYGO_VERSION=0.42.0
RUN arch="$(dpkg --print-architecture)" && \
    curl --proto '=https' --tlsv1.2 -fsSL -o /tmp/tinygo.deb "https://github.com/tinygo-org/tinygo/releases/download/v${TINYGO_VERSION}/tinygo_${TINYGO_VERSION}_${arch}.deb" && \
    dpkg -i /tmp/tinygo.deb && rm -f /tmp/tinygo.deb
COPY --from=wasi-sdk /opt/wasi-sdk /opt/wasi-sdk
COPY --from=rust /opt/rust /opt/rust
COPY --from=assemblyscript /usr/local/bin/node /usr/local/bin/node
COPY --from=assemblyscript /opt/assemblyscript /opt/assemblyscript
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=1 go build -o /out/server ./cmd/server

FROM debian:bookworm-slim
LABEL ft-lgtm=backend
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* && \
    useradd --create-home --uid 10001 app
COPY --from=wasi-sdk /opt/wasi-sdk /opt/wasi-sdk
COPY --from=rust /opt/rust /opt/rust
COPY --from=assemblyscript /usr/local/bin/node /usr/local/bin/node
COPY --from=assemblyscript /opt/assemblyscript /opt/assemblyscript
COPY --from=build /usr/local/lib/tinygo /usr/local/lib/tinygo
COPY --from=build /usr/local/bin/tinygo /usr/local/bin/tinygo
COPY --from=build /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}" \
    TINYGOROOT=/usr/local/lib/tinygo
USER 10001
RUN printf 'package main\n\nfunc main() {}\n' > /tmp/warm.go && \
    GOOS=wasip1 GOARCH=wasm tinygo build -o /tmp/warm.wasm /tmp/warm.go && \
    rm -f /tmp/warm.go /tmp/warm.wasm
COPY --from=build /out/server /usr/local/bin/server
ENTRYPOINT ["/usr/local/bin/server"]
