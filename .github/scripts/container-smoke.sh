#!/usr/bin/env bash

set -euo pipefail

image=${1:?container image is required}
network=gin-smoke

# Invoked by the EXIT trap.
# shellcheck disable=SC2329
cleanup() {
  docker rm --force gin-smoke-app gin-smoke-db gin-smoke-redis 2>/dev/null || true
  docker network rm "$network" 2>/dev/null || true
}

trap cleanup EXIT

docker network create "$network"
docker run --detach --name gin-smoke-db \
  --network "$network" \
  --env POSTGRES_USER=postgres \
  --env POSTGRES_PASSWORD=postgres \
  --env POSTGRES_DB=gin_monolith \
  postgres:18-alpine
docker run --detach --name gin-smoke-redis --network "$network" redis:alpine

for _ in $(seq 1 60); do
  if docker exec gin-smoke-db pg_isready -U postgres; then
    break
  fi
  sleep 1
done

if ! docker exec gin-smoke-db pg_isready -U postgres; then
  docker logs gin-smoke-db
  exit 1
fi

docker run --rm \
  --network "$network" \
  --env APP_ENV=production \
  --env "DATABASE_URL=postgres://postgres:postgres@gin-smoke-db:5432/gin_monolith?sslmode=disable" \
  "$image" /app/migrate up

docker run --detach --name gin-smoke-app \
  --network "$network" \
  --publish 127.0.0.1:43118:3000 \
  --env APP_ENV=production \
  --env "DATABASE_URL=postgres://postgres:postgres@gin-smoke-db:5432/gin_monolith?sslmode=disable" \
  --env REDIS_HOST=gin-smoke-redis \
  --env OTEL_ENABLED=false \
  "$image"

for _ in $(seq 1 30); do
  if curl --fail --silent http://127.0.0.1:43118/health/live >/dev/null; then
    exit 0
  fi
  sleep 1
done

docker logs gin-smoke-app
exit 1
