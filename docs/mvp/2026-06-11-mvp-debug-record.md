# MVP 调试记录：WebRTC 视频流 + 鼠标键盘输入

> **文档版本**: v1.0
> **创建日期**: 2026-06-11
> **状态**: 已解决

---

## 一、概述

本文档记录 EVDI MVP 阶段将 Agent 容器化的完整调试过程，涵盖视频流（黑屏问题）、音频、信令协议、DataChannel 输入、以及最终改用 WebSocket 输入通道的全链路问题排查与解决方案。

**最终成果**：浏览器通过 WebRTC 接收 H.264 视频流 + Opus 音频流，鼠标键盘输入通过 WebSocket 信令通道实时发送到 Agent，xdotool 执行输入命令，桌面画面、光标操作与键盘输入均正常工作。

---

## 二、环境信息

| 项目 | 值 |
|------|-----|
| 宿主机 | WSL2 (Linux 6.6.87.2-microsoft-standard-WSL2) |
| 容器运行时 | Podman Desktop (rootless, `--network host`) |
| Agent 容器镜像 | `localhost/evdi-agent:dev` (golang:1.24-bookworm + 全部运行时依赖) |
| Agent 容器 IP | 172.26.185.252 (WSL2 NAT 模式，所有 distro 共享) |
| Web 客户端 | React 18 + Vite (localhost:3000) |
| 调试页面 | http://localhost:3000/test.html |

---

## 三、容器化 Agent 构建

### 3.1 Dockerfile.dev 设计

开发镜像使用单阶段构建，包含 Go 编译器和所有运行时依赖：

```dockerfile
FROM golang:1.24-bookworm
RUN apt-get update && apt-get install -y \
    xvfb xdotool dbus-x11 pulseaudio \
    xfce4 xfwm4 \
    gstreamer1.0-tools gstreamer1.0-plugins-base \
    gstreamer1.0-plugins-good gstreamer1.0-plugins-libav \
    gstreamer1.0-x gstreamer1.0-plugins-ugly
```

> **注意**：需要添加 `non-free` 源才能安装 `gstreamer1.0-plugins-ugly`（提供 x264enc）。

### 3.2 容器入口脚本

```bash
# 1. dbus-daemon (XFCE 依赖)
dbus-daemon --system --fork

# 2. Xvfb 虚拟显示
Xvfb :100 -screen 0 1920x1080x24 -nolisten tcp &

# 3. PulseAudio
pulseaudio --daemonize=false --system &

# 4. XFCE 桌面
xfwm4 &
startxfce4 &

# 5. Agent 进程
DISPLAY=:100 /app/evdi-agent/agent
```

> **关键发现**：DISPLAY 编号不能与宿主机冲突。WSL2 内可能有 Xvfb :99 在运行，需使用 :100 或更高编号。

### 3.3 容器开发工作流

```bash
# 同步源码到容器
tar cf - evdi-agent/ | podman exec -i evdi-dev tar xf - -C /app

# 容器内编译
podman exec evdi-dev bash -c "cd /app/evdi-agent && go build -o agent ./cmd/agent/"

# 重启 Agent
podman exec evdi-dev bash -c "pkill agent; DISPLAY=:100 /app/evdi-agent/agent &"
```

也可通过 Podman REST API（Unix Socket）完成上述操作：

```bash
SOCKET=/mnt/wsl/podman-sockets/podman-machine-default/podman-root.sock
CID=<container_id>

# 拷贝源码
tar cf /tmp/evdi-agent.tar evdi-agent/
curl --unix-socket $SOCKET -X PUT \
  "http://localhost/v4.0.0/libpod/containers/$CID/archive?path=/app" \
  -H "Content-Type: application/x-tar" \
  --data-binary @/tmp/evdi-agent.tar

# 执行编译
curl --unix-socket $SOCKET -X POST \
  "http://localhost/v4.0.0/libpod/containers/$CID/exec" \
  -H "Content-Type: application/json" \
  -d '{"AttachStdout":true,"AttachStderr":true,"Cmd":["bash","-c","cd /app/evdi-agent && go build -o agent ./cmd/agent/"],"Tty":false}'
```

---

## 四、视频流问题排查

