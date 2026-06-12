# EVDI MVP 研发调测指南

> **文档版本**: v1.0
> **创建日期**: 2026-06-12
> **适用范围**: EVDI MVP 阶段的开发、调试、部署全流程

---

## 一、环境准备

### 1.1 宿主机环境

| 依赖 | 版本 | 说明 |
|------|------|------|
| WSL2 | Linux 6.6+ | Windows 上的 Linux 开发环境 |
| Podman Desktop | latest | 容器运行时（rootless 模式） |
| Node.js | 18+ | Web 客户端开发 |
| Go | 1.24+ | Agent 编译（容器内编译） |

### 1.2 Podman 使用要点

**WSL2 中调用 Podman**：通过 `podman` 命令直接操作（Podman Desktop 会配置好 PATH）。

```bash
# 常用操作
podman ps -a                              # 查看所有容器
podman logs --tail 50 <container>         # 查看容器日志
podman exec -it <container> bash          # 进入容器
podman restart <container>                # 重启容器
podman stop <container> && podman rm <container>  # 删除容器
```

**端口映射**：容器使用 `--network host` 时无需端口映射；使用 bridge 网络时需 `-p` 映射。

---

## 二、项目构建与部署

### 2.1 目录结构

```
EVDI/
├── docker-compose.yml          # 一键启动配置
├── Makefile                    # 常用构建命令
├── evdi-agent/
│   ├── Dockerfile              # 生产镜像（多阶段构建）
│   ├── Dockerfile.dev          # 开发镜像（含 Go 编译器）
│   ├── entrypoint.sh           # 容器入口脚本（进程管理）
│   ├── cmd/agent/main.go       # Agent 入口
│   └── pkg/                    # Agent 核心包
├── evdi-web-client/            # React 前端
│   └── src/
└── docs/mvp/                   # 文档
```

### 2.2 生产镜像构建

```bash
cd /home/yuan/EVDI

# 方式一：docker-compose 一键构建启动
podman-compose up --build

# 方式二：手动构建
podman build -t evdi-agent:mvp ./evdi-agent/

# 手动运行
podman run -d \
  --name evdi-agent-mvp \
  -p 8080:8080 \
  -p 50000-50100:50000-50100/udp \
  -e DISPLAY=:99 \
  -e AGENT_WS_PORT=8080 \
  -e VIDEO_WIDTH=1920 \
  -e VIDEO_HEIGHT=1080 \
  -e VIDEO_FPS=30 \
  -e WEBRTC_PORT_MIN=50000 \
  -e WEBRTC_PORT_MAX=50100 \
  --cap-add SYS_ADMIN \
  --restart unless-stopped \
  evdi-agent:mvp
```

**环境变量说明**：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `DISPLAY` | `:99` | Xvfb 虚拟显示编号（`--network host` 时确保宿主机无同编号 X display 残留） |
| `AGENT_WS_PORT` | `8080` | WebSocket 信令端口 |
| `VIDEO_WIDTH/HEIGHT` | `1920x1080` | 虚拟桌面分辨率 |
| `VIDEO_FPS` | `30` | 视频采集帧率 |
| `WEBRTC_PORT_MIN/MAX` | `50000-50100` | WebRTC UDP 端口范围 |
| `NAT_1TO1_IP` | 空 | NAT 1:1 映射 IP（跨网段时需要） |

### 2.3 开发镜像（迭代调试用）

开发镜像包含 Go 编译器和完整源码，支持容器内编译、快速迭代：

```bash
# 构建开发镜像
podman build -t evdi-agent:dev -f evdi-agent/Dockerfile.dev ./evdi-agent/

# 启动开发容器
podman run -d --name evdi-dev \
  -p 8080:8080 -p 50000-50100:50000-50100/udp \
  --cap-add SYS_ADMIN \
  evdi-agent:dev \
  bash -c "Xvfb :99 -screen 0 1920x1080x24 & sleep 1; sleep infinity"
```

### 2.4 Web 客户端

```bash
cd evdi-web-client
npm install
npm run dev          # Vite 开发服务器，端口 3000
```

---

## 三、开发迭代工作流

### 3.1 Agent 代码修改 → 容器内编译 → 验证

这是最高频的开发循环，核心操作是 **源码同步 → 容器内编译 → 重启验证**：

```bash
# 1. 同步源码到容器（tar 管道，最快方式）
cd /home/yuan/EVDI/evdi-agent
tar cf - cmd/ pkg/ go.mod go.sum | podman exec -i evdi-agent-mvp tar xf - -C /app

# 2. 容器内编译
podman exec evdi-agent-mvp bash -c "cd /app && go build -o /usr/local/bin/agent ./cmd/agent/"

# 3. 重启容器验证
podman restart evdi-agent-mvp
```

