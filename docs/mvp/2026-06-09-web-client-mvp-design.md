# Web 客户端直连 Agent MVP 设计文档

> **文档版本**: v1.0
> **创建日期**: 2026-06-09
> **状态**: 审批中

---

## 一、整体架构

```
┌─────────────────────────────────────────────────────────────────┐
│                         ���览器环境                                │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  React 客户端（localhost:3000）                          │   │
│  │  - Pion WebRTC PeerConnection                           │   │
│  │  - Canvas 视频渲染                                      │   │
│  │  - 键鼠事件通过 DataChannel 发送                         ���   │
│  └────────────────────────┬────────────────────────────────┘   │
└───────────────────────────┼─────────────────────────────────────┘
                            │ WebSocket 信令 + WebRTC 媒体流
                            │ 通过 docker-compose 端口映射访问
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Docker Compose 环境                             │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │  Agent 容器（evdi-agent:8080）                           │   │
│  │  ┌─────────────────────────────────────────────────┐   │   │
│  │  │  WebSocket 信令服务 (:8080/ws)                   │   │   │
│  │  └─────────────────────────────────────────────────┘   │   │
│  │  ┌─────────────────────────────────────────────────┐   │   │
│  │  │  Pion WebRTC 引擎 (Lite ICE)                    │   │   │
│  │  │  - PeerConnection                                │   │   │
│  │  │  - VideoTrack (H.264)                            │   │   │
│  │  │  - AudioTrack (Opus)                             │   │   │
│  │  │  - DataChannel (control/bulk)                    │   │   │
│  │  └──────────────────────┬──────────────────────────┘   │   │
│  │                         ���                                  │   │
│  │  ┌──────────────────────▼──────────────────────────┐   │   │
│  │  │  GStreamer Pipeline (独立进程)                  │   │   │
│  │  │  screen → x264enc → appsrc → Pion               │   │   │
│  │  └─────────────────────────────────────────────────┘   │   │
│  │  ┌─────────────────────────────────────────────────┐   │   │
│  │  │  虚拟显示 Xvfb (:99) + 轻量桌面 XFCE             │   │   │
│  │  └─────────────────────────────────────────────────┘   │   │
│  └─────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## 二、与正式架构的差异

| 维度 | MVP（直连模式） | 正式架构（Broker 模式） |
|------|----------------|----------------------|
| 信令服务器 | Agent 内建 WebSocket 服务 | Broker Gateway Service |
| 认证 | 简化的 Token 鉴权（可选） | JWT + Session Token |
| TURN 服务器 | 无 | Coturn Server |
| 桌面管理 | 硬编码配置 | Broker Desktop Service API |
| 多设备互斥 | 无 | Broker Session 管理 |

## 三、项目结构

```
evdi-mvp/
├── evdi-agent/                    # Go Agent
│   ├── cmd/agent/
│   │   └── main.go                # 入口
│   ├── pkg/
│   │   ├── webrtc/                # Pion WebRTC 引擎
│   │   │   ├── engine.go          # WebRTCEngine 核心结构
│   │   │   ├── signaling.go       # WebSocket 信令服务
│   │   │   └── datachannel.go     # DataChannel 消息处理
│   │   ├── gstreamer/             # GStreamer 集成
│   │   │   ├── pipeline.go        # Pipeline 管理
│   │   │   └── launcher.go        # 进程启动
│   │   ├── display/               # 虚拟显示管理
│   │   │   └── xvfb.go
│   │   └── input/                 # 输入事件处理
│   │       ├── keyboard.go
│   │       └── mouse.go
│   ├── proto/                     # gRPC proto（暂不使用，预留）
│   └── go.mod
│
├── evdi-web-client/               # React 客户端
│   ├── src/
│   │   ├── components/
│   │   │   ├── VideoCanvas.tsx    # Canvas 视频渲染
│   │   │   ├── ControlPanel.tsx   # 连接控制面板
│   │   │   └── StatusIndicator.tsx
│   │   ├── hooks/
│   │   │   └── useWebRTC.ts       # WebRTC 连接逻辑
│   │   ├── stores/
│   │   │   └── connectionStore.ts # Zustand 状态管理
│   │   ├── types/
│   │   │   └── webrtc.ts
│   │   ├── utils/
│   │   │   └── signaling.ts       # 信令消息处理
│   │   └── App.tsx
│   ├── package.json
│   └── vite.config.ts
│
├── docker-compose.yml             # Agent 部署配置
├── Makefile                       # 构建脚本
└── README.md
```

## 四、Agent 组件详细设计

### 4.1 WebSocket 信令服务

```go
type SignalingServer struct {
    addr      string
    wsConn    *websocket.Conn
    engine    *WebRTCEngine
    onOffer   chan *webrtc.SessionDescription
    onAnswer  chan *webrtc.SessionDescription
    onICE     chan *webrtc.ICECandidate
}
```

**信令消息格式**：
```json
{
  "type": "offer" | "answer" | "ice" | "ping" | "pong",
  "data": { ... }
}
```

**支持的客户端事件**：
- `offer` - 客户端发送的 SDP Offer
- `ice` - ICE 候选推送到 Agent
- `ping` - 连接保活心跳

**Agent 推送事件**：
- `answer` - Agent 创建的 SDP Answer
- `ice` - Agent 的 ICE 候选
- `pong` - 心跳响应

## 五、WebRTC 引擎设计

### 5.1 Pion 核心配置

```go
type WebRTCEngine struct {
    peerConnection *webrtc.PeerConnection
    videoTrack     *webrtc.TrackLocalStaticSample
    audioTrack     *webrtc.TrackLocalStaticSample
    dataChannel    *webrtc.DataChannel

    channelControl  *webrtc.DataChannel   // 键鼠事件
    channelBulk     *webrtc.DataChannel   // 剪贴板

    gstreamerPipe  *GStreamerPipeline

    config         *Config
}