### 4.1 GStreamer Pipeline 最终版本

```
ximagesrc display-name=:100 use-damage=false show-pointer=true
  startx=0 starty=0 endx=1919 endy=1079 !
video/x-raw,framerate=30/1 !
videoconvert !
video/x-raw,format=I420 !
x264enc tune=zerolatency speed-preset=ultrafast byte-stream=true !
video/x-h264,stream-format=byte-stream,profile=constrained-baseline !
fdsink fd=1 sync=false
```

### 4.2 逐项踩坑与解决

#### 问题 1：GStreamer "erroneous pipeline: syntax error"

**原因**：Caps 中的逗号被 shell 解析。`video/x-raw, framerate=30/1` 中逗号后有空格，当整体作为 `exec.Command` 的单参数传递时解析错误。

**解决**：使用 `exec.Command("sh", "-c", "gst-launch-1.0 -v "+pipelineStr)`，并去掉逗号后的空格。

#### 问题 2：x264enc not found

**原因**：容器只有 base/good/libav 插件，x264enc 在 ugly 插件包中。

**解决**：添加 `gstreamer1.0-plugins-ugly`，需先添加 `non-free` apt 源。

#### 问题 3：x264enc 输出 High 4:4:4 Predictive profile

**原因**：x264enc 默认根据输入格式选择 profile，I420 输入默认不选择 constrained-baseline。

**解决**：在 x264enc 前添加 `videoconvert ! video/x-raw,format=I420 !`，并在 caps filter 中指定 `profile=constrained-baseline`。不能通过 x264enc 的属性设置 profile（无此属性）。

#### 问题 4：NALU 类型始终为 9

**原因**：`findNALUStart` 只匹配 4 字节起始码 `00 00 00 01`，但 x264enc 同时输出 3 字节起始码 `00 00 01`。

**解决**：修改 NALU 起始码匹配逻辑，同时支持 3 字节和 4 字节起始码。

#### 问题 5：每个 NALU 单独发送 WriteSample，视频花屏

**原因**：浏览器需要完整的 Access Unit（AU），而非单个 NALU。

**解决**：以 AUD（NALU type=9）为分界点缓冲 NALU，在遇到下一个 AUD 时将缓冲区作为一个 AU 整体发送 WriteSample。

#### 问题 6：VP8 被 SDP 协商选中而非 H.264

**原因**：默认 MediaEngine 注册了所有编解码器，浏览器 SDP 中 VP8 排在 H.264 前面。

**解决**：创建自定义 MediaEngine，只注册 H.264（payload 96）和 Opus（payload 111）：

```go
m := &webrtc.MediaEngine{}
m.RegisterCodec(webrtc.RTPCodecParameters{
    RTPCodecCapability: webrtc.RTPCodecCapability{
        MimeType:    webrtc.MimeTypeH264,
        ClockRate:   90000,
        SDPFmtpLine: "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
    },
    PayloadType: 96,
}, webrtc.RTPCodecTypeVideo)
m.RegisterCodec(webrtc.RTPCodecParameters{
    RTPCodecCapability: webrtc.RTPCodecCapability{
        MimeType:  webrtc.MimeTypeOpus,
        ClockRate: 48000,
        Channels:  2,
    },
    PayloadType: 111,
}, webrtc.RTPCodecTypeAudio)
```

#### 问题 7（核心）：黑屏 — ontrack 覆盖视频流

**现象**：ICE connected，video loadedmetadata 显示 0x0 或 1920x1080，但画面黑屏。

**根因**：Pion 为 video 和 audio 分别创建不同的 MSID，导致 `pc.ontrack` 触发两次，每次的 `event.streams[0]` 是不同的 MediaStream 对象。直接赋值 `video.srcObject = event.streams[0]` 会导致音频流覆盖视频流。

**解决**：合并所有 track 到同一个 MediaStream：

```typescript
const combinedStream = new MediaStream()
pc.ontrack = (event) => {
    combinedStream.addTrack(event.track)
    // 创建新引用以触发 Zustand 状态变更
    setMediaStream(new MediaStream(combinedStream.getTracks()))
}
```

#### 问题 8：video.play() 未调用

