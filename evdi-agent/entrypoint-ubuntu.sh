#!/bin/bash

# ── Helper: restart a crashed background process ──
supervise() {
    local name="$1"; shift
    mkdir -p /run/supervise
    while true; do
        echo "[supervise] Starting $name: $*"
        "$@" &
        local pid=$!
        echo "$pid" > "/run/supervise/${name}.pid"
        wait "$pid" 2>/dev/null
        local exit_code=$?
        echo "[supervise] $name (pid $pid) exited with code $exit_code, restarting in 2s..."
        sleep 2
    done
}

DISP="${DISPLAY:-:99}"
DISP_NUM="${DISP#:}"

# ── 1. Clean up stale lock files from previous run ──
LOCK_FILE="/tmp/.X${DISP_NUM}-lock"
if [ -f "$LOCK_FILE" ]; then
    echo "[entrypoint] Removing stale X lock file: $LOCK_FILE"
    rm -f "$LOCK_FILE"
fi

# ── 2. D-Bus system bus ──
mkdir -p /run/dbus
if ! pgrep -x dbus-daemon >/dev/null 2>&1; then
    dbus-daemon --system --fork 2>/dev/null || true
fi
for i in $(seq 1 20); do
    [ -S /run/dbus/system_bus_socket ] && break
    sleep 0.5
done
echo "[entrypoint] D-Bus ready"

# ── 3. Xvfb (supervised) ──
supervise xvfb Xvfb "$DISP" \
    -screen 0 "${VIDEO_WIDTH:-1920}x${VIDEO_HEIGHT:-1080}x24" \
    -nolisten tcp &

for i in $(seq 1 30); do
    if [ -f "/tmp/.X${DISP_NUM}-lock" ]; then
        if DISPLAY="$DISP" timeout 1 xdotool getdisplaygeometry >/dev/null 2>&1; then
            break
        fi
    fi
    sleep 0.5
done
if ! DISPLAY="$DISP" timeout 1 xdotool getdisplaygeometry >/dev/null 2>&1; then
    echo "[entrypoint] FATAL: Xvfb failed to start on $DISP"
    exit 1
fi
echo "[entrypoint] Xvfb ready on $DISP"

# ── 4. PulseAudio (supervised) ──
mkdir -p /run/pulse

cat > /etc/pulse/system.pa <<'EOF'
load-module module-default-device-restore
load-module module-rescue-streams
load-module module-always-sink
load-module module-null-sink sink_name=EVDI_Sink rate=48000 channels=2
load-module module-null-source source_name=EVDI rate=48000 channels=2
load-module module-native-protocol-unix auth-anonymous=1
EOF

supervise pulseaudio pulseaudio \
    --daemonize=false --fail=false --log-target=stderr --system --high-priority=no &

for i in $(seq 1 20); do
    pactl info >/dev/null 2>&1 && break
    sleep 0.5
done
pactl set-default-source EVDI 2>/dev/null || true
echo "[entrypoint] PulseAudio ready"

# ── 5. Window manager + Desktop (supervised) ──
export DISPLAY="$DISP"
supervise xfwm4 xfwm4 &
sleep 0.5
supervise xfce4 startxfce4 &
sleep 2

echo "[entrypoint] Desktop environment ready"

# ── 6. Start Agent (PID 1 — if it dies, container dies & restart policy kicks in) ──
echo "[entrypoint] Starting agent..."
exec agent
