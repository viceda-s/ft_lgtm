FROM golang:1.25-bookworm AS build
ARG TINYGO_VERSION=0.42.0
RUN arch="$(dpkg --print-architecture)" && \
    curl -fsSL -o /tmp/tinygo.deb "https://github.com/tinygo-org/tinygo/releases/download/v${TINYGO_VERSION}/tinygo_${TINYGO_VERSION}_${arch}.deb" && \
    dpkg -i /tmp/tinygo.deb && rm -f /tmp/tinygo.deb
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /out/server ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* && \
    useradd --create-home --uid 10001 app
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
