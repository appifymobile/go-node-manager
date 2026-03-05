# Multi-stage build for minimal image size

# Builder stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git make ca-certificates

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build static binary with no CGO (except wireguard which is needed)
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o node-manager \
    ./cmd/node-manager

# Runtime stage - minimal Alpine image
FROM alpine:latest

RUN apk add --no-cache \
    ca-certificates \
    wireguard-tools \
    iproute2 \
    iptables \
    bash

WORKDIR /app

COPY --from=builder /app/node-manager /usr/local/bin/
COPY config.yaml .
COPY migrations /app/migrations

EXPOSE 8080 9090

HEALTHCHECK --interval=10s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/manage/health || exit 1

ENTRYPOINT ["/usr/local/bin/node-manager"]
CMD ["-config", "/app/config.yaml"]