type Config struct {
    VideoCodec     string  // "H264"
    AudioCodec     string  // "Opus"
    STUNServers    []string
    LiteICE        bool    // true - 作为服务端
}
```

**Lite ICE 配置**：
```go
settingsEngine := webrtc.SettingEngine{}
settingsEngine.SetLite(true)  // Lite ICE 模式
```

### 5.2 媒体轨道配置

| 轨道 | 类型 | 编码 | 方向 |
|------|------|------|------|
| Video | TrackLocalStaticSample | H.264 | Agent → Client |
| Audio | TrackLocalStaticSample | Opus | Agent → Client |
| DataChannel 1 | control | JSON | 双向（键鼠/心跳） |
| DataChannel 2 | bulk | JSON | 双向（剪贴板） |

### 5.3 SDP 协商流程（简化版）

```
Client                    Agent
  |                          |
  |  -- WS: offer (SDP) --> |
  |                          | 1. 创建 PeerConnection
  |                          | 2. 创建 Answer
  |                          | 3. 等待 ICE 候选
  |  <-- WS: answer (SDP) -- |
  |  -- WS: ice (cand-1) --> |
  |  <-- WS: ice (cand-1) -- |
  ...                       | (Trickle ICE)
  |  DTLS 握手完成           |
  |  ✓ 连接建立                |
```

## 六、GStreamer 管道设计

### 6.1 Pipeline 架构

```
xvfb屏幕(:99) → ximagesrc → videoconvert → x264enc
                                                 ↓
appsrc (收到 buffer) → Pion VideoTrack → Client
```

### 6.2 GStreamer Pipeline 代码

```go
pipelineStr := `
ximagesrc display-name=:99.0 !
video/x-raw, framerate=30/1, width=1920, height=1080 !
videoconvert !
x264enc tune=zerolatency speed-preset=ultrafast !
video/x-h264 !
appsink name=sink emit-signals=true max-buffers=1 drop=true sync=false
`

