#!/bin/bash
# EVDI Agent Entrypoint — inspired by Selkies docker-selkies-egl-desktop
# Runs inside supervisord's entrypoint program (runs as root)

set -e

trap "echo TRAPed signal" HUP INT QUIT TERM

# ── Ensure runtime directory exists with correct ownership ──
EVDI_USER="${USER:-evdi}"
mkdir -pm700 "${XDG_RUNTIME_DIR:-/tmp/runtime-evdi}"
chown -f "${EVDI_USER}:${EVDI_USER}" "${XDG_RUNTIME_DIR:-/tmp/runtime-evdi}"

# Ensure home directory ownership
chown -R -f "${EVDI_USER}:${EVDI_USER}" "${HOME:-/home/evdi}" 2>/dev/null || true

# Change operating system password to environment variable
echo "${EVDI_USER}:${PASSWD:-evdi}" | chpasswd 2>/dev/null || true

# Remove stale X11 locks
rm -rf /tmp/.X* "${HOME}"/.cache 2>/dev/null || true

# Set timezone
ln -snf "/usr/share/zoneinfo/${TZ:-UTC}" /etc/localtime 2>/dev/null && echo "${TZ:-UTC}" | tee /etc/timezone > /dev/null || true

# ── NVIDIA driver runtime install (if GPU present but no userspace driver) ──
if [ -z "$(ldconfig -N -v $(sed 's/:/ /g' <<< $LD_LIBRARY_PATH) 2>/dev/null | grep 'libEGL_nvidia.so.0')" ] || [ -z "$(ldconfig -N -v $(sed 's/:/ /g' <<< $LD_LIBRARY_PATH) 2>/dev/null | grep 'libGLX_nvidia.so.0')" ]; then
  export NVIDIA_DRIVER_ARCH="$(dpkg --print-architecture | sed -e 's/arm64/aarch64/' -e 's/armhf/32bit-ARM/' -e 's/i.*86/x86/' -e 's/amd64/x86_64/' -e 's/unknown/x86_64/')"
  if [ -z "${NVIDIA_DRIVER_VERSION}" ]; then
    if [ -f "/proc/driver/nvidia/version" ]; then
      export NVIDIA_DRIVER_VERSION="$(head -n1 </proc/driver/nvidia/version | awk '{for(i=1;i<=NF;i++) if ($i ~ /^[0-9]+\.[0-9\.]+/) {print $i; exit}}')"
    elif command -v nvidia-smi >/dev/null 2>&1; then
      export NVIDIA_DRIVER_VERSION="$(nvidia-smi --version | grep 'DRIVER version' | cut -d: -f2 | tr -d ' ')"
    else
      echo '[entrypoint] No NVIDIA GPU driver detected, skipping runtime install'
    fi
  fi
  if [ -n "${NVIDIA_DRIVER_VERSION}" ]; then
    cd /tmp
    if [ ! -f "/tmp/NVIDIA-Linux-${NVIDIA_DRIVER_ARCH}-${NVIDIA_DRIVER_VERSION}.run" ]; then
      curl -fsSL -O "https://international.download.nvidia.com/XFree86/Linux-${NVIDIA_DRIVER_ARCH}/${NVIDIA_DRIVER_VERSION}/NVIDIA-Linux-${NVIDIA_DRIVER_ARCH}-${NVIDIA_DRIVER_VERSION}.run" || \
      curl -fsSL -O "https://international.download.nvidia.com/tesla/${NVIDIA_DRIVER_VERSION}/NVIDIA-Linux-${NVIDIA_DRIVER_ARCH}-${NVIDIA_DRIVER_VERSION}.run" || \
      echo '[entrypoint] Failed to download NVIDIA driver'
    fi
    if [ -f "/tmp/NVIDIA-Linux-${NVIDIA_DRIVER_ARCH}-${NVIDIA_DRIVER_VERSION}.run" ]; then
      rm -rf "NVIDIA-Linux-${NVIDIA_DRIVER_ARCH}-${NVIDIA_DRIVER_VERSION}"
      sh "NVIDIA-Linux-${NVIDIA_DRIVER_ARCH}-${NVIDIA_DRIVER_VERSION}.run" -x
      cd "NVIDIA-Linux-${NVIDIA_DRIVER_ARCH}-${NVIDIA_DRIVER_VERSION}"
      ./nvidia-installer --silent \
        --no-kernel-module \
        --install-compat32-libs \
        --no-nouveau-check \
        --no-nvidia-modprobe \
        --no-systemd \
        --no-rpms \
        --no-backup \
        --no-check-for-alternate-installs
      rm -rf /tmp/NVIDIA* && cd ~
      echo "[entrypoint] NVIDIA driver ${NVIDIA_DRIVER_VERSION} installed"
    fi
  fi
