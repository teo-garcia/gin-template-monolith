# syntax=docker/dockerfile:1.7
#
# Deliberately free of BuildKit cache mounts so `docker build` works with the
# classic builder too. CI gets layer caching from the GHA cache backend instead.

FROM golang:1.25.13-alpine AS deps
WORKDIR /app
RUN apk add --no-cache git
# Copy the manifests alone so the dependency layer is cached until they change.
COPY go.mod go.sum ./
RUN go mod download

FROM deps AS builder
COPY . .
# CGO off produces a static binary that runs on a bare distroless/alpine base.
# -trimpath keeps build host paths out of the binary.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/seed ./cmd/seed

FROM alpine:3.21 AS production
RUN apk add --no-cache ca-certificates dumb-init wget && \
    addgroup -g 1001 -S app && \
    adduser -S app -u 1001 -G app

WORKDIR /app
COPY --from=builder --chown=app:app /out/api /out/migrate /out/seed ./

USER app
ENV PORT=3000
EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- "http://127.0.0.1:${PORT}/health/live" | grep -q '"ok"' || exit 1

# dumb-init is PID 1 so SIGTERM reaches the app and graceful shutdown runs.
ENTRYPOINT ["dumb-init", "--"]
CMD ["/app/api"]
