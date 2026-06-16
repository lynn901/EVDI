# Global ARG — must be declared before first FROM to be available in FROM
ARG DISTRIB_RELEASE=24.04

# ─── Builder Stage ────────────────────────────────────────────────────────────
FROM docker.io/library/golang:1.24-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
    gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
    gstreamer1.0-x gstreamer1.0-libav \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
ENV GOPROXY=https://goproxy.cn,direct
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /agent ./cmd/agent

# ─── Runtime Stage ────────────────────────────────────────────────────────────
FROM docker.io/library/ubuntu:${DISTRIB_RELEASE}

ARG DISTRIB_RELEASE
ARG DEBIAN_FRONTEND=noninteractive
ARG TZ=UTC

# ── 1. Base system: locale, timezone, user setup ──
# All installs run as root; switch to non-root user at the end
RUN apt-get clean && apt-get update && apt-get dist-upgrade -y && apt-get install --no-install-recommends -y \
        apt-utils \
        ca-certificates \
        dbus-user-session \
        fuse \
        kmod \
        locales \
        ssl-cert \
        sudo \
        udev \
        tzdata \
        curl \
        jq && \
    apt-get clean && rm -rf /var/lib/apt/lists/* /var/cache/debconf/* /var/log/* /tmp/* /var/tmp/* && \
    locale-gen en_US.UTF-8 && \
    ln -snf "/usr/share/zoneinfo/${TZ}" /etc/localtime && echo "${TZ}" > /etc/timezone && \
    # Create non-root user (Ubuntu 24.04 already has uid=1000 'ubuntu', reuse it)
    if id -u 1000 >/dev/null 2>&1; then \
        existing_user="$(getent passwd 1000 | cut -d: -f1)" && \
        usermod -l evdi -d /home/evdi -m "$existing_user" 2>/dev/null || true && \
        groupmod -n evdi "$existing_user" 2>/dev/null || true; \
    else \
        groupadd -g 1000 evdi && \
        useradd -ms /bin/bash evdi -u 1000 -g 1000; \
    fi && \
    usermod -a -G adm,audio,cdrom,dialout,dip,input,plugdev,render,ssl-cert,sudo,tty,video evdi && \
    echo "evdi ALL=(ALL:ALL) NOPASSWD: ALL" >> /etc/sudoers && \
    echo "evdi:evdi" | chpasswd

# Set locales
ENV LANG="en_US.UTF-8"
ENV LANGUAGE="en_US:en"
ENV LC_ALL="en_US.UTF-8"

# ── 2. Core runtime packages ──
RUN apt-get update && apt-get install --no-install-recommends -y \
        # ── X11 & display ──
        xvfb \
        xauth \
        xbitmaps \
        xdg-user-dirs \
        xdg-utils \
        xfonts-base \
        xfonts-scalable \
        xinit \
        xsettingsd \
        xkb-data \
        x11-xkb-utils \
        x11-xserver-utils \
        x11-utils \
        x11-apps \
        xdotool \
        xsel \
        xserver-xorg-input-all \
        xserver-xorg-video-all \
        # ── OpenGL / GPU libraries ──
        mesa-utils \
        mesa-va-drivers \
        libva2 \
        vainfo \
        vdpau-driver-all \
        libvdpau-va-gl1 \
        vdpauinfo \
        mesa-vulkan-drivers \
        vulkan-tools \
        libvulkan-dev \
        ocl-icd-libopencl1 \
        clinfo \
        libxau6 \
        libxdmcp6 \
        libxcb1 \
        libxext6 \
        libx11-6 \
        libxv1 \
        libxtst6 \
        libdrm2 \
        libegl1 \
        libgl1 \
        libopengl0 \
        libgles1 \
        libgles2 \
        libglvnd0 \
        libglx0 \
        libglu1 \
        libsm6 \
        libxrandr-dev \
        libxdamage1 \
        libxfixes3 \
        libx11-xcb1 \
        libxcb-dri3-0 \
        # ── NVIDIA driver installer deps ──
        libc6-dev \
        libpci3 \
        libelf-dev \
        libglvnd-dev \
        # ── GStreamer ──
        gstreamer1.0-plugins-base \
        gstreamer1.0-plugins-good \
        gstreamer1.0-plugins-bad \
        gstreamer1.0-plugins-ugly \
        gstreamer1.0-x \
        gstreamer1.0-libav \
        gstreamer1.0-tools \
        gstreamer1.0-vaapi \
        libgstreamer1.0-0 \
        libgstreamer-plugins-base1.0-0 \
        libgstreamer-plugins-bad1.0-0 \
        # ── Audio / multimedia ──
        libopus0 \
        libvpx-dev \
        x264 \
        x265 \
        lame \
        libpulse0 \
        alsa-utils \
        # ── Fonts (including CJK) ──
        fonts-dejavu \
        fonts-freefont-ttf \
        fonts-hack \
        fonts-liberation \
        fonts-noto \
        fonts-noto-cjk \
        fonts-noto-cjk-extra \
        fonts-noto-color-emoji \
        fonts-noto-extra \
        fonts-noto-mono \
        fonts-ubuntu \
        # ── Utilities ──
        supervisor \
        net-tools \
        netcat-openbsd \
        wmctrl \
        file \
        gnupg \
        wget \
        bzip2 \
        xz-utils \
        unzip \
        nano \
        vim \
        htop \
        less && \
    # ── i386 compatibility (amd64 only) ──
    if [ "$(dpkg --print-architecture)" = "amd64" ]; then \
    dpkg --add-architecture i386 && apt-get update && apt-get install --no-install-recommends -y \
        intel-gpu-tools \
        nvtop \
        va-driver-all \
        i965-va-driver-shaders \
        intel-media-va-driver-non-free \
        va-driver-all:i386 \
        i965-va-driver-shaders:i386 \
        intel-media-va-driver-non-free:i386 \
        libva2:i386 \
        vdpau-driver-all:i386 \
        mesa-vulkan-drivers:i386 \
        libvulkan-dev:i386 \
        libc6:i386 \
        libxau6:i386 \
        libxdmcp6:i386 \
        libxcb1:i386 \
        libxext6:i386 \
        libx11-6:i386 \
        libxv1:i386 \
        libxtst6:i386 \
        libdrm2:i386 \
        libegl1:i386 \
        libgl1:i386 \
        libglvnd0:i386 \
        libglx0:i386; fi && \
    apt-get clean && rm -rf /var/lib/apt/lists/* /var/cache/debconf/* /var/log/* /tmp/* /var/tmp/* && \
    # NVIDIA library paths
    echo "/usr/local/nvidia/lib" >> /etc/ld.so.conf.d/nvidia.conf && \
    echo "/usr/local/nvidia/lib64" >> /etc/ld.so.conf.d/nvidia.conf && \
    # OpenCL ICD
    mkdir -pm755 /etc/OpenCL/vendors && echo "libnvidia-opencl.so.1" > /etc/OpenCL/vendors/nvidia.icd && \
    # Vulkan ICD
    VULKAN_API_VERSION=$(dpkg -s libvulkan1 2>/dev/null | grep -oP 'Version: [0-9|\.]+' | grep -oP '[0-9]+(\.[0-9]+)(\.[0-9]+)' || echo "1.3.0") && \
    mkdir -pm755 /etc/vulkan/icd.d/ && echo "{\n\
    \"file_format_version\" : \"1.0.0\",\n\
    \"ICD\": {\n\
        \"library_path\": \"libGLX_nvidia.so.0\",\n\
        \"api_version\" : \"${VULKAN_API_VERSION}\"\n\
    }\n\
}" > /etc/vulkan/icd.d/nvidia_icd.json && \
    # EGL vendor
    mkdir -pm755 /usr/share/glvnd/egl_vendor.d/ && echo "{\n\
    \"file_format_version\" : \"1.0.0\",\n\
    \"ICD\": {\n\
        \"library_path\": \"libEGL_nvidia.so.0\"\n\
    }\n\
}" > /usr/share/glvnd/egl_vendor.d/10_nvidia.json

# ── 3. PipeWire + WirePlumber (replaces PulseAudio) ──
RUN mkdir -pm755 /etc/apt/trusted.gpg.d && \
    curl -fsSL "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0xFC43B7352BCC0EC8AF2EEB8B25088A0359807596" | gpg --dearmor -o /etc/apt/trusted.gpg.d/pipewire-debian-ubuntu-pipewire-upstream.gpg && \
    mkdir -pm755 /etc/apt/sources.list.d && \
    echo "deb https://ppa.launchpadcontent.net/pipewire-debian/pipewire-upstream/ubuntu $(grep '^VERSION_CODENAME=' /etc/os-release | cut -d= -f2 | tr -d '"') main" > "/etc/apt/sources.list.d/pipewire-debian-ubuntu-pipewire-upstream-$(grep '^VERSION_CODENAME=' /etc/os-release | cut -d= -f2 | tr -d '"').list" && \
    echo "deb https://ppa.launchpadcontent.net/pipewire-debian/wireplumber-upstream/ubuntu $(grep '^VERSION_CODENAME=' /etc/os-release | cut -d= -f2 | tr -d '"') main" > "/etc/apt/sources.list.d/pipewire-debian-ubuntu-wireplumber-upstream-$(grep '^VERSION_CODENAME=' /etc/os-release | cut -d= -f2 | tr -d '"').list" && \
    apt-get update && apt-get install --no-install-recommends -y \
        pipewire \
        pipewire-alsa \
        pipewire-audio-client-libraries \
        pipewire-jack \
        pipewire-locales \
        pipewire-v4l2 \
        libpipewire-0.3-modules \
        libspa-0.2-bluetooth \
        libspa-0.2-jack \
        libspa-0.2-modules \
        wireplumber \
        wireplumber-locales \
        gir1.2-wp-0.5 && \
    apt-get clean && rm -rf /var/lib/apt/lists/* /var/cache/debconf/* /var/log/* /tmp/* /var/tmp/*

# ── 4. VirtualGL (EGL rendering support) ──
RUN cd /tmp && VIRTUALGL_VERSION="$(curl -fsSL "https://api.github.com/repos/VirtualGL/virtualgl/releases/latest" | jq -r '.tag_name' | sed 's/[^0-9\.\-]*//g')" && \
    if [ "$(dpkg --print-architecture)" = "amd64" ]; then \
    dpkg --add-architecture i386 && \
    curl -fsSL -O "https://github.com/VirtualGL/virtualgl/releases/download/${VIRTUALGL_VERSION}/virtualgl_${VIRTUALGL_VERSION}_amd64.deb" && \
    curl -fsSL -O "https://github.com/VirtualGL/virtualgl/releases/download/${VIRTUALGL_VERSION}/virtualgl32_${VIRTUALGL_VERSION}_amd64.deb" && \
    apt-get update && apt-get install -y --no-install-recommends "./virtualgl_${VIRTUALGL_VERSION}_amd64.deb" "./virtualgl32_${VIRTUALGL_VERSION}_amd64.deb" && \
    rm -f "virtualgl_${VIRTUALGL_VERSION}_amd64.deb" "virtualgl32_${VIRTUALGL_VERSION}_amd64.deb" && \
    chmod -f u+s /usr/lib/libvglfaker.so /usr/lib/libvglfaker-nodl.so /usr/lib/libdlfaker.so /usr/lib/libgefaker.so && \
    chmod -f u+s /usr/lib32/libvglfaker.so /usr/lib32/libvglfaker-nodl.so /usr/lib32/libdlfaker.so /usr/lib32/libgefaker.so && \
    chmod -f u+s /usr/lib/i386-linux-gnu/libvglfaker.so /usr/lib/i386-linux-gnu/libvglfaker-nodl.so /usr/lib/i386-linux-gnu/libdlfaker.so /usr/lib/i386-linux-gnu/libgefaker.so; \
    elif [ "$(dpkg --print-architecture)" = "arm64" ]; then \
    curl -fsSL -O "https://github.com/VirtualGL/virtualgl/releases/download/${VIRTUALGL_VERSION}/virtualgl_${VIRTUALGL_VERSION}_arm64.deb" && \
    apt-get update && apt-get install -y --no-install-recommends ./virtualgl_${VIRTUALGL_VERSION}_arm64.deb && \
    rm -f "virtualgl_${VIRTUALGL_VERSION}_arm64.deb" && \
    chmod -f u+s /usr/lib/libvglfaker.so /usr/lib/libvglfaker-nodl.so /usr/lib/libdlfaker.so /usr/lib/libgefaker.so; fi && \
    apt-get clean && rm -rf /var/lib/apt/lists/* /var/cache/debconf/* /var/log/* /tmp/* /var/tmp/*

