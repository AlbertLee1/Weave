# Stage 1: Build the frontend
FROM node:20-alpine AS frontend

WORKDIR /web

COPY web/package.json web/package-lock.json ./
RUN npm ci

COPY web/ .
RUN npm run build

# Stage 2: Build the Go binary
FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Copy frontend build output into the Go embed location
COPY --from=frontend /web/dist cmd/server/web/dist

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /bin/weave ./cmd/server

# Stage 3: Minimal runtime
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
