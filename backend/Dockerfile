FROM golang:1.25-bookworm AS build
RUN curl -fsSL -o /tmp/tinygo.deb https://github.com/tinygo-org/tinygo/releases/download/v0.42.0/tinygo_0.42.0_amd64.deb && \
    dpkg -i /tmp/tinygo.deb && rm -f /tmp/tinygo.deb
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /out/server ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/server /usr/local/bin/server
COPY --from=build /usr/local/lib/tinygo /usr/local/lib/tinygo
COPY --from=build /usr/local/bin/tinygo /usr/local/bin/tinygo
COPY --from=build /usr/local/go /usr/local/go
ENV PATH="/usr/local/go/bin:${PATH}"
ENTRYPOINT ["/usr/local/bin/server"]