**原因**：VideoCanvas 组件只设置了 `srcObject` 但未调用 `play()`。

**解决**：添加 `video.play()` 并捕获 AbortError（浏览器在 track 变化时可能中断 play）。

#### 问题 9：光标不可见

**原因**：`ximagesrc` 默认不捕获 X 光标。

**解决**：添加 `show-pointer=true` 属性（注意不是 `show-cursor`，后者不存在于 ximagesrc）。需要 XFixes 扩展支持（libxfixes3 已安装）。

---

## 五、鼠标键盘输入问题排查

### 5.1 DataChannel 单向问题

**现象**：DataChannel 建立成功，Agent→浏览器方向正常（浏览器收到 `ctrl.ping`），但浏览器→Agent 方向失败（Agent 收不到任何 mouse_move 消息）。

**排查过程**：
1. 确认 DataChannel 状态为 OPEN
2. 确认浏览器端 `controlCh.send()` 执行成功
3. 确认 Agent 的 `dc.OnMessage()` 回调未触发
4. 排除 JSON 格式问题、DataChannel label 不匹配等

**根因分析**：Pion WebRTC Lite ICE 模式下 SCTP 传输存在单向性问题。这是 Pion v4 在 Lite 模式下的已知行为。

**解决方案**：改用 WebSocket 信令通道传输输入事件，绕过 DataChannel 的单向限制。

### 5.2 WebSocket 输入通道实现

#### 协议设计

输入事件复用 WebSocket 信令通道，消息格式与 DataChannel 消息保持一致：

```json
{
    "type": "input.mouse_move",
    "data": {
        "v": 1,
        "type": "input.mouse_move",
        "ts": 1781183003224,
        "seq": 13,
        "payload": { "x": 514, "y": 298, "display_id": 0 }
    }
}
```

外层 `type` 用于 WebSocket 消息路由，内层 `data` 是 `DataChannelMessage` 结构，与 DataChannel 协议格式统一，方便未来切换回 DataChannel。

#### Agent 端修改

**signaling.go**：添加 `OnInputFunc` 回调，在消息循环的 `default` 分支处理 input 事件：

```go
type OnInputFunc func(msg DataChannelMessage)

// 在 handleWebSocket 的 switch 中：
default:
    if s.onInput != nil {
        var dcMsg DataChannelMessage
        if err := json.Unmarshal(msg.Data, &dcMsg); err == nil && dcMsg.Type != "" {
            s.onInput(dcMsg)
        }
    }
```

**main.go**：统一输入处理函数，DataChannel 和 WebSocket 输入共用同一个 handler。

#### 客户端修改

**useWebRTC.ts**：添加 `sendInputMessage` 方法：

```typescript
const sendInputMessage = useCallback((msgType: string, payload: unknown) => {
    const signaling = signalingRef.current
    if (!signaling) return
    signaling.send({
        type: msgType as SignalingMessage['type'],
        data: { v: 1, type: msgType, ts: Date.now(), seq: nextSeq(), payload },
    })
}, [nextSeq])
```

**VideoCanvas.tsx**：所有输入事件改用 `sendInputMessage` 替代 `sendDataChannelMessage`。

### 5.3 鼠标延迟优化

**现象**：鼠标可以移动但延迟很大，明显跟不上操作。

**根因**：每个 mousemove 事件都启动一个新的 `xdotool mousemove --sync` 进程，`--sync` 阻塞等待移动完成，且 `exec.Command.Run()` 也是阻塞的，导致事件堆积。

**优化措施**：

1. **去掉 `--sync`**：`xdotool mousemove` 不再等待移动完成即返回
2. **`Start()` 替代 `Run()`**：非阻塞启动 xdotool，不等待进程退出
3. **鼠标位置合并（coalescing）**：通过 channel + drain 策略，当多个 mouse_move 排队时只执行最后一个位置：

```go
mouseCh := make(chan webrtc.MouseMovePayload, 64)
go func() {
    for p := range mouseCh {
        input.MouseMoveCmd(p.X, p.Y).Start()
        // 排空队列中积压的位置，只保留最新的
        drained := 0
        for {
            select {
            case p2 := <-mouseCh:
                p = p2
                drained++
            default:
                goto execute
            }
        }
    execute:
        if drained > 0 {
            input.MouseMoveCmd(p.X, p.Y).Start()
        }
    }
}()
```