**单行命令（适合反复执行）**：

```bash
cd /home/yuan/EVDI/evdi-agent && \
tar cf - cmd/ pkg/ go.mod go.sum | podman exec -i evdi-agent-mvp tar xf - -C /app && \
podman exec evdi-agent-mvp bash -c "cd /app && go build -o /usr/local/bin/agent ./cmd/agent/" && \
podman restart evdi-agent-mvp
```

### 3.2 entrypoint.sh 修改

entrypoint.sh 修改后需要同步到容器并确保可执行：

```bash
podman exec -i evdi-agent-mvp bash -c "cat > /entrypoint.sh" < evdi-agent/entrypoint.sh
podman exec evdi-agent-mvp chmod +x /entrypoint.sh
podman restart evdi-agent-mvp
```

**注意**：如果修改了 Dockerfile 或 entrypoint.sh 的逻辑，建议重新构建镜像而非在容器内修改，以保持一致性：

```bash
podman stop evdi-agent-mvp && podman rm evdi-agent-mvp
podman build -t evdi-agent:mvp ./evdi-agent/
podman run -d --name evdi-agent-mvp ... evdi-agent:mvp
```

### 3.3 前端修改

前端是 Vite HMR，修改即生效，无需手动刷新（大部分情况）：

```bash
cd evdi-web-client
npm run dev    # 修改源码后自动热更新
```

### 3.4 仅更新 Agent 二进制（跳过源码同步）

如果已在本地编译好二进制，可以直接拷入容器：

```bash
# 从宿主机拷入
podman cp evdi-agent/bin/agent evdi-agent-mvp:/usr/local/bin/agent

# 从一个容器拷到另一个
podman cp evdi-dev:/app/agent /tmp/agent
podman cp /tmp/agent evdi-agent-mvp:/usr/local/bin/agent
```

---

## 四、调试方法

### 4.1 容器日志查看

```bash
# 实时跟踪日志
podman logs -f evdi-agent-mvp

# 最近 N 行
podman logs --tail 100 evdi-agent-mvp

# 过滤关键信息
podman logs evdi-agent-mvp 2>&1 | grep -E "\[entrypoint\]|Agent ready|FATAL|Error|error"

# 分离 stdout 和 stderr
podman logs evdi-agent-mvp 1>/tmp/stdout.log 2>/tmp/stderr.log
```

### 4.2 容器内诊断

```bash
# 进入容器
podman exec -it evdi-agent-mvp bash

# 检查各服务状态
pgrep -a Xvfb          # Xvfb 是否运行
pgrep -a pulseaudio    # PulseAudio 是否运行
pgrep -a xfwm4         # 窗口管理器
pgrep -a agent         # Agent 进程

# 检查 X display
DISPLAY=:99 xdotool getdisplaygeometry   # X 是否可连接
ls -la /tmp/.X99-lock                    # lock 文件是否存在

# 检查 PulseAudio
pactl info                                # PA 状态
pactl list sources short                  # 音频源列表

# 检查端口
ss -tlnp | grep 8080                      # WebSocket 端口
ss -ulnp | grep 50000                     # WebRTC UDP 端口

# 手动测试 GStreamer pipeline
DISPLAY=:99 gst-launch-1.0 -v ximagesrc display-name=:99 use-damage=false \
  show-pointer=true ! video/x-raw,framerate=30/1 ! videoconvert \
  ! x264enc tune=zerolatency speed-preset=ultrafast threads=1 \
  ! video/x-h264,stream-format=byte-stream,profile=constrained-baseline \
  ! fdsink fd=1 sync=false > /dev/null

# 手动测试 xdotool
DISPLAY=:99 xdotool mousemove 500 500
DISPLAY=:99 xdotool key Return
```

### 4.3 WebRTC 连接调试

**浏览器端**：打开 Chrome DevTools → Console，关键日志前缀：

| 前缀 | 含义 |
|------|------|
| `[WebRTC] ontrack` | 收到远端媒体轨道 |
| `[WebRTC] Connection state` | 连接状态变更 |
| `[WebRTC] ICE connection state` | ICE 协商状态 |
| `[Signaling]` | WebSocket 信令消息 |
| `Sent input:` | 输入事件发送 |

**Agent 端**：查看容器日志中的关键信息：