fi

# ── Start Xvfb with required extensions (matching Selkies config) ──
DISP="${DISPLAY:-:20}"
DISP_NUM="${DISP#:}"

# Clean stale lock
rm -f "/tmp/.X${DISP_NUM}-lock"

/usr/bin/Xvfb "${DISP}" \
    -screen 0 "${DISPLAY_SIZEW:-1920}x${DISPLAY_SIZEH:-1080}x${DISPLAY_CDEPTH:-24}" \
    -dpi "${DISPLAY_DPI:-96}" \
    +extension "COMPOSITE" \
    +extension "DAMAGE" \
    +extension "GLX" \
    +extension "RANDR" \
    +extension "RENDER" \
    +extension "MIT-SHM" \
    +extension "XFIXES" \
    +extension "XTEST" \
    +iglx +render \
    -nolisten "tcp" -ac -noreset -shmem &

# Wait for X server
echo '[entrypoint] Waiting for X Socket'
until [ -S "/tmp/.X11-unix/X${DISP_NUM}" ]; do sleep 0.5; done
echo '[entrypoint] X Server is ready'

# Make X11 socket accessible to all users
chmod 1777 /tmp/.X11-unix 2>/dev/null || true
chmod 777 "/tmp/.X11-unix/X${DISP_NUM}" 2>/dev/null || true

# ── Start desktop environment as evdi user ──
export DISPLAY="${DISP}"
export XDG_SESSION_ID="${DISP_NUM}"

if [ -n "$(command -v nvidia-smi 2>/dev/null && nvidia-smi --query-gpu=uuid --format=csv,noheader 2>/dev/null | head -n1)" ] || [ -n "$(ls -A /dev/dri 2>/dev/null)" ]; then
  # GPU available → use VirtualGL for hardware-accelerated rendering
  export VGL_FPS="${DISPLAY_REFRESH:-60}"
  su - "${EVDI_USER}" -c "export DISPLAY='${DISP}' VGL_FPS='${VGL_FPS}' VGL_DISPLAY='${VGL_DISPLAY:-egl}' XDG_RUNTIME_DIR='${XDG_RUNTIME_DIR}' DBUS_SYSTEM_BUS_ADDRESS='${DBUS_SYSTEM_BUS_ADDRESS}' PIPEWIRE_LATENCY='${PIPEWIRE_LATENCY}' PULSE_SERVER='${PULSE_SERVER}' PULSE_RUNTIME_PATH='${PULSE_RUNTIME_PATH}' GTK_IM_MODULE='${GTK_IM_MODULE}' QT_IM_MODULE='${QT_IM_MODULE}' XMODIFIERS='${XMODIFIERS}' && /usr/bin/vglrun -d '${VGL_DISPLAY:-egl}' +wm /usr/bin/dbus-launch --exit-with-session /usr/bin/startxfce4" &
  echo '[entrypoint] Desktop started with VirtualGL (GPU acceleration)'
else
  # Software rendering fallback
  su - "${EVDI_USER}" -c "export DISPLAY='${DISP}' XDG_RUNTIME_DIR='${XDG_RUNTIME_DIR}' DBUS_SYSTEM_BUS_ADDRESS='${DBUS_SYSTEM_BUS_ADDRESS}' PIPEWIRE_LATENCY='${PIPEWIRE_LATENCY}' PULSE_SERVER='${PULSE_SERVER}' PULSE_RUNTIME_PATH='${PULSE_RUNTIME_PATH}' GTK_IM_MODULE='${GTK_IM_MODULE}' QT_IM_MODULE='${QT_IM_MODULE}' XMODIFIERS='${XMODIFIERS}' && /usr/bin/dbus-launch --exit-with-session /usr/bin/startxfce4" &
  echo '[entrypoint] Desktop started with software rendering (no GPU detected)'
fi

# Start fcitx input method as evdi user
su - "${EVDI_USER}" -c "export DISPLAY='${DISP}' XDG_RUNTIME_DIR='${XDG_RUNTIME_DIR}' && /usr/bin/fcitx" &

echo "[entrypoint] Desktop environment ready on display ${DISP}"

# Keep entrypoint alive — supervisord manages the agent process separately
echo "Session Running. Press [Return] to exit."
read
