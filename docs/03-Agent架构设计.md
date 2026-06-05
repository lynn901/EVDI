# Agent 架构设计

> **文档版本**: v1.0
> **创建日期**: 2026-06-05
> **状态**: 草稿

## 目录

1. [概述](#1-概述)
2. [架构设计](#2-架构设计)
3. [核心组件详细设计](#3-核心组件详细设计)
4. [通信协议设计](#4-通信协议设计)
5. [安全设计](#5-安全设计)
6. [部署与运维](#6-部署与运维)
7. [接口规范](#7-接口规范)

---

## 1. 概述

### 1.1 Agent 定位

[待填写]

### 1.2 设计目标

[待填写]

### 1.3 核心职责

[待填写]

---

## 2. 架构设计

### 2.1 Agent 在整体架构中的位置

Agent 是 EVDI 七层架构中 **L2 层（媒体与接入层）** 的核心组件，运行在桌面实例（Pod/VM）内部。

```
┌─────────────────────────────────────────────────────────────────────┐
│ L1  Client Layer              — Web Client + Tauri Native Client    │
├─────────────────────────────────────────────────────────────────────┤
│ L2  Media & Access            — Agent (Pion + GStreamer) + Coturn   │
│                                   ▲                                 │
│                                   │ Agent 运行在桌面实例内           │
│                                   │ 逻辑上属于 L2 层                │
├─────────────────────────────────────────────────────────────────────┤
│ L3  Business Orchestration    — Broker + Console                    │
├─────────────────────────────────────────────────────────────────────┤
│ L4  Desktop Delivery          — Linux Pod / Windows VM              │
└─────────────────────────────────────────────────────────────────────┘
```

**架构定位说明：**

- **物理位置**：Agent 运行在 L4 层的桌面实例（Pod/VM）内部
- **逻辑归属**：Agent 承担 L2 层的核心职责（媒体传输与接入控制）
- **设计原则**：参考 Citrix VDA 架构，Agent 最小化外部交互，仅与 Client 和 Broker 通信

**与 Citrix VDA 的对比：**

| 维度 | Citrix VDA | EVDI Agent |
|------|------------|------------|
| 层级归属 | 运行在 VM，属于 Delivery 层 | 运行在 Pod/VM，属于 L2 层 |
| 协议栈 | ICA (私有协议) | WebRTC (Pion，开源标准) |
| 安全网关 | 需要 Citrix Gateway | 不需要 (WebRTC 内置安全) |
| NAT 穿透 | Gateway 代理 | ICE/STUN/TURN 内置 |

### 2.2 Agent 组件划分

Agent 由以下核心组件组成：

```
┌─────────────────────────────────────────────────────────────────────┐
│                            Agent                                    │
│                                                                     │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │    Pion      │  │  GStreamer   │  │    会话      │              │
│  │   WebRTC     │  │    编码      │  │    管理      │              │
│  │   引擎       │  │    引擎      │  │    模块      │              │
│  └──────────────┘  └──────────────┘  └──────────────┘              │
│                                                                     │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │   Broker     │  │    监控      │  │    生命周期   │              │
│  │   通信       │  │    采集      │  │    管理      │              │
│  │   模块       │  │    模块      │  │    模块      │              │
│  └──────────────┘  └──────────────┘  └──────────────┘              │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**各组件职责：**

| 组件 | 职责 | 技术选型 |
|------|------|----------|
| **Pion WebRTC 引擎** | 媒体传输、ICE/DTLS/SCTP、DataChannel 管理 | Pion/WebRTC (Go) |
| **GStreamer 编码引擎** | 视频编码 (x264/nvh264/VAAPI)、音频编码 | GStreamer (C, 动态链接) |
| **会话管理模块** | 会话生命周期、用户状态机、输入事件处理 | Go |
| **Broker 通信模块** | gRPC 客户端、心跳上报、指令接收、配置拉取 | gRPC (Go) |
| **监控采集模块** | CPU/内存/磁盘/网络指标采集 | Go |
| **生命周期管理模块** | 进程守护、崩溃恢复、自动升级 | Go |

**组件间关系：**

```
┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│  GStreamer  │─────►│    Pion     │─────►│   Client    │
│  (编码)     │ 媒体流│  (WebRTC)   │ 媒体流│   (L1)      │
└─────────────┘      └──────┬──────┘      └─────────────┘
                            │
                     DataChannel
                            │
                     ┌──────▼──────┐
                     │    会话     │
                     │    管理     │
                     └──────┬──────┘
                            │
                     ┌──────▼──────┐
                     │    Broker   │
                     │    通信     │◄────── 指令下发
                     └──────┬──────┘
                            │
                     会话上报、监控数据
                            │
                            ▼
                     ┌─────────────┐
                     │   Broker    │
                     │   (L3)      │
                     └─────────────┘
```

### 2.3 与外部组件的交互

Agent 遵循最小化交互原则，仅与两个外部组件通信：

```
┌─────────────────────────────────────────────────────────────────────┐
│                        桌面实例 (Pod/VM)                             │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │                          Agent                               │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
        ▲                               │
        │                               │
        │ ① 入站连接                   │ ② 出站连接
        │ (Client 主动连接)             │ (Agent 主动连接)
        │                               │
        │                               ▼
┌───────┴───────┐               ┌──────────────┐
│    Client     │               │    Broker    │
│    (L1)       │               │    (L3)      │
└───────────────┘               └──────────────┘
        │
        │ P2P 失败时
        ▼
┌───────────────┐
│    Coturn     │
│   (TURN)      │
└───────────────┘
```

**交互清单：**

| 连接 | 方向 | 协议 | 内容 | 说明 |
|------|------|------|------|------|
| **① Client → Agent** | 入站 | WebRTC | 视频流、音频流、控制信号 (DataChannel) | Client 主动建立连接 |
| **② Agent → Broker** | 出站 | gRPC | 会话上报、监控数据、指令接收、配置拉取 | Agent 主动注册 |
| **③ Agent ↔ Coturn** | 双向 | TURN | 仅 P2P 失败时的中继 | 基础设施，非直接交互 |

**Coturn 的定位：**

Coturn 是基础设施组件，不是 Agent 的直接交互对象：
- Pion 内置 ICE/STUN/TURN 支持，Agent 只需配置 TURN 服务器地址
- TURN 地址由 Broker 下发，Agent 无需关心 Coturn 的部署细节
- 仅在 P2P 连接失败时使用，对 Agent 透明

**与 Citrix 架构的对应关系：**

| EVDI 组件 | Citrix 对应 | 说明 |
|-----------|-------------|------|
| Agent | VDA (Virtual Delivery Agent) | 桌面实例内的代理程序 |
| Broker | Delivery Controller | 会话管理和调度 |
| Client | Workspace App | 用户端应用 |
| Coturn | Citrix Gateway (部分功能) | NAT 穿透 |
| WebRTC | ICA 协议 | 远程显示协议 |

**关键设计决策：**

1. **无需独立安全网关**：WebRTC 内置 DTLS 加密和 ICE/STUN/TURN，不需要类似 Citrix Gateway 的独立组件
2. **配置集中管理**：所有配置从 Broker 下发，Agent 无本地配置文件
3. **监控数据代理**：Agent 上报监控数据给 Broker，由 Broker 转发给 Prometheus，Agent 不直接暴露指标端点
4. **健康检查由 Kubelet 处理**：Agent 无需实现 K8s 健康检查接口，由 Pod 的 liveness/readiness probe 处理

---

## 3. 核心组件详细设计

### 3.1 WebRTC 引擎（Pion 集成）

WebRTC 引擎是 Agent 的核心组件，负责媒体传输和实时通信。基于 Pion/WebRTC 库实现，采用 Lite ICE 模式作为服务端等待 Client 连接。

#### 3.1.1 整体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                      WebRTC 引擎                                    │
│                                                                     │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │    Pion      │  │  GStreamer   │  │   DataChannel│              │
│  │  PeerConnection│ │  Pipeline   │  │   Manager    │              │
│  └──────────────┘  └──────────────┘  └──────────────┘              │
│         │                 │                 │                       │
│         │                 │                 │                       │
│  ┌──────▼─────────────────▼─────────────────▼──────┐               │
│  │              连接状态管理器                       │               │
│  └──────────────────────────────────────────────────┘               │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**关键设计决策：**

| 决策点 | 选择 | 理由 |
|--------|------|------|
| ICE 模式 | Lite ICE | Agent 作为服务端，由 Client 主导连接建立 |
| 视频编码 | H.264 | 硬件支持最广，WebRTC 标准必须支持 |
| 音频编码 | Opus | WebRTC 标准，低延迟，自适应码率 |
| 流策略 | 单流 | 一条视频轨（屏幕）+ 一条音频轨 |
| 分辨率/帧率 | 动态调整 | 根据网络带宽自适应 |

#### 3.1.2 Pion 初始化流程

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Pion 初始化流程                                    │
└─────────────────────────────────────────────────────────────────────┘
        │
        ▼
┌───────────────┐
│ 1. 创建       │
│ SettingEngine │
└───────┬───────┘
        │
        ▼
┌───────────────┐
│ 2. 配置       │
│ Lite ICE = true│
└───────┬───────┘
        │
        ▼
┌───────────────┐
│ 3. 创建       │
│ WebRTC API    │
└───────┬───────┘
        │
        ▼
┌───────────────┐
│ 4. 创建       │
│ PeerConnection│
└───────┬───────┘
        │
        ▼
┌───────────────┐
│ 5. 注册       │
│ 事件处理器    │
└───────┬───────┘
        │
        ▼
┌───────────────┐
│ 6. 等待       │
│ Client 连接   │
└───────────────┘
```

**初始化代码结构：**

```go
type WebRTCEngine struct {
    peerConnection *webrtc.PeerConnection
    videoTrack     *webrtc.TrackLocalStaticSample
    audioTrack     *webrtc.TrackLocalStaticSample
    dataChannel    *webrtc.DataChannel
    gstreamer      *GStreamerPipeline
    stateManager   *ConnectionStateManager
    config         *WebRTCConfig
}

// 初始化流程
func NewWebRTCEngine(config *WebRTCConfig) (*WebRTCEngine, error) {
    // 1. 创建 SettingEngine，配置 Lite ICE
    settingsEngine := webrtc.SettingEngine{}
    settingsEngine.SetLite(true)

    // 2. 配置 ICE 服务器（从 Broker 获取）
    iceConfig := webrtc.Configuration{
        ICEServers: config.ICEServers,
    }

    // 3. 创建 WebRTC API
    api := webrtc.NewAPI(
        webrtc.WithSettingEngine(settingsEngine),
    )

    // 4. 创建 PeerConnection
    peerConnection, err := api.NewPeerConnection(iceConfig)

    // 5. 注册事件处理器
    engine := &WebRTCEngine{
        peerConnection: peerConnection,
        config:         config,
    }
    engine.registerHandlers()

    return engine, nil
}
```

#### 3.1.3 ICE 配置

**Lite ICE 模式：**

```go
// Lite ICE 配置
settingsEngine := webrtc.SettingEngine{}
settingsEngine.SetLite(true)

// Lite ICE 特点：
// - 只提供 Host 候选者
// - 不主动发送 STUN/TURN 探测
// - 由 Client (Full ICE) 主导连接检查
// - 资源消耗低，适合服务器端
```

**ICE 服务器配置：**

```go
// ICE 服务器配置（从 Broker 获取）
iceConfig := webrtc.Configuration{
    ICEServers: []webrtc.ICEServer{
        {
            URLs: []string{"stun:stun.example.com:3478"},
        },
        {
            URLs:       []string{"turn:turn.example.com:3478"},
            Username:   "user",
            Credential: "pass",
        },
    },
}
```

**配置来源：**

| 配置项 | 来源 | 说明 |
|--------|------|------|
| STUN 地址 | Broker 下发 | 用于 NAT 类型检测 |
| TURN 地址 | Broker 下发 | P2P 失败时的中继服务器 |
| TURN 凭证 | Broker 下发 | 短期凭证，定期刷新 |

#### 3.1.4 SDP 协商流程

```
┌─────────────┐          ┌─────────────┐          ┌─────────────┐
│   Client    │          │   Broker    │          │    Agent    │
│  (Full ICE) │          │   (信令)    │          │  (Lite ICE) │
└──────┬──────┘          └──────┬──────┘          └──────┬──────┘
       │                        │                        │
       │  1. 请求连接           │                        │
       │───────────────────────►│                        │
       │                        │                        │
       │                        │  2. 转发连接请求       │
       │                        │───────────────────────►│
       │                        │                        │
       │                        │                        │  3. 创建 PeerConnection
       │                        │                        │  4. 创建 Offer (SDP)
       │                        │                        │
       │                        │  5. 返回 Offer         │
       │                        │◄───────────────────────│
       │                        │                        │
       │  6. 转发 Offer         │                        │
       │◄───────────────────────│                        │
       │                        │                        │
       │  7. 创建 Answer (SDP)  │                        │
       │  8. 收集 ICE 候选者    │                        │
       │                        │                        │
       │  9. 发送 Answer        │                        │
       │───────────────────────►│                        │
       │                        │                        │
       │                        │  10. 转发 Answer       │
       │                        │───────────────────────►│
       │                        │                        │
       │  11. ICE 连接检查      │                        │
       │  (Full ICE 主导)       │                        │
       │═════════════════════════════════════════════════►│
       │                        │                        │
       │  12. 连接建立          │                        │
       │◄════════════════════════════════════════════════►│
       │                        │                        │
```

**SDP 协商代码：**

```go
// Agent 端：创建 Offer
func (e *WebRTCEngine) CreateOffer() (*webrtc.SessionDescription, error) {
    offer, err := e.peerConnection.CreateOffer(nil)
    if err != nil {
        return nil, err
    }

    // 设置本地描述
    err = e.peerConnection.SetLocalDescription(offer)
    if err != nil {
        return nil, err
    }

    return &offer, nil
}

// Agent 端：设置远程 Answer
func (e *WebRTCEngine) SetRemoteAnswer(answer webrtc.SessionDescription) error {
    return e.peerConnection.SetRemoteDescription(answer)
}
```

#### 3.1.5 DataChannel 管理

**DataChannel 创建：**

```go
// 创建 DataChannel（Agent 作为服务端）
func (e *WebRTCEngine) CreateDataChannel() error {
    // 配置可靠性分层
    options := &webrtc.DataChannelInit{
        Ordered: boolPtr(true),  // 保证顺序
    }

    dataChannel, err := e.peerConnection.CreateDataChannel("control", options)
    if err != nil {
        return err
    }

    e.dataChannel = dataChannel
    return nil
}
```

**可靠性分层设计：**

| 数据类型 | 通道配置 | 说明 |
|----------|----------|------|
| 键盘输入 | reliable, ordered | 按键顺序不能乱，丢失会导致输入错误 |
| 鼠标移动 | reliable, ordered | 保证轨迹完整，丢失会导致光标跳跃 |
| 剪贴板 | reliable, ordered | 数据不能丢失，丢失会导致内容不完整 |
| 心跳 | unreliable | 丢一两个没关系，保持连接活跃即可 |

**消息路由：**

```go
// 消息类型定义
type MessageType uint8

const (
    MessageTypeKeyboard   MessageType = 0x01
    MessageTypeMouse      MessageType = 0x02
    MessageTypeClipboard  MessageType = 0x03
    MessageTypeHeartbeat  MessageType = 0x04
)

// 消息结构
type ControlMessage struct {
    Type    MessageType
    Payload []byte
}

// 消息路由
func (e *WebRTCEngine) handleMessage(msg ControlMessage) {
    switch msg.Type {
    case MessageTypeKeyboard:
        e.handleKeyboardEvent(msg.Payload)
    case MessageTypeMouse:
        e.handleMouseEvent(msg.Payload)
    case MessageTypeClipboard:
        e.handleClipboardEvent(msg.Payload)
    case MessageTypeHeartbeat:
        e.handleHeartbeat(msg.Payload)
    }
}
```

#### 3.1.6 媒体流管理

**视频轨创建：**

```go
// 创建视频轨（屏幕内容）
func (e *WebRTCEngine) CreateVideoTrack() error {
    videoTrack, err := webrtc.NewTrackLocalStaticSample(
        webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
        "screen",
        "screen",
    )
    if err != nil {
        return err
    }

    _, err = e.peerConnection.AddTrack(videoTrack)
    if err != nil {
        return err
    }

    e.videoTrack = videoTrack
    return nil
}
```

**音频轨创建：**

```go
// 创建音频轨
func (e *WebRTCEngine) CreateAudioTrack() error {
    audioTrack, err := webrtc.NewTrackLocalStaticSample(
        webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
        "audio",
        "audio",
    )
    if err != nil {
        return err
    }

    _, err = e.peerConnection.AddTrack(audioTrack)
    if err != nil {
        return err
    }

    e.audioTrack = audioTrack
    return nil
}
```

**媒体流架构：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                        媒体流架构                                    │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│  桌面画面   │      │  GStreamer   │      │    Pion     │
│  采集       │─────►│  编码       │─────►│  RTP 打包   │
│  (X11/IDD)  │      │  (H.264)    │      │             │
└─────────────┘      └─────────────┘      └──────┬──────┘
                                                │
                                          ┌─────▼─────┐
                                          │  视频轨   │
                                          │  (Track)  │
                                          └─────┬─────┘
                                                │
                                          ┌─────▼─────┐
                                          │   Client  │
                                          └───────────┘

┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│  音频采集   │      │  GStreamer   │      │    Pion     │
│  (PulseAudio│─────►│  编码       │─────►│  RTP 打包   │
│   /WASAPI)  │      │  (Opus)     │      │             │
└─────────────┘      └─────────────┘      └──────┬──────┘
                                                │
                                          ┌─────▼─────┐
                                          │  音频轨   │
                                          │  (Track)  │
                                          └─────┬─────┘
                                                │
                                          ┌─────▼─────┐
                                          │   Client  │
                                          └───────────┘
```

#### 3.1.7 硬件检测

**GPU 能力检测流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                      GPU 硬件检测流程                                │
└─────────────────────────────────────────────────────────────────────┘
        │
        ▼
┌───────────────┐
│ 1. 检测       │
│ NVIDIA GPU    │
│ (nvidia-smi)  │
└───────┬───────┘
        │
        ├── 是 ──► 使用 NVENC 硬件编码
        │
        ▼
┌───────────────┐
│ 2. 检测       │
│ Intel GPU     │
│ (VAAPI)       │
└───────┬───────┘
        │
        ├── 是 ──► 使用 VAAPI 硬件编码
        │
        ▼
┌───────────────┐
│ 3. 检测       │
│ AMD GPU       │
│ (VAAPI/AMF)   │
└───────┬───────┘
        │
        ├── 是 ──► 使用 VAAPI 硬件编码
        │
        ▼
┌───────────────┐
│ 4. 无 GPU     │
│ 使用 x264     │
│ 软编码        │
└───────────────┘
```

**硬件检测代码：**

```go
type GPUType string

const (
    GPUTypeNVIDIA  GPUType = "nvidia"
    GPUTypeIntel   GPUType = "intel"
    GPUTypeAMD     GPUType = "amd"
    GPUTypeSoftware GPUType = "software"
)

type HardwareInfo struct {
    GPUType      GPUType
    GPUName      string
    DriverVersion string
    VRAMSize     uint64
}

// 检测 GPU 硬件
func DetectGPU() (*HardwareInfo, error) {
    // 1. 尝试检测 NVIDIA GPU
    if info, err := detectNVIDIA(); err == nil {
        return info, nil
    }

    // 2. 尝试检测 Intel/AMD GPU (VAAPI)
    if info, err := detectVAAPI(); err == nil {
        return info, nil
    }

    // 3. 回退到软件编码
    return &HardwareInfo{
        GPUType: GPUTypeSoftware,
        GPUName: "CPU (x264)",
    }, nil
}

// 检测 NVIDIA GPU
func detectNVIDIA() (*HardwareInfo, error) {
    cmd := exec.Command("nvidia-smi", "--query-gpu=name,driver_version,memory.total",
        "--format=csv,noheader,nounits")
    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }

    // 解析输出
    parts := strings.Split(strings.TrimSpace(string(output)), ", ")
    if len(parts) < 3 {
        return nil, fmt.Errorf("invalid nvidia-smi output")
    }

    vram, _ := strconv.ParseUint(parts[2], 10, 64)

    return &HardwareInfo{
        GPUType:       GPUTypeNVIDIA,
        GPUName:       parts[0],
        DriverVersion: parts[1],
        VRAMSize:      vram * 1024 * 1024, // 转换为字节
    }, nil
}

// 检测 VAAPI (Intel/AMD)
func detectVAAPI() (*HardwareInfo, error) {
    cmd := exec.Command("vainfo")
    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }

    // 解析 vainfo 输出，检测支持的编码器
    if strings.Contains(string(output), "H264") {
        return &HardwareInfo{
            GPUType: GPUTypeIntel, // 或 AMD
            GPUName: "VAAPI Compatible GPU",
        }, nil
    }

    return nil, fmt.Errorf("no VAAPI support")
}
```

#### 3.1.8 GStreamer Pipeline 初始化

**编码器选择策略：**

```go
// 根据 GPU 类型选择 GStreamer Pipeline
func NewGStreamerPipeline(gpuType GPUType, config *EncodingConfig) (*GStreamerPipeline, error) {
    switch gpuType {
    case GPUTypeNVIDIA:
        return newNVIDIAH264Pipeline(config)
    case GPUTypeIntel:
        return newVAAPIH264Pipeline(config)
    case GPUTypeAMD:
        return newVAAPIH264Pipeline(config)
    case GPUTypeSoftware:
        return newX264Pipeline(config)
    default:
        return nil, fmt.Errorf("unsupported GPU type: %s", gpuType)
    }
}
```

**GStreamer Pipeline 配置：**

```go
// NVIDIA NVENC Pipeline
func newNVIDIAH264Pipeline(config *EncodingConfig) (*GStreamerPipeline, error) {
    pipelineStr := fmt.Sprintf(
        "ximagesrc ! video/x-raw,width=%d,height=%d,framerate=%d/1 ! "+
            "nvvidconv ! video/x-raw(memory:NVMM) ! "+
            "nvh264enc bitrate=%d preset=low-latency ! "+
            "h264parse ! rtph264pay pt=96 ! "+
            "appsink name=video-sink",
        config.Width, config.Height, config.Framerate,
        config.Bitrate,
    )
    return newPipelineFromString(pipelineStr)
}

// Intel/AMD VAAPI Pipeline
func newVAAPIH264Pipeline(config *EncodingConfig) (*GStreamerPipeline, error) {
    pipelineStr := fmt.Sprintf(
        "ximagesrc ! video/x-raw,width=%d,height=%d,framerate=%d/1 ! "+
            "vaapipostproc ! video/x-raw(memory:VASurface) ! "+
            "vaapih264enc bitrate=%d ! "+
            "h264parse ! rtph264pay pt=96 ! "+
            "appsink name=video-sink",
        config.Width, config.Height, config.Framerate,
        config.Bitrate,
    )
    return newPipelineFromString(pipelineStr)
}

// CPU 软编码 Pipeline (兜底)
func newX264Pipeline(config *EncodingConfig) (*GStreamerPipeline, error) {
    pipelineStr := fmt.Sprintf(
        "ximagesrc ! video/x-raw,width=%d,height=%d,framerate=%d/1 ! "+
            "videoconvert ! video/x-raw,format=I420 ! "+
            "x264enc bitrate=%d tune=zerolatency speed-preset=ultrafast ! "+
            "h264parse ! rtph264pay pt=96 ! "+
            "appsink name=video-sink",
        config.Width, config.Height, config.Framerate,
        config.Bitrate,
    )
    return newPipelineFromString(pipelineStr)
}
```

#### 3.1.9 连接状态监控

**连接状态机：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                      连接状态机                                      │
└─────────────────────────────────────────────────────────────────────┘

        ┌─────────┐
        │  新建   │
        └────┬────┘
             │
             ▼
        ┌─────────┐
        │  连接中 │ ◄──── ICE 检查中
        └────┬────┘
             │
             │ 连接成功
             ▼
        ┌─────────┐
        │  已连接 │ ◄──── 正常工作状态
        └────┬────┘
             │
             │ 连接中断
             ▼
        ┌─────────┐
        │  重连中 │ ◄──── 尝试恢复连接
        └────┬────┘
             │
        ┌────┴────┐
        │         │
        ▼         ▼
   ┌─────────┐ ┌─────────┐
   │  已连接 │ │  已断开 │
   └─────────┘ └────┬────┘
                    │
                    ▼
               ┌─────────┐
               │  已关闭 │
               └─────────┘
```

**状态监控代码：**

```go
type ConnectionState string

const (
    ConnectionStateNew         ConnectionState = "new"
    ConnectionStateConnecting  ConnectionState = "connecting"
    ConnectionStateConnected   ConnectionState = "connected"
    ConnectionStateReconnecting ConnectionState = "reconnecting"
    ConnectionStateDisconnected ConnectionState = "disconnected"
    ConnectionStateClosed      ConnectionState = "closed"
)

type ConnectionStateManager struct {
    state           ConnectionState
    stateChangeTime time.Time
    mu              sync.RWMutex
    callbacks       []func(ConnectionState)
}

// 注册状态变化回调
func (m *ConnectionStateManager) OnStateChange(callback func(ConnectionState)) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.callbacks = append(m.callbacks, callback)
}

// 更新状态
func (m *ConnectionStateManager) SetState(newState ConnectionState) {
    m.mu.Lock()
    defer m.mu.Unlock()

    oldState := m.state
    m.state = newState
    m.stateChangeTime = time.Now()

    // 通知回调
    for _, callback := range m.callbacks {
        go callback(newState)
    }

    log.Printf("Connection state changed: %s -> %s", oldState, newState)
}

// 注册 Pion 连接状态回调
func (e *WebRTCEngine) registerConnectionStateHandler() {
    e.peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
        switch state {
        case webrtc.PeerConnectionStateNew:
            e.stateManager.SetState(ConnectionStateNew)
        case webrtc.PeerConnectionStateConnecting:
            e.stateManager.SetState(ConnectionStateConnecting)
        case webrtc.PeerConnectionStateConnected:
            e.stateManager.SetState(ConnectionStateConnected)
        case webrtc.PeerConnectionStateDisconnected:
            e.stateManager.SetState(ConnectionStateReconnecting)
            e.startReconnectTimer()
        case webrtc.PeerConnectionStateFailed:
            e.stateManager.SetState(ConnectionStateDisconnected)
        case webrtc.PeerConnectionStateClosed:
            e.stateManager.SetState(ConnectionStateClosed)
        }
    })
}
```

#### 3.1.10 动态参数调整

**自适应策略：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                      动态参数调整策略                                 │
└─────────────────────────────────────────────────────────────────────┘

网络带宽检测
        │
        ▼
┌───────────────┐
│  带宽 > 10Mbps│
│  高画质模式   │
│  1080p 30fps  │
│  4Mbps        │
└───────────────┘
        │
        ▼
┌───────────────┐
│  带宽 5-10Mbps│
│  中画质模式   │
│  720p 30fps   │
│  2Mbps        │
└───────────────┘
        │
        ▼
┌───────────────┐
│  带宽 2-5Mbps │
│  低画质模式   │
│  720p 15fps   │
│  1Mbps        │
└───────────────┘
        │
        ▼
┌───────────────┐
│  带宽 < 2Mbps │
│  极低画质模式 │
│  480p 15fps   │
│  500Kbps      │
└───────────────┘
```

**自适应代码：**

```go
type QualityProfile struct {
    Width     int
    Height    int
    Framerate int
    Bitrate   int // kbps
}

var qualityProfiles = map[string]QualityProfile{
    "high": {
        Width:     1920,
        Height:    1080,
        Framerate: 30,
        Bitrate:   4000,
    },
    "medium": {
        Width:     1280,
        Height:    720,
        Framerate: 30,
        Bitrate:   2000,
    },
    "low": {
        Width:     1280,
        Height:    720,
        Framerate: 15,
        Bitrate:   1000,
    },
    "very_low": {
        Width:     854,
        Height:    480,
        Framerate: 15,
        Bitrate:   500,
    },
}

// 动态调整编码参数
func (e *WebRTCEngine) adjustEncodingParameters(bandwidthKbps int) {
    var profile QualityProfile

    switch {
    case bandwidthKbps > 10000:
        profile = qualityProfiles["high"]
    case bandwidthKbps > 5000:
        profile = qualityProfiles["medium"]
    case bandwidthKbps > 2000:
        profile = qualityProfiles["low"]
    default:
        profile = qualityProfiles["very_low"]
    }

    // 更新 GStreamer Pipeline 参数
    e.gstreamer.UpdateProfile(profile)

    log.Printf("Adjusted encoding to %s profile: %dx%d@%dfps, %dkbps",
        profile.Width, profile.Height, profile.Framerate, profile.Bitrate)
}

// 带宽检测（定期调用）
func (e *WebRTCEngine) monitorBandwidth() {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        stats := e.peerConnection.GetStats()
        bandwidthKbps := e.calculateBandwidth(stats)
        e.adjustEncodingParameters(bandwidthKbps)
    }
}
```

#### 3.1.11 重连机制

**重连流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                      重连流程                                        │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  连接中断   │
│  检测       │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  等待       │
│  1秒        │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  尝试       │
│  重连 #1    │
└──────┬──────┘
       │
       ├── 成功 ──► 恢复连接
       │
       ▼
┌─────────────┐
│  等待       │
│  2秒        │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  尝试       │
│  重连 #2    │
└──────┬──────┘
       │
       ├── 成功 ──► 恢复连接
       │
       ▼
┌─────────────┐
│  等待       │
│  4秒        │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  尝试       │
│  重连 #3    │
└──────┬──────┘
       │
       ├── 成功 ──► 恢复连接
       │
       ▼
┌─────────────┐
│  重连失败   │
│  通知 Broker │
└─────────────┘
```

**重连代码：**

```go
type ReconnectConfig struct {
    MaxRetries      int
    InitialDelay    time.Duration
    MaxDelay        time.Duration
    BackoffFactor   float64
}

var defaultReconnectConfig = ReconnectConfig{
    MaxRetries:    3,
    InitialDelay:  1 * time.Second,
    MaxDelay:      10 * time.Second,
    BackoffFactor: 2.0,
}

// 启动重连定时器
func (e *WebRTCEngine) startReconnectTimer() {
    config := defaultReconnectConfig
    delay := config.InitialDelay

    for retry := 0; retry < config.MaxRetries; retry++ {
        time.Sleep(delay)

        log.Printf("Attempting reconnect %d/%d...", retry+1, config.MaxRetries)

        if e.tryReconnect() {
            log.Printf("Reconnect successful")
            return
        }

        // 指数退避
        delay = time.Duration(float64(delay) * config.BackoffFactor)
        if delay > config.MaxDelay {
            delay = config.MaxDelay
        }
    }

    log.Printf("Reconnect failed after %d attempts", config.MaxRetries)
    e.notifyBrokerReconnectFailed()
}

// 尝试重连
func (e *WebRTCEngine) tryReconnect() bool {
    // 1. 关闭旧的 PeerConnection
    e.peerConnection.Close()

    // 2. 创建新的 PeerConnection
    newPC, err := e.createPeerConnection()
    if err != nil {
        return false
    }

    e.peerConnection = newPC

    // 3. 重新创建 Offer 并通过 Broker 发送给 Client
    offer, err := e.CreateOffer()
    if err != nil {
        return false
    }

    // 4. 通过 Broker 发送给 Client
    err = e.sendOfferToBroker(offer)
    if err != nil {
        return false
    }

    // 5. 等待 Client 回复 Answer
    // （在 SetRemoteAnswer 中处理）

    return true
}
```

#### 3.1.12 资源释放

**优雅关闭流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                      优雅关闭流程                                    │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  收到关闭   │
│  信号       │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  停止       │
│  GStreamer   │
│  Pipeline   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  关闭       │
│  DataChannel│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  关闭       │
│  PeerConnection│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  释放       │
│  资源       │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  通知       │
│  Broker     │
└─────────────┘
```

**资源释放代码：**

```go
// 优雅关闭
func (e *WebRTCEngine) Shutdown() error {
    log.Printf("Starting graceful shutdown...")

    // 1. 停止 GStreamer Pipeline
    if e.gstreamer != nil {
        e.gstreamer.Stop()
        log.Printf("GStreamer pipeline stopped")
    }

    // 2. 关闭 DataChannel
    if e.dataChannel != nil {
        e.dataChannel.Close()
        log.Printf("DataChannel closed")
    }

    // 3. 关闭 PeerConnection
    if e.peerConnection != nil {
        e.peerConnection.Close()
        log.Printf("PeerConnection closed")
    }

    // 4. 通知 Broker
    e.notifyBrokerShutdown()

    log.Printf("Graceful shutdown completed")
    return nil
}

// 强制关闭（超时时使用）
func (e *WebRTCEngine) ForceShutdown() {
    log.Printf("Force shutdown initiated")

    // 直接关闭 PeerConnection，不等待
    if e.peerConnection != nil {
        e.peerConnection.Close()
    }

    // 通知 Broker 异常关闭
    e.notifyBrokerForceShutdown()
}
```

#### 3.1.13 WebRTC 引擎完整生命周期

```
┌─────────────────────────────────────────────────────────────────────┐
│                    WebRTC 引擎完整生命周期                            │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  Agent 启动 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 1. 检测     │
│ GPU 硬件    │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 2. 初始化   │
│ GStreamer    │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 3. 初始化   │
│ Pion (Lite) │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 4. 注册     │
│ 到 Broker   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 5. 等待     │
│ Client 连接 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 6. SDP 协商 │
│ ICE 连接    │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 7. 媒体传输 │
│ 开始        │
└──────┬──────┘
       │
       ├── 正常工作 ──────────────────────┐
       │                                  │
       │ 连接中断                         │
       ▼                                  │
┌─────────────┐                          │
│ 8. 重连     │                          │
│ 尝试        │                          │
└──────┬──────┘                          │
       │                                  │
       ├── 成功 ──► 返回步骤 7           │
       │                                  │
       ▼                                  │
┌─────────────┐                          │
│ 9. 重连失败 │                          │
│ 通知 Broker │                          │
└──────┬──────┘                          │
       │                                  │
       ▼                                  │
┌─────────────┐                          │
│ 10. 等待    │                          │
│ 新连接      │◄─────────────────────────┘
└──────┬──────┘
       │
       │ 收到关闭信号
       ▼
┌─────────────┐
│ 11. 优雅    │
│ 关闭        │
└─────────────┘
```

### 3.2 Broker 通信模块

[待填写]

### 3.3 会话管理模块

[待填写]

### 3.4 监控数据采集模块

[待填写]

### 3.5 生命周期管理模块

[待填写]

---

## 4. 通信协议设计

### 4.1 Agent ↔ Broker 通信协议

[待填写]

### 4.2 Agent ↔ Client 通信协议（WebRTC DataChannel）

[待填写]

### 4.3 消息格式定义

[待填写]

---

## 5. 安全设计

### 5.1 身份认证

[待填写]

### 5.2 访问控制

[待填写]

### 5.3 传输安全

[待填写]

### 5.4 进程保护

[待填写]

---

## 6. 部署与运维

### 6.1 生命周期管理

[待填写]

### 6.2 自动升级机制

[待填写]

### 6.3 崩溃恢复

[待填写]

### 6.4 监控告警

[待填写]

---

## 7. 接口规范

### 7.1 Broker API

[待填写]

### 7.2 监控数据接口

[待填写]

### 7.3 内部组件接口

[待填写]