| 关键词 | 含义 |
|--------|------|
| `Signaling server listening` | 信令服务器已就绪 |
| `New WebSocket connection` | 客户端已连接 |
| `SDP offer received` | 收到 SDP Offer |
| `ICE candidate` | ICE 候选交换 |
| `PeerConnection established` | WebRTC 连接建立 |
| `[Input] key:` | 收到键盘输入 |
| `[Input] Raw msg.Data` | 原始输入事件数据 |
| `H264 Frame` | H.264 帧计数 |

**常见问题排查**：

| 现象 | 排查方向 |
|------|----------|
| 连接不上 | 检查端口映射、防火墙、`NAT_1TO1_IP` 配置 |
| ICE connected 但无 ontrack | 检查 GStreamer pipeline 是否启动、x264enc 是否报错 |
| 有画面但黑屏 | 检查 XFCE 桌面是否启动（`pgrep -a xfce4-session`） |
| 鼠标不动 | 检查 `DISPLAY` 环境变量、xdotool 是否安装、输入事件是否到达 Agent |
| 键盘无效 | 检查 keycode 映射（浏览器 keyCode vs X11 KeySym） |
| 画面卡住 | 检查 zombie 进程（`ps aux | grep Z`）、容器 PID 资源 |

### 4.4 test.html 快速验证

`public/test.html` 是一个独立于 React 的纯 HTML 调试页面，用于快速验证信令和媒体链路：

```
http://localhost:3000/test.html
```

功能：
- WebSocket 连接/断开
- SDP Offer/Answer 交换
- ICE Candidate 转发
- 视频播放
- 鼠标/键盘输入事件发送
- 浏览器 Console 显示所有收发消息

**建议**：新功能先在 test.html 验证通过，再迁移到 React 组件。

### 4.5 网络连通性检查

```bash
# 从宿主机检查容器端口
curl http://localhost:8080/ws 2>&1  # WebSocket（应返回升级协议错误）

# 从容器内检查
podman exec evdi-agent-mvp curl -s http://localhost:8080/ws 2>&1

# 检查 WebRTC UDP 端口是否开放
podman exec evdi-agent-mvp ss -ulnp | grep 50000
```

---

## 五、容器服务架构

### 5.1 进程树

容器内由 entrypoint.sh 管理所有进程：

```
entrypoint.sh (PID 1 → exec agent)
├── supervise: xvfb    → Xvfb :99
├── supervise: pulseaudio → pulseaudio --system
├── supervise: xfwm4   → xfwm4
├── supervise: xfce4   → startxfce4
└── agent (最终 PID 1)
    └── GStreamer pipeline (子进程, fdsink → pipe)
```

### 5.2 服务启动顺序

```
1. 清理残留 X lock 文件
2. D-Bus system bus → 等待 socket 就绪
3. Xvfb → 等待 xdotool getdisplaygeometry 可连接
4. PulseAudio → 等待 pactl info 可用
5. xfwm4 + startxfce4
6. exec agent (替换 PID 1)
```

### 5.3 服务自动恢复机制

**两层恢复**：

1. **进程级**：`supervise` 函数监控每个后台服务，崩溃后 2 秒自动重启
2. **容器级**：`--restart unless-stopped` 策略，agent 退出（PID 1 死亡）→ 容器重启 → entrypoint.sh 从头执行

**残留文件清理**：容器重启前 Xvfb 不会优雅退出，留下 `/tmp/.X{N}-lock`。entrypoint.sh 在启动前自动清理。

### 5.4 Zombie 进程回收

Agent 高频调用 `exec.Command.Start()` 启动 xdotool 子进程，完成后不调用 `Wait()` 即变为 zombie。通过 `init()` 中的 goroutine 持续回收：

```go
func init() {
    go func() {
        for {
            syscall.Wait4(-1, nil, 0, nil)
        }
    }()
}
```

---

## 六、关键组件调测

### 6.1 GStreamer Pipeline

**最终 Pipeline**：

```
ximagesrc display-name=:99 use-damage=false show-pointer=true
  startx=0 starty=0 endx=1919 endy=1079 !
video/x-raw,framerate=30/1 !
videoconvert !
video/x-raw,format=I420 !
x264enc tune=zerolatency speed-preset=ultrafast byte-stream=true threads=1 !
video/x-h264,stream-format=byte-stream,profile=constrained-baseline !
fdsink fd=1 sync=false
```

**踩坑备忘**：

| 参数 | 说明 |
|------|------|
| `show-pointer=true` | 捕获 X 光标（不是 `show-cursor`） |
| `endx/endy` | 值为 width-1 / height-1，不是 width/height |
| `format=I420` | x264enc 前必须强制 I420 输入格式 |
| `profile=constrained-baseline` | 用 caps filter 指定，不能通过 x264enc 属性 |
| `threads=1` | 容器内多线程 x264 初始化可能失败 |
| `byte-stream=true` | Pion 需要字节流格式，不是 AVCC |

