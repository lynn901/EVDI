# EVDI Agent Dockerfile 优化记录

> 参考 [docker-selkies-egl-desktop](https://github.com/selkies-project/docker-selkies-egl-desktop) 项目，对 evdi-agent 的容器化方案进行全面重构。
>
> 日期：2026-06-16

---

## 1. 修改的文件

| 文件 | 变更 |
|------|------|
| `evdi-agent/Dockerfile` | 全面重写，9→10 层分层构建 |
| `evdi-agent/Dockerfile.ubuntu` | 同步重写，与主 Dockerfile 保持一致 |
| `evdi-agent/entrypoint.sh` | 重写，支持 root 运行 + 用户降权 |
| `evdi-agent/entrypoint-ubuntu.sh` | 同步更新 |
| `evdi-agent/supervisord.conf` | **新增**，supervisord 进程管理配置 |

---

## 2. 核心架构变更

### 2.1 进程管理：自定义 supervise() → supervisord

**旧方案：** entrypoint.sh 内嵌 `supervise()` 循环 + `exec agent` (PID 1)

**新方案：** supervisord (PID 1) 管理所有子进程，agent 崩溃自动重启

| 进程 | 用户 | 优先级 | 说明 |
|------|------|--------|------|
| `dbus` | root | 1 | D-Bus system bus |
| `entrypoint` | root | 1 | Xvfb + 桌面 + NVIDIA 驱动安装 |
| `pipewire` | evdi | 10 | 音频核心，等 X Socket 就绪后启动 |
| `wireplumber` | evdi | 10 | 音频策略，等 pipewire lock 后启动 |
| `pipewire-pulse` | evdi | 10 | PulseAudio 兼容层 |
| `agent` | evdi | 50 | EVDI Agent，等 X Socket 就绪后启动 |

### 2.2 音频系统：PulseAudio → PipeWire + WirePlumber

```
旧: pulseaudio --system (容器内系统模式，权限问题多)
新: pipewire + wireplumber + pipewire-pulse (现代方案，Pulse 兼容层)
```

### 2.3 桌面启动：root 运行 → su 降权到 evdi 用户

```
旧: 桌面进程直接以 root 运行
新: su - evdi -c "startxfce4" (桌面以普通用户运行)
```

---

## 3. 功能增强对比

| 特性 | 优化前 | 优化后 |
|------|--------|--------|
| **GPU 渲染加速** | 无 | VirtualGL + EGL（自动检测 GPU，有则 vglrun 加速） |
| **NVIDIA GPU 支持** | 无 | 运行时驱动安装 + `NVIDIA_VISIBLE_DEVICES=all` + `NVIDIA_DRIVER_CAPABILITIES=all` + EGL/Vulkan/OpenCL ICD 配置 |
| **Intel/AMD GPU** | 无 | VAAPI + Vulkan + Mesa 驱动 + `gstreamer1.0-vaapi` |
| **Xvfb 扩展** | 仅 `-nolisten tcp` | COMPOSITE/DAMAGE/GLX/RANDR/RENDER/MIT-SHM/XFIXES/XTEST + iglx |
| **GStreamer 硬件编码** | 仅 x264enc | 支持 nvh264enc / vaapih264enc / x264enc |
| **字体** | 无 | Noto CJK + Emoji + DejaVu + Ubuntu 完整字体集 |
| **输入法** | 无 | fcitx + libpinyin + mozc（中/日/韩） |
| **i386 兼容** | 无 | amd64 下自动安装 i386 多架构库 |
| **浏览器** | snap Firefox（容器不可用） | Mozilla PPA 原生 .deb Firefox |
| **进程重启** | agent 退出 = 容器退出 | supervisord `autorestart=true`，agent 崩溃自动重启 |

---

## 4. 构建调试中解决的问题

| # | 问题 | 原因 | 修复 |
|---|------|------|------|
| 1 | `FROM ubuntu:` 镜像名为空 | 多阶段构建中 `ARG` 在第一个 `FROM` 后声明 | 将 `ARG DISTRIB_RELEASE` 移到第一个 `FROM` 之前 |
| 2 | `apt-get` 权限拒绝 | podman OCI 格式不支持 `SHELL` 指令，fakeroot 被忽略 | 移除 fakeroot SHELL 技巧，所有安装以 root 执行 |
| 3 | UID 1000 用户创建失败 | Ubuntu 24.04 已有 `ubuntu` 用户占用了 UID 1000 | `usermod -l evdi` 重命名现有用户 |
| 4 | dbus/wireplumber/pipewire-pulse 崩溃 | supervisord 以 USER 1000 运行，系统服务需 root | supervisord 改为 root 运行，用户进程通过 `user=evdi` 降权 |
| 5 | supervisord `environment` 中 `${USER}` 不展开 | bash -c 内单引号阻止变量展开 | 使用 `%(ENV_USER)s` supervisord 模板语法 |
| 6 | agent/pipewire 等待 X Socket 卡住 | `'/tmp/.X11-unix/X${DISPLAY#*:}'` 单引号内变量不展开 | 改用 `DISP_NUM="${DISPLAY#*:}"` 先赋值再使用 |
| 7 | `NVIDIA_DRIVER_VERSION` supervisord 启动失败 | 该变量非 Dockerfile ENV，`%(ENV_NVIDIA_DRIVER_VERSION)s` 无法展开 | 从 supervisord environment 中移除 |
| 8 | Firefox 无法启动 | Ubuntu 24.04 默认 snap Firefox，容器无 snapd | 添加 Mozilla PPA，安装原生 .deb Firefox |
| 9 | 桌面进程以 root 运行 | entrypoint 中直接 `dbus-launch startxfce4` | 改用 `su - evdi -c "..."` 降权启动 |
| 10 | home 目录权限错误 | root 创建的 .cache/.config 属主为 root | entrypoint 中 `chown -R evdi:evdi /home/evdi` |

---

## 5. Dockerfile 分层结构

```
Stage 1: builder        → Go 编译（golang:1.24-bookworm）
Stage 2: runtime
  ├─ Layer 1  基础系统    → 用户/时区/locale/基础工具
  ├─ Layer 2  核心运行时  → X11/GPU/GStreamer/字体/工具/i386
  ├─ Layer 3  PipeWire   → 替代 PulseAudio
  ├─ Layer 4  VirtualGL  → EGL 渲染加速
  ├─ Layer 5  桌面+浏览器 → XFCE4 + fcitx + 原生 Firefox (Mozilla PPA)
  ├─ Layer 6  NVIDIA ENV → 环境变量
  ├─ Layer 7  显示/编码   → DISPLAY/PipeWire/桌面环境变量
  ├─ Layer 8  Agent 二进制
  ├─ Layer 9  配置文件    → entrypoint.sh + supervisord.conf
  └─ Layer 10 运行时上下文 → USER/HOME/WORKDIR/EXPOSE/ENTRYPOINT
```

---

## 6. 运行方式

### 6.1 构建

```bash
# 标准构建
podman build -t evdi-agent:latest -f Dockerfile .

# Docker 构建
docker build -t evdi-agent:latest -f Dockerfile .
```

### 6.2 运行（host 网络模式，推荐用于开发/调试）

```bash
podman run -d --name evdi-desktop \
  --network host \
  -e PASSWD=evdi \
  -e TZ=Asia/Shanghai \
  -e DISPLAY_SIZEW=1920 \
  -e DISPLAY_SIZEH=1080 \
  -e DISPLAY_REFRESH=60 \
  --shm-size=512m \
  evdi-agent:latest
```

### 6.3 运行（NVIDIA GPU）

```bash
podman run -d --name evdi-desktop \
  --network host \
  -e PASSWD=evdi \
  -e TZ=Asia/Shanghai \
  -e SELKIES_ENCODER=nvh264enc \
  --shm-size=512m \
  --device /dev/dri \
  evdi-agent:latest
```

### 6.4 访问云桌面

1. 启动 Web 客户端开发服务器（代理 `/ws` 到 Agent 的 8080 端口）：

```bash
cd evdi-web-client && npm run dev -- --host 0.0.0.0
```

2. 浏览器访问 `http://localhost:3000`

---

## 7. 环境变量参考

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DISPLAY` | `:20` | X11 显示号 |
| `DISPLAY_SIZEW` | `1920` | 屏幕宽度 |
| `DISPLAY_SIZEH` | `1080` | 屏幕高度 |
| `DISPLAY_REFRESH` | `60` | 刷新率 |
| `DISPLAY_DPI` | `96` | DPI |
| `DISPLAY_CDEPTH` | `24` | 色深 |
| `VGL_DISPLAY` | `egl` | VirtualGL 显示模式 |
| `PASSWD` | `evdi` | evdi 用户密码 |
| `TZ` | `UTC` | 时区 |
| `NVIDIA_DRIVER_VERSION` | _(自动检测)_ | 指定 NVIDIA 驱动版本 |
| `PIPEWIRE_LATENCY` | `128/48000` | PipeWire 延迟设置 |
| `XDG_RUNTIME_DIR` | `/tmp/runtime-evdi` | 运行时目录 |
| `GTK_IM_MODULE` | `fcitx` | GTK 输入法模块 |
| `QT_IM_MODULE` | `fcitx` | Qt 输入法模块 |
| `XMODIFIERS` | `@im=fcitx` | X 输入法修饰符 |

---

## 8. 后续建议

1. **GStreamer 动态编码器选择** — 当前 `launcher.go` 硬编码 `x264enc`，应实现 `nvh264enc > vaapih264enc > x264enc` 的自动降级逻辑
2. **GPU 透传测试** — 当前仅在软件渲染模式下验证，需要 NVIDIA GPU 环境测试 VirtualGL + nvh264enc
3. **Agent 端口冲突** — `--network host` 模式下 8080 端口与 Vite dev server 可能冲突，生产环境建议 Agent 只暴露 WebRTC UDP 端口，静态文件由 nginx 托管
4. **镜像瘦身** — 当前镜像约 3 GB，可考虑拆分为 base + desktop + gpu 多阶段，按需组合
5. **K8s 部署清单** — 参考 `docker-selkies-egl-desktop/egl.yml` 编写 K8s Deployment，包含 GPU 资源声明、`/dev/shm` tmpfs、PVC 挂载等
