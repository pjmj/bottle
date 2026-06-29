# Multi-stage build: compile the frontend, compile the Go server, then ship a
# tiny final image containing only the static binary and the built web assets.

# --- Stage 1: build the React frontend ---
FROM node:24-alpine AS frontend
WORKDIR /web
# Copy manifests first so `npm ci` is cached unless dependencies change.
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- Stage 2: build the Go server ---
FROM golang:1.26-alpine AS backend
WORKDIR /src
# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO_ENABLED=0 produces a fully static binary — possible only because we chose
# the pure-Go SQLite driver. That static binary runs on a minimal base image.
RUN CGO_ENABLED=0 go build -o /bin/bottle-server ./cmd/server

# --- Stage 3: minimal runtime ---
FROM alpine:3.20
WORKDIR /app
COPY --from=backend /bin/bottle-server ./bottle-server
COPY --from=frontend /web/dist ./web/dist
ENV STATIC_DIR=/app/web/dist
ENV DB_PATH=/app/data/bottle.db
RUN mkdir -p /app/data
EXPOSE 8080
CMD ["./bottle-server"]