**调试方法**：

```bash
# 验证 pipeline 基本可用（输出到文件）
gst-launch-1.0 -v ximagesrc display-name=:99 use-damage=false \
  ! video/x-raw,framerate=10/1 \
  ! videoconvert ! x264enc tune=zerolatency speed-preset=ultrafast threads=1 \
  ! video/x-h264,stream-format=byte-stream \
  ! filesink location=/tmp/test.h264

# 用 ffplay 验证输出
ffplay /tmp/test.h264

# 检查可用插件
gst-inspect-1.0 ximagesrc
gst-inspect-1.0 x264enc
```

### 6.2 WebRTC 信令协议

**WebSocket 路径**：`ws://<agent-ip>:8080/ws`

**消息格式**：

```json
// SDP Offer
{ "type": "offer", "data": { "sdp": "...", "type": "offer" } }

// SDP Answer
{ "type": "answer", "data": { "sdp": "...", "type": "answer" } }

// ICE Candidate
{ "type": "ice", "data": { "candidate": "...", "sdpMid": "0", "sdpMLineIndex": 0 } }

// 心跳
{ "type": "ping" }
{ "type": "pong" }

// 输入事件（复用信令通道）
{ "type": "input.mouse_move", "data": { "v":1, "type":"input.mouse_move", "ts":..., "seq":..., "payload": {"x":500,"y":300,"display_id":0} } }
{ "type": "input.mouse_button", "data": { "v":1, "type":"input.mouse_button", "ts":..., "seq":..., "payload": {"button":1,"action":"down","x":500,"y":300} } }
{ "type": "input.mouse_wheel", "data": { "v":1, "type":"input.mouse_wheel", "ts":..., "seq":..., "payload": {"delta_x":0,"delta_y":-120,"x":500,"y":300} } }
{ "type": "input.key", "data": { "v":1, "type":"input.key", "ts":..., "seq":..., "payload": {"keycode":65,"action":"down","shift":false,"ctrl":false,"alt":false,"capsLock":false} } }
```

### 6.3 输入事件映射

**浏览器 → Agent → X11** 全链路：

```
浏览器 KeyboardEvent.keyCode + getModifierState('CapsLock')
  → WebSocket input.key 消息
    → Agent handleInputMessage()
      → syncCapsLockSync() 同步 X11 Caps Lock 状态
      → keyCodeToXKeySym() 转换（字母键用小写 keysym）
        → xdotool key/mousedown/mouseup 执行
```

**KeyCode 映射关键点**：

| 浏览器 keyCode | X11 KeySym | 说明 |
|----------------|-----------|------|
| 65-90 | a-z | 字母键（**必须用小写 keysym**，见下方说明） |
| 48-57 | 0-9 | 数字键 |
| 13 | Return | 回车 |
| 27 | Escape | ESC |
| 32 | space | 空格 |
| 8 | BackSpace | 退格 |
| 9 | Tab | 制表符 |
| 112-123 | F1-F12 | 功能键 |
| 37-40 | Left/Up/Right/Down | 方向键 |

**修饰键处理**：使用 `xdotool key --clearmodifiers` 而非 `keydown`，避免 keysym 查找错误。

**⚠️ 字母键必须用小写 keysym**：xdotool 对大写 keysym（如 `A`）会自动按 Shift 修饰键，Caps Lock ON 时 Shift+Caps Lock = 小写，导致大小写反转。正确做法是用小写 keysym（`a`-`z`），让 X11 根据 Caps Lock 状态自然决定大小写。

**Caps Lock 状态同步**：客户端通过 `e.getModifierState('CapsLock')` 发送当前 Caps Lock 状态，Agent 在每次字母键按下前用 `syncCapsLockSync()`（同步 `.Run()`）确保 X11 的 Caps Lock 与客户端一致。不能用异步 `.Start()`，否则按键可能在 Caps Lock 切换完成前执行。

### 6.4 鼠标坐标映射

客户端将浏览器坐标映射到虚拟桌面坐标：

```typescript
const rect = container.getBoundingClientRect()
const x = Math.round((e.offsetX / rect.width) * video.videoWidth)
const y = Math.round((e.offsetY / rect.height) * video.videoHeight)
```

**注意**：`video.videoWidth` 在 `loadedmetadata` 之前为 0，必须做零值检查。

---

## 七、常见问题与解决

### 7.1 容器问题

