# Stage 1: Build the Go binary
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/weave ./cmd/server

# Stage 2: Minimal runtime
FROM alpine:3.21

RUN apk add --no-cache ca-certificates curl

WORKDIR /app

# Copy binary and migrations
COPY --from=builder /bin/weave /app/weave
COPY --from=builder /src/migrations /app/migrations

# Default data directory
RUN mkdir -p /app/data

ENV WEAVE_DATA_DIR=/app/data

EXPOSE 9117

ENTRYPOINT ["/app/weave"]
