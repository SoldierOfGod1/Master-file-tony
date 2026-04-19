# =============================================================================
# Backend Dockerfile — SOLDIER OF GOD Command Centre
# Multi-stage Go build with distroless runtime
# =============================================================================

FROM golang:1.25-bookworm AS builder

WORKDIR /build

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=$(git describe --tags --always 2>/dev/null || echo dev)" \
    -trimpath \
    -o /server ./cmd/server

# ---------------------------------------------------------------------------
# Runtime stage — distroless for minimal attack surface
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

LABEL maintainer="baptista.manuel@rain.co.za"
LABEL org.opencontainers.image.source="https://github.com/SoldierOfGod1/command-centre"
LABEL org.opencontainers.image.description="Command Centre API Server"

COPY --from=builder /server /app/server
COPY config.toml /app/config.toml

WORKDIR /app
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/app/server"]
