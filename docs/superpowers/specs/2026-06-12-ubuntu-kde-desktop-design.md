# Ubuntu KDE Plasma 桌面设计

> **日期**: 2026-06-12
> **状态**: 已批准

## 目标

在 EVDI Agent 容器中提供 Ubuntu + KDE Plasma 桌面体验，作为现有 Debian + XFCE 方案的替代选项。

## 设计决策

- 新增 `Dockerfile.ubuntu`，不修改现有 `Dockerfile`
- 新增 `entrypoint-ubuntu.sh`，KDE Plasma 启动逻辑与 XFCE 不同
- 基础镜像：`ubuntu:24.04`
- 桌面环境：`kde-plasma-desktop`
- 启动命令：`startplasma-x11`（KWin 集成，无需单独启动窗口管理器）

## 受影响文件

| 文件 | 变更 |
|------|------|
| `evdi-agent/Dockerfile.ubuntu` | **新增** — Ubuntu + KDE 构建文件 |
| `evdi-agent/entrypoint-ubuntu.sh` | **新增** — KDE Plasma 入口脚本 |
| `docker-compose.yml` | 无变更 — 可通过 `dockerfile` 字段切换 |
| `evdi-agent/Dockerfile` | 无变更 |
| `evdi-agent/entrypoint.sh` | 无变更 |

## 不受影响

- Agent Go 代码（只依赖 X11 display）
- GStreamer pipeline（ximagesrc 捕获 X screen）
- xdotool 输入（X11 协议层）
- WebRTC / 信令（网络层）

## 构建与运行

```bash
podman build -t evdi-agent:ubuntu -f evdi-agent/Dockerfile.ubuntu ./evdi-agent/

podman run -d --name evdi-agent-ubuntu \
  --network host \
  -e DISPLAY=:99 \
  -e AGENT_WS_PORT=8080 \
  -e VIDEO_WIDTH=1920 -e VIDEO_HEIGHT=1080 -e VIDEO_FPS=30 \
  -e WEBRTC_PORT_MIN=50000 -e WEBRTC_PORT_MAX=50100 \
  --cap-add SYS_ADMIN --restart unless-stopped \
  evdi-agent:ubuntu
```
