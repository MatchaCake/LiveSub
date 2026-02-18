# Build stage
FROM golang:1.25-bookworm AS builder

RUN apt-get update && apt-get install -y libsqlite3-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -o /livesub ./cmd/livesub

# Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    libsqlite3-0 \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /livesub /usr/local/bin/livesub

WORKDIR /app
VOLUME /app/configs
EXPOSE 8899

ENTRYPOINT ["livesub", "run", "configs/config.yaml"]