| 问题 | 现象 | 解决 |
|------|------|------|
| 容器重启后服务不恢复 | Xvfb/PulseAudio 启动失败 | entrypoint.sh 自动清理 lock 文件 + supervise 进程管理 |
| Xvfb 启动失败 | `Server is already active for display 99` | 删除 `/tmp/.X99-lock`，或修改 DISPLAY 编号 |
| 容器 PID 耗尽 | `fork: Resource temporarily unavailable` | `syscall.Wait4(-1,...)` 回收 zombie 进程 |
| x264enc 初始化失败 | `Can not initialize x264 encoder` | 添加 `threads=1` |
| DISPLAY 编号冲突 | Xvfb 绑定 :99 但宿主机已占用 | 改用 :100 或更高编号 |
| D-Bus 连接拒绝 | PulseAudio 报 `Failed to connect to system bus` | 确保 entrypoint.sh 先启动 `dbus-daemon --system --fork` |

### 7.2 WebRTC 问题

| 问题 | 现象 | 解决 |
|------|------|------|
| 黑屏 | ICE connected 但无画面 | 合并 video+audio track 到同一个 MediaStream |
| 编解码器协商错误 | 浏览器选择 VP8 而非 H.264 | 自定义 MediaEngine，只注册 H.264 + Opus |
| ontrack 未触发 | 连接成功但无媒体轨道 | 检查 GStreamer pipeline 是否启动 |
| DataChannel 单向 | 浏览器→Agent 方向数据丢失 | 改用 WebSocket 信令通道传输输入事件 |

### 7.3 前端问题

| 问题 | 现象 | 解决 |
|------|------|------|
| 输入事件不发 | React 组件间 Hook 实例隔离 | 通过 Zustand store 共享 sendInput 函数 |
| passive listener | `Unable to preventDefault inside passive event listener` | 用 `addEventListener('wheel', ..., { passive: false })` |
| 浏览器右键菜单 | 右键点击视频区弹出菜单 | `addEventListener('contextmenu', e => e.preventDefault())` |
| 双光标 | 本地鼠标 + 远程鼠标重叠 | CSS `cursor: none` 隐藏本地光标 |
| 坐标 (0,0) | 所有鼠标坐标为零 | 检查 `video.videoWidth` 是否为 0（metadata 未加载） |

---

## 八、性能优化备忘

| 优化项 | 当前方案 | 说明 |
|--------|----------|------|
| 鼠标位置合并 | channel drain 策略 | 只执行最新位置，丢弃中间帧 |
| xdotool 非阻塞 | `Start()` 替代 `Run()` | 不等待 xdotool 进程退出 |
| xdotool 去掉 --sync | `mousemove X Y` | 不等待 X server 确认移动完成 |
| x264enc 低延迟 | `tune=zerolatency speed-preset=ultrafast` | 牺牲压缩率换取最低延迟 |
| fdsink 非同步 | `sync=false` | 不按帧率节流，尽快输出 |
| GStreamer 进程隔离 | `fdsink fd=1` + pipe | 满足 LGPL 合规，进程崩溃不影响 Agent |

---

## 九、快速操作速查

```bash
# ═══════ 构建 ═══════
podman build -t evdi-agent:mvp ./evdi-agent/

# ═══════ 启动 ═══════
podman run -d --name evdi-agent-mvp \
  --network host \
  -e DISPLAY=:99 -e AGENT_WS_PORT=8080 \
  -e VIDEO_WIDTH=1920 -e VIDEO_HEIGHT=1080 -e VIDEO_FPS=30 \
  -e WEBRTC_PORT_MIN=50000 -e WEBRTC_PORT_MAX=50100 \
  --cap-add SYS_ADMIN --restart unless-stopped \
  evdi-agent:mvp

# ═══════ 迭代开发（源码同步+编译+重启） ═══════
cd /home/yuan/EVDI/evdi-agent && \
tar cf - cmd/ pkg/ go.mod go.sum | podman exec -i evdi-agent-mvp tar xf - -C /app && \
podman exec evdi-agent-mvp bash -c "cd /app && go build -o /usr/local/bin/agent ./cmd/agent/" && \
podman restart evdi-agent-mvp

# ═══════ 查看日志 ═══════
podman logs -f --tail 50 evdi-agent-mvp

# ═══════ 进入容器 ═══════
podman exec -it evdi-agent-mvp bash

# ═══════ 前端开发 ═══════
cd evdi-web-client && npm run dev

# ═══════ 完全重建 ═══════
podman stop evdi-agent-mvp && podman rm evdi-agent-mvp
podman build -t evdi-agent:mvp ./evdi-agent/
podman run -d --name evdi-agent-mvp ... evdi-agent:mvp
```