type GStreamerPipeline struct {
    pipeline   *gst.Pipeline
    appSink    *gst.Element
    videoTrack *webrtc.TrackLocalStaticSample
    stopChan   chan struct{}
}
```

### 6.3 关键参数说明

| 参数 | 值 | 说明 |
|------|-----|------|
| framerate | 30/1 | 30 FPS，平衡性能与流畅度 |
| width/height | 1920x1080 | 默认分辨率 |
| tune | zerolatency | 零延迟模式，关键帧丢失影响小 |
| speed-preset | ultrafast | 最快编码优先，画质稍降 |
| max-buffers | 1 | 单缓冲，避免延迟堆积 |
| drop | true | 满了立即丢弃，保持实时性 |

## 七、客户端组件设计

### 7.1 React 组件树

```
App
├── ConnectionStore (Zustand)
│   └── useWebRTC Hook
├── ControlPanel
│   ├── Agent Address Input
│   ├── Connect/Disconnect Button
│   └── Status Indicator
└── VideoCanvas
    └── HTML5 Canvas (video element)
```

### 7.2 WebRTC Hook 设计

```typescript
interface WebRTCState {
  connectionState: 'disconnected' | 'connecting' | 'connected' | 'error';
  iceServers: RTCIceServer[];
  localStream?: MediaStream;
}

interface SignalingMessage {
  type: 'offer' | 'answer' | 'ice' | 'ping' | 'pong';
  data: any;
}
```

```typescript
const useWebRTC = (agentAddress: string) => {
  const [state, setState] = useState<WebRTCState>(...);
  const peerConnection = useRef<RTCPeerConnection>();
  const wsConnection = useRef<WebSocket>();

  const connect = async () => {
    // 1. 创建 WebSocket 连接
    // 2. 创建 RTCPeerConnection
    // 3. 设置本地 tracks（接收视频/音频）
    // 4. 创建 Offer
    // 5. 发送信令消息
  };

  const handleSignalingMessage = (msg: SignalingMessage) => {
    // 处理 answer、ice 等消息
  };

  return { state, connect, disconnect };
};
```

### 7.3 DataChannel 消息处理

**客户端 → Agent（控制流）**：
```typescript
interface ControlMessage {
  v: number;
  type: 'input.mouse_move' | 'input.mouse_button' | 'input.key';
  ts: number;
  seq: number;
  payload: any;
}
```

**鼠标移动**：
```typescript
const sendMouseMove = (x: number, y: number) => {
  const msg: ControlMessage = {
    v: 1,
    type: 'input.mouse_move',
    ts: Date.now(),
    seq: seq++,
    payload: { x, y, display_id: 0 }
  };
  controlChannel.send(JSON.stringify(msg));
};
```

### 7.4 Canvas 视频渲染

```typescript
<ReactPlayer
  url={mediaStream}
  playing={connectionState === 'connected'}
  controls={false}
  width="100%"
  height="100%"
  style={{ pointerEvents: 'none' }}  // 事件穿透到 overlay 处理
/>
```

## 八、Docker Compose 配置

```yaml
version: '3.8'

services:
  evdi-agent:
    build:
      context: ./evdi-agent
      dockerfile: Dockerfile
    container_name: evdi-agent-mvp
    ports:
      - "8080:8080"    # WebSocket 信令 + WebRTC
      - "5900:5900"    # 可选：VNC 调试端口
    volumes:
      - /tmp/.X11-unix:/tmp/.X11-unix  # X11 socket（如需宿主机调试）
    environment:
      - DISPLAY=:99
      - AGENT_WS_PORT=8080
      - VIDEO_WIDTH=1920
      - VIDEO_HEIGHT=1080
      - VIDEO_FPS=30
    cap_add:
      - SYS_ADMIN  # 如需权限操作
```

## 九、Makefile 构建/运行脚本

```makefile
.PHONY: build run dev clean

# 构建 Agent
build-agent:
	cd evdi-agent && CGO_ENABLED=1 go build -o bin/agent ./cmd/agent

# 构建客户端
build-client:
	cd evdi-web-client && npm run build

# 运行 Agent（本地）
run-agent:
	cd evdi-agent && go run ./cmd/agent

# 运行 Agent（Docker）
run-docker:
	docker-compose up --build

# 运行客户端
run-client:
	cd evdi-web-client && npm run dev

# 清理
clean:
	docker-compose down -v
	rm -rf evdi-agent/bin/
	rm -rf evdi-web-client/dist/