# ── 5. Desktop environment (XFCE4 — lightweight for VDI) ──
# Block snap-firefox, install native .deb from Mozilla PPA instead
RUN mkdir -pm755 /etc/apt/preferences.d && echo "Package: firefox*\n\
Pin: version 1:1snap*\n\
Pin-Priority: -1" > /etc/apt/preferences.d/firefox-nosnap && \
    mkdir -pm755 /etc/apt/keyrings && \
    curl -fsSL "https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x738BEB9321D1AAEC13EA9391AEBDF4819BE21867" | gpg --dearmor -o /etc/apt/keyrings/mozillateam-ubuntu-ppa.gpg && \
    mkdir -pm755 /etc/apt/sources.list.d && \
    echo "deb [signed-by=/etc/apt/keyrings/mozillateam-ubuntu-ppa.gpg] https://ppa.launchpadcontent.net/mozillateam/ppa/ubuntu $(grep '^VERSION_CODENAME=' /etc/os-release | cut -d= -f2 | tr -d '"') main" > "/etc/apt/sources.list.d/mozillateam-ubuntu-ppa-$(grep '^VERSION_CODENAME=' /etc/os-release | cut -d= -f2 | tr -d '"').list" && \
    echo "Package: firefox*\n\
Pin: release o=LP-PPA-mozillateam\n\
Pin-Priority: 1001" >> /etc/apt/preferences.d/firefox-nosnap
RUN apt-get update && apt-get install --no-install-recommends -y \
        xfce4 \
        xfce4-terminal \
        xfwm4 \
        dbus-x11 \
        thunar-archive-plugin \
        xfce4-notifyd \
        xfce4-panel-profiles \
        xfce4-screenshooter \
        xfce4-taskmanager \
        # ── Input method ──
        fcitx \
        fcitx-frontend-gtk2 \
        fcitx-frontend-gtk3 \
        fcitx-frontend-qt5 \
        fcitx-module-dbus \
        fcitx-module-x11 \
        fcitx-tools \
        fcitx-libpinyin \
        fcitx-mozc \
        im-config \
        # ── Browser (native .deb from Mozilla PPA, not snap) ──
        firefox && \
    apt-get clean && rm -rf /var/lib/apt/lists/* /var/cache/debconf/* /var/log/* /tmp/* /var/tmp/*

# ── 6. NVIDIA environment (must come after apt installs) ──
ENV PATH="/usr/local/nvidia/bin${PATH:+:${PATH}}"
ENV LD_LIBRARY_PATH="${LD_LIBRARY_PATH:+${LD_LIBRARY_PATH}:}/usr/local/nvidia/lib:/usr/local/nvidia/lib64"
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=all
ENV __GL_SYNC_TO_VBLANK=0

# ── 7. Display / encoding defaults ──
ENV DISPLAY=":20"
ENV DISPLAY_SIZEW=1920
ENV DISPLAY_SIZEH=1080
ENV DISPLAY_REFRESH=60
ENV DISPLAY_DPI=96
ENV DISPLAY_CDEPTH=24
ENV VGL_DISPLAY=egl

# PipeWire audio defaults
ENV PIPEWIRE_LATENCY="128/48000"
ENV XDG_RUNTIME_DIR=/tmp/runtime-evdi
ENV PIPEWIRE_RUNTIME_DIR="${PIPEWIRE_RUNTIME_DIR:-${XDG_RUNTIME_DIR:-/tmp}}"
ENV PULSE_RUNTIME_PATH="${PULSE_RUNTIME_PATH:-${XDG_RUNTIME_DIR:-/tmp}/pulse}"
ENV PULSE_SERVER="${PULSE_SERVER:-unix:${PULSE_RUNTIME_PATH:-${XDG_RUNTIME_DIR:-/tmp}/pulse}/native}"
ENV DBUS_SYSTEM_BUS_ADDRESS="unix:path=${XDG_RUNTIME_DIR:-/tmp}/dbus-system-bus"

# Desktop session
ENV DESKTOP_SESSION=xfce
ENV XDG_SESSION_DESKTOP=xfce
ENV XDG_CURRENT_DESKTOP=XFCE
ENV XDG_SESSION_TYPE=x11

# Input method
ENV GTK_IM_MODULE=fcitx
ENV QT_IM_MODULE=fcitx
ENV XMODIFIERS="@im=fcitx"

# ── 8. Copy built agent binary ──
COPY --from=builder /agent /usr/local/bin/agent
RUN chmod 755 /usr/local/bin/agent

# ── 9. Copy entrypoint and supervisord config ──
COPY entrypoint.sh /etc/entrypoint.sh
RUN chmod 755 /etc/entrypoint.sh
COPY supervisord.conf /etc/supervisord.conf

# ── 10. Runtime user context (supervisord runs as root, user processes drop via supervisord user=) ──
ENV SHELL=/bin/bash
ENV USER=evdi
ENV HOME=/home/evdi
WORKDIR /home/evdi

EXPOSE 8080
EXPOSE 50000-50100/udp

ENTRYPOINT ["/usr/bin/supervisord"]
