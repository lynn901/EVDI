FROM golang:1.24-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
    gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
    gstreamer1.0-x gstreamer1.0-libav \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /agent ./cmd/agent

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    xvfb xdotool \
    gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
    gstreamer1.0-x gstreamer1.0-libav gstreamer1.0-tools \
    pulseaudio pulseaudio-utils \
    xfce4 xfce4-terminal \
    dbus \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /agent /usr/local/bin/agent

RUN mkdir -p /etc/pulse && \
    echo "default-server = /run/pulse/native" > /etc/pulse/client.conf && \
    echo "autospawn = no" >> /etc/pulse/client.conf

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 8080
EXPOSE 50000-50100/udp

ENTRYPOINT ["/entrypoint.sh"]
