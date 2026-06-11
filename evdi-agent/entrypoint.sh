#!/bin/bash
set -e

# 启动 D-Bus system bus
mkdir -p /run/dbus
dbus-daemon --system --fork 2>/dev/null || true

# 启动 Xvfb
Xvfb ${DISPLAY:-:99} -screen 0 ${VIDEO_WIDTH:-1920}x${VIDEO_HEIGHT:-1080}x24 -nolisten tcp &
sleep 1

# 启动 PulseAudio (前台运行，以 root 身份)
mkdir -p /run/pulse
pulseaudio --daemonize=false --fail=false --log-target=stderr --system &
sleep 1

# 加载 PulseAudio null source 用于音频捕获
pactl load-module module-null-source source_name=EVDI rate=48000 channels=2 2>/dev/null || true
pactl set-default-source EVDI 2>/dev/null || true

# 启动窗口管理器
xfwm4 &
sleep 0.5

# 启动 XFCE 桌面
startxfce4 &
sleep 2

# 启动 Agent
exec agent
