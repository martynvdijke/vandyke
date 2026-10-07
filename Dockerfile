# syntax=docker/dockerfile:1

# ---------- build ----------
FROM golang:1.27-alpine AS build

WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# modernc.org/sqlite is CGO-free, so a fully static binary is possible.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/vandyke ./cmd/vandyke

# ---------- runtime ----------
FROM alpine:3.24

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -g 10001 -S vandyke \
    && adduser -u 10001 -S -G vandyke -h /app vandyke \
    && mkdir -p /data /app \
    && chown -R vandyke:vandyke /data /app

WORKDIR /app
COPY --from=build /out/vandyke /app/vandyke

ENV PORT=8080 \
    DB_PATH=/data/vandyke.db

VOLUME ["/data"]
EXPOSE 8080

USER 10001:10001

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/app/vandyke"]
