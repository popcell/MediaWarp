FROM golang:1.24 AS builder
ENV GO111MODULE=on \
    GOPROXY=https://goproxy.io,direct \
    CGO_ENABLED=0

WORKDIR /builder
COPY . .
RUN set -eux; \
    test -f "static/dd-danmaku/ede.js"; \
    test -f "static/jellyfin-danmaku/ede.js"; \
    COMMIT_HASH="$(git rev-parse HEAD 2>/dev/null || echo unknown)"; \
    BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; \
    go mod download; \
    go build -ldflags "-s -w -X MediaWarp/internal/config.commitHash=${COMMIT_HASH} -X MediaWarp/internal/config.buildDate=${BUILD_DATE}" -o MediaWarp

FROM alpine:latest
COPY --from=builder /builder/MediaWarp /MediaWarp

RUN chmod +x /MediaWarp

EXPOSE 9000
VOLUME ["/etc/localtime", "/etc/timezone", "/config", "/logs", "/custom"]
ENTRYPOINT ["/MediaWarp"]
