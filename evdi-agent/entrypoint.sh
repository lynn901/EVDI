#!/bin/bash
set -e

# 启动 D-Bus
eval $(dbus-launch --sh-syntax)

# 启动 Xvfb
Xvfb ${DISPLAY:-:99} -screen 0 ${VIDEO_WIDTH:-1920}x${VIDEO_HEIGHT:-1080}x24 -nolisten tcp &
sleep 1

# 启动 PulseAudio
pulseaudio --start --fail=false --daemonize=false &
sleep 0.5

# 加载 PulseAudio null source 用于音频捕获
pacmd load-module module-null-source source_name=EVDI rate=48000 channels=2
pacmd set-default-source EVDI

# 启动窗口管理器
xfwm4 &
sleep 0.5

# 启动 XFCE 桌面
startxfce4 &
sleep 2

# 启动 Agent
exec agent