### 5.4 坐标 (0,0) 问题

**现象**：Agent 收到的所有鼠标坐标都是 (0, 0)。

**排查**：通过 raw data 日志发现 Agent 确实收到的 payload 就是 `{"x":0,"y":0}`，问题在客户端。

**原因**：test.html 中 mousemove 事件在 `video.videoWidth` 为 0（metadata 未加载）时触发，`0 * anything = 0`。

**解决**：在坐标计算前检查 `video.videoWidth` 和 `video.videoHeight`，为 0 时跳过发送。

### 5.5 键盘输入完全无效

**现象**：按键盘没有任何反应。

**根因**：`keycodeToXKeySym` 映射函数使用了错误的 keycode 编号体系。函数按 USB HID Usage Code 编写（a=4, b=5, ..., z=29），但浏览器 `e.keyCode` 返回的是 Windows Virtual Key Code / DOM Level 3 标准值（a=65, b=66, ..., z=90）。

对比：

| 按键 | 浏览器 keyCode | USB HID Code (旧映射期望) | 结果 |
|------|---------------|-------------------------|------|
| A | 65 | 4 | 65 落入 default 分支，返回 `0x0041` |
| 1 | 49 | 30 | 49 落入 default 分支，返回 `0x0031` |
| Enter | 13 | 40 | 13 无匹配，返回 `0x000d` |

xdotool 不认识 `0x0041` 等 keysym，所以所有键盘输入都静默失败。

**修复**：重写 `keyCodeToXKeySym`，正确映射浏览器 keyCode：

```go
// 字母: A=65 ... Z=90
if keycode >= 65 && keycode <= 90 {
    return fmt.Sprintf("%c", keycode)
}
// 数字: 0=48 ... 9=57
if keycode >= 48 && keycode <= 57 {
    return fmt.Sprintf("%c", keycode)
}
// 功能键、方向键、编辑键等通过 switch/case 逐一映射
```

同时修复了修饰键处理：`xdotool keydown "shift+A"` 的语义不是"按住 shift 再按 A"，而是把 `shift+A` 当作单个 keysym 查找。改为使用 `xdotool key --clearmodifiers shift+A`（key 命令会正确执行 press+release 序列）。

### 5.6 x264enc "Can not initialize" 错误

**现象**：容器重启后 x264enc 报 "Can not initialize x264 encoder"，之前能正常工作。

**排查**：手动测试发现 `videotestsrc ! x264enc` 在 1920x1080 下也失败，但 320x240 可以；添加 `threads=1` 后 1920x1080 也能工作。

**根因**：容器内多线程模式下 x264 初始化失败，可能与容器资源限制或 zombie 进程耗尽系统资源有关。

**修复**：在 GStreamer pipeline 的 x264enc 参数中添加 `threads=1`。

### 5.7 光标不可见

**原因**：`ximagesrc` 默认不捕获 X 光标。

**踩坑**：最初尝试 `show-cursor=true`（不存在），报 "no property show-cursor"。正确属性名是 `show-pointer=true`（依赖 XFixes 扩展）。

---

## 六、关键经验总结

### 6.1 Pion WebRTC Lite ICE 注意事项

| 问题 | 说明 |
|------|------|
| SCTP DataChannel 单向 | Lite ICE 模式下 Agent→浏览器方向正常，浏览器→Agent 方向可能失败 |
| 推荐 | 输入事件不要依赖 DataChannel，改用 WebSocket 等可靠通道 |
| MediaEngine | 必须自定义 MediaEngine 只注册目标编解码器，否则浏览器可能协商到不支持的编码 |

### 6.2 GStreamer Pipeline 要点

| 要点 | 说明 |
|------|------|
| x264enc profile | 不能通过属性设置，必须用 caps filter 指定 `profile=constrained-baseline` |
| 输入格式 | x264enc 前必须强制 I420 格式（`videoconvert ! video/x-raw,format=I420 !`） |
| 光标捕获 | `ximagesrc show-pointer=true`（不是 show-cursor），需要 XFixes 扩展 |
| NALU 解析 | 必须同时支持 3 字节和 4 字节起始码，以 AUD 为界组 AU |
| 进程隔离 | GStreamer 作为独立进程运行（`fdsink fd=1`），满足 LGPL 合规要求 |
| 多线程问题 | 容器内 x264enc 多线程初始化可能失败，添加 `threads=1` |