```

## 十、技术依赖清单

### Agent 依赖

```go
import (
    "github.com/pion/webrtc/v4"
    "github.com/gorilla/websocket"
    "github.com/lxn/win"  // Windows 输入（预留）
    "github.com/robotn/gohide"  // Linux 虚拟输入
)
```

### 客户端依赖

```json
{
  "dependencies": {
    "react": "^18.2.0",
    "react-dom": "^18.2.0",
    "zustand": "^4.4.0",
    "antd": "^5.10.0",
    "react-player": "^2.12.0"
  },
  "devDependencies": {
    "vite": "^5.0.0",
    "typescript": "^5.3.0"
  }
}
```

## 十一、API 接口规范（简化版）

### 11.1 WebSocket 信令 API

**连接地址**：
```
ws://localhost:8080/ws
```

**客户端 → Agent 消息**：

| type | payload | 说明 |
|------|---------|------|
| offer | `{ sdp: string }` | SDP Offer |
| ice | `{ candidate: string, sdpMid: string, sdpMLineIndex: number }` | ICE 候选 |
| ping | `{ ts: number }` | 心跳探测 |

**Agent → 客户端消息**：

| type | payload | 说明 |
|------|---------|------|
| answer | `{ sdp: string }` | SDP Answer |
| ice | `{ candidate: string, sdpMid: string, sdpMLineIndex: number }` | ICE 候选 |
| pong | `{ ts: number }` | 心跳响应 |

### 11.2 DataChannel 消息格式

**统一帧结构**：
```json
{
  "v": 1,
  "type": "消息类型",
  "ts": 1700000000123,
  "seq": 1024,
  "payload": { ... }
}
```

**支持的消息类型**：

| type | 方向 | 描述 |
|------|------|------|
| input.mouse_move | C→A | 鼠标移动 |
| input.mouse_button | C→A | 鼠标按键 |
| input.mouse_wheel | C→A | 滚轮事件 |
| input.key | C→A | 键盘按键 |
| clipboard.push | C↔A | 剪贴板推送 |
| ctrl.resize | C→A | 通知调整分辨率 |

## 十二、错误处理

| 错误 | 处理策略 | 客户端提示 |
|------|----------|-----------|
| WebSocket 连接失败 | 指数退避重连（1s→2s→4s→8s） | "连接 Agent 失败，正在重试..." |
| ICE 协商失败 | 显示详细错误，提供重试按钮 | "建立媒体连接失败，请检查网络后重试" |
| 媒体流中断 | 自动断开 DataChannel，提示用户 | "媒体流中断，请重新连接" |
| GStreamer Pipeline 崩溃 | Agent 记录日志，重启后接受新连接 | "桌面服务异常，请联系管理员" |

## 十三、测试计划

### 功能测试

- [ ] 启动 Agent 容器，检查 WebSocket 监听正常
- [ ] 启动客户端，能够连接到 Agent
- [ ] SDP/ICE 协商成功建立 P2P 连接
- [ ] 视频流正常显示 30 FPS 画面
- [ ] 鼠标移动和点击正确响应
- [ ] 键盘输入正确响应
- [ ] 剪贴板双向同步

### 性能测试

- [ ] 连接延迟 < 500ms
- [ ] 视频帧率稳定在 25-30 FPS
- [ ] CPU 使用率 < 30%（Agent 端）

### 兼容性测试

- [ ] Chrome/Edge 浏览器兼容
- [ ] 不同分辨率适配（1920x1080, 2560x1440）

## 十四、后续扩展路径

1. **接入 Broker** - 用 Broker Gateway 替换 Agent 内置信令
2. **Agent 侧增强** - 添加 gRPC Broker 通信模块
3. **功能扩展** - 添加多显示器、USB 重定向、打印机重定向
4. **监控集成** - 添加 Prometheus metrics 端点

## 十五、交付物清单

- [ ] Go Agent 源码 (`evdi-agent/`)
- [ ] React 客户端源码 (`evdi-web-client/`)
- [ ] Docker Compose 配置
- [ ] Makefile 构建/运行脚本
- [ ] README 部署/使用文档
- [ ] MVP 验证测试报告