### 6.3 WebRTC 前端要点

| 要点 | 说明 |
|------|------|
| ontrack 合并流 | Pion 为 video/audio 创建不同 MSID，必须合并到同一个 MediaStream |
| play() 调用 | 设置 srcObject 后必须显式调用 play()，并捕获 AbortError |
| 视频坐标映射 | 必须等 `loadedmetadata` 后才能使用 `videoWidth/videoHeight` 进行坐标映射 |

### 6.4 输入事件优化要点

| 要点 | 说明 |
|------|------|
| 位置合并 | 鼠标移动事件高频，必须合并（coalesce）只执行最新位置 |
| 非阻塞执行 | `exec.Command.Start()` 优于 `Run()`，避免阻塞消息处理循环 |
| 去掉 --sync | xdotool 的 `--sync` 在高频场景下造成严重延迟 |
| KeyCode 体系 | 浏览器 `e.keyCode` 是 Windows Virtual Key Code（A=65），不是 USB HID Code（A=4） |
| 修饰键 | `xdotool keydown "ctrl+A"` 不等于"按住ctrl按A"，用 `xdotool key --clearmodifiers ctrl+A` |

---

## 七、当前数据流全景

```
┌─────────────────────────────────────────────────────────────┐
│                       浏览器                                  │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  <video> ← MediaStream (video + audio tracks)       │   │
│  │  mouse/key events → WebSocket signaling             │   │
│  └──────────────────────────┬──────────────────────────┘   │
└─────────────────────────────┼───────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              │ WebSocket     │               │ WebRTC (UDP)
              │ 信令+输入     │               │ 媒体流
              ▼               │               ▼
┌─────────────────────────────┼───────────────────────────────┐
│  Agent 容器                  │                               │
│  ┌──────────────────────────┴──────────────────────────┐   │
│  │  SignalingServer (:8080/ws)                         │   │
│  │  - offer/answer SDP 交换                            │   │
│  │  - ICE candidate 转发                               │   │
│  │  - input.* 输入事件 → handleInputMessage()          │   │
│  └─────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  WebRTCEngine (Pion, Lite ICE)                      │   │
│  │  - VideoTrack (H.264 constrained-baseline)          │   │
│  │  - AudioTrack (Opus 48kHz stereo)                   │   │
│  │  - DataChannel control/bulk (仅 Agent→浏览器)       │   │
│  └──────────────────────────┬──────────────────────────┘   │
│                             │ WriteSample                   │
│  ┌──────────────────────────┴──────────────────────────┐   │
│  │  GStreamer Pipeline (独立进程)                       │   │
│  │  ximagesrc → videoconvert → x264enc → fdsink → pipe │   │
│  └─────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  xdotool (输入执行)                                  │   │
│  │  mousemove / mousedown / mouseup / click / key       │   │
│  └─────────────────────────────────────────────────────┘   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  Xvfb :100 + PulseAudio + XFCE                      │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

---

## 八、待办事项

| 优先级 | 事项 | 说明 |
|--------|------|------|
| P1 | 音频流测试 | 当前 Pipeline 只实现了视频，PulseAudio → Opus 通路待验证 |
| P1 | 键盘输入测试 | xdotool keydown/keyup 命令已实现，待端到端验证 |
| P2 | DataChannel 双向修复 | 升级 Pion 或调整 ICE 配置，恢复 DataChannel 双向通信 |
| P2 | 生产镜像构建 | 优化 Dockerfile 多阶段构建，减小镜像体积 |
| P2 | 断线重连 | WebSocket/WebRTC 断线后的自动重连机制 |
| P3 | 输入方式优化 | 考虑 CGo 直接调用 XTest 扩展，替代 xdotool 进程创建开销 |
| P3 | 清理 debug 日志 | 移除 H264 Frame 计数等调试日志 |
