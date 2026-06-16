# Agent 架构设计

> **文档版本**: v1.1
> **创建日期**: 2026-06-05
> **最后更新**: 2026-06-16（MVP 验证后更新）
> **状态**: 草稿

## 目录

1. [概述](#1-概述)
2. [架构设计](#2-架构设计)
3. [核心组件详细设计](#3-核心组件详细设计)
4. [通信协议设计](#4-通信协议设计)
5. [安全设计](#5-安全设计)
6. [部署与运维](#6-部署与运维)
7. [接口规范](#7-接口规范)
8. [MVP 验证记录](#8-mvp-验证记录)

---

## 1. 概述

### 1.1 Agent 定位

Agent 是 EVDI 七层架构中 **L2 层（媒体与接入层）** 的核心组件，运行在桌面实例（Linux Pod / Windows VM）内部，承担媒体流编码与传输、输入事件处理、会话管理三大核心职责。

参考 Citrix VDA（Virtual Delivery Agent）架构，Agent 遵循最小化外部交互原则——仅与 Client（入站 WebRTC）和 Broker（出站 gRPC/REST）通信，不直接暴露管理端口，不直接操作 K8s API。

MVP 阶段已验证 Agent 在 Linux 容器（Podman/Docker）内的完整工作链路：浏览器通过 WebRTC 接收 H.264 视频流 + Opus 音频流，鼠标键盘输入通过 WebSocket 信令通道实时传输，xdotool 执行输入命令，桌面画面与输入交互均正常工作。

### 1.2 设计目标

| 目标 | 说明 |
|------|------|
| 低延迟媒体传输 | 基于 WebRTC（Pion）+ H.264/Opus，局域网端到端延迟目标 < 100ms |
| 硬件编码自适应 | 自动检测 GPU 类型（NVIDIA NVENC → Intel/AMD VAAPI → x264 软编码），按最优路径编码 |
| 跨平台输入处理 | 统一接收 Client 输入事件，转换为操作系统原生输入（Linux: xdotool/XTest, Windows: SendInput） |
| 进程隔离合规 | GStreamer（LGPL）作为独立进程运行，通过管道与 Agent 通信，满足 LGPL 合规要求 |
| 容器化优先 | 以容器为交付单元，包含 Xvfb + 桌面环境 + Agent，一键启动即可提供远程桌面服务 |
| Broker 解耦 | Agent 不直接操作 K8s API，所有业务编排通过 Broker 完成，Agent 只负责执行 |

### 1.3 核心职责

Agent 的核心职责划分为以下六个领域：

| 职责 | 说明 | MVP 状态 |
|------|------|---------|
| **媒体编码与传输** | GStreamer 采集桌面画面 + 系统音频，编码后通过 Pion WebRTC 推送给 Client | ✅ 视频已验证，音频待验证 |
| **输入事件处理** | 接收 Client 键鼠事件，转换为操作系统输入指令 | ✅ 已验证（xdotool 方案） |
| **WebRTC 信令** | SDP/ICE 交换，建立 PeerConnection | ✅ 已验证（Agent 内建 WebSocket 信令） |
| **会话管理** | 连接生命周期管理、断线重连、状态上报 | ⏳ MVP 简化版，正式版接入 Broker |
| **Broker 通信** | gRPC/REST 心跳上报、配置拉取、指令接收 | ⏳ MVP 未实现，正式版实现 |
| **监控采集** | CPU/内存/磁盘/网络指标采集并上报 | ⏳ MVP 未实现，正式版实现 |

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

> **⚠️ MVP 验证结果**：以下流程描述的是正式架构中 Agent 发起 Offer 的设计。MVP 实测发现，浏览器端 `RTCPeerConnection` 在 Client 发起 Offer、Agent 回复 Answer 的模式下工作更可靠（这也是 WebRTC 的标准模式——由浏览器侧发起 Offer）。正式版本将统一修正为 Client 发 Offer、Agent 回 Answer 的流程，下方 §3.1.4-A 已补充 MVP 实际验证的协商流程。

**正式架构设计（Agent 发起 Offer）：**

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

**§3.1.4-A MVP 实际验证的协商流程（Client 发起 Offer）：**

MVP 阶段验证通过的流程如下，由 Client（浏览器）发起 Offer，Agent 回复 Answer：

```
┌─────────────┐                         ┌─────────────┐
│   Client    │                         │    Agent    │
│  (Full ICE) │                         │  (Lite ICE) │
└──────┬──────┘                         └──────┬──────┘
       │                                       │
       │  1. WebSocket 连接 (/ws)              │
       │──────────────────────────────────────►│
       │                                       │
       │  2. SDP Offer (via WS)                │
       │──────────────────────────────────────►│
       │                                       │  3. SetRemoteDescription(Offer)
       │                                       │  4. CreateAnswer
       │                                       │  5. SetLocalDescription(Answer)
       │  6. SDP Answer (via WS)               │
       │◄──────────────────────────────────────│
       │                                       │
       │  7. ICE Candidate (via WS)            │
       │──────────────────────────────────────►│  8. AddICECandidate
       │  9. ICE Candidate (via WS)            │
       │◄──────────────────────────────────────│  10. ICE Candidate 转发
       │                                       │
       │  11. DTLS 握手完成                     │
       │◄═══════════════════════════════════════│
       │  12. 连接建立                          │
       │                                       │
```

**MVP 实现代码（Agent 端 HandleOffer）：**

```go
// Agent 端：接收 Offer，返回 Answer
func (e *WebRTCEngine) HandleOffer(offerData json.RawMessage) (json.RawMessage, error) {
    var offer webrtc.SessionDescription
    if err := json.Unmarshal(offerData, &offer); err != nil {
        return nil, fmt.Errorf("unmarshal offer: %w", err)
    }
    if err := e.peerConnection.SetRemoteDescription(offer); err != nil {
        return nil, fmt.Errorf("set remote description: %w", err)
    }
    answer, err := e.peerConnection.CreateAnswer(nil)
    if err != nil {
        return nil, fmt.Errorf("create answer: %w", err)
    }
    if err := e.peerConnection.SetLocalDescription(answer); err != nil {
        return nil, fmt.Errorf("set local description: %w", err)
    }
    return json.Marshal(answer)
}
```

> **设计修正说明**：正式架构中将统一采用 Client 发 Offer、Agent 回 Answer 的模式，与 WebRTC 浏览器标准行为一致。原设计中 Agent 发 Offer 的流程不再保留。

#### 3.1.5 DataChannel 管理

> **⚠️ MVP 验证结果**：Pion WebRTC v4 Lite ICE 模式下，SCTP DataChannel 存在单向性问题——Agent→浏览器方向正常（浏览器可收到 `ctrl.ping`），但浏览器→Agent 方向失败（Agent 收不到任何消息）。MVP 阶段已改用 WebSocket 信令通道传输输入事件作为临时方案（详见 §3.3.6 加注）。正式版本需升级 Pion 或调整 ICE 配置修复 DataChannel 双向通信，使输入事件回归 DataChannel 传输。

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

**DataChannel 消息格式（与 Client 一致）：**

所有 DataChannel 消息均采用统一 JSON 帧结构：

```json
{
  "v": 1,
  "type": "input.mouse_move",
  "ts": 1700000000123,
  "seq": 1024,
  "payload": { "x": 960, "y": 540, "display_id": 0 }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `v` | number | 是 | 协议版本号，当前为 `1` |
| `type` | string | 是 | 消息类型，见消息类型总览 |
| `ts` | number | 是 | 客户端发送时间戳（Unix 毫秒），用于端到端延迟统计 |
| `seq` | number | 是 | 单调递增序列号，每条通道独立计数，用于乱序检测与日志追踪 |
| `payload` | object | 是 | 消息体，结构因 `type` 而异 |

**支持的 `type` 值（完整定义见 Client 文档 §7.3）：**

| `type` 值 | 通道 | 方向 | 说明 |
|-----------|------|------|------|
| `input.mouse_move` | control | Client → Desktop | 鼠标移动 |
| `input.mouse_button` | control | Client → Desktop | 鼠标按键按下/释放 |
| `input.mouse_wheel` | control | Client → Desktop | 滚轮事件 |
| `input.key` | control | Client → Desktop | 键盘按键按下/释放 |
| `clipboard.push` | bulk | Client ↔ Desktop | 推送剪贴板内容（双向） |
| `clipboard.request` | bulk | Client → Desktop | 请求桌面当前剪贴板内容 |
| `ctrl.ping` | control | Client → Desktop | 心跳探测 |
| `ctrl.pong` | control | Desktop → Client | 心跳响应 |
| `ctrl.resize` | control | Client → Desktop | 通知桌面调整分辨率 |

**Go 代码实现：**

```go
// DataChannel 消息结构（JSON 统一格式）
type ControlMessage struct {
    V       int                    `json:"v"`
    Type    string                 `json:"type"`
    Ts      int64                  `json:"ts"`
    Seq     int                    `json:"seq"`
    Payload map[string]interface{} `json:"payload"`
}

// 消息路由
func (e *WebRTCEngine) handleMessage(jsonMsg string) error {
    var msg ControlMessage
    if err := json.Unmarshal([]byte(jsonMsg), &msg); err != nil {
        return fmt.Errorf("invalid message format: %w", err)
    }

    // 版本检查
    if msg.V != 1 {
        return fmt.Errorf("unsupported protocol version: %d", msg.V)
    }

    // 根据 type 字段路由消息
    switch msg.Type {
    case "input.mouse_move":
        e.handleMouseMove(msg.Payload)
    case "input.mouse_button":
        e.handleMouseButton(msg.Payload)
    case "input.mouse_wheel":
        e.handleMouseWheel(msg.Payload)
    case "input.key":
        e.handleKeyboardEvent(msg.Payload)
    case "clipboard.push":
        e.handleClipboardPush(msg.Payload)
    case "clipboard.request":
        e.handleClipboardRequest(msg.Payload)
    case "ctrl.ping":
        e.handlePing(msg.Payload)
    case "ctrl.resize":
        e.handleResize(msg.Payload)
    default:
        return fmt.Errorf("unknown message type: %s", msg.Type)
    }
    return nil
}
```

#### 3.1.6 媒体流管理

> **⚠️ MVP 验证结果**：Pion 为 video 和 audio 轨道分别创建不同的 MSID，导致浏览器端 `ontrack` 触发两次，每次的 `event.streams[0]` 是不同的 MediaStream 对象。如果直接赋值 `video.srcObject = event.streams[0]`，音频流会覆盖视频流导致黑屏。**Client 端必须将所有 track 合并到同一个 MediaStream 中**（详见 Client 文档 §6.4）。此外，MVP 实测中自定义 MediaEngine 只注册 H.264（payload 96）+ Opus（payload 111）是必要的，否则浏览器 SDP 协商会优先选择 VP8。

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

> **⚠️ MVP 验证结果**：以下 Pipeline 使用 `appsink` + `rtph264pay` 的方式在 MVP 中未采用。MVP 实际使用 **`fdsink fd=1` 进程隔离方案**——GStreamer 作为独立子进程运行，通过 stdout 管道输出 H.264 字节流，Agent 主进程从管道读取后自行解析 NALU 并调用 Pion `WriteSample` 发送。这种方式满足 LGPL 合规要求（进程隔离 + 动态链接），且 Pipeline 崩溃不会影响 Agent 主进程。MVP 实际的 x264 Pipeline 见下方 §3.1.8-A。

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

**§3.1.8-A MVP 实际验证的 x264 Pipeline（进程隔离方案）：**

MVP 采用 GStreamer 子进程 + stdout 管道的方式，Agent 主进程解析 NALU 并调用 Pion `WriteSample`：

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

**MVP Pipeline 关键参数踩坑记录：**

| 参数 | 说明 |
|------|------|
| `show-pointer=true` | 捕获 X 光标（不是 `show-cursor`，后者不存在于 ximagesrc），依赖 XFixes 扩展 |
| `endx/endy` | 值为 `width-1 / height-1`，不是 `width/height` |
| `format=I420` | x264enc 前必须强制 I420 输入格式，否则默认可能输出 High 4:4:4 profile |
| `profile=constrained-baseline` | 用 caps filter 指定，不能通过 x264enc 属性设置（x264enc 无 profile 属性） |
| `byte-stream=true` | Pion 需要字节流格式（Annex B），不是 AVCC 格式 |
| `threads=1` | 容器内 x264enc 多线程初始化可能失败，需添加 `threads=1` |
| `fdsink fd=1` | 输出到 stdout，由 Agent 管道读取。满足 LGPL 进程隔离合规 |

**MVP NALU 解析要点：**

- 必须同时支持 3 字节（`00 00 01`）和 4 字节（`00 00 00 01`）起始码，x264enc 两种都会输出
- 以 AUD（NALU type=9）为界缓冲 NALU，在遇到下一个 AUD 时将缓冲区作为一个 Access Unit（AU）整体调用 `WriteSample` 发送
- 逐个 NALU 单独发送 WriteSample 会导致花屏——浏览器需要完整的 AU

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

Broker 通信模块是 Agent 与 Broker 之间的唯一通信通道，基于 HTTP REST 实现，负责服务注册、心跳上报、会话事件上报、监控数据上报、指令接收与执行、配置拉取等功能。

#### 3.2.1 整体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Broker 通信模块架构                                │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                         Agent                                        │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │                    Broker 通信模块                             │  │
│  │                                                               │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │  │
│  │  │  HTTP 客户端│  │  事件上报器 │  │  指令执行器 │           │  │
│  │  └─────────────┘  └─────────────┘  └─────────────┘           │  │
│  │                                                               │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │  │
│  │  │  心跳管理器 │  │  监控上报器 │  │  配置管理器 │           │  │
│  │  └─────────────┘  └─────────────┘  └─────────────┘           │  │
│  │                                                               │  │
│  └───────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              │ HTTP REST
                              │ (JSON)
                              ▼
                     ┌─────────────────┐
                     │    Broker       │
                     │ (K8s Service)   │
                     │                 │
                     │ broker.vdi.svc  │
                     │ .cluster.local  │
                     └─────────────────┘
```

**设计决策：**

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 通信协议 | HTTP REST | 与 Broker 设计一致，JSON 格式 |
| 连接模式 | 短连接 | REST 风格，每次请求独立 |
| 地址发现 | DNS 服务发现 | K8s 原生、自动负载均衡 |
| 多 Broker | 高可用（3 节点） | 避免单点故障 |
| 消息可靠性 | 至少一次 | 实现简单，业务层去重 |

#### 3.2.2 HTTP 客户端初始化

**客户端初始化流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    HTTP 客户端初始化流程                              │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  Agent 启动 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 1. DNS 解析 │
│ broker.vdi  │
│ .svc.cluster│
│ .local      │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 2. 创建     │
│ HTTP Client │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 3. 配置     │
│ 超时、重试  │
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
│ 5. 启动     │
│ 心跳        │
└─────────────┘
```

**连接初始化代码：**

```go
type BrokerClient struct {
    conn          *grpc.ClientConn
    client        pb.AgentServiceClient
    agentID       string
    brokerAddr    string
    state         ConnectionState
    mu            sync.RWMutex
}

// 创建 Broker 客户端
func NewBrokerClient(agentID string) (*BrokerClient, error) {
    // 1. DNS 解析 Broker 地址
    brokerAddr := "broker.vdi.svc.cluster.local:8080"

    // 2. 配置 gRPC 连接选项
    opts := []grpc.DialOption{
        // Keepalive 配置
        grpc.WithKeepaliveParams(keepalive.ClientParameters{
            Time:                10 * time.Second,  // 每 10 秒发送心跳
            Timeout:             3 * time.Second,   // 心跳超时 3 秒
            PermitWithoutStream: true,              // 无活跃流也发送心跳
        }),

        // 负载均衡策略
        grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),

        // 连接超时
        grpc.WithConnectParams(grpc.ConnectParams{
            MinConnectTimeout: 5 * time.Second,
        }),
    }

    // 3. 建立 gRPC 连接
    conn, err := grpc.Dial(brokerAddr, opts...)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to broker: %w", err)
    }

    // 4. 创建 gRPC 客户端
    client := pb.NewAgentServiceClient(conn)

    return &BrokerClient{
        conn:       conn,
        client:     client,
        agentID:    agentID,
        brokerAddr: brokerAddr,
        state:      ConnectionStateConnected,
    }, nil
}
```

**Keepalive 参数说明：**

| 参数 | 值 | 说明 |
|------|-----|------|
| Time | 10s | 每 10 秒发送一次心跳 |
| Timeout | 3s | 心跳超时 3 秒 |
| PermitWithoutStream | true | 无活跃流也发送心跳 |

#### 3.2.3 连接状态监控

**连接状态机：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    gRPC 连接状态机                                    │
└─────────────────────────────────────────────────────────────────────┘

        ┌─────────┐
        │  新建   │
        └────┬────┘
             │
             ▼
        ┌─────────┐
        │  连接中 │ ◄──── TCP 握手中
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

// 监控 gRPC 连接状态
func (c *BrokerClient) monitorConnectionState() {
    for {
        state := c.conn.GetState()

        switch state {
        case connectivity.Idle:
            c.SetState(ConnectionStateNew)
        case connectivity.Connecting:
            c.SetState(ConnectionStateConnecting)
        case connectivity.Ready:
            c.SetState(ConnectionStateConnected)
        case connectivity.TransientFailure:
            c.SetState(ConnectionStateReconnecting)
            c.startReconnect()
        case connectivity.Shutdown:
            c.SetState(ConnectionStateClosed)
        }

        // 等待状态变化
        if !c.conn.WaitForStateChange(context.Background(), state) {
            break
        }
    }
}
```

#### 3.2.4 自动重连机制

**重连策略：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    自动重连策略（指数退避）                            │
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
       ├── 成功 ──► 恢复连接，重新注册
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
       ├── 成功 ──► 恢复连接，重新注册
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
       ├── 成功 ──► 恢复连接，重新注册
       │
       ▼
┌─────────────┐
│  重连失败   │
│  记录日志   │
│  继续重试   │
└─────────────┘
```

**重连代码：**

```go
type ReconnectConfig struct {
    MaxRetries    int
    InitialDelay  time.Duration
    MaxDelay      time.Duration
    BackoffFactor float64
}

var defaultReconnectConfig = ReconnectConfig{
    MaxRetries:    10,              // 最大重试次数
    InitialDelay:  1 * time.Second, // 初始延迟
    MaxDelay:      30 * time.Second, // 最大延迟
    BackoffFactor: 2.0,             // 退避因子
}

// 启动重连
func (c *BrokerClient) startReconnect() {
    config := defaultReconnectConfig
    delay := config.InitialDelay

    for retry := 0; retry < config.MaxRetries; retry++ {
        log.Printf("Attempting reconnect %d/%d (delay: %v)...", 
            retry+1, config.MaxRetries, delay)

        time.Sleep(delay)

        // 尝试重新连接
        err := c.reconnect()
        if err == nil {
            log.Printf("Reconnect successful")
            
            // 重新注册
            err = c.register()
            if err != nil {
                log.Printf("Re-registration failed: %v", err)
                continue
            }
            
            return
        }

        log.Printf("Reconnect failed: %v", err)

        // 指数退避
        delay = time.Duration(float64(delay) * config.BackoffFactor)
        if delay > config.MaxDelay {
            delay = config.MaxDelay
        }
    }

    log.Printf("Reconnect failed after %d attempts", config.MaxRetries)
}

// 重新连接
func (c *BrokerClient) reconnect() error {
    // 关闭旧连接
    c.conn.Close()

    // 创建新连接
    opts := c.buildDialOptions()
    conn, err := grpc.Dial(c.brokerAddr, opts...)
    if err != nil {
        return err
    }

    c.conn = conn
    c.client = pb.NewAgentServiceClient(conn)

    return nil
}
```

#### 3.2.5 服务注册

**注册流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Agent 注册流程                                     │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Agent     │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. 注册请求                     │
       │  (AgentID, IP, GPU, 版本)        │
       │─────────────────────────────────►│
       │                                  │
       │                                  │  2. 验证 Agent 身份
       │                                  │  3. 存储 Agent 信息
       │                                  │  4. 分配资源池
       │                                  │
       │  5. 注册响应                     │
       │  (TURN 地址, 编码参数, 策略)      │
       │◄─────────────────────────────────│
       │                                  │
       │  6. 启动心跳                     │
       │─────────────────────────────────►│
       │                                  │
```

**注册信息：**

```protobuf
// Agent 注册请求
message RegisterRequest {
    string agent_id = 1;           // Agent 唯一标识
    string desktop_id = 2;         // 桌面实例 ID
    string ip_address = 3;         // Agent IP 地址
    string gpu_type = 4;           // GPU 类型 (nvidia/intel/amd/software)
    string gpu_name = 5;           // GPU 名称
    string agent_version = 6;      // Agent 版本
    string os_type = 7;            // 操作系统类型 (linux/windows)
    repeated string capabilities = 8; // 支持的能力
}

// Agent 注册响应
message RegisterResponse {
    bool success = 1;
    string message = 2;
    
    // Broker 下发的配置
    IceConfig ice_config = 3;      // ICE 服务器配置
    EncodingConfig encoding_config = 4; // 编码参数
    SessionPolicy session_policy = 5;   // 会话策略
    MonitorConfig monitor_config = 6;   // 监控配置
}
```

**注册代码：**

```go
// Agent 注册信息
type AgentInfo struct {
    AgentID      string
    DesktopID    string
    IPAddress    string
    GPUType      string
    GPUName      string
    AgentVersion string
    OSType       string
    Capabilities []string
}

// 注册到 Broker
func (c *BrokerClient) register() error {
    // 获取本机 IP
    ip, err := getLocalIP()
    if err != nil {
        return fmt.Errorf("failed to get local IP: %w", err)
    }

    // 检测 GPU 信息
    gpuInfo, err := DetectGPU()
    if err != nil {
        return fmt.Errorf("failed to detect GPU: %w", err)
    }

    // 构建注册请求
    req := &pb.RegisterRequest{
        AgentId:      c.agentID,
        DesktopId:    getDesktopID(),
        IpAddress:    ip,
        GpuType:      string(gpuInfo.GPUType),
        GpuName:      gpuInfo.GPUName,
        AgentVersion: getAgentVersion(),
        OsType:       runtime.GOOS,
        Capabilities: []string{"webrtc", "gstreamer", "monitoring"},
    }

    // 发送注册请求
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    resp, err := c.client.Register(ctx, req)
    if err != nil {
        return fmt.Errorf("registration failed: %w", err)
    }

    if !resp.Success {
        return fmt.Errorf("registration rejected: %s", resp.Message)
    }

    // 保存 Broker 下发的配置
    c.applyConfig(resp)

    log.Printf("Agent registered successfully: %s", c.agentID)
    return nil
}
```

#### 3.2.6 心跳上报

**心跳机制：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    心跳上报机制                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Agent     │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  每 15 秒                        │
       │  ───────────────────────────────►│
       │  Heartbeat {                     │
       │    agent_id: "xxx",              │
       │    status: "running",            │
       │    session_count: 1,             │
       │    cpu_usage: 45.2,              │
       │    memory_usage: 68.5,           │
       │    disk_usage: 72.3,             │
       │    network_latency: 15,          │
       │    uptime: 3600                  │
       │  }                               │
       │                                  │
       │◄─────────────────────────────────│
       │  HeartbeatResponse {             │
       │    ok: true,                     │
       │    commands: [...]               │
       │  }                               │
       │                                  │
```

**心跳消息：**

```protobuf
// 心跳请求
message HeartbeatRequest {
    string agent_id = 1;
    AgentStatus status = 2;
    int32 session_count = 3;
    double cpu_usage = 4;           // CPU 使用率 (%)
    double memory_usage = 5;        // 内存使用率 (%)
    double disk_usage = 6;          // 磁盘使用率 (%)
    int64 network_latency = 7;      // 网络延迟 (ms)
    int64 uptime = 8;               // 运行时长 (秒)
    int64 timestamp = 9;            // 时间戳
}

// Agent 状态枚举
enum AgentStatus {
    AGENT_STATUS_UNKNOWN = 0;
    AGENT_STATUS_RUNNING = 1;
    AGENT_STATUS_BUSY = 2;
    AGENT_STATUS_ERROR = 3;
    AGENT_STATUS_SHUTTING_DOWN = 4;
}

// 心跳响应
message HeartbeatResponse {
    bool ok = 1;
    repeated Command commands = 2;  // Broker 下发的指令
}
```

**心跳代码：**

```go
type HeartbeatManager struct {
    client     *BrokerClient
    interval   time.Duration
    ticker     *time.Ticker
    stopCh     chan struct{}
    agentInfo  *AgentInfo
}

// 创建心跳管理器
func NewHeartbeatManager(client *BrokerClient, interval time.Duration) *HeartbeatManager {
    return &HeartbeatManager{
        client:   client,
        interval: interval,
        stopCh:   make(chan struct{}),
    }
}

// 启动心跳
func (m *HeartbeatManager) Start() {
    m.ticker = time.NewTicker(m.interval)

    go func() {
        for {
            select {
            case <-m.ticker.C:
                m.sendHeartbeat()
            case <-m.stopCh:
                return
            }
        }
    }()
}

// 发送心跳
func (m *HeartbeatManager) sendHeartbeat() {
    // 收集监控数据
    metrics := m.collectMetrics()

    // 构建心跳请求
    req := &pb.HeartbeatRequest{
        AgentId:       m.client.agentID,
        Status:        m.getAgentStatus(),
        SessionCount:  m.getSessionCount(),
        CpuUsage:      metrics.CPUUsage,
        MemoryUsage:   metrics.MemoryUsage,
        DiskUsage:     metrics.DiskUsage,
        NetworkLatency: metrics.NetworkLatency,
        Uptime:        m.getUptime(),
        Timestamp:     time.Now().Unix(),
    }

    // 发送心跳
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    resp, err := m.client.client.Heartbeat(ctx, req)
    if err != nil {
        log.Printf("Heartbeat failed: %v", err)
        return
    }

    // 处理 Broker 下发的指令
    if len(resp.Commands) > 0 {
        m.processCommands(resp.Commands)
    }
}

// 收集监控数据
func (m *HeartbeatManager) collectMetrics() *MonitorMetrics {
    return &MonitorMetrics{
        CPUUsage:       getCPUUsage(),
        MemoryUsage:    getMemoryUsage(),
        DiskUsage:      getDiskUsage(),
        NetworkLatency: getNetworkLatency(),
    }
}
```

#### 3.2.7 会话事件上报

**事件类型：**

| 事件类型 | 说明 | 触发时机 |
|----------|------|----------|
| SESSION_CREATED | 会话创建 | Client 请求连接时 |
| SESSION_CONNECTED | 会话连接 | WebRTC 连接建立时 |
| SESSION_DISCONNECTED | 会话断开 | Client 断开连接时 |
| SESSION_ENDED | 会话结束 | 会话正常结束时 |
| SESSION_ERROR | 会话异常 | 发生错误时 |

**事件上报流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    会话事件上报流程                                   │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Agent     │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  会话状态变化                     │
       │                                  │
       │  1. 事件上报                     │
       │  (SESSION_CONNECTED)             │
       │─────────────────────────────────►│
       │                                  │
       │  2. 确认                         │
       │◄─────────────────────────────────│
       │                                  │
       │                                  │
       │  ... 会话进行中 ...              │
       │                                  │
       │                                  │
       │  3. 事件上报                     │
       │  (SESSION_DISCONNECTED)          │
       │─────────────────────────────────►│
       │                                  │
       │  4. 确认                         │
       │◄─────────────────────────────────│
       │                                  │
```

**事件消息：**

```protobuf
// 会话事件
message SessionEvent {
    string event_id = 1;           // 事件 ID (UUID)
    string agent_id = 2;
    string session_id = 3;
    SessionEventType type = 4;
    int64 timestamp = 5;
    string reason = 6;             // 事件原因
    map<string, string> metadata = 7; // 扩展信息
}

// 事件类型枚举
enum SessionEventType {
    SESSION_EVENT_UNKNOWN = 0;
    SESSION_EVENT_CREATED = 1;
    SESSION_EVENT_CONNECTED = 2;
    SESSION_EVENT_DISCONNECTED = 3;
    SESSION_EVENT_ENDED = 4;
    SESSION_EVENT_ERROR = 5;
}

// 事件上报响应
message SessionEventResponse {
    bool success = 1;
    string message = 2;
}
```

**事件上报代码：**

```go
type SessionEventReporter struct {
    client   *BrokerClient
    retryCh  chan *pb.SessionEvent
}

// 创建事件上报器
func NewSessionEventReporter(client *BrokerClient) *SessionEventReporter {
    return &SessionEventReporter{
        client:  client,
        retryCh: make(chan *pb.SessionEvent, 100),
    }
}

// 上报会话事件
func (r *SessionEventReporter) ReportEvent(eventType pb.SessionEventType, sessionID, reason string) {
    event := &pb.SessionEvent{
        EventId:   uuid.New().String(),
        AgentId:   r.client.agentID,
        SessionId: sessionID,
        Type:      eventType,
        Timestamp: time.Now().Unix(),
        Reason:    reason,
    }

    // 异步上报
    go func() {
        err := r.sendEvent(event)
        if err != nil {
            log.Printf("Failed to report session event: %v", err)
            // 放入重试队列
            r.retryCh <- event
        }
    }()
}

// 发送事件
func (r *SessionEventReporter) sendEvent(event *pb.SessionEvent) error {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    resp, err := r.client.client.ReportSessionEvent(ctx, event)
    if err != nil {
        return err
    }

    if !resp.Success {
        return fmt.Errorf("event rejected: %s", resp.Message)
    }

    return nil
}

// 重试队列处理
func (r *SessionEventReporter) processRetryQueue() {
    for event := range r.retryCh {
        // 指数退避重试
        for retry := 0; retry < 3; retry++ {
            time.Sleep(time.Duration(retry+1) * time.Second)
            
            err := r.sendEvent(event)
            if err == nil {
                break
            }
            
            log.Printf("Retry %d failed: %v", retry+1, err)
        }
    }
}
```

#### 3.2.8 监控数据上报

**监控指标：**

| 指标 | 说明 | 采集周期 |
|------|------|----------|
| CPU 使用率 | 当前 CPU 使用百分比 | 15 秒 |
| 内存使用率 | 当前内存使用百分比 | 15 秒 |
| 磁盘使用率 | 当前磁盘使用百分比 | 15 秒 |
| 网络延迟 | 到 Broker 的网络延迟 | 15 秒 |
| 会话时长 | 当前会话持续时间 | 15 秒 |

**上报流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    监控数据上报流程                                   │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  采集模块   │
│  (每 15 秒) │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  缓存模块   │
│  (批量收集) │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  压缩模块   │
│  (可选)     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  上报模块   │
│  (gRPC)     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Broker    │
└─────────────┘
```

**监控消息：**

```protobuf
// 监控数据上报请求
message MonitorDataRequest {
    string agent_id = 1;
    repeated MonitorMetric metrics = 2;
    int64 timestamp = 3;
}

// 监控指标
message MonitorMetric {
    string name = 1;          // 指标名称
    double value = 2;         // 指标值
    string unit = 3;          // 单位
    map<string, string> labels = 4; // 标签
}

// 监控数据上报响应
message MonitorDataResponse {
    bool success = 1;
    string message = 2;
}
```

**监控上报代码：**

```go
type MonitorReporter struct {
    client    *BrokerClient
    interval  time.Duration
    ticker    *time.Ticker
    stopCh    chan struct{}
    buffer    []*pb.MonitorMetric
    mu        sync.Mutex
}

// 创建监控上报器
func NewMonitorReporter(client *BrokerClient, interval time.Duration) *MonitorReporter {
    return &MonitorReporter{
        client:   client,
        interval: interval,
        stopCh:   make(chan struct{}),
    }
}

// 启动监控上报
func (r *MonitorReporter) Start() {
    r.ticker = time.NewTicker(r.interval)

    go func() {
        for {
            select {
            case <-r.ticker.C:
                r.collectAndReport()
            case <-r.stopCh:
                return
            }
        }
    }()
}

// 采集并上报
func (r *MonitorReporter) collectAndReport() {
    // 采集监控数据
    metrics := r.collectMetrics()

    // 批量上报
    err := r.reportMetrics(metrics)
    if err != nil {
        log.Printf("Failed to report monitor data: %v", err)
    }
}

// 采集监控数据
func (r *MonitorReporter) collectMetrics() []*pb.MonitorMetric {
    metrics := make([]*pb.MonitorMetric, 0)

    // CPU 使用率
    cpuUsage := getCPUUsage()
    metrics = append(metrics, &pb.MonitorMetric{
        Name:  "cpu_usage",
        Value: cpuUsage,
        Unit:  "%",
    })

    // 内存使用率
    memUsage := getMemoryUsage()
    metrics = append(metrics, &pb.MonitorMetric{
        Name:  "memory_usage",
        Value: memUsage,
        Unit:  "%",
    })

    // 磁盘使用率
    diskUsage := getDiskUsage()
    metrics = append(metrics, &pb.MonitorMetric{
        Name:  "disk_usage",
        Value: diskUsage,
        Unit:  "%",
    })

    // 网络延迟
    latency := getNetworkLatency()
    metrics = append(metrics, &pb.MonitorMetric{
        Name:  "network_latency",
        Value: float64(latency),
        Unit:  "ms",
    })

    // 会话时长
    sessionDuration := getSessionDuration()
    metrics = append(metrics, &pb.MonitorMetric{
        Name:  "session_duration",
        Value: float64(sessionDuration),
        Unit:  "s",
    })

    return metrics
}

// 上报监控数据
func (r *MonitorReporter) reportMetrics(metrics []*pb.MonitorMetric) error {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    req := &pb.MonitorDataRequest{
        AgentId:   r.client.agentID,
        Metrics:   metrics,
        Timestamp: time.Now().Unix(),
    }

    resp, err := r.client.client.ReportMonitorData(ctx, req)
    if err != nil {
        return err
    }

    if !resp.Success {
        return fmt.Errorf("monitor data rejected: %s", resp.Message)
    }

    return nil
}
```

#### 3.2.9 指令接收与执行

**指令类型：**

| 指令类型 | 说明 | 参数 |
|----------|------|------|
| LOCK_SCREEN | 锁定桌面 | - |
| UNLOCK_SCREEN | 解锁桌面 | - |
| LOGOFF | 注销用户 | reason |
| REBOOT | 重启桌面 | reason |
| UPDATE_CONFIG | 更新配置 | config |
| UPDATE_AGENT | 升级 Agent | version, url |

**指令执行流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    指令执行流程                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Broker    │                    │   Agent     │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. 指令下发                     │
       │  (LOCK_SCREEN)                   │
       │─────────────────────────────────►│
       │                                  │
       │                                  │  2. 验证指令
       │                                  │  3. 执行指令
       │                                  │  4. 锁定桌面
       │                                  │
       │  5. 执行结果                     │
       │  (SUCCESS)                       │
       │◄─────────────────────────────────│
       │                                  │
```

**指令消息：**

```protobuf
// 指令定义
message Command {
    string command_id = 1;         // 指令 ID
    CommandType type = 2;          // 指令类型
    map<string, string> params = 3; // 指令参数
    int64 timestamp = 4;           // 指令时间戳
}

// 指令类型枚举
enum CommandType {
    COMMAND_UNKNOWN = 0;
    COMMAND_LOCK_SCREEN = 1;
    COMMAND_UNLOCK_SCREEN = 2;
    COMMAND_LOGOFF = 3;
    COMMAND_REBOOT = 4;
    COMMAND_UPDATE_CONFIG = 5;
    COMMAND_UPDATE_AGENT = 6;
}

// 指令执行结果
message CommandResult {
    string command_id = 1;
    bool success = 2;
    string message = 3;
    int64 executed_at = 4;
}
```

**指令执行代码：**

```go
type CommandExecutor struct {
    client      *BrokerClient
    handlers    map[pb.CommandType]CommandHandler
    executedIDs sync.Map // 记录已执行的指令 ID，保证幂等性
}

// 指令处理器接口
type CommandHandler interface {
    Execute(params map[string]string) error
}

// 创建指令执行器
func NewCommandExecutor(client *BrokerClient) *CommandExecutor {
    executor := &CommandExecutor{
        client:   client,
        handlers: make(map[pb.CommandType]CommandHandler),
    }

    // 注册指令处理器
    executor.handlers[pb.CommandType_COMMAND_LOCK_SCREEN] = &LockScreenHandler{}
    executor.handlers[pb.CommandType_COMMAND_UNLOCK_SCREEN] = &UnlockScreenHandler{}
    executor.handlers[pb.CommandType_COMMAND_LOGOFF] = &LogoffHandler{}
    executor.handlers[pb.CommandType_COMMAND_REBOOT] = &RebootHandler{}
    executor.handlers[pb.CommandType_COMMAND_UPDATE_CONFIG] = &UpdateConfigHandler{}
    executor.handlers[pb.CommandType_COMMAND_UPDATE_AGENT] = &UpdateAgentHandler{}

    return executor
}

// 执行指令
func (e *CommandExecutor) Execute(cmd *pb.Command) {
    // 检查是否已执行（幂等性）
    if _, loaded := e.executedIDs.LoadOrStore(cmd.CommandId, true); loaded {
        log.Printf("Command %s already executed, skipping", cmd.CommandId)
        return
    }

    // 查找处理器
    handler, ok := e.handlers[cmd.Type]
    if !ok {
        log.Printf("Unknown command type: %v", cmd.Type)
        e.reportResult(cmd.CommandId, false, "unknown command type")
        return
    }

    // 执行指令
    err := handler.Execute(cmd.Params)
    if err != nil {
        log.Printf("Command %s execution failed: %v", cmd.CommandId, err)
        e.reportResult(cmd.CommandId, false, err.Error())
        return
    }

    log.Printf("Command %s executed successfully", cmd.CommandId)
    e.reportResult(cmd.CommandId, true, "success")
}

// 上报执行结果
func (e *CommandExecutor) reportResult(commandID string, success bool, message string) {
    result := &pb.CommandResult{
        CommandId:   commandID,
        Success:     success,
        Message:     message,
        ExecutedAt:  time.Now().Unix(),
    }

    go func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()

        _, err := e.client.client.ReportCommandResult(ctx, result)
        if err != nil {
            log.Printf("Failed to report command result: %v", err)
        }
    }()
}

// 锁屏指令处理器
type LockScreenHandler struct{}

func (h *LockScreenHandler) Execute(params map[string]string) error {
    // 执行锁屏操作
    return lockScreen()
}

// 注销指令处理器
type LogoffHandler struct{}

func (h *LogoffHandler) Execute(params map[string]string) error {
    reason := params["reason"]
    log.Printf("Logging off user, reason: %s", reason)
    return logoffUser()
}
```

#### 3.2.10 配置拉取

**配置类型：**

| 配置类型 | 说明 | 更新频率 |
|----------|------|----------|
| 编码参数 | 分辨率、帧率、码率 | 按需 |
| TURN 地址 | TURN 服务器地址和凭证 | 定期 |
| 会话策略 | 超时时间、最大会话数 | 按需 |
| 监控配置 | 采集周期、上报周期 | 按需 |

**配置拉取流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    配置拉取流程                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Agent     │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. 注册时自动获取               │
       │◄─────────────────────────────────│
       │  (编码参数、TURN 地址等)          │
       │                                  │
       │                                  │
       │  2. 定期刷新 (每 5 分钟)         │
       │  GetConfigRequest                │
       │─────────────────────────────────►│
       │                                  │
       │  3. 返回最新配置                 │
       │  GetConfigResponse               │
       │◄─────────────────────────────────│
       │                                  │
       │                                  │
       │  4. Broker 推送配置变更          │
       │  ConfigUpdateNotification        │
       │◄─────────────────────────────────│
       │                                  │
       │  5. 确认                         │
       │─────────────────────────────────►│
       │                                  │
```

**配置消息：**

```protobuf
// 获取配置请求
message GetConfigRequest {
    string agent_id = 1;
    string current_version = 2; // 当前配置版本
}

// 获取配置响应
message GetConfigResponse {
    bool success = 1;
    AgentConfig config = 2;
    string version = 3; // 配置版本
}

// Agent 配置
message AgentConfig {
    EncodingConfig encoding_config = 1;
    IceConfig ice_config = 2;
    SessionPolicy session_policy = 3;
    MonitorConfig monitor_config = 4;
}

// 编码配置
message EncodingConfig {
    int32 width = 1;
    int32 height = 2;
    int32 framerate = 3;
    int32 bitrate = 4;
}

// ICE 配置
message IceConfig {
    repeated IceServer servers = 1;
}

message IceServer {
    repeated string urls = 1;
    string username = 2;
    string credential = 3;
}

// 会话策略
message SessionPolicy {
    int32 max_sessions = 1;
    int32 session_timeout = 2; // 秒
    bool allow_reconnect = 3;
    int32 reconnect_timeout = 4; // 秒
}

// 监控配置
message MonitorConfig {
    int32 collect_interval = 1;  // 采集周期 (秒)
    int32 report_interval = 2;   // 上报周期 (秒)
}

// 配置更新通知
message ConfigUpdateNotification {
    string agent_id = 1;
    AgentConfig config = 2;
    string version = 3;
}
```

**配置管理代码：**

```go
type ConfigManager struct {
    client       *BrokerClient
    currentConfig *pb.AgentConfig
    version      string
    refreshInterval time.Duration
    mu           sync.RWMutex
}

// 创建配置管理器
func NewConfigManager(client *BrokerClient, refreshInterval time.Duration) *ConfigManager {
    return &ConfigManager{
        client:          client,
        refreshInterval: refreshInterval,
    }
}

// 启动配置刷新
func (m *ConfigManager) Start() {
    // 定期刷新配置
    go func() {
        ticker := time.NewTicker(m.refreshInterval)
        defer ticker.Stop()

        for range ticker.C {
            m.refreshConfig()
        }
    }()
}

// 刷新配置
func (m *ConfigManager) refreshConfig() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    req := &pb.GetConfigRequest{
        AgentId:        m.client.agentID,
        CurrentVersion: m.version,
    }

    resp, err := m.client.client.GetConfig(ctx, req)
    if err != nil {
        log.Printf("Failed to refresh config: %v", err)
        return
    }

    if !resp.Success {
        log.Printf("Config refresh failed: %s", resp.Message)
        return
    }

    // 更新配置
    m.mu.Lock()
    m.currentConfig = resp.Config
    m.version = resp.Version
    m.mu.Unlock()

    // 应用配置
    m.applyConfig(resp.Config)

    log.Printf("Config refreshed: version=%s", resp.Version)
}

// 应用配置
func (m *ConfigManager) applyConfig(config *pb.AgentConfig) {
    // 应用编码配置
    if config.EncodingConfig != nil {
        m.applyEncodingConfig(config.EncodingConfig)
    }

    // 应用 ICE 配置
    if config.IceConfig != nil {
        m.applyIceConfig(config.IceConfig)
    }

    // 应用会话策略
    if config.SessionPolicy != nil {
        m.applySessionPolicy(config.SessionPolicy)
    }

    // 应用监控配置
    if config.MonitorConfig != nil {
        m.applyMonitorConfig(config.MonitorConfig)
    }
}

// 处理配置更新通知
func (m *ConfigManager) HandleConfigUpdate(notification *pb.ConfigUpdateNotification) {
    m.mu.Lock()
    m.currentConfig = notification.Config
    m.version = notification.Version
    m.mu.Unlock()

    m.applyConfig(notification.Config)

    log.Printf("Config updated via notification: version=%s", notification.Version)
}
```

#### 3.2.11 错误处理

**错误类型：**

| 错误类型 | 说明 | 处理方式 |
|----------|------|----------|
| 网络错误 | 连接超时、连接断开、DNS 解析失败 | 自动重连 |
| 业务错误 | 注册失败、指令执行失败 | 记录日志、上报 Broker |
| 超时错误 | 请求超时 | 重试 |

**错误处理流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    错误处理流程                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  错误发生   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  错误分类   │
└──────┬──────┘
       │
       ├── 网络错误 ──► 自动重连
       │
       ├── 业务错误 ──► 记录日志、上报 Broker
       │
       └── 超时错误 ──► 重试
```

**错误上报消息：**

```protobuf
// 错误上报
message ErrorReport {
    string agent_id = 1;
    string error_type = 2;
    string error_message = 3;
    string stack_trace = 4;
    int64 timestamp = 5;
    map<string, string> context = 6;
}

// 错误上报响应
message ErrorReportResponse {
    bool success = 1;
}
```

**错误处理代码：**

```go
type ErrorHandler struct {
    client *BrokerClient
}

// 处理错误
func (h *ErrorHandler) HandleError(err error, context map[string]string) {
    // 记录日志
    log.Printf("Error: %v", err)

    // 分类处理
    switch {
    case isNetworkError(err):
        // 网络错误，触发重连
        h.client.startReconnect()
    case isTimeoutError(err):
        // 超时错误，记录日志
        log.Printf("Timeout error: %v", err)
    default:
        // 其他错误，上报 Broker
        h.reportError(err, context)
    }
}

// 上报错误
func (h *ErrorHandler) reportError(err error, context map[string]string) {
    report := &pb.ErrorReport{
        AgentId:      h.client.agentID,
        ErrorType:    getErrorType(err),
        ErrorMessage: err.Error(),
        StackTrace:   getStackTrace(),
        Timestamp:    time.Now().Unix(),
        Context:      context,
    }

    go func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()

        _, err := h.client.client.ReportError(ctx, report)
        if err != nil {
            log.Printf("Failed to report error: %v", err)
        }
    }()
}

// 判断是否为网络错误
func isNetworkError(err error) bool {
    // 检查是否为网络相关错误
    return strings.Contains(err.Error(), "connection refused") ||
        strings.Contains(err.Error(), "connection reset") ||
        strings.Contains(err.Error(), "network is unreachable")
}

// 判断是否为超时错误
func isTimeoutError(err error) bool {
    return strings.Contains(err.Error(), "timeout") ||
        strings.Contains(err.Error(), "deadline exceeded")
}
```

#### 3.2.12 Broker 通信模块完整生命周期

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Broker 通信模块完整生命周期                        │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  Agent 启动 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 1. DNS 解析 │
│ Broker 地址 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 2. 创建     │
│ gRPC 连接   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 3. 注册     │
│ 到 Broker   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 4. 获取     │
│ 配置        │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 5. 启动     │
│ 心跳        │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 6. 启动     │
│ 监控上报    │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ 7. 等待     │
│ 指令        │
└──────┬──────┘
       │
       ├── 正常工作 ──────────────────────┐
       │                                  │
       │ 连接中断                         │
       ▼                                  │
┌─────────────┐                          │
│ 8. 自动重连 │                          │
└──────┬──────┘                          │
       │                                  │
       ├── 成功 ──► 重新注册，继续工作    │
       │                                  │
       ▼                                  │
┌─────────────┐                          │
│ 9. 重连失败 │                          │
│ 记录日志    │                          │
└──────┬──────┘                          │
       │                                  │
       ▼                                  │
┌─────────────┐                          │
│ 10. 继续重试│◄─────────────────────────┘
└──────┬──────┘
       │
       │ 收到关闭信号
       ▼
┌─────────────┐
│ 11. 优雅关闭│
│ 通知 Broker │
└─────────────┘
```

### 3.3 会话管理模块

会话管理模块负责管理用户会话的完整生命周期，包括会话状态管理、用户认证、输入事件处理、剪贴板同步、会话超时处理等功能。

#### 3.3.1 整体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                    会话管理模块架构                                   │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                       会话管理模块                                   │
│                                                                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│  │  会话状态   │  │  用户认证   │  │  输入事件   │                 │
│  │  管理器     │  │  管理器     │  │  处理器     │                 │
│  └─────────────┘  └─────────────┘  └─────────────┘                 │
│                                                                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│  │  剪贴板     │  │  会话超时   │  │  会话事件   │                 │
│  │  同步器     │  │  管理器     │  │  上报器     │                 │
│  └─────────────┘  └─────────────┘  └─────────────┘                 │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
        │              │              │
        ▼              ▼              ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│   Broker    │ │   Client    │ │   桌面环境  │
│   (认证)    │ │ (输入事件)  │ │ (执行操作)  │
└─────────────┘ └─────────────┘ └─────────────┘
```

**设计决策：**

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 会话模型 | 一对一 | 一个桌面实例只允许一个用户会话 |
| 新会话处理 | 替换旧会话 | 新会话踢掉旧会话 |
| 认证方式 | Token 认证 | Broker 签发的 Token |
| 剪贴板 | 双向同步 | Client ↔ Agent |
| 超时方式 | 空闲超时 | 用户可配置 |
| 输入格式 | 二进制 | Pion DataChannel 二进制格式 |

#### 3.3.2 会话状态机

**会话状态定义：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                      会话状态机                                      │
└─────────────────────────────────────────────────────────────────────┘

        ┌─────────┐
        │  新建   │ ◄──── Client 请求连接
        └────┬────┘
             │
             │ Token 验证成功
             ▼
        ┌─────────┐
        │  就绪   │ ◄──── 认证完成，等待 WebRTC 连接
        └────┬────┘
             │
             │ WebRTC 连接建立
             ▼
        ┌─────────┐
        │  活跃   │ ◄──── 正常工作状态
        └────┬────┘
             │
             │ 连接中断
             ▼
        ┌─────────┐
        │  断开   │ ◄──── 等待重连
        └────┬────┘
             │
        ┌────┴────┐
        │         │
        ▼         ▼
   ┌─────────┐ ┌─────────┐
   │  活跃   │ │  结束   │
   │ (重连)  │ │         │
   └─────────┘ └─────────┘
```

**状态转换规则：**

| 当前状态 | 触发事件 | 目标状态 | 说明 |
|----------|----------|----------|------|
| 新建 | Token 验证成功 | 就绪 | 认证完成 |
| 新建 | Token 验证失败 | 结束 | 认证失败 |
| 就绪 | WebRTC 连接建立 | 活跃 | 连接成功 |
| 就绪 | 连接超时 | 结束 | 连接失败 |
| 活跃 | 连接中断 | 断开 | 网络中断 |
| 活跃 | 用户登出 | 结束 | 正常结束 |
| 活跃 | 空闲超时 | 断开 | 用户无操作 |
| 断开 | 重连成功 | 活跃 | 恢复连接 |
| 断开 | 重连超时 | 结束 | 重连失败 |

**会话状态代码：**

```go
type SessionState string

const (
    SessionStateNew         SessionState = "new"
    SessionStateReady       SessionState = "ready"
    SessionStateActive      SessionState = "active"
    SessionStateDisconnected SessionState = "disconnected"
    SessionStateClosed      SessionState = "closed"
)

type Session struct {
    ID           string
    UserID       string
    DesktopID    string
    State        SessionState
    CreatedAt    time.Time
    ConnectedAt  time.Time
    DisconnectedAt time.Time
    ClosedAt     time.Time
    LastActivity time.Time
    mu           sync.RWMutex
}

// 状态转换
func (s *Session) TransitionTo(newState SessionState) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    // 验证状态转换是否合法
    if !s.canTransitionTo(newState) {
        return fmt.Errorf("invalid transition: %s -> %s", s.State, newState)
    }

    oldState := s.State
    s.State = newState

    // 记录状态变化时间
    switch newState {
    case SessionStateReady:
        s.CreatedAt = time.Now()
    case SessionStateActive:
        s.ConnectedAt = time.Now()
    case SessionStateDisconnected:
        s.DisconnectedAt = time.Now()
    case SessionStateClosed:
        s.ClosedAt = time.Now()
    }

    log.Printf("Session %s: %s -> %s", s.ID, oldState, newState)
    return nil
}

// 检查状态转换是否合法
func (s *Session) canTransitionTo(newState SessionState) bool {
    validTransitions := map[SessionState][]SessionState{
        SessionStateNew:          {SessionStateReady, SessionStateClosed},
        SessionStateReady:        {SessionStateActive, SessionStateClosed},
        SessionStateActive:       {SessionStateDisconnected, SessionStateClosed},
        SessionStateDisconnected: {SessionStateActive, SessionStateClosed},
    }

    for _, valid := range validTransitions[s.State] {
        if valid == newState {
            return true
        }
    }
    return false
}
```

#### 3.3.3 会话生命周期

**会话创建流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    会话创建流程                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐                    ┌─────────────┐
│   Client    │                    │   Agent     │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘                    └──────┬──────┘
       │                                  │                                  │
       │  1. 请求连接                     │                                  │
       │  (携带 Token)                    │                                  │
       │─────────────────────────────────►│                                  │
       │                                  │                                  │
       │                                  │  2. 验证 Token                   │
       │                                  │─────────────────────────────────►│
       │                                  │                                  │
       │                                  │  3. Token 有效                   │
       │                                  │◄─────────────────────────────────│
       │                                  │                                  │
       │                                  │  4. 检查是否有旧会话             │
       │                                  │                                  │
       │                                  │  5. 创建新会话                   │
       │                                  │                                  │
       │  6. 返回会话信息                 │                                  │
       │  (SessionID)                     │                                  │
       │◄─────────────────────────────────│                                  │
       │                                  │                                  │
       │  7. WebRTC Offer                 │                                  │
       │─────────────────────────────────►│                                  │
       │                                  │                                  │
       │  8. WebRTC Answer                │                                  │
       │◄─────────────────────────────────│                                  │
       │                                  │                                  │
       │  9. ICE 连接建立                 │                                  │
       │◄═════════════════════════════════►│                                  │
       │                                  │                                  │
       │                                  │  10. 上报会话事件                │
       │                                  │  (SESSION_CONNECTED)             │
       │                                  │─────────────────────────────────►│
       │                                  │                                  │
```

**新会话替换旧会话流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    新会话替换旧会话流程                               │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Agent     │                    │   旧会话    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. 新会话请求                   │
       │  (同一用户)                      │
       │                                  │
       │  2. 检测到旧会话                 │
       │                                  │
       │  3. 通知旧会话被替换             │
       │─────────────────────────────────►│
       │                                  │
       │                                  │  4. 发送 SESSION_REPLACED 事件给旧 Client
       │                                  │
       │  5. 关闭旧 WebRTC 连接           │
       │─────────────────────────────────►│
       │                                  │
       │                                  │  6. 清理旧会话资源
       │                                  │
       │  7. 创建新会话                   │
       │                                  │
       │  8. 建立新 WebRTC 连接           │
       │                                  │
```

**会话替换代码：**

```go
type SessionManager struct {
    sessions sync.Map // map[string]*Session
    mu       sync.RWMutex
}

// 创建或替换会话
func (m *SessionManager) CreateOrReplaceSession(userID string) (*Session, error) {
    m.mu.Lock()
    defer m.mu.Unlock()

    // 检查是否有旧会话
    if oldSession, exists := m.getSessionByUserID(userID); exists {
        // 通知旧会话被替换
        m.notifySessionReplaced(oldSession, "new_session_created")
        
        // 关闭旧会话
        m.closeSession(oldSession)
    }

    // 创建新会话
    session := &Session{
        ID:        uuid.New().String(),
        UserID:    userID,
        State:     SessionStateNew,
        CreatedAt: time.Now(),
    }

    m.sessions.Store(session.ID, session)

    log.Printf("Session created: %s (user: %s)", session.ID, userID)
    return session, nil
}

// 通知旧会话被替换
func (m *SessionManager) notifySessionReplaced(session *Session, reason string) {
    event := &SessionEvent{
        Type:      SessionEventReplaced,
        SessionID: session.ID,
        Reason:    reason,
        Timestamp: time.Now(),
    }

    // 发送给旧 Client
    m.sendEventToClient(session, event)
}

// 关闭会话
func (m *SessionManager) closeSession(session *Session) {
    // 关闭 WebRTC 连接
    if session.WebRTCConnection != nil {
        session.WebRTCConnection.Close()
    }

    // 更新状态
    session.TransitionTo(SessionStateClosed)

    // 从会话列表移除
    m.sessions.Delete(session.ID)

    log.Printf("Session closed: %s", session.ID)
}
```

#### 3.3.4 会话恢复

**断线重连流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    断线重连流程                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Client    │                    │   Agent     │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. 连接中断                     │
       │  (网络故障)                      │
       │                                  │
       │                                  │  2. 检测到连接中断
       │                                  │  3. 会话状态：活跃 → 断开
       │                                  │  4. 启动重连定时器
       │                                  │
       │  5. 重新连接                     │
       │  (携带 SessionID + Token)        │
       │─────────────────────────────────►│
       │                                  │
       │                                  │  6. 验证 SessionID + Token
       │                                  │  7. 恢复会话状态
       │                                  │
       │  8. 重新建立 WebRTC 连接         │
       │◄═════════════════════════════════►│
       │                                  │
       │                                  │  9. 会话状态：断开 → 活跃
       │                                  │
```

**会话恢复代码：**

```go
// 会话恢复
func (m *SessionManager) RestoreSession(sessionID, token string) (*Session, error) {
    // 1. 查找会话
    session, exists := m.getSessionByID(sessionID)
    if !exists {
        return nil, fmt.Errorf("session not found: %s", sessionID)
    }

    // 2. 验证会话状态
    if session.State != SessionStateDisconnected {
        return nil, fmt.Errorf("session is not in disconnected state: %s", session.State)
    }

    // 3. 验证 Token
    if !m.validateToken(token) {
        return nil, fmt.Errorf("invalid token")
    }

    // 4. 检查重连超时
    if time.Since(session.DisconnectedAt) > m.config.ReconnectTimeout {
        return nil, fmt.Errorf("reconnect timeout exceeded")
    }

    // 5. 恢复会话
    session.TransitionTo(SessionStateActive)

    log.Printf("Session restored: %s", sessionID)
    return session, nil
}
```

#### 3.3.5 用户认证

**Token 验证流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Token 验证流程                                     │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐                    ┌─────────────┐
│   Client    │                    │   Agent     │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘                    └──────┬──────┘
       │                                  │                                  │
       │  1. 请求连接                     │                                  │
       │  (携带 Token)                    │                                  │
       │─────────────────────────────────►│                                  │
       │                                  │                                  │
       │                                  │  2. 解析 Token                   │
       │                                  │  (本地验证签名)                   │
       │                                  │                                  │
       │                                  │  3. 验证 Token 有效性            │
       │                                  │─────────────────────────────────►│
       │                                  │                                  │
       │                                  │  4. 返回验证结果                  │
       │                                  │◄─────────────────────────────────│
       │                                  │                                  │
       │  5. 返回连接结果                 │                                  │
       │◄─────────────────────────────────│                                  │
       │                                  │                                  │
```

**Token 结构：**

```go
// Token 结构
type TokenClaims struct {
    UserID    string    `json:"user_id"`
    DesktopID string    `json:"desktop_id"`
    SessionID string    `json:"session_id"`
    ExpiresAt time.Time `json:"expires_at"`
    IssuedAt  time.Time `json:"issued_at"`
    Issuer    string    `json:"issuer"`
}

// Token 验证
func (m *SessionManager) validateToken(tokenString string) (*TokenClaims, error) {
    // 1. 解析 Token
    token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
        // 验证签名算法
        if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
        }
        
        // 返回公钥（从 Broker 获取）
        return m.getPublicKey()
    })
    if err != nil {
        return nil, fmt.Errorf("failed to parse token: %w", err)
    }

    // 2. 验证 Token 有效性
    if !token.Valid {
        return nil, fmt.Errorf("invalid token")
    }

    // 3. 提取 Claims
    claims, ok := token.Claims.(jwt.MapClaims)
    if !ok {
        return nil, fmt.Errorf("invalid token claims")
    }

    // 4. 验证过期时间
    expiresAt := time.Unix(int64(claims["expires_at"].(float64)), 0)
    if time.Now().After(expiresAt) {
        return nil, fmt.Errorf("token expired")
    }

    // 5. 返回 Claims
    return &TokenClaims{
        UserID:    claims["user_id"].(string),
        DesktopID: claims["desktop_id"].(string),
        SessionID: claims["session_id"].(string),
        ExpiresAt: expiresAt,
    }, nil
}
```

#### 3.3.6 输入事件处理

> **⚠️ MVP 验证结果**：MVP 阶段使用 **xdotool 命令行工具**（通过 `os/exec` 调用）作为输入注入方案，而非正式架构的 XTest CGo 直接调用。此外，由于 Pion Lite ICE DataChannel 单向问题（见 §3.1.5 加注），MVP 中输入事件通过 **WebSocket 信令通道** 传输而非 DataChannel。两者均为临时方案，正式版本规划如下：
>
> | 维度 | MVP 临时方案 | 正式架构方案 |
> |------|------------|------------|
> | 输入传输通道 | WebSocket 信令通道 | WebRTC DataChannel（control 通道） |
> | 输入注入方式 | xdotool（`exec.Command`） | XTest 扩展（CGo 直接调用） |
> | 输入事件来源 | WebSocket `default` 分支 | DataChannel `OnMessage` 回调 |
>
> **MVP 输入处理关键经验（xdotool 方案）：**
>
> 1. **字母键必须用小写 keysym**：xdotool 对大写 keysym（如 `A`）会自动按 Shift 修饰键，Caps Lock ON 时 Shift+Caps Lock = 小写，导致大小写反转。正确做法是用小写 keysym（`a`-`z`），让 X11 根据 Caps Lock 状态自然决定大小写
> 2. **Caps Lock 状态同步**：客户端发送 `capsLock` 状态，Agent 在每次字母键按下前用同步 `.Run()` 确保 X11 的 Caps Lock 与客户端一致。不能用异步 `.Start()`，否则按键可能在 Caps Lock 切换完成前执行
> 3. **鼠标位置合并（coalescing）**：鼠标移动事件高频，通过 channel + drain 策略，只执行最新位置，丢弃中间帧
> 4. **非阻塞执行**：`exec.Command.Start()` 优于 `Run()`，避免阻塞消息处理循环
> 5. **去掉 `--sync`**：xdotool 的 `--sync` 在高频场景下造成严重延迟
> 6. **修饰键处理**：`xdotool keydown "ctrl+A"` 不等于"按住 ctrl 按 A"，应使用 `xdotool key --clearmodifiers ctrl+A`
> 7. **KeyCode 体系**：浏览器 `e.keyCode` 是 Windows Virtual Key Code（A=65），不是 USB HID Code（A=4）
> 8. **Zombie 进程回收**：`exec.Command.Start()` 启动的进程必须有人 wait，否则变成 zombie。用 `syscall.Wait4(-1, ...)` goroutine 回收

**输入事件架构：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    输入事件处理架构                                   │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐      ┌─────────────┐      ┌─────────────┐
│   Client    │      │   Agent     │      │   桌面环境  │
│             │      │             │      │             │
│  键盘输入   │─────►│  事件解析   │─────►│  事件注入   │
│  鼠标输入   │      │  事件路由   │      │  (X11/Win32)│
│  触控输入   │      │             │      │             │
└─────────────┘      └─────────────┘      └─────────────┘
```

**输入事件类型：**

```protobuf
// 输入事件类型
enum InputEventType {
    INPUT_EVENT_UNKNOWN = 0;
    
    // 键盘事件
    INPUT_EVENT_KEY_DOWN = 1;
    INPUT_EVENT_KEY_UP = 2;
    
    // 鼠标事件
    INPUT_EVENT_MOUSE_MOVE = 10;
    INPUT_EVENT_MOUSE_DOWN = 11;
    INPUT_EVENT_MOUSE_UP = 12;
    INPUT_EVENT_MOUSE_SCROLL = 13;
    
    // 触控事件
    INPUT_EVENT_TOUCH_START = 20;
    INPUT_EVENT_TOUCH_MOVE = 21;
    INPUT_EVENT_TOUCH_END = 22;
    
    // 手势事件
    INPUT_EVENT_GESTURE_PINCH = 30;
    INPUT_EVENT_GESTURE_SWIPE = 31;
    INPUT_EVENT_GESTURE_ROTATE = 32;
}

// 输入事件
message InputEvent {
    InputEventType type = 1;
    int64 timestamp = 2;
    
    oneof payload {
        KeyEvent key_event = 3;
        MouseEvent mouse_event = 4;
        TouchEvent touch_event = 5;
        GestureEvent gesture_event = 6;
    }
}

// 键盘事件
message KeyEvent {
    int32 key_code = 1;          // 按键码
    string key_name = 2;         // 按键名称
    bool ctrl = 3;               // Ctrl 键状态
    bool shift = 4;              // Shift 键状态
    bool alt = 5;                // Alt 键状态
    bool meta = 6;               // Meta/Windows 键状态
}

// 鼠标事件
message MouseEvent {
    int32 x = 1;                 // X 坐标
    int32 y = 2;                 // Y 坐标
    MouseButton button = 3;      // 鼠标按钮
    int32 delta_x = 4;           // 滚轮 X 偏移
    int32 delta_y = 5;           // 滚轮 Y 偏移
}

// 鼠标按钮
enum MouseButton {
    MOUSE_BUTTON_NONE = 0;
    MOUSE_BUTTON_LEFT = 1;
    MOUSE_BUTTON_RIGHT = 2;
    MOUSE_BUTTON_MIDDLE = 3;
}

// 触控事件
message TouchEvent {
    int32 touch_id = 1;          // 触摸点 ID
    int32 x = 2;                 // X 坐标
    int32 y = 3;                 // Y 坐标
    float pressure = 4;          // 压力 (0.0-1.0)
}

// 手势事件
message GestureEvent {
    GestureType type = 1;
    float scale = 2;             // 缩放比例
    float rotation = 3;          // 旋转角度
    float delta_x = 4;           // X 偏移
    float delta_y = 5;           // Y 偏移
}

// 手势类型
enum GestureType {
    GESTURE_PINCH = 0;           // 缩放
    GESTURE_SWIPE = 1;           // 滑动
    GESTURE_ROTATE = 2;          // 旋转
}
```

**输入事件处理代码：**

```go
type InputEventHandler struct {
    session *Session
    desktop DesktopInterface
}

// 处理输入事件
func (h *InputEventHandler) HandleEvent(event *pb.InputEvent) error {
    // 更新会话最后活动时间
    h.session.UpdateLastActivity()

    switch event.Type {
    case pb.InputEventType_INPUT_EVENT_KEY_DOWN:
        return h.handleKeyDown(event.KeyEvent)
    case pb.InputEventType_INPUT_EVENT_KEY_UP:
        return h.handleKeyUp(event.KeyEvent)
    case pb.InputEventType_INPUT_EVENT_MOUSE_MOVE:
        return h.handleMouseMove(event.MouseEvent)
    case pb.InputEventType_INPUT_EVENT_MOUSE_DOWN:
        return h.handleMouseDown(event.MouseEvent)
    case pb.InputEventType_INPUT_EVENT_MOUSE_UP:
        return h.handleMouseUp(event.MouseEvent)
    case pb.InputEventType_INPUT_EVENT_MOUSE_SCROLL:
        return h.handleMouseScroll(event.MouseEvent)
    case pb.InputEventType_INPUT_EVENT_TOUCH_START:
        return h.handleTouchStart(event.TouchEvent)
    case pb.InputEventType_INPUT_EVENT_TOUCH_MOVE:
        return h.handleTouchMove(event.TouchEvent)
    case pb.InputEventType_INPUT_EVENT_TOUCH_END:
        return h.handleTouchEnd(event.TouchEvent)
    case pb.InputEventType_INPUT_EVENT_GESTURE_PINCH:
        return h.handleGesturePinch(event.GestureEvent)
    case pb.InputEventType_INPUT_EVENT_GESTURE_SWIPE:
        return h.handleGestureSwipe(event.GestureEvent)
    case pb.InputEventType_INPUT_EVENT_GESTURE_ROTATE:
        return h.handleGestureRotate(event.GestureEvent)
    default:
        return fmt.Errorf("unknown input event type: %v", event.Type)
    }
}

// 处理键盘按下事件
func (h *InputEventHandler) handleKeyDown(event *pb.KeyEvent) error {
    // 检查是否为自定义快捷键
    if h.isCustomShortcut(event) {
        return h.handleCustomShortcut(event)
    }

    // 注入键盘事件到桌面环境
    return h.desktop.InjectKeyEvent(event.KeyCode, true)
}

// 处理鼠标移动事件
func (h *InputEventHandler) handleMouseMove(event *pb.MouseEvent) error {
    // 注入鼠标移动事件
    return h.desktop.InjectMouseMove(event.X, event.Y)
}

// 处理鼠标按下事件
func (h *InputEventHandler) handleMouseDown(event *pb.MouseEvent) error {
    // 注入鼠标按下事件
    return h.desktop.InjectMouseButtonEvent(event.Button, true)
}

// 处理触控开始事件
func (h *InputEventHandler) handleTouchStart(event *pb.TouchEvent) error {
    // 注入触控事件
    return h.desktop.InjectTouchEvent(event.TouchId, event.X, event.Y, event.Pressure)
}

// 处理手势缩放事件
func (h *InputEventHandler) handleGesturePinch(event *pb.GestureEvent) error {
    // 转换为鼠标滚轮事件
    delta := int32(event.Scale * 10)
    return h.desktop.InjectMouseScroll(0, delta)
}
```

#### 3.3.7 自定义快捷键

**快捷键配置：**

```json
{
  "shortcuts": [
    {
      "name": "触发安全注意序列",
      "description": "发送 Ctrl+Alt+Del 到桌面",
      "client_shortcut": "Ctrl+Shift+Del",
      "command": "TRIGGER_SAS"
    },
    {
      "name": "任务管理器",
      "description": "打开任务管理器",
      "client_shortcut": "Ctrl+Shift+Esc",
      "command": "OPEN_TASK_MANAGER"
    },
    {
      "name": "锁定桌面",
      "description": "锁定当前桌面",
      "client_shortcut": "Ctrl+Shift+L",
      "command": "LOCK_SCREEN"
    }
  ]
}
```

**快捷键处理代码：**

```go
// 自定义快捷键处理器
type ShortcutHandler struct {
    shortcuts map[string]ShortcutConfig
    executor  *CommandExecutor
}

// 快捷键配置
type ShortcutConfig struct {
    Name           string
    Description    string
    ClientShortcut string
    Command        string
}

// 检查是否为自定义快捷键
func (h *ShortcutHandler) isCustomShortcut(event *pb.KeyEvent) bool {
    // 构建快捷键字符串
    shortcut := h.buildShortcutString(event)
    
    // 检查是否匹配
    _, exists := h.shortcuts[shortcut]
    return exists
}

// 处理自定义快捷键
func (h *ShortcutHandler) handleCustomShortcut(event *pb.KeyEvent) error {
    shortcut := h.buildShortcutString(event)
    config, exists := h.shortcuts[shortcut]
    if !exists {
        return fmt.Errorf("shortcut not found: %s", shortcut)
    }

    // 执行对应的命令
    return h.executor.ExecuteCommand(config.Command)
}

// 构建快捷键字符串
func (h *ShortcutHandler) buildShortcutString(event *pb.KeyEvent) string {
    parts := []string{}
    
    if event.Ctrl {
        parts = append(parts, "Ctrl")
    }
    if event.Shift {
        parts = append(parts, "Shift")
    }
    if event.Alt {
        parts = append(parts, "Alt")
    }
    if event.Meta {
        parts = append(parts, "Meta")
    }
    
    parts = append(parts, event.KeyName)
    
    return strings.Join(parts, "+")
}

// 执行命令
func (h *ShortcutHandler) ExecuteCommand(command string) error {
    switch command {
    case "TRIGGER_SAS":
        return h.triggerSAS()
    case "OPEN_TASK_MANAGER":
        return h.openTaskManager()
    case "LOCK_SCREEN":
        return h.lockScreen()
    default:
        return fmt.Errorf("unknown command: %s", command)
    }
}

// 触发安全注意序列 (Ctrl+Alt+Del)
func (h *ShortcutHandler) triggerSAS() error {
    // 注入 Ctrl+Alt+Del
    return h.desktop.InjectKeyEvent(0x2E, true, true, true) // Del + Ctrl + Alt
}

// 打开任务管理器
func (h *ShortcutHandler) openTaskManager() error {
    // 注入 Ctrl+Shift+Esc
    return h.desktop.InjectKeyEvent(0x1B, true, true, false) // Esc + Ctrl + Shift
}

// 锁定桌面
func (h *ShortcutHandler) lockScreen() error {
    // 注入 Win+L
    return h.desktop.InjectKeyEvent(0x4C, false, false, true) // L + Meta
}
```

#### 3.3.8 剪贴板同步

**双向同步机制：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    剪贴板双向同步                                     │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Client    │                    │   Agent     │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  用户复制内容                     │
       │  (Ctrl+C)                        │
       │                                  │
       │  1. 剪贴板变化检测               │
       │                                  │
       │  2. 发送剪贴板内容               │
       │  (CLIPBOARD_UPDATE)              │
       │─────────────────────────────────►│
       │                                  │
       │                                  │  3. 更新桌面剪贴板
       │                                  │
       │                                  │
       │                                  │  用户复制内容
       │                                  │  (Ctrl+C)
       │                                  │
       │                                  │  4. 剪贴板变化检测
       │                                  │
       │  5. 接收剪贴板内容               │
       │  (CLIPBOARD_UPDATE)              │
       │◄─────────────────────────────────│
       │                                  │
       │  6. 更新 Client 剪贴板           │
       │                                  │
```

**剪贴板消息：**

```protobuf
// 剪贴板事件
message ClipboardEvent {
    ClipboardEventType type = 1;
    ClipboardContentType content_type = 2;
    bytes content = 3;             // 剪贴板内容
    int64 timestamp = 4;
}

// 剪贴板事件类型
enum ClipboardEventType {
    CLIPBOARD_UPDATE = 0;          // 剪贴板更新
    CLIPBOARD_REQUEST = 1;         // 请求剪贴板内容
}

// 剪贴板内容类型
enum ClipboardContentType {
    CONTENT_TEXT = 0;              // 文本
    CONTENT_IMAGE = 1;             // 图片
    CONTENT_FILE = 2;              // 文件
}
```

**剪贴板同步代码：**

```go
type ClipboardSync struct {
    session    *Session
    lastHash   string
    mu         sync.Mutex
}

// 启动剪贴板同步
func (c *ClipboardSync) Start() {
    // 监听 Client 剪贴板变化
    go c.watchClientClipboard()
    
    // 监听桌面剪贴板变化
    go c.watchDesktopClipboard()
}

// 监听 Client 剪贴板变化
func (c *ClipboardSync) watchClientClipboard() {
    for {
        // 接收 Client 发送的剪贴板内容
        event := c.receiveClipboardEvent()
        
        // 检查是否重复
        if c.isDuplicate(event) {
            continue
        }
        
        // 更新桌面剪贴板
        c.updateDesktopClipboard(event)
        
        // 更新最后的哈希值
        c.updateLastHash(event)
    }
}

// 监听桌面剪贴板变化
func (c *ClipboardSync) watchDesktopClipboard() {
    for {
        // 检测桌面剪贴板变化
        content := c.detectDesktopClipboardChange()
        
        if content == nil {
            continue
        }
        
        // 检查是否重复
        if c.isDuplicateContent(content) {
            continue
        }
        
        // 发送给 Client
        c.sendClipboardToClient(content)
        
        // 更新最后的哈希值
        c.updateLastHash(content)
    }
}

// 检查是否重复
func (c *ClipboardSync) isDuplicate(event *pb.ClipboardEvent) bool {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    hash := c.calculateHash(event.Content)
    return hash == c.lastHash
}

// 更新桌面剪贴板
func (c *ClipboardSync) updateDesktopClipboard(event *pb.ClipboardEvent) error {
    switch event.ContentType {
    case pb.ClipboardContentType_CONTENT_TEXT:
        return c.desktop.SetClipboardText(string(event.Content))
    case pb.ClipboardContentType_CONTENT_IMAGE:
        return c.desktop.SetClipboardImage(event.Content)
    case pb.ClipboardContentType_CONTENT_FILE:
        return c.desktop.SetClipboardFiles(event.Content)
    default:
        return fmt.Errorf("unsupported content type: %v", event.ContentType)
    }
}
```

#### 3.3.9 会话超时

**超时机制：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    会话超时机制                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  用户操作   │
│  (输入事件) │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  更新最后   │
│  活动时间   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  重置超时   │
│  定时器     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  等待       │
│  N 分钟     │
└──────┬──────┘
       │
       │ 无操作
       ▼
┌─────────────┐
│  发送超时   │
│  警告       │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  等待       │
│  M 秒       │
└──────┬──────┘
       │
       │ 用户确认
       ▼
┌─────────────┐
│  重置超时   │
│  定时器     │
└─────────────┘
       │
       │ 用户无响应
       ▼
┌─────────────┐
│  断开会话   │
│  或锁定桌面 │
└─────────────┘
```

**超时配置：**

```go
type SessionTimeoutConfig struct {
    IdleTimeout    time.Duration // 空闲超时时间
    WarningTimeout time.Duration // 警告超时时间
    Action         string        // 超时动作: "disconnect" 或 "lock"
}

// 默认配置
var defaultTimeoutConfig = SessionTimeoutConfig{
    IdleTimeout:    30 * time.Minute, // 30 分钟空闲超时
    WarningTimeout: 5 * time.Minute,  // 5 分钟警告
    Action:         "lock",           // 默认锁定桌面
}
```

**超时管理代码：**

```go
type SessionTimeoutManager struct {
    session      *Session
    config       SessionTimeoutConfig
    lastActivity time.Time
    timer        *time.Timer
    warningTimer *time.Timer
    mu           sync.Mutex
}

// 创建超时管理器
func NewSessionTimeoutManager(session *Session, config SessionTimeoutConfig) *SessionTimeoutManager {
    return &SessionTimeoutManager{
        session:      session,
        config:       config,
        lastActivity: time.Now(),
    }
}

// 启动超时监控
func (m *SessionTimeoutManager) Start() {
    m.resetTimer()
}

// 更新最后活动时间
func (m *SessionTimeoutManager) UpdateActivity() {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    m.lastActivity = time.Now()
    m.resetTimer()
}

// 重置定时器
func (m *SessionTimeoutManager) resetTimer() {
    // 停止现有定时器
    if m.timer != nil {
        m.timer.Stop()
    }
    if m.warningTimer != nil {
        m.warningTimer.Stop()
    }
    
    // 设置警告定时器
    warningDuration := m.config.IdleTimeout - m.config.WarningTimeout
    m.warningTimer = time.AfterFunc(warningDuration, func() {
        m.sendWarning()
    })
    
    // 设置超时定时器
    m.timer = time.AfterFunc(m.config.IdleTimeout, func() {
        m.handleTimeout()
    })
}

// 发送超时警告
func (m *SessionTimeoutManager) sendWarning() {
    event := &SessionEvent{
        Type:      SessionEventIdleWarning,
        SessionID: m.session.ID,
        Reason:    fmt.Sprintf("Session will timeout in %v", m.config.WarningTimeout),
        Timestamp: time.Now(),
    }
    
    m.sendEventToClient(event)
}

// 处理超时
func (m *SessionTimeoutManager) handleTimeout() {
    log.Printf("Session timeout: %s", m.session.ID)
    
    switch m.config.Action {
    case "disconnect":
        m.session.TransitionTo(SessionStateClosed)
    case "lock":
        m.lockScreen()
    }
}

// 锁定桌面
func (m *SessionTimeoutManager) lockScreen() {
    // 发送锁定命令
    command := &pb.Command{
        Type: pb.CommandType_COMMAND_LOCK_SCREEN,
    }
    m.executor.Execute(command)
}
```

#### 3.3.10 会话事件

**会话事件类型：**

| 事件类型 | 说明 | 触发时机 |
|----------|------|----------|
| SESSION_CREATED | 会话创建 | Client 请求连接时 |
| SESSION_CONNECTED | 会话连接 | WebRTC 连接建立时 |
| SESSION_DISCONNECTED | 会话断开 | Client 断开连接时 |
| SESSION_ENDED | 会话结束 | 会话正常结束时 |
| SESSION_ERROR | 会话异常 | 发生错误时 |
| SESSION_REPLACED | 会话被替换 | 新会话替换旧会话时 |
| SESSION_IDLE_WARNING | 空闲警告 | 即将超时时 |

**事件上报代码：**

```go
// 会话事件上报器
type SessionEventReporter struct {
    client *BrokerClient
}

// 上报会话事件
func (r *SessionEventReporter) ReportEvent(event *SessionEvent) error {
    // 构建事件消息
    pbEvent := &pb.SessionEvent{
        EventId:   uuid.New().String(),
        AgentId:   r.client.agentID,
        SessionId: event.SessionID,
        Type:      event.Type,
        Timestamp: event.Timestamp.Unix(),
        Reason:    event.Reason,
        Metadata:  event.Metadata,
    }

    // 发送给 Broker
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    resp, err := r.client.client.ReportSessionEvent(ctx, pbEvent)
    if err != nil {
        return fmt.Errorf("failed to report session event: %w", err)
    }

    if !resp.Success {
        return fmt.Errorf("session event rejected: %s", resp.Message)
    }

    return nil
}
```

#### 3.3.11 会话管理模块完整生命周期

```
┌─────────────────────────────────────────────────────────────────────┐
│                    会话管理模块完整生命周期                           │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  Client     │
│  请求连接   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  1. Token   │
│  验证       │
└──────┬──────┘
       │
       ├── 验证失败 ──► 拒绝连接
       │
       ▼
┌─────────────┐
│  2. 检查    │
│  旧会话     │
└──────┬──────┘
       │
       ├── 存在旧会话 ──► 替换旧会话
       │
       ▼
┌─────────────┐
│  3. 创建    │
│  新会话     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  4. 建立    │
│  WebRTC     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  5. 启动    │
│  输入处理   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  6. 启动    │
│  剪贴板同步 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  7. 启动    │
│  超时监控   │
└──────┬──────┘
       │
       ├── 正常工作 ──────────────────────┐
       │                                  │
       │ 用户无操作                       │
       ▼                                  │
┌─────────────┐                          │
│  8. 超时    │                          │
│  警告       │                          │
└──────┬──────┘                          │
       │                                  │
       ├── 用户响应 ──► 重置定时器        │
       │                                  │
       ▼                                  │
┌─────────────┐                          │
│  9. 超时    │                          │
│  处理       │                          │
└──────┬──────┘                          │
       │                                  │
       ▼                                  │
┌─────────────┐                          │
│  10. 断开   │                          │
│  或锁定     │                          │
└──────┬──────┘                          │
       │                                  │
       ▼                                  │
┌─────────────┐                          │
│  11. 上报   │                          │
│  事件       │◄─────────────────────────┘
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  12. 清理   │
│  资源       │
└─────────────┘
```

### 3.4 监控数据采集模块

监控数据采集模块负责采集桌面实例的系统资源使用情况和会话状态，并将数据上报给 Broker。所有告警逻辑由 Broker 处理，Agent 仅负责数据采集和上报。

#### 3.4.1 整体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                    监控数据采集模块架构                               │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                    监控数据采集模块                                   │
│                                                                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│  │  系统指标   │  │  会话指标   │  │  数据处理   │                 │
│  │  采集器     │  │  采集器     │  │  器         │                 │
│  └─────────────┘  └─────────────┘  └─────────────┘                 │
│                                                                     │
│  ┌─────────────┐  ┌─────────────┐                                  │
│  │  上报管理器 │  │  配置管理器 │                                  │
│  └─────────────┘  └─────────────┘                                  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
        │              │              │
        ▼              ▼              ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│  操作系统   │ │  会话管理   │ │   Broker    │
│  (系统资源) │ │  模块       │ │  (接收数据) │
└─────────────┘ └─────────────┘ └─────────────┘
```

**设计决策：**

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 监控指标 | CPU/内存/磁盘/网络延迟/会话时长 | 覆盖关键资源 |
| 采集周期 | 可配置，默认 1 分钟 | 平衡精度和资源消耗 |
| 数据精度 | 保留 1 位小数 | 足够精度，减少数据量 |
| 数据存储 | 仅上报 | Agent 无状态，简化设计 |
| 告警机制 | Broker 处理 | 集中告警管理 |

#### 3.4.2 采集流程

**采集上报流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    采集上报流程                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  定时器触发 │
│  (每分钟)   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  采集系统   │
│  指标       │
└──────┬──────┘
       │
       ├── CPU 使用率
       ├── 内存使用率
       ├── 磁盘使用率
       ├── 网络延迟
       │
       ▼
┌─────────────┐
│  采集会话   │
│  指标       │
└──────┬──────┘
       │
       └── 会话时长
       │
       ▼
┌─────────────┐
│  数据处理   │
│  (精度、格式)│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  批量上报   │
│  给 Broker  │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  等待确认   │
└──────┬──────┘
       │
       ├── 成功 ──► 等待下次采集
       │
       ▼
┌─────────────┐
│  上报失败   │
│  重试       │
└─────────────┘
```

**采集管理器代码：**

```go
type MonitorCollector struct {
    config         *MonitorConfig
    ticker         *time.Ticker
    stopCh         chan struct{}
    reporter       *MonitorReporter
    sessionManager *SessionManager
}

// 监控配置
type MonitorConfig struct {
    CollectInterval time.Duration // 采集周期
    ReportInterval  time.Duration // 上报周期
    MetricsEnabled  map[string]bool // 指标开关
}

// 默认配置
var defaultMonitorConfig = MonitorConfig{
    CollectInterval: 1 * time.Minute, // 默认 1 分钟
    ReportInterval:  1 * time.Minute,
    MetricsEnabled: map[string]bool{
        "cpu_usage":       true,
        "memory_usage":    true,
        "disk_usage":      true,
        "network_latency": true,
        "session_duration": true,
    },
}

// 创建监控采集器
func NewMonitorCollector(config *MonitorConfig, reporter *MonitorReporter, sessionManager *SessionManager) *MonitorCollector {
    return &MonitorCollector{
        config:         config,
        stopCh:         make(chan struct{}),
        reporter:       reporter,
        sessionManager: sessionManager,
    }
}

// 启动采集
func (c *MonitorCollector) Start() {
    c.ticker = time.NewTicker(c.config.CollectInterval)

    go func() {
        for {
            select {
            case <-c.ticker.C:
                c.collectAndReport()
            case <-c.stopCh:
                return
            }
        }
    }()
}

// 停止采集
func (c *MonitorCollector) Stop() {
    c.ticker.Stop()
    close(c.stopCh)
}

// 采集并上报
func (c *MonitorCollector) collectAndReport() {
    // 采集所有指标
    metrics := c.collectAllMetrics()

    // 上报给 Broker
    err := c.reporter.ReportMetrics(metrics)
    if err != nil {
        log.Printf("Failed to report metrics: %v", err)
    }
}

// 采集所有指标
func (c *MonitorCollector) collectAllMetrics() []*pb.MonitorMetric {
    metrics := make([]*pb.MonitorMetric, 0)

    // CPU 使用率
    if c.config.MetricsEnabled["cpu_usage"] {
        cpuUsage := c.collectCPUUsage()
        metrics = append(metrics, &pb.MonitorMetric{
            Name:  "cpu_usage",
            Value: cpuUsage,
            Unit:  "%",
        })
    }

    // 内存使用率
    if c.config.MetricsEnabled["memory_usage"] {
        memUsage := c.collectMemoryUsage()
        metrics = append(metrics, &pb.MonitorMetric{
            Name:  "memory_usage",
            Value: memUsage,
            Unit:  "%",
        })
    }

    // 磁盘使用率
    if c.config.MetricsEnabled["disk_usage"] {
        diskUsage := c.collectDiskUsage()
        metrics = append(metrics, &pb.MonitorMetric{
            Name:  "disk_usage",
            Value: diskUsage,
            Unit:  "%",
        })
    }

    // 网络延迟
    if c.config.MetricsEnabled["network_latency"] {
        latency := c.collectNetworkLatency()
        metrics = append(metrics, &pb.MonitorMetric{
            Name:  "network_latency",
            Value: latency,
            Unit:  "ms",
        })
    }

    // 会话时长
    if c.config.MetricsEnabled["session_duration"] {
        duration := c.collectSessionDuration()
        metrics = append(metrics, &pb.MonitorMetric{
            Name:  "session_duration",
            Value: duration,
            Unit:  "s",
        })
    }

    return metrics
}
```

#### 3.4.3 CPU 使用率采集

**采集方法：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    CPU 使用率采集                                     │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  读取       │
│  /proc/stat │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  计算       │
│  CPU 时间   │
└──────┬──────┘
       │
       ├── user: 用户态时间
       ├── nice: 低优先级用户态时间
       ├── system: 内核态时间
       ├── idle: 空闲时间
       ├── iowait: I/O 等待时间
       ├── irq: 硬中断时间
       ├── softirq: 软中断时间
       │
       ▼
┌─────────────┐
│  计算       │
│  使用率     │
└──────┬──────┘
       │
       │  公式：
       │  CPU% = (total - idle) / total * 100
       │
       ▼
┌─────────────┐
│  保留       │
│  1 位小数   │
└─────────────┘
```

**采集代码：**

```go
// CPU 使用率采集
func (c *MonitorCollector) collectCPUUsage() float64 {
    // 读取 /proc/stat
    data, err := os.ReadFile("/proc/stat")
    if err != nil {
        log.Printf("Failed to read /proc/stat: %v", err)
        return 0.0
    }

    // 解析 CPU 时间
    lines := strings.Split(string(data), "\n")
    for _, line := range lines {
        if strings.HasPrefix(line, "cpu ") {
            fields := strings.Fields(line)
            if len(fields) < 8 {
                continue
            }

            // 解析各字段
            user, _ := strconv.ParseUint(fields[1], 10, 64)
            nice, _ := strconv.ParseUint(fields[2], 10, 64)
            system, _ := strconv.ParseUint(fields[3], 10, 64)
            idle, _ := strconv.ParseUint(fields[4], 10, 64)
            iowait, _ := strconv.ParseUint(fields[5], 10, 64)
            irq, _ := strconv.ParseUint(fields[6], 10, 64)
            softirq, _ := strconv.ParseUint(fields[7], 10, 64)

            // 计算总时间和空闲时间
            total := user + nice + system + idle + iowait + irq + softirq
            idleTotal := idle + iowait

            // 计算使用率
            if total > 0 {
                usage := float64(total-idleTotal) / float64(total) * 100.0
                return math.Round(usage*10) / 10 // 保留 1 位小数
            }
        }
    }

    return 0.0
}
```

**Windows 采集代码：**

```go
// Windows CPU 使用率采集
func (c *MonitorCollector) collectCPUUsageWindows() float64 {
    // 使用 Windows API
    var idleTime, kernelTime, userTime FILETIME

    GetSystemTimes(&idleTime, &kernelTime, &userTime)

    // 计算 CPU 使用率
    idle := fileTimeToUint64(idleTime)
    kernel := fileTimeToUint64(kernelTime)
    user := fileTimeToUint64(userTime)

    total := kernel + user
    if total > 0 {
        usage := float64(total-idle) / float64(total) * 100.0
        return math.Round(usage*10) / 10
    }

    return 0.0
}
```

#### 3.4.4 内存使用率采集

**采集方法：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    内存使用率采集                                     │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  读取       │
│  /proc/meminfo│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  解析       │
│  内存信息   │
└──────┬──────┘
       │
       ├── MemTotal: 总内存
       ├── MemFree: 空闲内存
       ├── Buffers: 缓冲区
       ├── Cached: 缓存
       │
       ▼
┌─────────────┐
│  计算       │
│  使用率     │
└──────┬──────┘
       │
       │  公式：
       │  Used = Total - Free - Buffers - Cached
       │  Memory% = Used / Total * 100
       │
       ▼
┌─────────────┐
│  保留       │
│  1 位小数   │
└─────────────┘
```

**采集代码：**

```go
// 内存使用率采集
func (c *MonitorCollector) collectMemoryUsage() float64 {
    // 读取 /proc/meminfo
    data, err := os.ReadFile("/proc/meminfo")
    if err != nil {
        log.Printf("Failed to read /proc/meminfo: %v", err)
        return 0.0
    }

    // 解析内存信息
    lines := strings.Split(string(data), "\n")
    memInfo := make(map[string]uint64)

    for _, line := range lines {
        fields := strings.Fields(line)
        if len(fields) >= 2 {
            key := strings.TrimSuffix(fields[0], ":")
            value, _ := strconv.ParseUint(fields[1], 10, 64)
            memInfo[key] = value
        }
    }

    // 计算使用率
    total := memInfo["MemTotal"]
    free := memInfo["MemFree"]
    buffers := memInfo["Buffers"]
    cached := memInfo["Cached"]

    if total > 0 {
        used := total - free - buffers - cached
        usage := float64(used) / float64(total) * 100.0
        return math.Round(usage*10) / 10
    }

    return 0.0
}
```

**Windows 采集代码：**

```go
// Windows 内存使用率采集
func (c *MonitorCollector) collectMemoryUsageWindows() float64 {
    var memStatus MEMORYSTATUSEX
    memStatus.dwLength = sizeof(MEMORYSTATUSEX)

    GlobalMemoryStatusEx(&memStatus)

    // 计算使用率
    total := memStatus.ullTotalPhys
    avail := memStatus.ullAvailPhys

    if total > 0 {
        used := total - avail
        usage := float64(used) / float64(total) * 100.0
        return math.Round(usage*10) / 10
    }

    return 0.0
}
```

#### 3.4.5 磁盘使用率采集

**采集方法：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    磁盘使用率采集                                     │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  调用       │
│  statfs()   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  获取       │
│  文件系统   │
│  信息       │
└──────┬──────┘
       │
       ├── f_blocks: 总块数
       ├── f_bfree: 空闲块数
       ├── f_bavail: 可用块数
       ├── f_bsize: 块大小
       │
       ▼
┌─────────────┐
│  计算       │
│  使用率     │
└──────┬──────┘
       │
       │  公式：
       │  Total = f_blocks * f_bsize
       │  Free = f_bfree * f_bsize
       │  Disk% = (Total - Free) / Total * 100
       │
       ▼
┌─────────────┐
│  保留       │
│  1 位小数   │
└─────────────┘
```

**采集代码：**

```go
// 磁盘使用率采集
func (c *MonitorCollector) collectDiskUsage() float64 {
    // 获取根目录的磁盘信息
    var stat syscall.Statfs_t
    err := syscall.Statfs("/", &stat)
    if err != nil {
        log.Printf("Failed to get disk stats: %v", err)
        return 0.0
    }

    // 计算使用率
    total := stat.Blocks * uint64(stat.Bsize)
    free := stat.Bfree * uint64(stat.Bsize)

    if total > 0 {
        used := total - free
        usage := float64(used) / float64(total) * 100.0
        return math.Round(usage*10) / 10
    }

    return 0.0
}
```

**Windows 采集代码：**

```go
// Windows 磁盘使用率采集
func (c *MonitorCollector) collectDiskUsageWindows() float64 {
    // 获取 C 盘信息
    var freeBytesAvailable, totalBytes, totalFreeBytes uint64

    GetDiskFreeSpaceEx("C:\\", &freeBytesAvailable, &totalBytes, &totalFreeBytes)

    if totalBytes > 0 {
        used := totalBytes - totalFreeBytes
        usage := float64(used) / float64(totalBytes) * 100.0
        return math.Round(usage*10) / 10
    }

    return 0.0
}
```

#### 3.4.6 网络延迟采集

**采集方法：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    网络延迟采集                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  测量到     │
│  Broker 的  │
│  网络延迟   │
└──────┬──────┘
       │
       ├── 方法 1: TCP 连接延迟
       │   测量 TCP 握手时间
       │
       ├── 方法 2: gRPC 调用延迟
       │   测量 gRPC 请求响应时间
       │
       ▼
┌─────────────┐
│  计算       │
│  平均延迟   │
└──────┬──────┘
       │
       │  多次测量取平均值
       │
       ▼
┌─────────────┐
│  保留       │
│  整数 (ms)  │
└─────────────┘
```

**采集代码：**

```go
// 网络延迟采集
func (c *MonitorCollector) collectNetworkLatency() float64 {
    // 测量到 Broker 的延迟
    latency := c.measureBrokerLatency()
    return latency
}

// 测量 Broker 延迟
func (c *MonitorCollector) measureBrokerLatency() float64 {
    // 使用 gRPC 调用测量延迟
    start := time.Now()

    // 发送一个轻量级请求
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    _, err := c.brokerClient.client.Ping(ctx, &pb.PingRequest{})
    if err != nil {
        log.Printf("Failed to measure latency: %v", err)
        return -1.0
    }

    // 计算延迟
    latency := time.Since(start)
    return float64(latency.Milliseconds())
}
```

#### 3.4.7 会话时长统计

**统计方法：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    会话时长统计                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  会话创建   │
│  记录开始   │
│  时间       │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  定期计算   │
│  会话时长   │
└──────┬──────┘
       │
       │  公式：
       │  Duration = Now - SessionStartTime
       │
       ▼
┌─────────────┐
│  转换为秒   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  上报       │
└─────────────┘
```

**统计代码：**

```go
// 会话时长统计
func (c *MonitorCollector) collectSessionDuration() float64 {
    // 获取当前会话
    session := c.sessionManager.GetCurrentSession()
    if session == nil {
        return 0.0
    }

    // 计算会话时长
    duration := time.Since(session.ConnectedAt)
    return duration.Seconds()
}
```

#### 3.4.8 数据处理

**数据精度处理：**

```go
// 数据精度处理
func (c *MonitorCollector) roundToDecimal(value float64, decimal int) float64 {
    multiplier := math.Pow(10, float64(decimal))
    return math.Round(value*multiplier) / multiplier
}

// 数据格式化
func (c *MonitorCollector) formatMetric(name string, value float64, unit string) *pb.MonitorMetric {
    return &pb.MonitorMetric{
        Name:  name,
        Value: c.roundToDecimal(value, 1), // 保留 1 位小数
        Unit:  unit,
    }
}
```

**数据校验：**

```go
// 数据校验
func (c *MonitorCollector) validateMetric(metric *pb.MonitorMetric) bool {
    // 检查值是否在合理范围内
    switch metric.Name {
    case "cpu_usage":
        return metric.Value >= 0 && metric.Value <= 100
    case "memory_usage":
        return metric.Value >= 0 && metric.Value <= 100
    case "disk_usage":
        return metric.Value >= 0 && metric.Value <= 100
    case "network_latency":
        return metric.Value >= 0
    case "session_duration":
        return metric.Value >= 0
    default:
        return true
    }
}

// 异常值处理
func (c *MonitorCollector) handleAbnormalValue(metric *pb.MonitorMetric) {
    // 记录异常值日志
    log.Printf("Abnormal metric value: %s = %f %s", metric.Name, metric.Value, metric.Unit)
}
```

#### 3.4.9 上报机制

**批量上报：**

```go
// 批量上报
func (r *MonitorReporter) ReportMetrics(metrics []*pb.MonitorMetric) error {
    // 构建上报请求
    req := &pb.MonitorDataRequest{
        AgentId:   r.client.agentID,
        Metrics:   metrics,
        Timestamp: time.Now().Unix(),
    }

    // 上报给 Broker
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    resp, err := r.client.client.ReportMonitorData(ctx, req)
    if err != nil {
        return fmt.Errorf("failed to report metrics: %w", err)
    }

    if !resp.Success {
        return fmt.Errorf("metrics rejected: %s", resp.Message)
    }

    return nil
}
```

**上报失败重试：**

```go
// 上报失败重试
func (r *MonitorReporter) ReportWithRetry(metrics []*pb.MonitorMetric, maxRetries int) error {
    var lastErr error

    for retry := 0; retry < maxRetries; retry++ {
        err := r.ReportMetrics(metrics)
        if err == nil {
            return nil
        }

        lastErr = err
        log.Printf("Report metrics failed (retry %d/%d): %v", retry+1, maxRetries, err)

        // 指数退避
        delay := time.Duration(retry+1) * time.Second
        time.Sleep(delay)
    }

    return fmt.Errorf("report metrics failed after %d retries: %w", maxRetries, lastErr)
}
```

#### 3.4.10 配置管理

**配置更新：**

```go
// 配置管理器
type MonitorConfigManager struct {
    client          *BrokerClient
    currentConfig   *MonitorConfig
    configVersion   string
    refreshInterval time.Duration
    mu              sync.RWMutex
}

// 创建配置管理器
func NewMonitorConfigManager(client *BrokerClient, refreshInterval time.Duration) *MonitorConfigManager {
    return &MonitorConfigManager{
        client:          client,
        currentConfig:   &defaultMonitorConfig,
        refreshInterval: refreshInterval,
    }
}

// 启动配置刷新
func (m *MonitorConfigManager) Start() {
    go func() {
        ticker := time.NewTicker(m.refreshInterval)
        defer ticker.Stop()

        for range ticker.C {
            m.refreshConfig()
        }
    }()
}

// 刷新配置
func (m *MonitorConfigManager) refreshConfig() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    req := &pb.GetMonitorConfigRequest{
        AgentId:        m.client.agentID,
        CurrentVersion: m.configVersion,
    }

    resp, err := m.client.client.GetMonitorConfig(ctx, req)
    if err != nil {
        log.Printf("Failed to refresh monitor config: %v", err)
        return
    }

    if !resp.Success {
        log.Printf("Monitor config refresh failed: %s", resp.Message)
        return
    }

    // 更新配置
    m.mu.Lock()
    m.currentConfig = &MonitorConfig{
        CollectInterval: time.Duration(resp.Config.CollectInterval) * time.Second,
        ReportInterval:  time.Duration(resp.Config.ReportInterval) * time.Second,
        MetricsEnabled:  resp.Config.MetricsEnabled,
    }
    m.configVersion = resp.Version
    m.mu.Unlock()

    log.Printf("Monitor config refreshed: version=%s", resp.Version)
}

// 处理配置更新通知
func (m *MonitorConfigManager) HandleConfigUpdate(notification *pb.MonitorConfigUpdateNotification) {
    m.mu.Lock()
    m.currentConfig = &MonitorConfig{
        CollectInterval: time.Duration(notification.Config.CollectInterval) * time.Second,
        ReportInterval:  time.Duration(notification.Config.ReportInterval) * time.Second,
        MetricsEnabled:  notification.Config.MetricsEnabled,
    }
    m.configVersion = notification.Version
    m.mu.Unlock()

    log.Printf("Monitor config updated via notification: version=%s", notification.Version)
}
```

#### 3.4.11 监控数据采集模块完整生命周期

```
┌─────────────────────────────────────────────────────────────────────┐
│                    监控数据采集模块完整生命周期                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  Agent 启动 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  1. 初始化  │
│  配置管理器 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  2. 从 Broker│
│  获取配置   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  3. 初始化  │
│  采集器     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  4. 启动    │
│  定时采集   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  5. 等待    │
│  采集周期   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  6. 采集    │
│  系统指标   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  7. 采集    │
│  会话指标   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  8. 数据    │
│  处理       │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  9. 批量    │
│  上报       │
└──────┬──────┘
       │
       ├── 成功 ──► 等待下次采集
       │
       ▼
┌─────────────┐
│  10. 上报   │
│  失败重试   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  11. 配置   │
│  更新       │◄──── Broker 推送
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  12. 应用   │
│  新配置     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  13. 继续   │
│  采集       │
└─────────────┘
```

### 3.5 生命周期管理模块

生命周期管理模块负责管理 Agent 的完整生命周期，包括进程守护、崩溃恢复、自动升级、防卸载等功能。采用自守护模式，Agent 自己管理自己的生命周期。

#### 3.5.1 整体架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                    生命周期管理模块架构                               │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                    生命周期管理模块                                   │
│                                                                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│  │  进程守护   │  │  崩溃恢复   │  │  自动升级   │                 │
│  │  管理器     │  │  管理器     │  │  管理器     │                 │
│  └─────────────┘  └─────────────┘  └─────────────┘                 │
│                                                                     │
│  ┌─────────────┐  ┌─────────────┐                                  │
│  │  防卸载     │  │  版本管理   │                                  │
│  │  管理器     │  │  器         │                                  │
│  └─────────────┘  └─────────────┘                                  │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
        │              │              │
        ▼              ▼              ▼
┌─────────────┐ ┌─────────────┐ ┌─────────────┐
│  操作系统   │ │   Broker    │ │  升级服务器 │
│  (进程管理) │ │  (升级指令) │ │  (新版本)   │
└─────────────┘ └─────────────┘ └─────────────┘
```

**设计决策：**

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 进程守护模式 | 自守护模式 | Agent 自己管理自己，无需外部依赖 |
| 崩溃恢复策略 | 立即重启 | 快速恢复服务 |
| 自动升级方式 | Broker 推送 + Agent 拉取 | 灵活，支持两种模式 |
| 升级流程 | 冷升级 | 简单可靠，适合 VDI 场景 |
| 防卸载机制 | 文件保护 + 进程保护 | 双重保护，防止用户卸载 |

#### 3.5.2 自守护架构

**守护架构：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    自守护架构                                        │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                    Agent 主进程                                      │
│                                                                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                 │
│  │  守护线程   │  │  业务线程   │  │  监控线程   │                 │
│  │  (Watchdog) │  │  (WebRTC等) │  │  (Health)   │                 │
│  └─────────────┘  └─────────────┘  └─────────────┘                 │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
        │
        │ 监控主进程
        ▼
┌─────────────────────────────────────────────────────────────────────┐
│                    守护进程 (可选)                                    │
│                                                                     │
│  - 独立于主进程运行                                                  │
│  - 监控主进程状态                                                    │
│  - 主进程崩溃时重启                                                  │
│  - 自身崩溃由系统重启                                                │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**守护线程代码：**

```go
type LifecycleManager struct {
    processGuardian  *ProcessGuardian
    crashRecovery    *CrashRecoveryManager
    autoUpgrade      *AutoUpgradeManager
    antiUninstall    *AntiUninstallManager
    versionManager   *VersionManager
    config           *LifecycleConfig
}

// 生命周期配置
type LifecycleConfig struct {
    MaxRestartAttempts int           // 最大重启尝试次数
    RestartDelay       time.Duration // 重启延迟
    HealthCheckInterval time.Duration // 健康检查间隔
    UpgradeCheckInterval time.Duration // 升级检查间隔
}

// 默认配置
var defaultLifecycleConfig = LifecycleConfig{
    MaxRestartAttempts:  10,
    RestartDelay:        0, // 立即重启
    HealthCheckInterval: 30 * time.Second,
    UpgradeCheckInterval: 1 * time.Hour,
}

// 创建生命周期管理器
func NewLifecycleManager(config *LifecycleConfig) *LifecycleManager {
    return &LifecycleManager{
        processGuardian: NewProcessGuardian(config),
        crashRecovery:   NewCrashRecoveryManager(config),
        autoUpgrade:     NewAutoUpgradeManager(config),
        antiUninstall:   NewAntiUninstallManager(config),
        versionManager:  NewVersionManager(),
        config:          config,
    }
}

// 启动生命周期管理
func (m *LifecycleManager) Start() error {
    // 1. 初始化防卸载机制
    if err := m.antiUninstall.Enable(); err != nil {
        return fmt.Errorf("failed to enable anti-uninstall: %w", err)
    }

    // 2. 启动进程守护
    if err := m.processGuardian.Start(); err != nil {
        return fmt.Errorf("failed to start process guardian: %w", err)
    }

    // 3. 启动崩溃恢复
    if err := m.crashRecovery.Start(); err != nil {
        return fmt.Errorf("failed to start crash recovery: %w", err)
    }

    // 4. 启动自动升级检查
    if err := m.autoUpgrade.Start(); err != nil {
        return fmt.Errorf("failed to start auto upgrade: %w", err)
    }

    return nil
}

// 停止生命周期管理
func (m *LifecycleManager) Stop() {
    m.autoUpgrade.Stop()
    m.crashRecovery.Stop()
    m.processGuardian.Stop()
    m.antiUninstall.Disable()
}
```

#### 3.5.3 进程启动流程

**启动顺序：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    进程启动流程                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  系统启动   │
│  Agent 服务 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  1. 环境检查│
│  (依赖、权限)│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  2. 加载配置│
│  (本地配置) │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  3. 初始化   │
│  日志系统   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  4. 启动    │
│  防卸载机制 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  5. 启动    │
│  进程守护   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  6. 启动    │
│  崩溃恢复   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  7. 连接    │
│  Broker     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  8. 注册    │
│  到 Broker  │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  9. 启动    │
│  WebRTC 引擎│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  10. 启动   │
│  会话管理   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  11. 启动   │
│  监控采集   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  12. 启动   │
│  自动升级   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  13. 就绪   │
│  等待连接   │
└─────────────┘
```

**启动代码：**

```go
// Agent 启动
func main() {
    // 1. 环境检查
    if err := checkEnvironment(); err != nil {
        log.Fatalf("Environment check failed: %v", err)
    }

    // 2. 加载配置
    config, err := loadConfig()
    if err != nil {
        log.Fatalf("Failed to load config: %v", err)
    }

    // 3. 初始化日志
    initLogger(config.LogLevel)

    // 4. 创建生命周期管理器
    lifecycleManager := NewLifecycleManager(&config.Lifecycle)

    // 5. 启动生命周期管理
    if err := lifecycleManager.Start(); err != nil {
        log.Fatalf("Failed to start lifecycle manager: %v", err)
    }

    // 6. 创建并启动 Agent
    agent, err := NewAgent(config)
    if err != nil {
        log.Fatalf("Failed to create agent: %v", err)
    }

    if err := agent.Start(); err != nil {
        log.Fatalf("Failed to start agent: %v", err)
    }

    // 7. 等待退出信号
    waitForShutdown()

    // 8. 优雅关闭
    agent.Stop()
    lifecycleManager.Stop()
}

// 环境检查
func checkEnvironment() error {
    // 检查依赖
    if err := checkDependencies(); err != nil {
        return fmt.Errorf("dependency check failed: %w", err)
    }

    // 检查权限
    if err := checkPermissions(); err != nil {
        return fmt.Errorf("permission check failed: %w", err)
    }

    // 检查端口
    if err := checkPorts(); err != nil {
        return fmt.Errorf("port check failed: %w", err)
    }

    return nil
}
```

#### 3.5.4 进程状态监控

**状态监控：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    进程状态监控                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  守护线程   │
│  (Watchdog) │
└──────┬──────┘
       │
       │ 每 30 秒
       ▼
┌─────────────┐
│  检查       │
│  主进程状态 │
└──────┬──────┘
       │
       ├── 正常 ──► 继续监控
       │
       ▼
┌─────────────┐
│  检查       │
│  业务组件   │
└──────┬──────┘
       │
       ├── WebRTC 引擎状态
       ├── Broker 连接状态
       ├── 会话管理状态
       │
       ▼
┌─────────────┐
│  上报       │
│  健康状态   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Broker    │
└─────────────┘
```

**状态监控代码：**

```go
type ProcessGuardian struct {
    config        *LifecycleConfig
    healthChecker *HealthChecker
    ticker        *time.Ticker
    stopCh        chan struct{}
}

// 健康检查器
type HealthChecker struct {
    checks []HealthCheck
}

// 健康检查接口
type HealthCheck interface {
    Name() string
    Check() error
}

// 创建进程守护
func NewProcessGuardian(config *LifecycleConfig) *ProcessGuardian {
    return &ProcessGuardian{
        config: config,
        healthChecker: &HealthChecker{
            checks: []HealthCheck{
                &WebRTCEngineCheck{},
                &BrokerConnectionCheck{},
                &SessionManagerCheck{},
                &MemoryCheck{},
                &DiskCheck{},
            },
        },
        stopCh: make(chan struct{}),
    }
}

// 启动进程守护
func (g *ProcessGuardian) Start() error {
    g.ticker = time.NewTicker(g.config.HealthCheckInterval)

    go func() {
        for {
            select {
            case <-g.ticker.C:
                g.checkHealth()
            case <-g.stopCh:
                return
            }
        }
    }()

    return nil
}

// 健康检查
func (g *ProcessGuardian) checkHealth() {
    for _, check := range g.healthChecker.checks {
        if err := check.Check(); err != nil {
            log.Printf("Health check failed: %s - %v", check.Name(), err)
            g.handleUnhealthy(check.Name(), err)
        }
    }
}

// 处理不健康状态
func (g *ProcessGuardian) handleUnhealthy(component string, err error) {
    // 上报给 Broker
    g.reportUnhealthy(component, err)

    // 尝试恢复
    g.tryRecover(component)
}
```

#### 3.5.5 崩溃检测

**信号处理：**

```go
type CrashRecoveryManager struct {
    config       *LifecycleConfig
    signalCh     chan os.Signal
    crashCount   int
    lastCrashTime time.Time
    mu           sync.Mutex
}

// 创建崩溃恢复管理器
func NewCrashRecoveryManager(config *LifecycleConfig) *CrashRecoveryManager {
    return &CrashRecoveryManager{
        config:   config,
        signalCh: make(chan os.Signal, 1),
    }
}

// 启动崩溃恢复
func (m *CrashRecoveryManager) Start() error {
    // 注册信号处理
    signal.Notify(m.signalCh,
        syscall.SIGSEGV, // 段错误
        syscall.SIGABRT, // 中止
        syscall.SIGFPE,  // 浮点异常
        syscall.SIGILL,  // 非法指令
        syscall.SIGBUS,  // 总线错误
    )

    // 启动信号处理协程
    go m.handleSignals()

    // 设置 panic 恢复
    m.setupPanicRecovery()

    return nil
}

// 处理信号
func (m *CrashRecoveryManager) handleSignals() {
    for sig := range m.signalCh {
        log.Printf("Received signal: %v", sig)
        m.handleCrash(sig)
    }
}

// 处理崩溃
func (m *CrashRecoveryManager) handleCrash(sig os.Signal) {
    m.mu.Lock()
    defer m.mu.Unlock()

    // 记录崩溃信息
    m.crashCount++
    m.lastCrashTime = time.Now()

    // 收集崩溃信息
    crashInfo := m.collectCrashInfo(sig)

    // 记录崩溃日志
    m.logCrash(crashInfo)

    // 上报给 Broker
    m.reportCrash(crashInfo)

    // 立即重启
    m.restartImmediately()
}

// 收集崩溃信息
func (m *CrashRecoveryManager) collectCrashInfo(sig os.Signal) *CrashInfo {
    return &CrashInfo{
        Signal:      sig.String(),
        Timestamp:   time.Now(),
        StackTrace:  string(debug.Stack()),
        Goroutines:  runtime.NumGoroutine(),
        MemoryStats: m.getMemoryStats(),
    }
}

// 设置 panic 恢复
func (m *CrashRecoveryManager) setupPanicRecovery() {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Panic recovered: %v", r)
            m.handleCrash(syscall.SIGABRT)
        }
    }()
}
```

#### 3.5.6 立即重启

**重启流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    立即重启流程                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  崩溃检测   │
│  (信号/panic)│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  记录       │
│  崩溃信息   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  上报       │
│  Broker     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  清理       │
│  资源       │
└──────┬──────┘
       │
       ├── 关闭 WebRTC 连接
       ├── 关闭 Broker 连接
       ├── 释放系统资源
       │
       ▼
┌─────────────┐
│  重启       │
│  Agent      │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  恢复       │
│  会话       │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  继续       │
│  服务       │
└─────────────┘
```

**重启代码：**

```go
// 立即重启
func (m *CrashRecoveryManager) restartImmediately() {
    log.Printf("Restarting agent immediately...")

    // 1. 清理资源
    m.cleanup()

    // 2. 重启进程
    m.restartProcess()
}

// 清理资源
func (m *CrashRecoveryManager) cleanup() {
    // 关闭所有连接
    m.closeAllConnections()

    // 释放所有资源
    m.releaseAllResources()

    // 等待清理完成
    time.Sleep(100 * time.Millisecond)
}

// 重启进程
func (m *CrashRecoveryManager) restartProcess() {
    // 获取当前可执行文件路径
    executable, err := os.Executable()
    if err != nil {
        log.Printf("Failed to get executable path: %v", err)
        return
    }

    // 重启进程
    err = syscall.Exec(executable, os.Args, os.Environ())
    if err != nil {
        log.Printf("Failed to restart process: %v", err)
    }
}
```

#### 3.5.7 升级检查

**升级检查流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    升级检查流程                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  定时器触发 │
│  (每小时)   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  查询       │
│  Broker     │
│  最新版本   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  比较       │
│  版本号     │
└──────┬──────┘
       │
       ├── 版本相同 ──► 无需升级
       │
       ▼
┌─────────────┐
│  检查       │
│  升级条件   │
└──────┬──────┘
       │
       ├── 有活跃会话 ──► 延迟升级
       │
       ▼
┌─────────────┐
│  开始       │
│  升级流程   │
└─────────────┘
```

**升级检查代码：**

```go
type AutoUpgradeManager struct {
    client          *BrokerClient
    config          *LifecycleConfig
    currentVersion  string
    ticker          *time.Ticker
    stopCh          chan struct{}
}

// 创建自动升级管理器
func NewAutoUpgradeManager(client *BrokerClient, config *LifecycleConfig) *AutoUpgradeManager {
    return &AutoUpgradeManager{
        client:         client,
        config:         config,
        currentVersion: GetAgentVersion(),
        stopCh:         make(chan struct{}),
    }
}

// 启动自动升级
func (m *AutoUpgradeManager) Start() error {
    m.ticker = time.NewTicker(m.config.UpgradeCheckInterval)

    go func() {
        for {
            select {
            case <-m.ticker.C:
                m.checkForUpgrade()
            case <-m.stopCh:
                return
            }
        }
    }()

    return nil
}

// 检查升级
func (m *AutoUpgradeManager) checkForUpgrade() {
    // 查询最新版本
    latestVersion, err := m.queryLatestVersion()
    if err != nil {
        log.Printf("Failed to query latest version: %v", err)
        return
    }

    // 比较版本
    if !m.needsUpgrade(latestVersion) {
        return
    }

    log.Printf("New version available: %s -> %s", m.currentVersion, latestVersion)

    // 检查升级条件
    if !m.canUpgrade() {
        log.Printf("Upgrade delayed: active sessions exist")
        return
    }

    // 开始升级
    m.startUpgrade(latestVersion)
}

// 查询最新版本
func (m *AutoUpgradeManager) queryLatestVersion() (string, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    resp, err := m.client.client.GetLatestVersion(ctx, &pb.GetLatestVersionRequest{})
    if err != nil {
        return "", err
    }

    return resp.Version, nil
}

// 比较版本
func (m *AutoUpgradeManager) needsUpgrade(latestVersion string) bool {
    return m.currentVersion != latestVersion
}

// 检查是否可以升级
func (m *AutoUpgradeManager) canUpgrade() bool {
    // 检查是否有活跃会话
    // VDI 场景：有活跃会话时延迟升级
    return !m.hasActiveSessions()
}
```

#### 3.5.8 Broker 推送升级

**推送升级流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Broker 推送升级流程                                │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Broker    │                    │   Agent     │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. 升级指令                     │
       │  (版本、下载地址、校验和)        │
       │─────────────────────────────────►│
       │                                  │
       │                                  │  2. 验证升级条件
       │                                  │  3. 下载新版本
       │                                  │  4. 校验文件
       │                                  │  5. 备份当前版本
       │                                  │  6. 替换二进制
       │                                  │  7. 重启 Agent
       │                                  │
       │  8. 升级结果                     │
       │  (成功/失败)                     │
       │◄─────────────────────────────────│
       │                                  │
```

**推送升级代码：**

```go
// 处理升级指令
func (m *AutoUpgradeManager) HandleUpgradeCommand(cmd *pb.UpgradeCommand) error {
    log.Printf("Received upgrade command: version=%s", cmd.Version)

    // 1. 验证升级条件
    if !m.canUpgrade() {
        return fmt.Errorf("cannot upgrade: active sessions exist")
    }

    // 2. 下载新版本
    if err := m.downloadNewVersion(cmd.DownloadUrl, cmd.Checksum); err != nil {
        return fmt.Errorf("failed to download new version: %w", err)
    }

    // 3. 备份当前版本
    if err := m.backupCurrentVersion(); err != nil {
        return fmt.Errorf("failed to backup current version: %w", err)
    }

    // 4. 替换二进制
    if err := m.replaceBinary(); err != nil {
        // 回滚
        m.rollback()
        return fmt.Errorf("failed to replace binary: %w", err)
    }

    // 5. 重启 Agent
    m.restartAgent()

    return nil
}

// 下载新版本
func (m *AutoUpgradeManager) downloadNewVersion(url, checksum string) error {
    // 下载文件
    resp, err := http.Get(url)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    // 读取文件内容
    data, err := io.ReadAll(resp.Body)
    if err != nil {
        return err
    }

    // 校验文件
    if err := m.verifyChecksum(data, checksum); err != nil {
        return err
    }

    // 保存到临时文件
    return os.WriteFile(m.getTempPath(), data, 0755)
}

// 校验文件
func (m *AutoUpgradeManager) verifyChecksum(data []byte, expectedChecksum string) error {
    hash := sha256.Sum256(data)
    actualChecksum := hex.EncodeToString(hash[:])

    if actualChecksum != expectedChecksum {
        return fmt.Errorf("checksum mismatch: expected=%s, actual=%s", 
            expectedChecksum, actualChecksum)
    }

    return nil
}
```

#### 3.5.9 Agent 拉取升级

**拉取升级流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Agent 拉取升级流程                                 │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  Agent      │
│  定时检查   │
│  (每小时)   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  查询       │
│  最新版本   │
└──────┬──────┘
       │
       ├── 版本相同 ──► 无需升级
       │
       ▼
┌─────────────┐
│  检查       │
│  升级条件   │
└──────┬──────┘
       │
       ├── 有活跃会话 ──► 延迟升级
       │
       ▼
┌─────────────┐
│  下载       │
│  新版本     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  校验       │
│  文件       │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  备份       │
│  当前版本   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  替换       │
│  二进制     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  重启       │
│  Agent      │
└─────────────┘
```

#### 3.5.10 冷升级流程

**冷升级流程：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    冷升级流程                                         │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  1. 停止    │
│  Agent 服务 │
└──────┬──────┘
       │
       ├── 关闭所有 WebRTC 连接
       ├── 关闭 Broker 连接
       ├── 释放所有资源
       │
       ▼
┌─────────────┐
│  2. 备份    │
│  当前版本   │
└──────┬──────┘
       │
       ├── 备份二进制文件
       ├── 备份配置文件
       │
       ▼
┌─────────────┐
│  3. 替换    │
│  二进制文件 │
└──────┬──────┘
       │
       ├── 用新版本替换旧版本
       ├── 设置文件权限
       │
       ▼
┌─────────────┐
│  4. 启动    │
│  Agent 服务 │
└──────┬──────┘
       │
       ├── 启动新版本
       ├── 连接 Broker
       ├── 恢复会话
       │
       ▼
┌─────────────┐
│  5. 验证    │
│  升级成功   │
└──────┬──────┘
       │
       ├── 检查版本号
       ├── 检查健康状态
       │
       ▼
┌─────────────┐
│  6. 清理    │
│  备份文件   │
└─────────────┘
```

**冷升级代码：**

```go
// 冷升级
func (m *AutoUpgradeManager) performColdUpgrade(newVersion, downloadUrl, checksum string) error {
    log.Printf("Starting cold upgrade to version: %s", newVersion)

    // 1. 停止 Agent 服务
    log.Printf("Stopping agent service...")
    m.stopAgentService()

    // 2. 备份当前版本
    log.Printf("Backing up current version...")
    if err := m.backupCurrentVersion(); err != nil {
        return fmt.Errorf("backup failed: %w", err)
    }

    // 3. 下载新版本
    log.Printf("Downloading new version...")
    if err := m.downloadNewVersion(downloadUrl, checksum); err != nil {
        // 回滚
        m.rollback()
        return fmt.Errorf("download failed: %w", err)
    }

    // 4. 替换二进制文件
    log.Printf("Replacing binary...")
    if err := m.replaceBinary(); err != nil {
        // 回滚
        m.rollback()
        return fmt.Errorf("replace failed: %w", err)
    }

    // 5. 启动 Agent 服务
    log.Printf("Starting agent service...")
    if err := m.startAgentService(); err != nil {
        // 回滚
        m.rollback()
        return fmt.Errorf("start failed: %w", err)
    }

    // 6. 验证升级
    log.Printf("Verifying upgrade...")
    if err := m.verifyUpgrade(newVersion); err != nil {
        // 回滚
        m.rollback()
        return fmt.Errorf("verification failed: %w", err)
    }

    // 7. 清理备份
    log.Printf("Cleaning up backup...")
    m.cleanupBackup()

    log.Printf("Cold upgrade completed successfully")
    return nil
}

// 停止 Agent 服务
func (m *AutoUpgradeManager) stopAgentService() {
    // 发送停止信号
    m.stopCh <- struct{}{}

    // 等待服务停止
    time.Sleep(2 * time.Second)
}

// 启动 Agent 服务
func (m *AutoUpgradeManager) startAgentService() error {
    // 获取当前可执行文件路径
    executable, err := os.Executable()
    if err != nil {
        return err
    }

    // 启动新进程
    cmd := exec.Command(executable, os.Args[1:]...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    return cmd.Start()
}
```

#### 3.5.11 版本管理

**版本号规范：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    版本号规范                                        │
└─────────────────────────────────────────────────────────────────────┘

格式：主版本号.次版本号.修订号

示例：1.0.0

主版本号：重大功能变更或不兼容的 API 修改
次版本号：向下兼容的功能性新增
修订号：向下兼容的问题修正

构建版本号：可选，用于标识构建信息

示例：1.0.0+build.123
```

**版本管理代码：**

```go
type VersionManager struct {
    currentVersion string
    versionHistory []VersionInfo
    mu             sync.RWMutex
}

// 版本信息
type VersionInfo struct {
    Version     string
    ReleaseDate time.Time
    Changes     []string
    Checksum    string
}

// 创建版本管理器
func NewVersionManager() *VersionManager {
    return &VersionManager{
        currentVersion: GetAgentVersion(),
    }
}

// 获取当前版本
func (m *VersionManager) GetCurrentVersion() string {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.currentVersion
}

// 更新版本
func (m *VersionManager) UpdateVersion(newVersion string) {
    m.mu.Lock()
    defer m.mu.Unlock()

    // 记录版本历史
    m.versionHistory = append(m.versionHistory, VersionInfo{
        Version:     m.currentVersion,
        ReleaseDate: time.Now(),
    })

    // 更新当前版本
    m.currentVersion = newVersion
}

// 获取版本历史
func (m *VersionManager) GetVersionHistory() []VersionInfo {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.versionHistory
}

// 比较版本
func (m *VersionManager) CompareVersions(v1, v2 string) int {
    // 解析版本号
    parts1 := strings.Split(v1, ".")
    parts2 := strings.Split(v2, ".")

    // 比较主版本号
    if parts1[0] != parts2[0] {
        return compareInt(parts1[0], parts2[0])
    }

    // 比较次版本号
    if parts1[1] != parts2[1] {
        return compareInt(parts1[1], parts2[1])
    }

    // 比较修订号
    return compareInt(parts1[2], parts2[2])
}
```

#### 3.5.12 文件保护

**文件保护机制：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    文件保护机制                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  设置       │
│  文件权限   │
└──────┬──────┘
       │
       ├── 二进制文件：755 (rwxr-xr-x)
       ├── 配置文件：644 (rw-r--r--)
       ├── 日志文件：644 (rw-r--r--)
       │
       ▼
┌─────────────┐
│  设置       │
│  文件属性   │
└──────┬──────┘
       │
       ├── 不可删除 (Linux: +i 属性)
       ├── 不可修改 (Linux: +i 属性)
       │
       ▼
┌─────────────┐
│  定期       │
│  完整性校验 │
└──────┬──────┘
       │
       ├── 计算文件哈希
       ├── 与预期哈希对比
       │
       ▼
┌─────────────┐
│  检测到     │
│  篡改       │
└──────┬──────┘
       │
       ├── 上报 Broker
       ├── 尝试恢复
       │
       ▼
┌─────────────┐
│  恢复       │
│  原始文件   │
└─────────────┘
```

**文件保护代码：**

```go
type AntiUninstallManager struct {
    config       *LifecycleConfig
    protectedFiles []ProtectedFile
    mu           sync.Mutex
}

// 受保护的文件
type ProtectedFile struct {
    Path     string
    Hash     string
    Permission os.FileMode
}

// 创建防卸载管理器
func NewAntiUninstallManager(config *LifecycleConfig) *AntiUninstallManager {
    return &AntiUninstallManager{
        config: config,
        protectedFiles: []ProtectedFile{
            {
                Path:       "/usr/local/bin/evdi-agent",
                Permission: 0755,
            },
            {
                Path:       "/etc/evdi/agent.yaml",
                Permission: 0644,
            },
        },
    }
}

// 启用防卸载
func (m *AntiUninstallManager) Enable() error {
    m.mu.Lock()
    defer m.mu.Unlock()

    // 1. 设置文件权限
    if err := m.setFilePermissions(); err != nil {
        return fmt.Errorf("failed to set file permissions: %w", err)
    }

    // 2. 设置文件属性（不可删除、不可修改）
    if err := m.setFileAttributes(); err != nil {
        return fmt.Errorf("failed to set file attributes: %w", err)
    }

    // 3. 计算文件哈希
    if err := m.calculateFileHashes(); err != nil {
        return fmt.Errorf("failed to calculate file hashes: %w", err)
    }

    // 4. 启动完整性检查
    m.startIntegrityCheck()

    return nil
}

// 设置文件权限
func (m *AntiUninstallManager) setFilePermissions() error {
    for _, file := range m.protectedFiles {
        if err := os.Chmod(file.Path, file.Permission); err != nil {
            return err
        }
    }
    return nil
}

// 设置文件属性（Linux）
func (m *AntiUninstallManager) setFileAttributes() error {
    for _, file := range m.protectedFiles {
        // 设置不可删除属性
        cmd := exec.Command("chattr", "+i", file.Path)
        if err := cmd.Run(); err != nil {
            return err
        }
    }
    return nil
}

// 计算文件哈希
func (m *AntiUninstallManager) calculateFileHashes() error {
    for i, file := range m.protectedFiles {
        hash, err := m.calculateFileHash(file.Path)
        if err != nil {
            return err
        }
        m.protectedFiles[i].Hash = hash
    }
    return nil
}

// 完整性检查
func (m *AntiUninstallManager) startIntegrityCheck() {
    go func() {
        ticker := time.NewTicker(5 * time.Minute)
        defer ticker.Stop()

        for range ticker.C {
            m.checkIntegrity()
        }
    }()
}

// 检查完整性
func (m *AntiUninstallManager) checkIntegrity() {
    for _, file := range m.protectedFiles {
        currentHash, err := m.calculateFileHash(file.Path)
        if err != nil {
            log.Printf("Failed to calculate hash for %s: %v", file.Path, err)
            continue
        }

        if currentHash != file.Hash {
            log.Printf("File integrity check failed: %s", file.Path)
            m.handleIntegrityViolation(file)
        }
    }
}
```

#### 3.5.13 进程保护

**进程保护机制：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    进程保护机制                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  进程       │
│  隐藏       │
└──────┬──────┘
       │
       ├── 修改进程名
       ├── 隐藏进程信息
       │
       ▼
┌─────────────┐
│  信号       │
│  屏蔽       │
└──────┬──────┘
       │
       ├── 屏蔽 SIGKILL (需要 root)
       ├── 屏蔽 SIGTERM
       ├── 屏蔽 SIGINT
       │
       ▼
┌─────────────┐
│  进程       │
│  监控       │
└──────┬──────┘
       │
       ├── 监控进程状态
       ├── 检测进程被杀
       │
       ▼
┌─────────────┐
│  检测到     │
│  进程被杀   │
└──────┬──────┘
       │
       ├── 上报 Broker
       ├── 自动重启
       │
       ▼
┌─────────────┐
│  自动       │
│  重启       │
└─────────────┘
```

**进程保护代码：**

```go
// 进程保护
func (m *AntiUninstallManager) EnableProcessProtection() error {
    // 1. 屏蔽信号
    if err := m.blockSignals(); err != nil {
        return fmt.Errorf("failed to block signals: %w", err)
    }

    // 2. 启动进程监控
    m.startProcessMonitor()

    return nil
}

// 屏蔽信号
func (m *AntiUninstallManager) blockSignals() error {
    // 屏蔽 SIGTERM 和 SIGINT
    signal.Notify(make(chan os.Signal, 1),
        syscall.SIGTERM,
        syscall.SIGINT,
    )

    // 注意：SIGKILL 无法在用户空间屏蔽
    // 需要 root 权限才能屏蔽 SIGKILL

    return nil
}

// 启动进程监控
func (m *AntiUninstallManager) startProcessMonitor() {
    go func() {
        for {
            // 检查进程状态
            if !m.isProcessAlive() {
                log.Printf("Process detected as killed")
                m.handleProcessKilled()
            }

            time.Sleep(1 * time.Second)
        }
    }()
}

// 检查进程是否存活
func (m *AntiUninstallManager) isProcessAlive() bool {
    // 检查 /proc/self 存在
    _, err := os.Stat("/proc/self")
    return err == nil
}

// 处理进程被杀
func (m *AntiUninstallManager) handleProcessKilled() {
    // 上报 Broker
    m.reportProcessKilled()

    // 自动重启
    m.restartProcess()
}
```

#### 3.5.14 卸载检测

**卸载检测机制：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    卸载检测机制                                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  监控       │
│  文件系统   │
└──────┬──────┘
       │
       ├── 监控 /usr/local/bin/evdi-agent
       ├── 监控 /etc/evdi/
       │
       ▼
┌─────────────┐
│  检测到     │
│  文件被删   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  上报       │
│  Broker     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  尝试       │
│  恢复文件   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  恢复失败   │
│  重新安装   │
└─────────────┘
```

**卸载检测代码：**

```go
// 启动卸载检测
func (m *AntiUninstallManager) StartUninstallDetection() {
    go func() {
        ticker := time.NewTicker(1 * time.Minute)
        defer ticker.Stop()

        for range ticker.C {
            m.detectUninstallAttempt()
        }
    }()
}

// 检测卸载尝试
func (m *AntiUninstallManager) detectUninstallAttempt() {
    for _, file := range m.protectedFiles {
        // 检查文件是否存在
        if _, err := os.Stat(file.Path); os.IsNotExist(err) {
            log.Printf("Protected file missing: %s", file.Path)
            m.handleUninstallAttempt(file)
        }
    }
}

// 处理卸载尝试
func (m *AntiUninstallManager) handleUninstallAttempt(file ProtectedFile) {
    // 上报 Broker
    m.reportUninstallAttempt(file)

    // 尝试恢复文件
    if err := m.restoreFile(file); err != nil {
        log.Printf("Failed to restore file: %v", err)
        m.handleRestoreFailure(file)
    }
}

// 恢复文件
func (m *AntiUninstallManager) restoreFile(file ProtectedFile) error {
    // 从备份恢复
    backupPath := file.Path + ".backup"
    if _, err := os.Stat(backupPath); err == nil {
        return copyFile(backupPath, file.Path)
    }

    // 从 Broker 下载
    return m.downloadFromBroker(file)
}
```

#### 3.5.15 生命周期管理模块完整生命周期

```
┌─────────────────────────────────────────────────────────────────────┐
│                    生命周期管理模块完整生命周期                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  系统启动   │
│  Agent 服务 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  1. 环境检查│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  2. 加载配置│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  3. 启动    │
│  防卸载机制 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  4. 启动    │
│  进程守护   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  5. 启动    │
│  崩溃恢复   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  6. 连接    │
│  Broker     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  7. 启动    │
│  自动升级   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  8. 就绪    │
│  等待连接   │
└──────┬──────┘
       │
       ├── 正常运行 ──────────────────────┐
       │                                  │
       │ 崩溃发生                         │
       ▼                                  │
┌─────────────┐                          │
│  9. 崩溃    │                          │
│  检测       │                          │
└──────┬──────┘                          │
       │                                  │
       ▼                                  │
┌─────────────┐                          │
│  10. 记录   │                          │
│  崩溃信息   │                          │
└──────┬──────┘                          │
       │                                  │
       ▼                                  │
┌─────────────┐                          │
│  11. 上报   │                          │
│  Broker     │                          │
└──────┬──────┘                          │
       │                                  │
       ▼                                  │
┌─────────────┐                          │
│  12. 立即   │                          │
│  重启       │                          │
└──────┬──────┘                          │
       │                                  │
       ▼                                  │
┌─────────────┐                          │
│  13. 恢复   │                          │
│  会话       │◄─────────────────────────┘
└──────┬──────┘
       │
       │ 升级指令
       ▼
┌─────────────┐
│  14. 停止   │
│  Agent 服务 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  15. 备份   │
│  当前版本   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  16. 替换   │
│  二进制     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  17. 启动   │
│  新版本     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  18. 验证   │
│  升级成功   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  19. 继续   │
│  服务       │
└─────────────┘
```

---

## 4. 通信协议设计

### 4.1 Agent ↔ Broker 通信协议

Agent 与 Broker 之间采用 HTTP REST 协议通信，消息格式为 JSON。所有接口遵循 Broker 设计文档中定义的统一规范。

#### 4.1.1 协议概述

**通信规范：**

| 规范项 | 定义 | 说明 |
|--------|------|------|
| 协议 | HTTP/HTTPS | REST 风格 |
| 消息格式 | JSON | 请求/响应均为 JSON |
| 基础路径 | `/api/v1/agent` | Agent 专用接口前缀 |
| 认证方式 | Bearer Token | Agent 注册后获取 Token |
| 时间格式 | ISO 8601 UTC | 例如：`2024-01-01T08:00:00Z` |
| ID 格式 | UUID v4 | 应用层生成 |

**统一响应格式：**

```json
{
  "code": 0,
  "message": "success",
  "data": { }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `code` | int | 业务错误码，0 表示成功 |
| `message` | string | 错误描述，成功时为 `"success"` |
| `data` | object / array / null | 业务数据 |

#### 4.1.2 认证方式

**认证流程：**

```
┌─────────────┐                    ┌─────────────┐
│   Agent     │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. 注册请求                     │
       │  (无需认证)                      │
       │─────────────────────────────────►│
       │                                  │
       │  2. 返回 Agent Token             │
       │◄─────────────────────────────────│
       │                                  │
       │  3. 后续请求                     │
       │  Header: Authorization: Bearer <token>
       │─────────────────────────────────►│
       │                                  │
```

**Token 说明：**

- Agent Token 由 Broker 在注册成功后签发
- Token 有效期与 Agent 生命周期绑定
- Agent 重启后需重新注册获取新 Token
- Token 格式：`agent_{desktop_id}_{random}`

#### 4.1.3 Agent 注册接口

**请求：**

```
POST /api/v1/agent/register
Content-Type: application/json
```

**Request Body：**

```json
{
  "desktopId": "desktop_001",
  "agentVersion": "1.0.0",
  "hostname": "desktop-001-pod",
  "ip": "10.100.1.5",
  "osType": "linux",
  "gpuInfo": {
    "vendor": "nvidia",
    "model": "Tesla T4",
    "driverVersion": "535.104.05"
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `desktopId` | string | 是 | 桌面实例 ID |
| `agentVersion` | string | 是 | Agent 版本号 |
| `hostname` | string | 是 | 主机名 |
| `ip` | string | 是 | Agent IP 地址 |
| `osType` | string | 是 | 操作系统类型（linux / windows） |
| `gpuInfo` | object | 否 | GPU 信息 |

**gpuInfo 结构：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `vendor` | string | GPU 厂商（nvidia / intel / amd） |
| `model` | string | GPU 型号 |
| `driverVersion` | string | 驱动版本 |

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "agentToken": "agent_desktop_001_abc123",
    "iceConfig": {
      "servers": [
        { "urls": "stun:stun.example.com:3478" },
        {
          "urls": ["turn:turn.example.com:3478"],
          "username": "user",
          "credential": "pass"
        }
      ]
    },
    "encodingConfig": {
      "width": 1920,
      "height": 1080,
      "framerate": 30,
      "bitrate": 4000
    },
    "sessionPolicy": {
      "maxSessions": 1,
      "sessionTimeout": 1800,
      "allowReconnect": true,
      "reconnectTimeout": 30
    },
    "monitorConfig": {
      "collectInterval": 60,
      "reportInterval": 60
    }
  }
}
```

**注册失败处理：**

| 错误码 | 说明 | 处理方式 |
|--------|------|----------|
| 1001 | Token 无效 | 重新注册 |
| 1003 | 桌面不存在 | 检查 desktopId |
| 1004 | 桌面状态冲突 | 等待桌面就绪 |
| 5000 | 服务内部错误 | 指数退避重试 |

#### 4.1.4 心跳上报接口

**请求：**

```
POST /api/v1/agent/heartbeat
Authorization: Bearer <agentToken>
Content-Type: application/json
```

**Request Body：**

```json
{
  "desktopId": "desktop_001",
  "agentVersion": "1.0.0",
  "uptime": 3600,
  "ready": {
    "agent": true,
    "desktopService": true,
    "captureService": true,
    "loginReady": true
  },
  "system": {
    "cpuUsagePercent": 25.5,
    "memoryUsagePercent": 45.2,
    "diskUsagePercent": 60.0
  },
  "network": {
    "clientConnected": true,
    "bitrateKbps": 8000,
    "packetLossRate": 0.001,
    "latencyMs": 15
  },
  "session": {
    "sessionId": "sess_xyz",
    "sessionState": "Connected",
    "connectedAt": "2024-01-01T08:00:05Z",
    "durationSec": 1800
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `desktopId` | string | 是 | 桌面 ID |
| `agentVersion` | string | 是 | Agent 版本号 |
| `uptime` | int | 是 | Agent 运行时长（秒） |
| `ready` | object | 是 | 四项就绪状态 |
| `system` | object | 是 | 系统资源使用率 |
| `network` | object | 否 | 网络质量指标 |
| `session` | object | 否 | 当前会话信息 |

**ready 字段说明：**

| 字段 | 含义 | 为 true 的条件 |
|------|------|---------------|
| `agent` | Agent 进程正常 | Agent 启动成功并完成注册 |
| `desktopService` | 桌面服务就绪 | GuestOS 启动完成、用户环境加载成功 |
| `captureService` | 屏幕捕获就绪 | GStreamer 捕获 pipeline 启动成功 |
| `loginReady` | 用户可登录 | 登录服务（LightDM / GDM）启动完成 |

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "nextHeartbeatIntervalSec": 15,
    "configVersion": "v3",
    "commands": [
      {
        "commandId": "cmd_001",
        "type": "LOCK_SCREEN",
        "params": {},
        "timestamp": "2024-01-01T08:00:00Z"
      }
    ]
  }
}
```

**心跳频率：**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| 心跳间隔 | 15 秒 | Agent 上报频率 |
| 超时阈值 | 60 秒 | 超过 4 个心跳周期未上报视为超时 |

#### 4.1.5 配置拉取接口

**请求：**

```
GET /api/v1/agent/config?version=v3
Authorization: Bearer <agentToken>
```

**查询参数：**

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `version` | string | 否 | 当前配置版本，用于增量更新 |

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "version": "v4",
    "iceConfig": {
      "servers": [
        { "urls": "stun:stun.example.com:3478" },
        {
          "urls": ["turn:turn.example.com:3478"],
          "username": "user",
          "credential": "pass"
        }
      ]
    },
    "encodingConfig": {
      "width": 1920,
      "height": 1080,
      "framerate": 30,
      "bitrate": 4000
    },
    "sessionPolicy": {
      "maxSessions": 1,
      "sessionTimeout": 1800,
      "allowReconnect": true,
      "reconnectTimeout": 30
    },
    "monitorConfig": {
      "collectInterval": 60,
      "reportInterval": 60
    }
  }
}
```

**配置版本管理：**

- Broker 维护配置版本号（递增）
- Agent 请求时携带当前版本号
- 版本相同：返回 `"data": null`，无需更新
- 版本不同：返回完整配置

#### 4.1.6 会话事件上报接口

**请求：**

```
POST /api/v1/agent/session-event
Authorization: Bearer <agentToken>
Content-Type: application/json
```

**Request Body：**

```json
{
  "eventId": "evt_001",
  "desktopId": "desktop_001",
  "sessionId": "sess_xyz",
  "eventType": "SESSION_CONNECTED",
  "timestamp": "2024-01-01T08:00:00Z",
  "reason": "WebRTC connection established",
  "metadata": {
    "clientIp": "10.0.0.1",
    "clientType": "tauri"
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `eventId` | string | 是 | 事件 ID（UUID） |
| `desktopId` | string | 是 | 桌面 ID |
| `sessionId` | string | 是 | 会话 ID |
| `eventType` | string | 是 | 事件类型 |
| `timestamp` | string | 是 | 事件时间（ISO 8601） |
| `reason` | string | 否 | 事件原因 |
| `metadata` | object | 否 | 扩展信息 |

**事件类型枚举：**

| eventType | 说明 |
|-----------|------|
| `SESSION_CREATED` | 会话创建 |
| `SESSION_CONNECTED` | 会话连接（WebRTC 连接建立） |
| `SESSION_DISCONNECTED` | 会话断开 |
| `SESSION_ENDED` | 会话结束 |
| `SESSION_ERROR` | 会话异常 |
| `SESSION_REPLACED` | 会话被替换（新会话踢掉旧会话） |

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": null
}
```

#### 4.1.7 监控数据上报接口

**请求：**

```
POST /api/v1/agent/monitor-data
Authorization: Bearer <agentToken>
Content-Type: application/json
```

**Request Body：**

```json
{
  "desktopId": "desktop_001",
  "timestamp": "2024-01-01T08:00:00Z",
  "metrics": [
    {
      "name": "cpu_usage",
      "value": 25.5,
      "unit": "%"
    },
    {
      "name": "memory_usage",
      "value": 45.2,
      "unit": "%"
    },
    {
      "name": "disk_usage",
      "value": 60.0,
      "unit": "%"
    },
    {
      "name": "network_latency",
      "value": 15,
      "unit": "ms"
    },
    {
      "name": "session_duration",
      "value": 1800,
      "unit": "s"
    }
  ]
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `desktopId` | string | 是 | 桌面 ID |
| `timestamp` | string | 是 | 采集时间（ISO 8601） |
| `metrics` | array | 是 | 监控指标列表 |

**metrics 结构：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | string | 指标名称 |
| `value` | float | 指标值（保留 1 位小数） |
| `unit` | string | 单位 |

**监控指标定义：**

| 指标名称 | 单位 | 说明 |
|----------|------|------|
| `cpu_usage` | % | CPU 使用率 |
| `memory_usage` | % | 内存使用率 |
| `disk_usage` | % | 磁盘使用率 |
| `network_latency` | ms | 网络延迟 |
| `session_duration` | s | 会话时长 |

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": null
}
```

#### 4.1.8 指令接收与确认

**指令接收：**

Agent 通过心跳响应接收 Broker 下发的指令：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "nextHeartbeatIntervalSec": 15,
    "commands": [
      {
        "commandId": "cmd_001",
        "type": "LOCK_SCREEN",
        "params": {},
        "timestamp": "2024-01-01T08:00:00Z"
      }
    ]
  }
}
```

**指令类型：**

| 指令类型 | 说明 | 参数 |
|----------|------|------|
| `LOCK_SCREEN` | 锁定桌面 | 无 |
| `UNLOCK_SCREEN` | 解锁桌面 | 无 |
| `LOGOFF` | 注销用户 | `reason` |
| `REBOOT` | 重启桌面 | `reason` |
| `UPDATE_CONFIG` | 更新配置 | 配置对象 |
| `UPDATE_AGENT` | 升级 Agent | `version`, `downloadUrl`, `checksum` |

**指令确认：**

```
POST /api/v1/agent/command-result
Authorization: Bearer <agentToken>
Content-Type: application/json
```

**Request Body：**

```json
{
  "commandId": "cmd_001",
  "success": true,
  "message": "Lock screen executed successfully",
  "executedAt": "2024-01-01T08:00:01Z"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `commandId` | string | 是 | 指令 ID |
| `success` | bool | 是 | 执行是否成功 |
| `message` | string | 否 | 执行结果描述 |
| `executedAt` | string | 是 | 执行时间（ISO 8601） |

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": null
}
```

#### 4.1.9 错误码规范

**错误码定义（与 Broker 设计文档一致）：**

| 错误码 | HTTP 状态码 | 说明 |
|--------|-------------|------|
| 0 | 200 | 成功 |
| 1001 | 401 | Token 无效或已过期 |
| 1002 | 403 | 无权限 |
| 1003 | 404 | 资源不存在 |
| 1004 | 409 | 资源状态冲突 |
| 1005 | 429 | 请求频率超限 |
| 2001 | 400 | Desktop 不存在 |
| 2002 | 409 | Desktop 状态不允许此操作 |
| 5000 | 500 | 服务内部错误 |

**错误响应示例：**

```json
{
  "code": 1001,
  "message": "Token 无效或已过期",
  "data": null
}
```

#### 4.1.10 错误重试策略

**重试策略：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    错误重试策略（指数退避）                            │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│  请求失败   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  判断错误   │
│  类型       │
└──────┬──────┘
       │
       ├── 4xx 错误（客户端错误）──► 不重试，记录日志
       │
       ├── 5xx 错误（服务端错误）──► 指数退避重试
       │
       └── 网络错误 ──► 指数退避重试
       │
       ▼
┌─────────────┐
│  等待       │
│  1秒        │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  重试 #1    │
└──────┬──────┘
       │
       ├── 成功 ──► 返回结果
       │
       ▼
┌─────────────┐
│  等待       │
│  2秒        │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  重试 #2    │
└──────┬──────┘
       │
       ├── 成功 ──► 返回结果
       │
       ▼
┌─────────────┐
│  等待       │
│  4秒        │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  重试 #3    │
└──────┬──────┘
       │
       └── 失败 ──► 记录日志，上报 Broker
```

**重试配置：**

| 参数 | 值 | 说明 |
|------|-----|------|
| 最大重试次数 | 3 | 5xx 和网络错误 |
| 初始延迟 | 1 秒 | 第一次重试等待时间 |
| 最大延迟 | 10 秒 | 延迟上限 |
| 退避因子 | 2.0 | 指数退避倍数 |

**重试代码：**

```go
type RetryConfig struct {
    MaxRetries    int
    InitialDelay  time.Duration
    MaxDelay      time.Duration
    BackoffFactor float64
}

var defaultRetryConfig = RetryConfig{
    MaxRetries:    3,
    InitialDelay:  1 * time.Second,
    MaxDelay:      10 * time.Second,
    BackoffFactor: 2.0,
}

// 带重试的 HTTP 请求
func (c *BrokerClient) doWithRetry(req *http.Request) (*http.Response, error) {
    config := defaultRetryConfig
    delay := config.InitialDelay

    for retry := 0; retry <= config.MaxRetries; retry++ {
        // 发送请求
        resp, err := c.httpClient.Do(req)
        if err != nil {
            // 网络错误，重试
            if retry < config.MaxRetries {
                log.Printf("Request failed (retry %d/%d): %v", retry+1, config.MaxRetries, err)
                time.Sleep(delay)
                delay = time.Duration(float64(delay) * config.BackoffFactor)
                if delay > config.MaxDelay {
                    delay = config.MaxDelay
                }
                continue
            }
            return nil, err
        }

        // 检查 HTTP 状态码
        if resp.StatusCode >= 500 {
            // 服务端错误，重试
            resp.Body.Close()
            if retry < config.MaxRetries {
                log.Printf("Server error %d (retry %d/%d)", resp.StatusCode, retry+1, config.MaxRetries)
                time.Sleep(delay)
                delay = time.Duration(float64(delay) * config.BackoffFactor)
                if delay > config.MaxDelay {
                    delay = config.MaxDelay
                }
                continue
            }
            return nil, fmt.Errorf("server error: %d", resp.StatusCode)
        }

        // 4xx 错误或成功，不重试
        return resp, nil
    }

    return nil, fmt.Errorf("max retries exceeded")
}
```

#### 4.1.11 错误上报

**错误上报接口：**

```
POST /api/v1/agent/error
Authorization: Bearer <agentToken>
Content-Type: application/json
```

**Request Body：**

```json
{
  "desktopId": "desktop_001",
  "errorType": "NETWORK_ERROR",
  "errorMessage": "Failed to connect to broker: connection refused",
  "stackTrace": "goroutine 1 [running]:\nmain.main()...",
  "timestamp": "2024-01-01T08:00:00Z",
  "context": {
    "component": "broker_client",
    "operation": "heartbeat"
  }
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `desktopId` | string | 是 | 桌面 ID |
| `errorType` | string | 是 | 错误类型 |
| `errorMessage` | string | 是 | 错误消息 |
| `stackTrace` | string | 否 | 堆栈跟踪 |
| `timestamp` | string | 是 | 错误时间（ISO 8601） |
| `context` | object | 否 | 错误上下文 |

**错误类型枚举：**

| errorType | 说明 |
|-----------|------|
| `NETWORK_ERROR` | 网络错误 |
| `TIMEOUT_ERROR` | 超时错误 |
| `AUTH_ERROR` | 认证错误 |
| `BUSINESS_ERROR` | 业务错误 |
| `SYSTEM_ERROR` | 系统错误 |

#### 4.1.12 完整通信时序

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Agent ↔ Broker 完整通信时序                        │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Agent     │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. POST /agent/register         │
       │  (desktopId, agentVersion, ...)  │
       │─────────────────────────────────►│
       │                                  │
       │  2. 返回 agentToken + 配置       │
       │◄─────────────────────────────────│
       │                                  │
       │  3. POST /agent/heartbeat        │
       │  (每 15 秒)                      │
       │  Header: Bearer <agentToken>     │
       │─────────────────────────────────►│
       │                                  │
       │  4. 返回 commands + config       │
       │◄─────────────────────────────────│
       │                                  │
       │  5. POST /agent/session-event    │
       │  (会话状态变化时)                │
       │─────────────────────────────────►│
       │                                  │
       │  6. 确认                         │
       │◄─────────────────────────────────│
       │                                  │
       │  7. POST /agent/monitor-data     │
       │  (每 60 秒)                      │
       │─────────────────────────────────►│
       │                                  │
       │  8. 确认                         │
       │◄─────────────────────────────────│
       │                                  │
       │  9. POST /agent/command-result   │
       │  (执行指令后)                    │
       │─────────────────────────────────►│
       │                                  │
       │  10. 确认                        │
       │◄─────────────────────────────────│
       │                                  │
```

### 4.2 Agent ↔ Client 通信协议（WebRTC DataChannel）

Agent 与 Client 之间通过 WebRTC DataChannel 传输控制信号和输入事件。DataChannel 基于 SCTP 协议，支持可靠有序和不可靠两种传输模式。

#### 4.2.1 DataChannel 概述

**DataChannel 特性：**

| 特性 | 说明 |
|------|------|
| 协议 | SCTP over DTLS |
| 传输模式 | 可靠有序 / 不可靠有序 |
| 消息格式 | 二进制 |
| 多路复用 | 单条 WebRTC 连接承载多个 DataChannel |
| 加密 | DTLS 内置加密 |

**与媒体流的关系：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    WebRTC 连接结构                                   │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Client    │                    │    Agent    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  WebRTC PeerConnection           │
       │  ┌─────────────────────────────┐ │
       │  │  视频轨 (H.264)            │ │
       │  │  ← 桌面画面                │ │
       │  ├─────────────────────────────┤ │
       │  │  音频轨 (Opus)             │ │
       │  │  ← 桌面音频                │ │
       │  ├─────────────────────────────┤ │
       │  │  DataChannel: control      │ │
       │  │  ↔ 控制信号                │ │
       │  ├─────────────────────────────┤ │
       │  │  DataChannel: input        │ │
       │  │  → 键鼠/触控输入           │ │
       │  ├─────────────────────────────┤ │
       │  │  DataChannel: clipboard    │ │
       │  │  ↔ 剪贴板同步              │ │
       │  └─────────────────────────────┘ │
       │                                  │
       └──────────────────────────────────┘
```

#### 4.2.2 通道划分

**DataChannel 命名规范：**

| 通道名称 | 用途 | 可靠性 | 方向 |
|----------|------|--------|------|
| `control` | 控制信号（系统命令、媒体控制） | 可靠有序 | 双向 |
| `input` | 输入事件（键盘、鼠标、触控） | 可靠有序 | Client → Agent |
| `clipboard` | 剪贴板同步 | 可靠有序 | 双向 |
| `heartbeat` | 心跳保活 | 不可靠 | 双向 |

**通道创建代码：**

```go
// 创建 DataChannel
func (e *WebRTCEngine) CreateDataChannels() error {
    // 控制通道（可靠有序）
    controlChannel, err := e.peerConnection.CreateDataChannel("control", &webrtc.DataChannelInit{
        Ordered: boolPtr(true),
    })
    if err != nil {
        return err
    }
    e.controlChannel = controlChannel

    // 输入通道（可靠有序）
    inputChannel, err := e.peerConnection.CreateDataChannel("input", &webrtc.DataChannelInit{
        Ordered: boolPtr(true),
    })
    if err != nil {
        return err
    }
    e.inputChannel = inputChannel

    // 剪贴板通道（可靠有序）
    clipboardChannel, err := e.peerConnection.CreateDataChannel("clipboard", &webrtc.DataChannelInit{
        Ordered: boolPtr(true),
    })
    if err != nil {
        return err
    }
    e.clipboardChannel = clipboardChannel

    // 心跳通道（不可靠）
    heartbeatChannel, err := e.peerConnection.CreateDataChannel("heartbeat", &webrtc.DataChannelInit{
        Ordered: boolPtr(false),
    })
    if err != nil {
        return err
    }
    e.heartbeatChannel = heartbeatChannel

    return nil
}
```

#### 4.2.3 可靠性分层

**可靠性配置：**

| 通道 | Ordered | MaxRetransmits | 说明 |
|------|---------|----------------|------|
| `control` | true | 无限 | 必须到达，顺序不能乱 |
| `input` | true | 无限 | 按键顺序不能乱 |
| `clipboard` | true | 无限 | 数据不能丢失 |
| `heartbeat` | false | 0 | 丢一两个没关系 |

**可靠性说明：**

- **可靠有序（Ordered: true）**：保证消息到达且顺序正确，类似 TCP
- **不可靠（Ordered: false, MaxRetransmits: 0）**：不保证到达，类似 UDP

#### 4.2.4 消息格式

**消息头格式（8 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

| 字段 | 偏移 | 长度 | 说明 |
|------|------|------|------|
| Version | 0 | 1 字节 | 协议版本，当前为 1 |
| Type | 1 | 1 字节 | 消息类型 |
| Reserved | 2 | 2 字节 | 保留字段 |
| Timestamp | 4 | 4 字节 | 时间戳（毫秒） |

**消息类型定义：**

| Type | 名称 | 说明 |
|------|------|------|
| 0x01 | KEY_DOWN | 键盘按下 |
| 0x02 | KEY_UP | 键盘释放 |
| 0x03 | MOUSE_MOVE | 鼠标移动 |
| 0x04 | MOUSE_DOWN | 鼠标按下 |
| 0x05 | MOUSE_UP | 鼠标释放 |
| 0x06 | MOUSE_SCROLL | 鼠标滚轮 |
| 0x07 | TOUCH_START | 触摸开始 |
| 0x08 | TOUCH_MOVE | 触摸移动 |
| 0x09 | TOUCH_END | 触摸结束 |
| 0x0A | GESTURE_PINCH | 手势缩放 |
| 0x0B | GESTURE_SWIPE | 手势滑动 |
| 0x0C | GESTURE_ROTATE | 手势旋转 |
| 0x10 | SYSTEM_COMMAND | 系统命令 |
| 0x11 | COMMAND_RESULT | 命令结果 |
| 0x20 | CLIPBOARD_UPDATE | 剪贴板更新 |
| 0x21 | CLIPBOARD_REQUEST | 剪贴板请求 |
| 0x30 | HEARTBEAT | 心跳 |
| 0x31 | HEARTBEAT_ACK | 心跳响应 |
| 0x40 | MEDIA_CONTROL | 媒体控制 |
| 0x41 | MEDIA_STATE | 媒体状态 |

**字节序：** 大端序（Big-Endian）

#### 4.2.5 键盘事件消息

**消息格式（12 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|          KeyCode             |            Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

| 字段 | 偏移 | 长度 | 说明 |
|------|------|------|------|
| Version | 0 | 1 字节 | 协议版本 |
| Type | 1 | 1 字节 | 0x01 (KEY_DOWN) 或 0x02 (KEY_UP) |
| Reserved | 2 | 2 字节 | 保留 |
| Timestamp | 4 | 4 字节 | 时间戳 |
| KeyCode | 8 | 2 字节 | 按键码 |
| Reserved | 10 | 2 字节 | 保留 |

**特殊按键码：**

| KeyCode | 按键 |
|---------|------|
| 0x0008 | Backspace |
| 0x0009 | Tab |
| 0x000D | Enter |
| 0x0010 | Shift |
| 0x0011 | Ctrl |
| 0x0012 | Alt |
| 0x0014 | Caps Lock |
| 0x001B | Escape |
| 0x0020 | Space |
| 0x0025 | Left Arrow |
| 0x0026 | Up Arrow |
| 0x0027 | Right Arrow |
| 0x0028 | Down Arrow |
| 0x002E | Delete |
| 0x005B | Windows Key |

**组合键处理：**

组合键通过多个 KEY_DOWN 事件实现：

```
Ctrl+C:
1. KEY_DOWN (KeyCode: 0x0011, Ctrl)
2. KEY_DOWN (KeyCode: 0x0043, C)
3. KEY_UP (KeyCode: 0x0043, C)
4. KEY_UP (KeyCode: 0x0011, Ctrl)
```

#### 4.2.6 鼠标事件消息

**鼠标移动消息（16 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                              X                                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                              Y                                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

**鼠标点击消息（20 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                              X                                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                              Y                                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Button     |                   Reserved                    |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

**鼠标按钮定义：**

| Button | 说明 |
|--------|------|
| 0x00 | 无 |
| 0x01 | 左键 |
| 0x02 | 右键 |
| 0x03 | 中键 |

**鼠标滚轮消息（16 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                            DeltaX                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                            DeltaY                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

#### 4.2.7 触控事件消息

**触控消息（24 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                           TouchID                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                              X                                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                              Y                                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                           Pressure                            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

| 字段 | 偏移 | 长度 | 说明 |
|------|------|------|------|
| TouchID | 8 | 4 字节 | 触摸点 ID |
| X | 12 | 4 字节 | X 坐标 |
| Y | 16 | 4 字节 | Y 坐标 |
| Pressure | 20 | 4 字节 | 压力（0.0-1.0，IEEE 754 浮点） |

#### 4.2.8 手势事件消息

**手势消息（24 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                            Scale                              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                           Rotation                            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                            DeltaX                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                            DeltaY                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

**手势类型：**

| Type | 名称 | Scale | Rotation | DeltaX/Y |
|------|------|-------|----------|----------|
| 0x0A | 缩放 | 缩放比例 | 0 | 0 |
| 0x0B | 滑动 | 0 | 0 | 滑动距离 |
| 0x0C | 旋转 | 0 | 旋转角度 | 0 |

#### 4.2.9 系统命令消息

**命令请求消息（16 字节 + 参数）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          CommandID                            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    CommandType|                   Reserved                    |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                         Parameters...                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

**命令类型定义：**

| CommandType | 名称 | 说明 |
|-------------|------|------|
| 0x01 | TRIGGER_SAS | 触发 Ctrl+Alt+Del |
| 0x02 | OPEN_TASK_MANAGER | 打开任务管理器 |
| 0x03 | LOCK_SCREEN | 锁定桌面 |

**命令响应消息（16 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          CommandID                            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Success    |                   Reserved                    |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

| 字段 | 偏移 | 长度 | 说明 |
|------|------|------|------|
| CommandID | 8 | 4 字节 | 命令 ID |
| Success | 12 | 1 字节 | 0x01 成功，0x00 失败 |

#### 4.2.10 剪贴板消息

**剪贴板更新消息（变长）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |       ContentType             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        ContentLength                          |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Content...                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

| 字段 | 偏移 | 长度 | 说明 |
|------|------|------|------|
| ContentType | 2 | 2 字节 | 内容类型 |
| ContentLength | 8 | 4 字节 | 内容长度 |
| Content | 12 | 变长 | 内容数据 |

**内容类型定义：**

| ContentType | 名称 | 说明 |
|-------------|------|------|
| 0x0001 | TEXT | 文本（UTF-8 编码） |
| 0x0002 | IMAGE | 图片（PNG/JPEG） |
| 0x0003 | FILE | 文件（自定义格式） |

**剪贴板请求消息（8 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

#### 4.2.11 心跳消息

**心跳请求（8 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

**心跳响应（8 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

**心跳间隔：** 30 秒

#### 4.2.12 媒体控制消息

**媒体控制消息（16 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|      ControlType             |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                            Value                              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

**控制类型定义：**

| ControlType | 名称 | Value 说明 |
|-------------|------|------------|
| 0x0001 | SET_RESOLUTION | 分辨率（高 16 位宽，低 16 位高） |
| 0x0002 | SET_FRAMERATE | 帧率 |
| 0x0003 | SET_BITRATE | 码率（kbps） |

**媒体状态消息（16 字节）：**

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |           Reserved            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                        Timestamp (ms)                         |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                            Width                              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                            Height                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

#### 4.2.13 消息处理代码

**Go 代码实现：**

```go
// DataChannel 消息结构（JSON 统一格式）
type ControlMessage struct {
    V       int                    `json:"v"`
    Type    string                 `json:"type"`
    Ts      int64                  `json:"ts"`
    Seq     int                    `json:"seq"`
    Payload map[string]interface{} `json:"payload"`
}

// 消息路由与处理
func (e *WebRTCEngine) handleMessage(channel string, jsonData []byte) error {
    var msg ControlMessage
    if err := json.Unmarshal(jsonData, &msg); err != nil {
        return fmt.Errorf("invalid message format: %w", err)
    }

    // 版本检查
    if msg.V != 1 {
        return fmt.Errorf("unsupported protocol version: %d", msg.V)
    }

    // 根据通道和消息类型路由
    if channel == "control" {
        return e.handleControlMessage(msg)
    } else if channel == "bulk" {
        return e.handleBulkMessage(msg)
    }
    return fmt.Errorf("unknown channel: %s", channel)
}

// 控制通道消息处理
func (e *WebRTCEngine) handleControlMessage(msg ControlMessage) error {
    switch msg.Type {
    case "input.mouse_move":
        return e.handleMouseMove(msg.Payload)
    case "input.mouse_button":
        return e.handleMouseButton(msg.Payload)
    case "input.mouse_wheel":
        return e.handleMouseWheel(msg.Payload)
    case "input.key":
        return e.handleKeyboardEvent(msg.Payload)
    case "ctrl.ping":
        return e.handlePing(msg.Payload)
    case "ctrl.resize":
        return e.handleResize(msg.Payload)
    default:
        return fmt.Errorf("unknown control message type: %s", msg.Type)
    }
}

// 大数据通道消息处理
func (e *WebRTCEngine) handleBulkMessage(msg ControlMessage) error {
    switch msg.Type {
    case "clipboard.push":
        return e.handleClipboardPush(msg.Payload)
    case "clipboard.request":
        return e.handleClipboardRequest(msg.Payload)
    default:
        return fmt.Errorf("unknown bulk message type: %s", msg.Type)
    }
}
```

**消息发送辅助函数：**

```go
// 构造并 JSON 序列化消息
func buildMessage(msgType string, ts int64, seq int, payload map[string]interface{}) ([]byte, error) {
    msg := ControlMessage{
        V:       1,
        Type:    msgType,
        Ts:      ts,
        Seq:     seq,
        Payload: payload,
    }
    return json.Marshal(msg)
}

// 发送控制消息
func (e *WebRTCEngine) sendControlMessage(msgType string, payload map[string]interface{}, channelName string) error {
    ts := time.Now().UnixNano() / int64(time.Millisecond)
    msgData, err := buildMessage(msgType, ts, e.sequenceNumber(channelName), payload)
    if err != nil {
        return err
    }
    return e.dataChannel[channelName].Send(msgData)
}
```

**键盘事件处理示例：**

```go
// 处理键盘按下事件
func (e *WebRTCEngine) handleKeyDown(data []byte) error {
    if len(data) < 12 {
        return fmt.Errorf("key down message too short")
    }

    keyCode := binary.BigEndian.Uint16(data[8:10])

    // 检查是否为自定义快捷键
    if e.isCustomShortcut(keyCode) {
        return e.handleCustomShortcut(keyCode)
    }

    // 注入键盘事件
    return e.desktop.InjectKeyEvent(keyCode, true)
}

// 处理键盘释放事件
func (e *WebRTCEngine) handleKeyUp(data []byte) error {
    if len(data) < 12 {
        return fmt.Errorf("key up message too short")
    }

    keyCode := binary.BigEndian.Uint16(data[8:10])

    // 注入键盘事件
    return e.desktop.InjectKeyEvent(keyCode, false)
}
```

**鼠标事件处理：**

```go
// 处理鼠标移动事件
func (e *WebRTCEngine) handleMouseMove(data []byte) error {
    if len(data) < 16 {
        return fmt.Errorf("mouse move message too short")
    }

    x := int32(binary.BigEndian.Uint32(data[8:12]))
    y := int32(binary.BigEndian.Uint32(data[12:16]))

    // 注入鼠标移动事件
    return e.desktop.InjectMouseMove(x, y)
}

// 处理鼠标点击事件
func (e *WebRTCEngine) handleMouseDown(data []byte) error {
    if len(data) < 20 {
        return fmt.Errorf("mouse down message too short")
    }

    x := int32(binary.BigEndian.Uint32(data[8:12]))
    y := int32(binary.BigEndian.Uint32(data[12:16]))
    button := data[16]

    // 注入鼠标点击事件
    return e.desktop.InjectMouseButtonEvent(x, y, button, true)
}
```

**系统命令处理：**

```go
// 处理系统命令
func (e *WebRTCEngine) handleSystemCommand(data []byte) error {
    if len(data) < 16 {
        return fmt.Errorf("system command message too short")
    }

    commandID := binary.BigEndian.Uint32(data[8:12])
    commandType := data[12]

    var success bool
    var err error

    switch commandType {
    case CommandTypeTriggerSAS:
        err = e.desktop.InjectKeyEvent(0x2E, true, true, true) // Ctrl+Alt+Del
        success = err == nil
    case CommandTypeOpenTaskManager:
        err = e.desktop.InjectKeyEvent(0x1B, true, true, false) // Ctrl+Shift+Esc
        success = err == nil
    case CommandTypeLockScreen:
        err = e.lockScreen()
        success = err == nil
    default:
        return fmt.Errorf("unknown command type: %d", commandType)
    }

    // 发送命令响应
    return e.sendCommandResponse(commandID, success)
}

// 发送命令响应
func (e *WebRTCEngine) sendCommandResponse(commandID uint32, success bool) error {
    response := make([]byte, 16)
    response[0] = ProtocolVersion
    response[1] = MessageTypeCommandResult
    binary.BigEndian.PutUint32(response[4:8], uint32(time.Now().UnixMilli()))
    binary.BigEndian.PutUint32(response[8:12], commandID)
    if success {
        response[12] = 0x01
    } else {
        response[12] = 0x00
    }

    return e.controlChannel.Send(response)
}
```

#### 4.2.14 完整交互时序

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Agent ↔ Client 完整交互时序                        │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐                    ┌─────────────┐
│   Client    │                    │    Agent    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. WebRTC 连接建立              │
       │  (SDP 协商 + ICE)                │
       │◄═════════════════════════════════►│
       │                                  │
       │  2. DataChannel 创建             │
       │  (control, input, clipboard, heartbeat)
       │◄═════════════════════════════════►│
       │                                  │
       │  3. 输入事件                      │
       │  KEY_DOWN (0x01)                 │
       │─────────────────────────────────►│
       │                                  │
       │  4. 鼠标事件                      │
       │  MOUSE_MOVE (0x03)               │
       │─────────────────────────────────►│
       │                                  │
       │  5. 系统命令                      │
       │  SYSTEM_COMMAND (0x10)           │
       │─────────────────────────────────►│
       │                                  │
       │  6. 命令响应                      │
       │  COMMAND_RESULT (0x11)           │
       │◄─────────────────────────────────│
       │                                  │
       │  7. 剪贴板同步                    │
       │  CLIPBOARD_UPDATE (0x20)         │
       │─────────────────────────────────►│
       │                                  │
       │  8. 剪贴板同步                    │
       │  CLIPBOARD_UPDATE (0x20)         │
       │◄─────────────────────────────────│
       │                                  │
       │  9. 心跳保活                      │
       │  HEARTBEAT (0x30)                │
       │─────────────────────────────────►│
       │                                  │
       │  10. 心跳响应                     │
       │  HEARTBEAT_ACK (0x31)            │
       │◄─────────────────────────────────│
       │                                  │
       │  11. 媒体控制                     │
       │  MEDIA_CONTROL (0x40)            │
       │─────────────────────────────────►│
       │                                  │
```

### 4.3 消息格式定义

本节定义通信协议的版本管理、序列化规范、消息验证、消息压缩和扩展机制。

#### 4.3.1 协议版本规范

**版本号定义：**

```
主版本号.次版本号

示例：1.0
```

| 版本号 | 含义 | 兼容性 |
|--------|------|--------|
| 主版本号 | 不兼容的协议变更 | 需要双方升级 |
| 次版本号 | 向后兼容的功能新增 | 新版本兼容旧版本 |

**当前协议版本：**

| 协议 | 版本 | 说明 |
|------|------|------|
| Agent ↔ Broker REST | 1.0 | HTTP REST，JSON 格式 |
| Agent ↔ Client DataChannel | 1.0 | WebRTC DataChannel，二进制格式 |

**版本兼容性矩阵：**

| Agent 版本 | Client 版本 | 兼容性 |
|------------|-------------|--------|
| 1.0 | 1.0 | ✅ 完全兼容 |
| 1.0 | 1.1 | ✅ 向后兼容 |
| 1.1 | 1.0 | ✅ 向后兼容 |
| 2.0 | 1.0 | ❌ 不兼容 |

#### 4.3.2 版本协商机制

**协商流程：**

```
┌─────────────┐                    ┌─────────────┐
│   Client    │                    │    Agent    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. WebRTC 连接建立              │
       │◄═════════════════════════════════►│
       │                                  │
       │  2. DataChannel 创建             │
       │  (control)                       │
       │◄═════════════════════════════════►│
       │                                  │
       │  3. 版本协商请求                  │
       │  VERSION_NEGOTIATE (0xFF)        │
       │─────────────────────────────────►│
       │  {                               │
       │    "clientVersion": "1.0",       │
       │    "supportedVersions": ["1.0", "1.1"]
       │  }                               │
       │                                  │
       │  4. 版本协商响应                  │
       │  VERSION_NEGOTIATE_ACK (0xFE)    │
       │◄─────────────────────────────────│
       │  {                               │
       │    "agentVersion": "1.0",        │
       │    "selectedVersion": "1.0",     │
       │    "status": "OK"                │
       │  }                               │
       │                                  │
       │  5. 开始正常通信                  │
       │                                  │
```

**版本协商消息格式：**

```json
// 版本协商请求
{
  "clientVersion": "1.0",
  "supportedVersions": ["1.0", "1.1"],
  "features": ["keyboard", "mouse", "touch", "clipboard"]
}

// 版本协商响应
{
  "agentVersion": "1.0",
  "selectedVersion": "1.0",
  "status": "OK",
  "features": ["keyboard", "mouse", "touch", "clipboard"]
}
```

**状态码定义：**

| status | 说明 |
|--------|------|
| `OK` | 协商成功 |
| `VERSION_MISMATCH` | 无兼容版本 |
| `FEATURE_NOT_SUPPORTED` | 请求的功能不支持 |

#### 4.3.3 向后兼容性

**兼容性规则：**

| 变更类型 | 兼容性 | 处理方式 |
|----------|--------|----------|
| 新增消息类型 | ✅ 兼容 | 旧版本忽略未知消息类型 |
| 新增消息字段 | ✅ 兼容 | 旧版本忽略未知字段 |
| 删除消息类型 | ❌ 不兼容 | 需要主版本号升级 |
| 修改消息格式 | ❌ 不兼容 | 需要主版本号升级 |
| 修改字段含义 | ❌ 不兼容 | 需要主版本号升级 |

**未知消息处理：**

```go
// 处理未知消息类型（JSON 格式）
func (e *WebRTCEngine) handleUnknownMessage(msg ControlMessage) {
    log.Printf("Unknown message type: %s, version: %d, seq: %d", msg.Type, msg.V, msg.Seq)
    
    // 忽略未知消息，不报错
    // 这保证了向后兼容性
}
```

#### 4.3.4 JSON 序列化规范

**REST API JSON 规范：**

| 规范项 | 定义 | 示例 |
|--------|------|------|
| 字段命名 | camelCase | `desktopId`, `agentVersion` |
| 时间格式 | ISO 8601 UTC | `"2024-01-01T08:00:00Z"` |
| ID 格式 | UUID v4 | `"550e8400-e29b-41d4-a716-446655440000"` |
| 枚举值 | UPPER_SNAKE_CASE | `"SESSION_CONNECTED"` |
| 空值 | null | `"data": null` |
| 数组 | 空数组 | `"items": []` |

**JSON 示例：**

```json
{
  "desktopId": "desktop_001",
  "agentVersion": "1.0.0",
  "timestamp": "2024-01-01T08:00:00Z",
  "ready": {
    "agent": true,
    "desktopService": true,
    "captureService": true,
    "loginReady": true
  },
  "metrics": [
    {
      "name": "cpu_usage",
      "value": 25.5,
      "unit": "%"
    }
  ]
}
```

**JSON 序列化代码：**

```go
// JSON 序列化
func toJSON(v interface{}) ([]byte, error) {
    return json.Marshal(v)
}

// JSON 反序列化
func fromJSON(data []byte, v interface{}) error {
    return json.Unmarshal(data, v)
}
```

#### 4.3.5 JSON 序列化工具函数

**注意**：本协议统一使用 JSON 格式，不再使用二进制序列化。

**消息序列化辅助函数：**

```go
// 使用标准库 json 进行序列化
func serialize(msg ControlMessage) ([]byte, error) {
    return json.Marshal(msg)
}

// 使用标准库 json 进行反序列化
func deserialize(data []byte, msg *ControlMessage) error {
    return json.Unmarshal(data, msg)
}

// payload 字段提取辅助函数
func getIntPayload(payload map[string]interface{}, key string, defaultValue int) int {
    if val, ok := payload[key]; ok {
        if num, ok := val.(float64); ok {
            return int(num)
        }
    }
    return defaultValue
}

func getStringPayload(payload map[string]interface{}, key string, defaultValue string) string {
    if val, ok := payload[key]; ok {
        if str, ok := val.(string); ok {
            return str
        }
    }
    return defaultValue
}

func getBoolPayload(payload map[string]interface{}, key string, defaultValue bool) bool {
    if val, ok := payload[key]; ok {
        if b, ok := val.(bool); ok {
            return b
        }
    }
    return defaultValue
}
```

**消息发送示例：**

```go
// 发送鼠标移动消息
func (e *WebRTCEngine) sendMouseMove(x, y int, displayID int) error {
    payload := map[string]interface{}{
        "x":          x,
        "y":          y,
        "display_id": displayID,
    }
    return e.sendControlMessage("input.mouse_move", payload, "control")
}

// 发送键盘按键消息
func (e *WebRTCEngine) sendKey(code string, action string, modifiers []string) error {
    payload := map[string]interface{}{
        "code":      code,
        "action":    action,
        "modifiers": modifiers,
    }
    return e.sendControlMessage("input.key", payload, "control")
}

// 发送剪贴板推送消息
func (e *WebRTCEngine) sendClipboard(formats []string, data map[string]string) error {
    payload := map[string]interface{}{
        "formats": formats,
        "data":    data,
    }
    return e.sendControlMessage("clipboard.push", payload, "bulk")
}
```

#### 4.3.6 JSON 序列化优势说明

| 特性 | JSON | 二进制 |
|------|------|--------|
| 可读性 | ✅ 易于调试 | ❌ 需要工具解析 |
| 扩展性 | ✅ 新字段自动忽略 | ❌ 需要协议更新 |
| 字符串 | ✅ Unicode 原生支持 | ❌ 需要长度前缀 |
| 嵌套结构 | ✅ 原生支持 | ❌ 需要手动序列化 |
| 网络带宽 | ⚠️ 较大 | ✅ 更小 |
| 解析速度 | ⚠️ 慢 10-20% | ✅ 更快 |

**采用 JSON 的理由：**
1. **可调试性强**：远程桌面场景需要快速定位网络层输入异常
2. **版本兼容性**：`v` 版本字段允许协议演进，新字段可默认忽略
3. **开发效率高**：前端和后端使用相同的 JSON 结构
4. **性能可接受**：控制消息流量小，JSON 额外开销影响可忽略

```go

```
消息格式（带校验）：
┌──────────┬──────────┬──────────┬──────────┐
│  Header  │  Body    │  CRC32   │  Length   │
│  (8B)    │  (NB)    │  (4B)    │  (4B)    │
└──────────┴──────────┴──────────┴──────────┘
```

**校验码计算：**

```go
// 计算 CRC32 校验码
func calculateCRC32(data []byte) uint32 {
    return crc32.ChecksumIEEE(data)
}

// 验证消息完整性
func verifyMessage integrity(data []byte) bool {
    if len(data) < 16 {
        return false
    }
    
    // 提取校验码
    expectedCRC := binary.BigEndian.Uint32(data[len(data)-8 : len(data)-4])
    
    // 计算校验码
    actualCRC := calculateCRC32(data[:len(data)-8])
    
    return expectedCRC == actualCRC
}
```

**消息长度验证：**

```go
// 验证消息长度
func verifyMessageLength(data []byte) bool {
    if len(data) < 16 {
        return false
    }
    
    // 提取长度字段
    expectedLength := binary.BigEndian.Uint32(data[len(data)-4:])
    
    // 验证长度
    return uint32(len(data)) == expectedLength
}
```

#### 4.3.8 消息合法性验证

**字段范围检查：**

```go
// 验证坐标范围
func validateCoordinates(x, y int32, maxWidth, maxHeight int32) bool {
    return x >= 0 && x <= maxWidth && y >= 0 && y <= maxHeight
}

// 验证按键码范围
func validateKeyCode(keyCode uint16) bool {
    return keyCode <= 0xFF
}

// 验证按钮范围
func validateMouseButton(button uint8) bool {
    return button >= 0 && button <= 3
}

// 验证压力范围
func validatePressure(pressure float32) bool {
    return pressure >= 0.0 && pressure <= 1.0
}
```

**类型检查：**

```go
// 验证消息类型（JSON 字符串匹配）
func validateMessageType(msgType string) bool {
    // 动态消息类型字符串（遵循 Client 文档 §7.3 定义）
    validTypes := map[string]bool{
        // 输入事件类型
        "input.mouse_move":   true,
        "input.mouse_button": true,
        "input.mouse_wheel":  true,
        "input.key":          true,
        "input.touch":        true,
        // 剪贴板类型
        "clipboard.push":     true,
        "clipboard.request":  true,
        // 控制类型
        "ctrl.ping":          true,
        "ctrl.pong":          true,
        "ctrl.resize":        true,
    }
    if _, ok := validTypes[msgType]; ok {
        return true
    }
    // 未知类型记录日志但不拒绝（支持向后兼容）
    log.Printf("Unknown message type: %s", msgType)
    return true
}
```

#### 4.3.9 消息去重机制

**基于消息 ID 去重：**

```go
type MessageDeduplicator struct {
    seenIDs sync.Map
    ttl     time.Duration
}

// 创建去重器
func NewMessageDeduplicator(ttl time.Duration) *MessageDeduplicator {
    d := &MessageDeduplicator{
        ttl: ttl,
    }
    
    // 启动清理协程
    go d.cleanup()
    
    return d
}

// 检查消息是否重复
func (d *MessageDeduplicator) IsDuplicate(messageID string) bool {
    if _, loaded := d.seenIDs.LoadOrStore(messageID, time.Now()); loaded {
        return true
    }
    return false
}

// 清理过期消息 ID
func (d *MessageDeduplicator) cleanup() {
    ticker := time.NewTicker(d.ttl)
    defer ticker.Stop()
    
    for range ticker.C {
        now := time.Now()
        d.seenIDs.Range(func(key, value interface{}) bool {
            timestamp := value.(time.Time)
            if now.Sub(timestamp) > d.ttl {
                d.seenIDs.Delete(key)
            }
            return true
        })
    }
}
```

**使用示例：**

```go
// 创建去重器（TTL 5 分钟）
deduplicator := NewMessageDeduplicator(5 * time.Minute)

// 处理消息
func (e *WebRTCEngine) handleMessageWithDedup(jsonData []byte) error {
    // 反序列化 JSON 消息
    var msg ControlMessage
    if err := json.Unmarshal(jsonData, &msg); err != nil {
        return err
    }
    
    // 提取消息 ID（使用类型 + 序列号作为消息 ID）
    messageID := fmt.Sprintf("%s_%d", msg.Type, msg.Seq)
    
    // 检查是否重复
    if deduplicator.IsDuplicate(messageID) {
        log.Printf("Duplicate message ignored: %s", messageID)
        return nil
    }
    
    // 处理消息
    return e.handleMessage(jsonData)
}
```

#### 4.3.10 消息压缩

**压缩算法选择：**

| 算法 | 压缩率 | 速度 | 适用场景 |
|------|--------|------|----------|
| gzip | 高 | 慢 | 大消息、网络带宽受限 |
| snappy | 中 | 快 | 实时性要求高 |
| lz4 | 中 | 最快 | 实时性要求最高 |

**推荐：snappy**（平衡压缩率和速度）

**压缩策略：**

| 消息大小 | 处理方式 |
|----------|----------|
| < 1 KB | 不压缩 |
| 1 KB - 10 KB | 可选压缩 |
| > 10 KB | 强制压缩 |

**压缩代码：**

```go
import "github.com/golang/snappy"

// 压缩消息
func compressMessage(data []byte) []byte {
    // 小消息不压缩
    if len(data) < 1024 {
        return data
    }
    
    // 使用 snappy 压缩
    compressed := snappy.Encode(nil, data)
    
    // 添加压缩标记
    result := make([]byte, 1+len(compressed))
    result[0] = 0x01 // 压缩标记
    copy(result[1:], compressed)
    
    return result
}

// 解压消息
func decompressMessage(data []byte) ([]byte, error) {
    if len(data) < 1 {
        return nil, fmt.Errorf("invalid compressed message")
    }
    
    // 检查压缩标记
    if data[0] == 0x00 {
        // 未压缩
        return data[1:], nil
    }
    
    // 解压
    decompressed, err := snappy.Decode(nil, data[1:])
    if err != nil {
        return nil, err
    }
    
    return decompressed, nil
}
```

**压缩级别配置：**

```go
type CompressionConfig struct {
    Enabled   bool
    Algorithm string  // "gzip", "snappy", "lz4"
    Level     int     // 压缩级别
    Threshold int     // 压缩阈值（字节）
}

var defaultCompressionConfig = CompressionConfig{
    Enabled:   true,
    Algorithm: "snappy",
    Level:     1,
    Threshold: 1024,
}
```

#### 4.3.11 自定义消息类型

**消息类型空间分配：**

```
0x00 - 0x0F: 系统消息
0x10 - 0x1F: 系统命令
0x20 - 0x2F: 剪贴板消息
0x30 - 0x3F: 心跳消息
0x40 - 0x4F: 媒体控制消息
0x50 - 0xAF: 保留（未来扩展）
0xB0 - 0xEF: 自定义消息
0xF0 - 0xFF: 协议控制消息
```

**自定义消息类型注册：**

```go
type CustomMessageHandler func(data []byte) error

type MessageRegistry struct {
    handlers map[uint8]CustomMessageHandler
}

// 创建消息注册表
func NewMessageRegistry() *MessageRegistry {
    return &MessageRegistry{
        handlers: make(map[uint8]CustomMessageHandler),
    }
}

// 注册自定义消息处理器
func (r *MessageRegistry) RegisterHandler(msgType uint8, handler CustomMessageHandler) error {
    // 检查是否为保留类型
    if msgType < 0xB0 || msgType > 0xEF {
        return fmt.Errorf("message type %d is reserved", msgType)
    }
    
    r.handlers[msgType] = handler
    return nil
}

// 获取消息处理器
func (r *MessageRegistry) GetHandler(msgType uint8) (CustomMessageHandler, bool) {
    handler, ok := r.handlers[msgType]
    return handler, ok
}
```

#### 4.3.12 扩展字段

**消息体中的扩展字段：**

```json
{
  "type": "SYSTEM_COMMAND",
  "timestamp": 1700000000000,
  "commandType": "LOCK_SCREEN",
  "extensions": {
    "source": "client",
    "reason": "user_request",
    "metadata": {
      "userId": "user_123",
      "sessionId": "sess_xyz"
    }
  }
}
```

**扩展字段处理：**

```go
// 处理扩展字段
func (e *WebRTCEngine) handleExtensions(extensions map[string]interface{}) {
    // 忽略未知扩展字段（向后兼容）
    for key, value := range extensions {
        switch key {
        case "source":
            // 处理来源
            if source, ok := value.(string); ok {
                log.Printf("Message source: %s", source)
            }
        case "reason":
            // 处理原因
            if reason, ok := value.(string); ok {
                log.Printf("Message reason: %s", reason)
            }
        default:
            // 忽略未知扩展字段
            log.Printf("Unknown extension field: %s", key)
        }
    }
}
```

#### 4.3.13 插件机制

**消息处理插件接口：**

```go
// 消息处理插件接口
type MessagePlugin interface {
    Name() string
    OnMessage(msgType uint8, data []byte) error
    OnError(err error)
}

// 插件管理器
type PluginManager struct {
    plugins []MessagePlugin
}

// 创建插件管理器
func NewPluginManager() *PluginManager {
    return &PluginManager{
        plugins: make([]MessagePlugin, 0),
    }
}

// 注册插件
func (m *PluginManager) RegisterPlugin(plugin MessagePlugin) {
    m.plugins = append(m.plugins, plugin)
}

// 处理消息（经过所有插件）
func (m *PluginManager) ProcessMessage(msgType uint8, data []byte) error {
    for _, plugin := range m.plugins {
        if err := plugin.OnMessage(msgType, data); err != nil {
            plugin.OnError(err)
            return err
        }
    }
    return nil
}
```

**日志插件示例：**

```go
type LogPlugin struct {
    logger *log.Logger
}

func (p *LogPlugin) Name() string {
    return "log"
}

func (p *LogPlugin) OnMessage(msgType uint8, data []byte) error {
    p.logger.Printf("Message received: type=%d, size=%d", msgType, len(data))
    return nil
}

func (p *LogPlugin) OnError(err error) {
    p.logger.Printf("Error: %v", err)
}

// 使用示例
pluginManager := NewPluginManager()
pluginManager.RegisterPlugin(&LogPlugin{
    logger: log.Default(),
})
```

---

## 5. 安全设计

本章定义 Agent 的安全设计，包括身份认证、访问控制、传输安全、进程保护和安全审计。

### 5.1 身份认证

#### 5.1.1 Agent 认证

**Agent 注册认证：**

```
┌─────────────┐                    ┌─────────────┐
│    Agent    │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. 注册请求                     │
       │  POST /api/v1/agent/register     │
       │  {                               │
       │    "desktopId": "desktop_001",   │
       │    "agentVersion": "1.0.0",      │
       │    "hostname": "desktop-001-pod",│
       │    "ip": "10.100.1.5"            │
       │  }                               │
       │─────────────────────────────────►│
       │                                  │
       │                                  │  2. 验证 desktopId
       │                                  │  3. 检查桌面状态
       │                                  │  4. 签发 Agent Token
       │                                  │
       │  5. 返回 Agent Token             │
       │  {                               │
       │    "agentToken": "agent_xxx"     │
       │  }                               │
       │◄─────────────────────────────────│
       │                                  │
```

**Agent Token 结构：**

```
agent_{desktop_id}_{random}

示例：agent_desktop_001_abc123xyz
```

**Agent Token 特性：**

| 特性 | 说明 |
|------|------|
| 签发方 | Broker |
| 有效期 | 与 Agent 生命周期绑定 |
| 刷新 | Agent 重启后重新注册获取新 Token |
| 撤销 | Agent 注销或桌面销毁时撤销 |

#### 5.1.2 Client 认证

**Client Session Token 认证：**

```
┌─────────────┐          ┌─────────────┐          ┌─────────────┐
│   Client    │          │   Broker    │          │    Agent    │
└──────┬──────┘          └──────┬──────┘          └──────┬──────┘
       │                        │                        │
       │  1. 创建 Session       │                        │
       │  POST /api/v1/sessions │                        │
       │───────────────────────►│                        │
       │                        │                        │
       │  2. 返回 Session Token │                        │
       │  + signalUrl           │                        │
       │◄───────────────────────│                        │
       │                        │                        │
       │  3. WebSocket 连接     │                        │
       │  ws://broker/signal?token=<sessionToken>        │
       │───────────────────────►│                        │
       │                        │                        │
       │                        │  4. 验证 Session Token │
       │                        │  5. SDP/ICE 交换       │
       │                        │◄═══════════════════════│
       │                        │                        │
       │  6. WebRTC 连接建立    │                        │
       │◄═══════════════════════════════════════════════►│
       │                        │                        │
```

**Session Token 结构（JWT）：**

```json
{
  "sub": "user_123",
  "session_id": "sess_xyz",
  "desktop_id": "desktop_001",
  "tenant_id": "tenant_abc",
  "iat": 1700000000,
  "exp": 1700086400
}
```

**Session Token 特性：**

| 特性 | 说明 |
|------|------|
| 签发方 | Broker |
| 有效期 | 与 Session 生命周期绑定 |
| 刷新 | 不刷新，Session 结束后重新创建 |
| 撤销 | Session 关闭时撤销 |

#### 5.1.3 Token 管理

**Token 签发：**

```go
// 签发 Agent Token
func (b *Broker) IssueAgentToken(desktopID string) (string, error) {
    // 生成随机字符串
    random := generateRandomString(16)
    
    // 构造 Token
    token := fmt.Sprintf("agent_%s_%s", desktopID, random)
    
    // 存储 Token（Redis）
    err := b.redis.Set(ctx, "agent_token:"+desktopID, token, 0).Err()
    if err != nil {
        return "", err
    }
    
    return token, nil
}

// 签发 Session Token（JWT）
func (b *Broker) IssueSessionToken(userID, sessionID, desktopID, tenantID string) (string, error) {
    // 构造 Claims
    claims := jwt.MapClaims{
        "sub":        userID,
        "session_id": sessionID,
        "desktop_id": desktopID,
        "tenant_id":  tenantID,
        "iat":        time.Now().Unix(),
        "exp":        time.Now().Add(24 * time.Hour).Unix(),
    }
    
    // 签发 JWT
    token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
    tokenString, err := token.SignedString(b.privateKey)
    if err != nil {
        return "", err
    }
    
    return tokenString, nil
}
```

**Token 验证：**

```go
// 验证 Agent Token
func (b *Broker) ValidateAgentToken(desktopID, token string) bool {
    // 从 Redis 获取存储的 Token
    storedToken, err := b.redis.Get(ctx, "agent_token:"+desktopID).Result()
    if err != nil {
        return false
    }
    
    return token == storedToken
}

// 验证 Session Token（JWT）
func (b *Broker) ValidateSessionToken(tokenString string) (*SessionClaims, error) {
    // 解析 JWT
    token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
        // 验证签名算法
        if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
        }
        return b.publicKey, nil
    })
    if err != nil {
        return nil, err
    }
    
    // 提取 Claims
    if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
        return &SessionClaims{
            UserID:    claims["sub"].(string),
            SessionID: claims["session_id"].(string),
            DesktopID: claims["desktop_id"].(string),
            TenantID:  claims["tenant_id"].(string),
        }, nil
    }
    
    return nil, fmt.Errorf("invalid token")
}
```

**Token 撤销：**

```go
// 撤销 Agent Token
func (b *Broker) RevokeAgentToken(desktopID string) error {
    return b.redis.Del(ctx, "agent_token:"+desktopID).Err()
}

// 撤销 Session Token
func (b *Broker) RevokeSessionToken(sessionID string) error {
    // 将 Token 加入黑名单
    return b.redis.Set(ctx, "token_blacklist:"+sessionID, 1, 24*time.Hour).Err()
}
```

#### 5.1.4 多因素认证（可选）

**MFA 支持：**

| 认证方式 | 说明 | 适用场景 |
|----------|------|----------|
| TOTP | 基于时间的一次性密码 | 移动端 App |
| SMS | 短信验证码 | 手机号绑定 |
| Email | 邮件验证码 | 邮箱绑定 |
| Hardware Token | 硬件令牌 | 高安全场景 |

**MFA 流程：**

```
┌─────────────┐                    ┌─────────────┐
│   Client    │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. 登录请求                     │
       │  POST /api/v1/auth/login         │
       │  { username, password }          │
       │─────────────────────────────────►│
       │                                  │
       │  2. 返回 MFA 要求                │
       │  { "mfaRequired": true,          │
       │    "mfaType": "totp" }           │
       │◄─────────────────────────────────│
       │                                  │
       │  3. 提交 MFA 验证码              │
       │  POST /api/v1/auth/mfa           │
       │  { "code": "123456" }            │
       │─────────────────────────────────►│
       │                                  │
       │  4. 返回 Access Token            │
       │  { "accessToken": "xxx" }        │
       │◄─────────────────────────────────│
       │                                  │
```

### 5.2 访问控制

#### 5.2.1 Agent 访问控制

**Agent API 权限矩阵：**

| API | Agent Token | Session Token | 说明 |
|-----|-------------|---------------|------|
| POST /agent/register | ❌ | ❌ | 无需认证 |
| POST /agent/heartbeat | ✅ | ❌ | Agent Token |
| POST /agent/session-event | ✅ | ❌ | Agent Token |
| POST /agent/monitor-data | ✅ | ❌ | Agent Token |
| POST /agent/command-result | ✅ | ❌ | Agent Token |
| POST /agent/error | ✅ | ❌ | Agent Token |

**访问控制代码：**

```go
// Agent Token 中间件
func AgentAuthMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 排除注册接口
        if r.URL.Path == "/api/v1/agent/register" {
            next.ServeHTTP(w, r)
            return
        }
        
        // 提取 Token
        token := extractBearerToken(r)
        if token == "" {
            http.Error(w, "Missing token", http.StatusUnauthorized)
            return
        }
        
        // 验证 Token
        if !validateAgentToken(token) {
            http.Error(w, "Invalid token", http.StatusUnauthorized)
            return
        }
        
        next.ServeHTTP(w, r)
    })
}
```

#### 5.2.2 Client 访问控制

**Client 输入事件权限：**

| 事件类型 | 权限要求 | 说明 |
|----------|----------|------|
| 键盘事件 | Session Token | 需要活跃 Session |
| 鼠标事件 | Session Token | 需要活跃 Session |
| 触控事件 | Session Token | 需要活跃 Session |
| 手势事件 | Session Token | 需要活跃 Session |
| 系统命令 | Session Token + 权限检查 | 需要特殊权限 |
| 剪贴板 | Session Token + 策略检查 | 受剪贴板策略控制 |

**访问控制代码：**

```go
// 检查输入事件权限
func (e *WebRTCEngine) checkInputPermission(eventType uint8) error {
    // 检查 Session 是否存在
    if e.session == nil {
        return fmt.Errorf("no active session")
    }
    
    // 检查 Session 状态
    if e.session.State != SessionStateActive {
        return fmt.Errorf("session is not active")
    }
    
    return nil
}

// 检查系统命令权限
func (e *WebRTCEngine) checkCommandPermission(commandType uint8) error {
    // 检查基础权限
    if err := e.checkInputPermission(MessageTypeSystemCommand); err != nil {
        return err
    }
    
    // 检查命令特定权限
    switch commandType {
    case CommandTypeTriggerSAS:
        // 需要管理员权限或特定策略允许
        if !e.policy.AllowTriggerSAS {
            return fmt.Errorf("permission denied: TRIGGER_SAS")
        }
    case CommandTypeOpenTaskManager:
        // 需要管理员权限或特定策略允许
        if !e.policy.AllowOpenTaskManager {
            return fmt.Errorf("permission denied: OPEN_TASK_MANAGER")
        }
    case CommandTypeLockScreen:
        // 允许所有用户
        break
    }
    
    return nil
}
```

#### 5.2.3 命令权限控制

**命令权限矩阵：**

| 命令 | 用户 | 管理员 | 说明 |
|------|------|--------|------|
| LOCK_SCREEN | ✅ | ✅ | 所有用户可执行 |
| UNLOCK_SCREEN | ❌ | ✅ | 仅管理员 |
| LOGOFF | ❌ | ✅ | 仅管理员 |
| REBOOT | ❌ | ✅ | 仅管理员 |
| TRIGGER_SAS | ❌ | ✅ | 仅管理员（策略控制） |
| OPEN_TASK_MANAGER | ❌ | ✅ | 仅管理员（策略控制） |

**命令权限检查代码：**

```go
// 检查命令权限
func (e *WebRTCEngine) checkCommandAuthorization(commandType uint8, userRole string) error {
    // 管理员拥有所有权限
    if userRole == "admin" || userRole == "tenant_admin" {
        return nil
    }
    
    // 普通用户权限检查
    switch commandType {
    case CommandTypeLockScreen:
        return nil // 允许
    default:
        return fmt.Errorf("permission denied for command type: %d", commandType)
    }
}
```

#### 5.2.4 剪贴板权限控制

**剪贴板策略：**

| 策略 | 说明 | 限制 |
|------|------|------|
| `disabled` | 禁用剪贴板 | 不允许任何剪贴板操作 |
| `readonly` | 只读 | 只允许从 Client 复制到 Agent |
| `writeonly` | 只写 | 只允许从 Agent 复制到 Client |
| `readwrite` | 读写 | 允许双向复制 |

**剪贴板权限检查代码：**

```go
// 检查剪贴板权限
func (e *WebRTCEngine) checkClipboardPermission(direction string) error {
    switch e.policy.ClipboardPolicy {
    case "disabled":
        return fmt.Errorf("clipboard is disabled")
    case "readonly":
        if direction == "agent_to_client" {
            return fmt.Errorf("clipboard read is not allowed")
        }
    case "writeonly":
        if direction == "client_to_agent" {
            return fmt.Errorf("clipboard write is not allowed")
        }
    case "readwrite":
        // 允许所有操作
    }
    
    return nil
}
```

### 5.3 传输安全

#### 5.3.1 WebRTC DTLS

**DTLS 加密：**

```
┌─────────────┐                    ┌─────────────┐
│   Client    │                    │    Agent    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. DTLS 握手                    │
       │  (证书交换、密钥协商)            │
       │◄═════════════════════════════════►│
       │                                  │
       │  2. 加密数据传输                 │
       │  (DataChannel + DTLS)            │
       │◄═════════════════════════════════►│
       │                                  │
```

**DTLS 特性：**

| 特性 | 说明 |
|------|------|
| 加密算法 | AES-128/256-GCM |
| 密钥交换 | ECDHE |
| 完整性 | SHA-256 |
| 前向安全 | ✅ 支持 |

**Pion DTLS 配置：**

```go
// DTLS 配置
dtlsConfig := &dtls.Config{
    Certificate:    certificate,
    PrivateKey:     privateKey,
    CipherSuites:   []dtls.CipherSuiteID{
        dtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
        dtls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
    },
    ExtendedMasterSecret: dtls.ExtendedMasterSecretRequire,
}

// WebRTC SettingEngine 配置
settingsEngine := webrtc.SettingEngine{}
settingsEngine.SetDTLSRetransmissionInterval(dtlsRetransmissionInterval)
```

#### 5.3.2 HTTPS

**Agent ↔ Broker HTTPS：**

```
┌─────────────┐                    ┌─────────────┐
│    Agent    │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. TLS 握手                     │
       │  (证书验证、密钥协商)            │
       │◄═════════════════════════════════►│
       │                                  │
       │  2. 加密 HTTP 请求               │
       │  POST /api/v1/agent/heartbeat    │
       │  Authorization: Bearer <token>   │
       │─────────────────────────────────►│
       │                                  │
       │  3. 加密 HTTP 响应               │
       │◄─────────────────────────────────│
       │                                  │
```

**HTTPS 配置：**

```go
// HTTP Client 配置
httpClient := &http.Client{
    Transport: &http.Transport{
        TLSClientConfig: &tls.Config{
            MinVersion: tls.VersionTLS12,
            MaxVersion: tls.VersionTLS13,
            CipherSuites: []uint16{
                tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
                tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
            },
        },
    },
    Timeout: 30 * time.Second,
}
```

#### 5.3.3 证书管理

**证书类型：**

| 证书类型 | 用途 | 有效期 |
|----------|------|--------|
| Broker TLS 证书 | HTTPS 服务 | 90 天 |
| Agent TLS 证书 | Agent ↔ Broker | 90 天 |
| WebRTC 证书 | DTLS 加密 | 自动生成 |

**证书轮换流程：**

```
┌─────────────┐                    ┌─────────────┐
│    Agent    │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. 定期检查证书有效期           │
       │  (每 24 小时)                    │
       │                                  │
       │  2. 证书即将过期                 │
       │  (剩余 < 30 天)                  │
       │                                  │
       │  3. 请求新证书                   │
       │  POST /api/v1/agent/cert         │
       │─────────────────────────────────►│
       │                                  │
       │  4. 签发新证书                   │
       │◄─────────────────────────────────│
       │                                  │
       │  5. 更新本地证书                 │
       │                                  │
       │  6. 使用新证书                   │
       │                                  │
```

**证书管理代码：**

```go
type CertificateManager struct {
    certPath    string
    keyPath     string
    certificate *tls.Certificate
    mu          sync.RWMutex
}

// 创建证书管理器
func NewCertificateManager(certPath, keyPath string) *CertificateManager {
    return &CertificateManager{
        certPath: certPath,
        keyPath:  keyPath,
    }
}

// 加载证书
func (m *CertificateManager) LoadCertificate() error {
    cert, err := tls.LoadX509KeyPair(m.certPath, m.keyPath)
    if err != nil {
        return err
    }
    
    m.mu.Lock()
    m.certificate = &cert
    m.mu.Unlock()
    
    return nil
}

// 获取证书
func (m *CertificateManager) GetCertificate() *tls.Certificate {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.certificate
}

// 检查证书是否即将过期
func (m *CertificateManager) IsExpiringSoon(days int) bool {
    m.mu.RLock()
    defer m.mu.RUnlock()
    
    if m.certificate == nil {
        return true
    }
    
    // 解析证书
    x509Cert, err := x509.ParseCertificate(m.certificate.Certificate[0])
    if err != nil {
        return true
    }
    
    // 检查过期时间
    expiry := x509Cert.NotAfter
    threshold := time.Now().AddDate(0, 0, days)
    
    return expiry.Before(threshold)
}

// 启动证书轮换检查
func (m *CertificateManager) StartRotationCheck() {
    go func() {
        ticker := time.NewTicker(24 * time.Hour)
        defer ticker.Stop()
        
        for range ticker.C {
            if m.IsExpiringSoon(30) {
                log.Printf("Certificate expiring soon, requesting new certificate")
                m.requestNewCertificate()
            }
        }
    }()
}
```

#### 5.3.4 密钥管理

**密钥类型：**

| 密钥类型 | 用途 | 存储位置 |
|----------|------|----------|
| TLS 私钥 | HTTPS/DTLS 加密 | 文件系统（加密） |
| JWT 私钥 | Token 签发 | Broker 内存 |
| JWT 公钥 | Token 验证 | Agent（从 Broker 获取） |

**密钥保护：**

```go
// 密钥存储
type KeyStore struct {
    keys map[string][]byte
    mu   sync.RWMutex
}

// 创建密钥存储
func NewKeyStore() *KeyStore {
    return &KeyStore{
        keys: make(map[string][]byte),
    }
}

// 存储密钥
func (s *KeyStore) StoreKey(name string, key []byte) error {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    // 加密密钥
    encryptedKey, err := encryptKey(key)
    if err != nil {
        return err
    }
    
    s.keys[name] = encryptedKey
    return nil
}

// 获取密钥
func (s *KeyStore) GetKey(name string) ([]byte, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()
    
    encryptedKey, ok := s.keys[name]
    if !ok {
        return nil, fmt.Errorf("key not found: %s", name)
    }
    
    // 解密密钥
    key, err := decryptKey(encryptedKey)
    if err != nil {
        return nil, err
    }
    
    return key, nil
}

// 加密密钥
func encryptKey(key []byte) ([]byte, error) {
    // 使用 AES-GCM 加密
    // ...
}

// 解密密钥
func decryptKey(encryptedKey []byte) ([]byte, error) {
    // 使用 AES-GCM 解密
    // ...
}
```

### 5.4 进程保护

#### 5.4.1 防卸载机制（已在 3.5 节定义）

**防卸载机制概要：**

| 机制 | 说明 |
|------|------|
| 文件保护 | 设置文件不可删除、不可修改属性 |
| 进程保护 | 屏蔽危险信号、进程监控 |
| 完整性校验 | 定期校验文件哈希 |
| 卸载检测 | 检测卸载尝试并上报 |

#### 5.4.2 进程隐藏

**进程隐藏机制：**

```go
// 进程隐藏
func hideProcess() error {
    // 修改进程名
    if err := changeProcessName(); err != nil {
        return err
    }
    
    // 隐藏进程信息
    if err := hideProcessInfo(); err != nil {
        return err
    }
    
    return nil
}

// 修改进程名
func changeProcessName() error {
    // 使用 prctl 修改进程名
    // ...
    return nil
}

// 隐藏进程信息
func hideProcessInfo() error {
    // 修改 /proc/self 权限
    // ...
    return nil
}
```

#### 5.4.3 信号屏蔽

**信号屏蔽机制：**

```go
// 屏蔽危险信号
func blockDangerousSignals() error {
    // 创建信号通道
    sigCh := make(chan os.Signal, 1)
    
    // 屏蔽信号
    signal.Notify(sigCh,
        syscall.SIGTERM,  // 终止信号
        syscall.SIGINT,   // 中断信号
        syscall.SIGQUIT,  // 退出信号
        syscall.SIGHUP,   // 挂起信号
    )
    
    // 启动信号处理协程
    go func() {
        for sig := range sigCh {
            log.Printf("Received signal: %v, ignoring", sig)
            // 忽略信号，继续运行
        }
    }()
    
    return nil
}
```

#### 5.4.4 完整性校验

**完整性校验机制：**

```go
// 完整性校验
type IntegrityChecker struct {
    protectedFiles []ProtectedFile
    mu             sync.Mutex
}

// 受保护的文件
type ProtectedFile struct {
    Path     string
    Hash     string
    Expected string
}

// 创建完整性校验器
func NewIntegrityChecker() *IntegrityChecker {
    return &IntegrityChecker{
        protectedFiles: []ProtectedFile{
            {
                Path: "/usr/local/bin/evdi-agent",
            },
            {
                Path: "/etc/evdi/agent.yaml",
            },
        },
    }
}

// 计算文件哈希
func (c *IntegrityChecker) calculateFileHash(path string) (string, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return "", err
    }
    
    hash := sha256.Sum256(data)
    return hex.EncodeToString(hash[:]), nil
}

// 校验完整性
func (c *IntegrityChecker) VerifyIntegrity() bool {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    for _, file := range c.protectedFiles {
        currentHash, err := c.calculateFileHash(file.Path)
        if err != nil {
            log.Printf("Failed to calculate hash for %s: %v", file.Path, err)
            return false
        }
        
        if currentHash != file.Expected {
            log.Printf("Integrity check failed for %s", file.Path)
            return false
        }
    }
    
    return true
}

// 启动完整性校验
func (c *IntegrityChecker) StartIntegrityCheck() {
    go func() {
        ticker := time.NewTicker(5 * time.Minute)
        defer ticker.Stop()
        
        for range ticker.C {
            if !c.VerifyIntegrity() {
                log.Printf("Integrity check failed, reporting to broker")
                c.reportIntegrityViolation()
            }
        }
    }()
}
```

### 5.5 安全审计

#### 5.5.1 操作审计

**审计事件类型：**

| 事件类型 | 说明 | 严重程度 |
|----------|------|----------|
| `agent.register` | Agent 注册 | Info |
| `agent.heartbeat` | 心跳上报 | Info |
| `session.create` | 会话创建 | Info |
| `session.connect` | 会话连接 | Info |
| `session.disconnect` | 会话断开 | Info |
| `session.close` | 会话关闭 | Info |
| `command.execute` | 命令执行 | Warning |
| `clipboard.sync` | 剪贴板同步 | Info |
| `security.violation` | 安全违规 | Critical |

**审计日志格式：**

```json
{
  "eventId": "evt_001",
  "eventType": "command.execute",
  "severity": "Warning",
  "timestamp": "2024-01-01T08:00:00Z",
  "agentId": "agent_desktop_001",
  "desktopId": "desktop_001",
  "sessionId": "sess_xyz",
  "userId": "user_123",
  "details": {
    "commandType": "LOCK_SCREEN",
    "success": true,
    "clientIp": "10.0.0.1"
  }
}
```

**审计日志代码：**

```go
// 审计日志记录器
type AuditLogger struct {
    brokerClient *BrokerClient
}

// 创建审计日志记录器
func NewAuditLogger(brokerClient *BrokerClient) *AuditLogger {
    return &AuditLogger{
        brokerClient: brokerClient,
    }
}

// 记录审计事件
func (l *AuditLogger) LogEvent(eventType, severity string, details map[string]interface{}) {
    event := &AuditEvent{
        EventID:   uuid.New().String(),
        EventType: eventType,
        Severity:  severity,
        Timestamp: time.Now(),
        AgentID:   l.brokerClient.agentID,
        DesktopID: l.brokerClient.desktopID,
        Details:   details,
    }
    
    // 异步发送到 Broker
    go func() {
        if err := l.sendToBroker(event); err != nil {
            log.Printf("Failed to send audit event: %v", err)
        }
    }()
}

// 发送审计事件到 Broker
func (l *AuditLogger) sendToBroker(event *AuditEvent) error {
    // POST /api/v1/agent/audit-event
    // ...
    return nil
}
```

#### 5.5.2 安全事件

**安全事件类型：**

| 事件类型 | 说明 | 严重程度 | 处理方式 |
|----------|------|----------|----------|
| `unauthorized_access` | 未授权访问尝试 | Critical | 立即告警 |
| `invalid_token` | 无效 Token | Warning | 记录日志 |
| `permission_denied` | 权限拒绝 | Warning | 记录日志 |
| `integrity_violation` | 完整性违规 | Critical | 立即告警 |
| `uninstall_attempt` | 卸载尝试 | Critical | 立即告警 |
| `signal_received` | 收到危险信号 | Warning | 记录日志 |

**安全事件处理：**

```go
// 安全事件处理器
type SecurityEventHandler struct {
    auditLogger *AuditLogger
}

// 处理安全事件
func (h *SecurityEventHandler) HandleSecurityEvent(eventType string, details map[string]interface{}) {
    // 记录审计日志
    h.auditLogger.LogEvent("security."+eventType, "Critical", details)
    
    // 上报 Broker
    h.reportToBroker(eventType, details)
    
    // 触发告警
    h.triggerAlert(eventType, details)
}

// 上报 Broker
func (h *SecurityEventHandler) reportToBroker(eventType string, details map[string]interface{}) {
    // POST /api/v1/agent/security-event
    // ...
}

// 触发告警
func (h *SecurityEventHandler) triggerAlert(eventType string, details map[string]interface{}) {
    // 发送告警到监控系统
    // ...
}
```

#### 5.5.3 合规性检查

**合规性检查项：**

| 检查项 | 说明 | 检查频率 |
|--------|------|----------|
| TLS 版本 | 检查 TLS 版本是否 >= 1.2 | 启动时 |
| 密码套件 | 检查是否使用安全的密码套件 | 启动时 |
| 证书有效期 | 检查证书是否即将过期 | 每 24 小时 |
| 文件权限 | 检查关键文件权限 | 每 5 分钟 |
| 进程权限 | 检查进程运行权限 | 启动时 |

**合规性检查代码：**

```go
// 合规性检查器
type ComplianceChecker struct {
    checks []ComplianceCheck
}

// 合规性检查接口
type ComplianceCheck interface {
    Name() string
    Check() error
}

// 创建合规性检查器
func NewComplianceChecker() *ComplianceChecker {
    return &ComplianceChecker{
        checks: []ComplianceCheck{
            &TLSVersionCheck{},
            &CipherSuiteCheck{},
            &CertificateExpiryCheck{},
            &FilePermissionCheck{},
            &ProcessPermissionCheck{},
        },
    }
}

// 执行合规性检查
func (c *ComplianceChecker) RunChecks() []ComplianceViolation {
    violations := make([]ComplianceViolation, 0)
    
    for _, check := range c.checks {
        if err := check.Check(); err != nil {
            violations = append(violations, ComplianceViolation{
                Check:   check.Name(),
                Error:   err.Error(),
            })
        }
    }
    
    return violations
}

// TLS 版本检查
type TLSVersionCheck struct{}

func (c *TLSVersionCheck) Name() string {
    return "tls_version"
}

func (c *TLSVersionCheck) Check() error {
    // 检查 TLS 版本是否 >= 1.2
    // ...
    return nil
}

// 密码套件检查
type CipherSuiteCheck struct{}

func (c *CipherSuiteCheck) Name() string {
    return "cipher_suite"
}

func (c *CipherSuiteCheck) Check() error {
    // 检查是否使用安全的密码套件
    // ...
    return nil
}
```

---

## 6. 部署与运维

本章定义 Agent 的部署方式、配置管理、生命周期管理、自动升级、崩溃恢复和监控告警。

### 6.1 生命周期管理

#### 6.1.1 Agent 启动流程（已在 3.5 节定义）

**启动流程概要：**

```
┌─────────────┐
│  系统启动   │
│  Agent 服务 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  1. 环境检查│
│  (依赖、权限)│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  2. 加载配置│
│  (本地配置) │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  3. 初始化   │
│  日志系统   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  4. 启动    │
│  防卸载机制 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  5. 启动    │
│  进程守护   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  6. 连接    │
│  Broker     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  7. 注册    │
│  到 Broker  │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  8. 启动    │
│  WebRTC 引擎│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  9. 启动    │
│  会话管理   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  10. 启动   │
│  监控采集   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  11. 就绪   │
│  等待连接   │
└─────────────┘
```

#### 6.1.2 Agent 停止流程

**优雅关闭流程：**

```
┌─────────────┐
│  收到停止   │
│  信号       │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  1. 通知    │
│  Broker     │
│  (Agent 关闭)│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  2. 停止    │
│  接受新连接 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  3. 等待    │
│  会话结束   │
│  (超时 30 秒)│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  4. 关闭    │
│  WebRTC 连接│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  5. 停止    │
│  GStreamer   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  6. 释放    │
│  系统资源   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  7. 记录    │
│  关闭日志   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  8. 进程    │
│  退出       │
└─────────────┘
```

**优雅关闭代码：**

```go
// 优雅关闭
func (a *Agent) GracefulShutdown() error {
    log.Printf("Starting graceful shutdown...")
    
    // 1. 通知 Broker
    a.notifyBrokerShutdown()
    
    // 2. 停止接受新连接
    a.stopAcceptingConnections()
    
    // 3. 等待会话结束
    a.waitForSessionsToComplete(30 * time.Second)
    
    // 4. 关闭 WebRTC 连接
    a.closeWebRTCConnections()
    
    // 5. 停止 GStreamer
    a.stopGStreamer()
    
    // 6. 释放系统资源
    a.releaseResources()
    
    // 7. 记录关闭日志
    log.Printf("Agent shutdown completed")
    
    return nil
}

// 等待会话结束
func (a *Agent) waitForSessionsToComplete(timeout time.Duration) {
    deadline := time.Now().Add(timeout)
    
    for time.Now().Before(deadline) {
        if a.sessionManager.GetActiveSessionCount() == 0 {
            return
        }
        time.Sleep(100 * time.Millisecond)
    }
    
    log.Printf("Timeout waiting for sessions to complete, forcing close")
}
```

#### 6.1.3 Agent 状态监控

**健康检查接口：**

```
GET /api/v1/agent/health
```

**健康检查响应：**

```json
{
  "status": "healthy",
  "version": "1.0.0",
  "uptime": 3600,
  "components": {
    "webRTC": "ok",
    "gstreamer": "ok",
    "brokerConnection": "ok",
    "sessionManager": "ok"
  }
}
```

**状态定义：**

| 状态 | 说明 |
|------|------|
| `healthy` | 所有组件正常 |
| `degraded` | 部分组件异常，但核心功能可用 |
| `unhealthy` | 核心组件异常，服务不可用 |

**健康检查代码：**

```go
// 健康检查
func (a *Agent) HealthCheck() *HealthStatus {
    status := &HealthStatus{
        Status:  "healthy",
        Version: a.version,
        Uptime:  time.Since(a.startTime).Seconds(),
        Components: make(map[string]string),
    }
    
    // 检查 WebRTC 引擎
    if a.webRTCEngine.IsHealthy() {
        status.Components["webRTC"] = "ok"
    } else {
        status.Components["webRTC"] = "error"
        status.Status = "degraded"
    }
    
    // 检查 GStreamer
    if a.gstreamer.IsHealthy() {
        status.Components["gstreamer"] = "ok"
    } else {
        status.Components["gstreamer"] = "error"
        status.Status = "degraded"
    }
    
    // 检查 Broker 连接
    if a.brokerClient.IsConnected() {
        status.Components["brokerConnection"] = "ok"
    } else {
        status.Components["brokerConnection"] = "error"
        status.Status = "degraded"
    }
    
    // 检查会话管理器
    if a.sessionManager.IsHealthy() {
        status.Components["sessionManager"] = "ok"
    } else {
        status.Components["sessionManager"] = "error"
        status.Status = "degraded"
    }
    
    return status
}
```

#### 6.1.4 K8s 集成

**K8s Deployment 配置：**

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: evdi-agent
  namespace: vdi-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: evdi-agent
  template:
    metadata:
      labels:
        app: evdi-agent
    spec:
      containers:
      - name: agent
        image: evdi-agent:1.0.0
        ports:
        - containerPort: 8080
          name: http
        - containerPort: 8443
          name: https
        env:
        - name: BROKER_URL
          value: "https://broker.vdi-system.svc.cluster.local:8080"
        - name: DESKTOP_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        livenessProbe:
          httpGet:
            path: /api/v1/agent/health
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3
        readinessProbe:
          httpGet:
            path: /api/v1/agent/health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 5
          timeoutSeconds: 3
          failureThreshold: 3
        volumeMounts:
        - name: agent-config
          mountPath: /etc/evdi
          readOnly: true
      volumes:
      - name: agent-config
        configMap:
          name: evdi-agent-config
```

**K8s 健康探针：**

| 探针类型 | 路径 | 初始延迟 | 检查周期 | 超时 | 失败阈值 |
|----------|------|----------|----------|------|----------|
| livenessProbe | /api/v1/agent/health | 30 秒 | 10 秒 | 5 秒 | 3 次 |
| readinessProbe | /api/v1/agent/health | 10 秒 | 5 秒 | 3 秒 | 3 次 |

**探针说明：**

- **livenessProbe**：检测 Agent 是否存活，失败时 K8s 会重启 Pod
- **readinessProbe**：检测 Agent 是否就绪，失败时 K8s 会从 Service 中移除 Pod

### 6.2 自动升级机制（已在 3.5 节定义）

#### 6.2.1 升级策略概要

**升级方式：**

| 方式 | 说明 | 适用场景 |
|------|------|----------|
| Broker 推送 | Broker 下发升级指令 | 紧急升级、安全补丁 |
| Agent 拉取 | Agent 定期检查新版本 | 常规升级 |

**升级流程概要：**

```
┌─────────────┐
│  1. 停止    │
│  Agent 服务 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  2. 备份    │
│  当前版本   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  3. 替换    │
│  二进制文件 │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  4. 启动    │
│  新版本     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  5. 验证    │
│  升级成功   │
└─────────────┘
```

#### 6.2.2 版本管理

**版本号规范：**

```
主版本号.次版本号.修订号

示例：1.0.0
```

**版本兼容性：**

| 版本变更 | 兼容性 | 处理方式 |
|----------|--------|----------|
| 修订号 +1 | 完全兼容 | 自动升级 |
| 次版本号 +1 | 向后兼容 | 自动升级 |
| 主版本号 +1 | 可能不兼容 | 需要人工确认 |

#### 6.2.3 升级失败处理

**回滚机制：**

```
┌─────────────┐
│  升级失败   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  停止       │
│  新版本     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  恢复       │
│  备份版本   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  启动       │
│  旧版本     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  上报       │
│  Broker     │
└─────────────┘
```

**回滚代码：**

```go
// 回滚到备份版本
func (m *AutoUpgradeManager) rollback() error {
    log.Printf("Rolling back to backup version...")
    
    // 1. 停止新版本
    m.stopNewVersion()
    
    // 2. 恢复备份
    if err := m.restoreBackup(); err != nil {
        return fmt.Errorf("failed to restore backup: %w", err)
    }
    
    // 3. 启动旧版本
    if err := m.startOldVersion(); err != nil {
        return fmt.Errorf("failed to start old version: %w", err)
    }
    
    // 4. 上报 Broker
    m.reportRollback()
    
    log.Printf("Rollback completed successfully")
    return nil
}
```

### 6.3 崩溃恢复（已在 3.5 节定义）

#### 6.3.1 崩溃检测概要

**崩溃检测机制：**

| 机制 | 说明 |
|------|------|
| 信号处理 | 捕获 SIGSEGV、SIGABRT 等信号 |
| panic 恢复 | 使用 defer + recover 捕获 panic |
| coredump | 记录崩溃现场 |

#### 6.3.2 自动重启概要

**重启策略：**

| 策略 | 说明 |
|------|------|
| 立即重启 | 崩溃后立即重启，不等待 |
| 指数退避 | 重启间隔逐渐增加 |
| 最大重试次数 | 限制重启次数 |

#### 6.3.3 崩溃日志

**崩溃日志格式：**

```json
{
  "timestamp": "2024-01-01T08:00:00Z",
  "signal": "SIGSEGV",
  "stackTrace": "goroutine 1 [running]:\nmain.main()...",
  "goroutines": 10,
  "memoryStats": {
    "alloc": 1024000,
    "totalAlloc": 2048000,
    "sys": 3072000
  }
}
```

**崩溃日志代码：**

```go
// 记录崩溃日志
func (m *CrashRecoveryManager) logCrash(crashInfo *CrashInfo) {
    // 写入日志文件
    logData, err := json.MarshalIndent(crashInfo, "", "  ")
    if err != nil {
        log.Printf("Failed to marshal crash info: %v", err)
        return
    }
    
    logFile := fmt.Sprintf("/var/log/evdi/crash_%s.json", 
        crashInfo.Timestamp.Format("20060102_150405"))
    
    if err := os.WriteFile(logFile, logData, 0644); err != nil {
        log.Printf("Failed to write crash log: %v", err)
    }
}
```

#### 6.3.4 崩溃上报

**上报 Broker：**

```go
// 上报崩溃信息到 Broker
func (m *CrashRecoveryManager) reportCrash(crashInfo *CrashInfo) {
    report := &CrashReport{
        AgentID:     m.agentID,
        DesktopID:   m.desktopID,
        Signal:      crashInfo.Signal,
        StackTrace:  crashInfo.StackTrace,
        Timestamp:   crashInfo.Timestamp,
    }
    
    // POST /api/v1/agent/crash-report
    // ...
}
```

### 6.4 监控告警

#### 6.4.1 监控指标（已在 3.4 节定义）

**监控指标概要：**

| 指标 | 单位 | 采集周期 | 说明 |
|------|------|----------|------|
| cpu_usage | % | 1 分钟 | CPU 使用率 |
| memory_usage | % | 1 分钟 | 内存使用率 |
| disk_usage | % | 1 分钟 | 磁盘使用率 |
| network_latency | ms | 1 分钟 | 网络延迟 |
| session_duration | s | 1 分钟 | 会话时长 |

#### 6.4.2 告警规则

**告警阈值配置：**

```yaml
alerts:
  - name: high_cpu_usage
    metric: cpu_usage
    threshold: 90
    duration: 5m
    severity: warning
    
  - name: high_memory_usage
    metric: memory_usage
    threshold: 90
    duration: 5m
    severity: warning
    
  - name: high_disk_usage
    metric: disk_usage
    threshold: 90
    duration: 5m
    severity: warning
    
  - name: high_network_latency
    metric: network_latency
    threshold: 100
    duration: 5m
    severity: warning
    
  - name: agent_heartbeat_timeout
    metric: heartbeat_timeout
    threshold: 60
    duration: 0s
    severity: critical
```

**告警级别：**

| 级别 | 说明 | 处理方式 |
|------|------|----------|
| `info` | 信息 | 仅记录日志 |
| `warning` | 警告 | 记录日志 + 通知 |
| `critical` | 严重 | 记录日志 + 通知 + 自动处理 |

#### 6.4.3 告警通知

**通知渠道：**

| 渠道 | 说明 | 适用场景 |
|------|------|----------|
| 钉钉 | 钉钉机器人 | 团队通知 |
| 企业微信 | 企业微信机器人 | 团队通知 |
| 邮件 | SMTP 邮件 | 正式通知 |
| 短信 | 短信网关 | 紧急通知 |

**通知模板：**

```json
{
  "title": "【{severity}】Agent 告警：{alertName}",
  "body": "告警名称：{alertName}\n严重程度：{severity}\nAgent ID：{agentId}\n桌面 ID：{desktopId}\n触发时间：{firedAt}\n当前值：{currentValue}\n阈值：{threshold}"
}
```

**通知代码：**

```go
// 告警通知器
type AlertNotifier struct {
    channels []NotificationChannel
}

// 通知渠道接口
type NotificationChannel interface {
    Send(alert *Alert) error
}

// 创建告警通知器
func NewAlertNotifier() *AlertNotifier {
    return &AlertNotifier{
        channels: []NotificationChannel{
            &DingTalkChannel{},
            &WeComChannel{},
            &EmailChannel{},
        },
    }
}

// 发送告警通知
func (n *AlertNotifier) Notify(alert *Alert) {
    for _, channel := range n.channels {
        go func(ch NotificationChannel) {
            if err := ch.Send(alert); err != nil {
                log.Printf("Failed to send alert via %T: %v", ch, err)
            }
        }(channel)
    }
}
```

#### 6.4.4 告警处理

**自动处理策略：**

| 告警类型 | 自动处理 | 人工介入 |
|----------|----------|----------|
| high_cpu_usage | 降低编码质量 | 检查进程 |
| high_memory_usage | 清理缓存 | 检查内存泄漏 |
| high_disk_usage | 清理日志 | 扩容磁盘 |
| high_network_latency | 切换编码器 | 检查网络 |
| agent_heartbeat_timeout | 重启 Agent | 检查 Broker |

**自动处理代码：**

```go
// 告警处理器
type AlertHandler struct {
    autoHandlers map[string]AutoHandler
}

// 自动处理接口
type AutoHandler interface {
    Handle(alert *Alert) error
}

// 创建告警处理器
func NewAlertHandler() *AlertHandler {
    return &AlertHandler{
        autoHandlers: map[string]AutoHandler{
            "high_cpu_usage":       &HighCPUHandler{},
            "high_memory_usage":    &HighMemoryHandler{},
            "high_disk_usage":      &HighDiskHandler{},
            "high_network_latency": &HighLatencyHandler{},
        },
    }
}

// 处理告警
func (h *AlertHandler) HandleAlert(alert *Alert) {
    // 记录告警日志
    log.Printf("Alert received: %s (severity: %s)", alert.Name, alert.Severity)
    
    // 尝试自动处理
    if handler, ok := h.autoHandlers[alert.Name]; ok {
        if err := handler.Handle(alert); err != nil {
            log.Printf("Auto handle failed: %v", err)
            // 升级为人工处理
            h.escalateToHuman(alert)
        }
    } else {
        // 无自动处理，直接升级为人工处理
        h.escalateToHuman(alert)
    }
}

// 升级为人工处理
func (h *AlertHandler) escalateToHuman(alert *Alert) {
    log.Printf("Escalating alert to human: %s", alert.Name)
    // 发送通知给运维人员
    // ...
}
```

### 6.5 部署配置

#### 6.5.1 部署方式

**K8s Deployment（推荐）：**

适用于无状态 Agent，每个桌面实例一个 Pod。

**K8s DaemonSet：**

适用于需要在每个节点运行 Agent 的场景。

**部署方式选择：**

| 方式 | 适用场景 | 优点 | 缺点 |
|------|----------|------|------|
| Deployment | 每个桌面一个 Agent | 隔离性好、易于管理 | 资源消耗较大 |
| DaemonSet | 每个节点一个 Agent | 资源消耗小 | 隔离性差 |

#### 6.5.2 配置管理

**配置文件格式（YAML）：**

```yaml
# /etc/evdi/agent.yaml
agent:
  version: "1.0.0"
  logLevel: "info"
  
broker:
  url: "https://broker.vdi-system.svc.cluster.local:8080"
  timeout: "30s"
  
webrtc:
  iceServers:
    - urls: "stun:stun.example.com:3478"
    - urls: "turn:turn.example.com:3478"
      username: "user"
      credential: "pass"
  
encoding:
  width: 1920
  height: 1080
  framerate: 30
  bitrate: 4000
  
monitoring:
  collectInterval: "60s"
  reportInterval: "60s"
  
security:
  antiUninstall: true
  processProtection: true
```

**环境变量配置：**

| 环境变量 | 说明 | 默认值 |
|----------|------|--------|
| `BROKER_URL` | Broker 地址 | - |
| `DESKTOP_ID` | 桌面 ID | - |
| `LOG_LEVEL` | 日志级别 | info |
| `WEBRTC_ICE_SERVERS` | ICE 服务器 | - |
| `ENCODING_WIDTH` | 编码宽度 | 1920 |
| `ENCODING_HEIGHT` | 编码高度 | 1080 |
| `ENCODING_FRAMERATE` | 编码帧率 | 30 |
| `ENCODING_BITRATE` | 编码码率 | 4000 |
| `MONITOR_COLLECT_INTERVAL` | 采集周期 | 60s |
| `MONITOR_REPORT_INTERVAL` | 上报周期 | 60s |

**ConfigMap 配置：**

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: evdi-agent-config
  namespace: vdi-system
data:
  agent.yaml: |
    agent:
      version: "1.0.0"
      logLevel: "info"
    broker:
      url: "https://broker.vdi-system.svc.cluster.local:8080"
      timeout: "30s"
    encoding:
      width: 1920
      height: 1080
      framerate: 30
      bitrate: 4000
```

#### 6.5.3 资源限制

**资源配额配置：**

```yaml
resources:
  requests:
    memory: "256Mi"
    cpu: "250m"
  limits:
    memory: "512Mi"
    cpu: "500m"
```

**资源说明：**

| 资源 | 请求 | 限制 | 说明 |
|------|------|------|------|
| CPU | 250m | 500m | 编码需要 CPU 资源 |
| 内存 | 256Mi | 512Mi | WebRTC + GStreamer 需要内存 |

**资源监控：**

```go
// 资源监控
func (a *Agent) monitorResources() {
    // 监控 CPU 使用率
    cpuUsage := getCPUUsage()
    if cpuUsage > 90 {
        log.Printf("High CPU usage: %.1f%%", cpuUsage)
        a.adjustEncodingQuality()
    }
    
    // 监控内存使用率
    memUsage := getMemoryUsage()
    if memUsage > 90 {
        log.Printf("High memory usage: %.1f%%", memUsage)
        a.cleanupCache()
    }
}
```

#### 6.5.4 网络配置

**端口暴露：**

| 端口 | 协议 | 用途 |
|------|------|------|
| 8080 | HTTP | 健康检查、监控 |
| 8443 | HTTPS | Broker 通信 |
| 50000-60000 | UDP | WebRTC 媒体流 |

**K8s Service 配置：**

```yaml
apiVersion: v1
kind: Service
metadata:
  name: evdi-agent
  namespace: vdi-system
spec:
  selector:
    app: evdi-agent
  ports:
  - name: http
    port: 8080
    targetPort: 8080
  - name: https
    port: 8443
    targetPort: 8443
  type: ClusterIP
```

**网络策略配置：**

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: evdi-agent-network-policy
  namespace: vdi-system
spec:
  podSelector:
    matchLabels:
      app: evdi-agent
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: vdi-system
    ports:
    - protocol: TCP
      port: 8080
    - protocol: TCP
      port: 8443
    - protocol: UDP
      port: 50000-60000
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: vdi-system
    ports:
    - protocol: TCP
      port: 8080
    - protocol: TCP
      port: 8443
  - to:
    - ipBlock:
        cidr: 0.0.0.0/0
    ports:
    - protocol: UDP
      port: 3478
    - protocol: TCP
      port: 3478
```

---

## 7. 接口规范

本章定义 Agent 的接口规范，包括 Broker API、监控数据接口、内部组件接口和错误码规范。

### 7.1 Broker API

#### 7.1.1 API 概述（已在 4.1 节定义）

**API 基础信息：**

| 规范项 | 定义 |
|--------|------|
| 协议 | HTTP/HTTPS |
| 基础路径 | `/api/v1/agent` |
| 消息格式 | JSON |
| 认证方式 | Bearer Token |
| 时间格式 | ISO 8601 UTC |

#### 7.1.2 API 列表

**完整 API 列表：**

| API | 方法 | 路径 | 说明 |
|-----|------|------|------|
| Agent 注册 | POST | `/api/v1/agent/register` | Agent 启动时注册 |
| 心跳上报 | POST | `/api/v1/agent/heartbeat` | 15 秒间隔上报 |
| 配置拉取 | GET | `/api/v1/agent/config` | 获取最新配置 |
| 会话事件 | POST | `/api/v1/agent/session-event` | 上报会话事件 |
| 监控数据 | POST | `/api/v1/agent/monitor-data` | 上报监控数据 |
| 指令确认 | POST | `/api/v1/agent/command-result` | 上报指令执行结果 |
| 错误上报 | POST | `/api/v1/agent/error` | 上报错误信息 |
| 健康检查 | GET | `/api/v1/agent/health` | Agent 健康状态 |

**API 详细说明（已在 4.1 节定义）：**

- Agent 注册：4.1.3 节
- 心跳上报：4.1.4 节
- 配置拉取：4.1.5 节
- 会话事件：4.1.6 节
- 监控数据：4.1.7 节
- 指令确认：4.1.8 节
- 错误上报：4.1.11 节

#### 7.1.3 API 版本管理

**版本号规范：**

```
/v1/agent/register
│ │
│ └── API 路径
└──── 版本号
```

**版本兼容性规则：**

| 变更类型 | 兼容性 | 处理方式 |
|----------|--------|----------|
| 新增可选字段 | ✅ 兼容 | 客户端忽略未知字段 |
| 新增 API | ✅ 兼容 | 客户端不需要调用 |
| 删除字段 | ❌ 不兼容 | 需要版本升级 |
| 修改字段含义 | ❌ 不兼容 | 需要版本升级 |
| 删除 API | ❌ 不兼容 | 需要版本升级 |

**版本协商：**

```
┌─────────────┐                    ┌─────────────┐
│    Agent    │                    │   Broker    │
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │  1. 注册请求                     │
       │  POST /api/v1/agent/register     │
       │  Header: X-API-Version: 1.0      │
       │─────────────────────────────────►│
       │                                  │
       │  2. 响应                         │
       │  Header: X-API-Version: 1.0      │
       │◄─────────────────────────────────│
       │                                  │
```

#### 7.1.4 API 限流

**限流策略：**

| API | 限流规则 | 说明 |
|-----|----------|------|
| POST /agent/register | 1 次/分钟 | 防止重复注册 |
| POST /agent/heartbeat | 1 次/10 秒 | 防止心跳风暴 |
| POST /agent/session-event | 10 次/分钟 | 防止事件风暴 |
| POST /agent/monitor-data | 1 次/30 秒 | 防止监控风暴 |
| POST /agent/command-result | 10 次/分钟 | 防止确认风暴 |
| POST /agent/error | 5 次/分钟 | 防止错误风暴 |

**限流响应：**

```json
{
  "code": 1005,
  "message": "Rate limit exceeded",
  "data": {
    "retryAfter": 60
  }
}
```

**限流代码：**

```go
// API 限流器
type RateLimiter struct {
    limiters map[string]*rate.Limiter
    mu       sync.RWMutex
}

// 创建限流器
func NewRateLimiter() *RateLimiter {
    return &RateLimiter{
        limiters: map[string]*rate.Limiter{
            "/api/v1/agent/register":      rate.NewLimiter(rate.Every(time.Minute), 1),
            "/api/v1/agent/heartbeat":     rate.NewLimiter(rate.Every(10*time.Second), 1),
            "/api/v1/agent/session-event": rate.NewLimiter(rate.Every(6*time.Second), 10),
            "/api/v1/agent/monitor-data":  rate.NewLimiter(rate.Every(30*time.Second), 1),
            "/api/v1/agent/command-result": rate.NewLimiter(rate.Every(6*time.Second), 10),
            "/api/v1/agent/error":         rate.NewLimiter(rate.Every(12*time.Second), 5),
        },
    }
}

// 检查限流
func (l *RateLimiter) Allow(path string) bool {
    l.mu.RLock()
    defer l.mu.RUnlock()
    
    limiter, ok := l.limiters[path]
    if !ok {
        return true
    }
    
    return limiter.Allow()
}
```

### 7.2 监控数据接口

#### 7.2.1 监控数据上报（已在 4.1 节定义）

**接口定义：**

```
POST /api/v1/agent/monitor-data
Authorization: Bearer <agentToken>
Content-Type: application/json
```

**请求体：**

```json
{
  "desktopId": "desktop_001",
  "timestamp": "2024-01-01T08:00:00Z",
  "metrics": [
    {
      "name": "cpu_usage",
      "value": 25.5,
      "unit": "%"
    },
    {
      "name": "memory_usage",
      "value": 45.2,
      "unit": "%"
    },
    {
      "name": "disk_usage",
      "value": 60.0,
      "unit": "%"
    },
    {
      "name": "network_latency",
      "value": 15,
      "unit": "ms"
    },
    {
      "name": "session_duration",
      "value": 1800,
      "unit": "s"
    }
  ]
}
```

#### 7.2.2 监控指标定义（已在 3.4 节定义）

**监控指标列表：**

| 指标名称 | 单位 | 说明 | 采集周期 |
|----------|------|------|----------|
| cpu_usage | % | CPU 使用率 | 1 分钟 |
| memory_usage | % | 内存使用率 | 1 分钟 |
| disk_usage | % | 磁盘使用率 | 1 分钟 |
| network_latency | ms | 网络延迟 | 1 分钟 |
| session_duration | s | 会话时长 | 1 分钟 |

**指标精度：** 保留 1 位小数

#### 7.2.3 监控配置（已在 3.4 节定义）

**配置参数：**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| collectInterval | 60 秒 | 采集周期 |
| reportInterval | 60 秒 | 上报周期 |

**配置获取：**

Agent 通过心跳响应或配置拉取接口获取监控配置：

```json
{
  "monitorConfig": {
    "collectInterval": 60,
    "reportInterval": 60
  }
}
```

### 7.3 内部组件接口

#### 7.3.1 WebRTC 引擎接口

**WebRTC 引擎对外接口：**

```go
// WebRTC 引擎接口
type WebRTCEngine interface {
    // 初始化
    Initialize(config *WebRTCConfig) error
    
    // 创建 PeerConnection
    CreatePeerConnection() error
    
    // 创建 DataChannel
    CreateDataChannels() error
    
    // 创建媒体轨
    CreateVideoTrack() error
    CreateAudioTrack() error
    
    // SDP 协商
    CreateOffer() (*webrtc.SessionDescription, error)
    SetRemoteAnswer(answer webrtc.SessionDescription) error
    
    // ICE 配置
    SetICEServers(servers []ICEServer) error
    
    // 连接状态
    GetConnectionState() ConnectionState
    OnConnectionStateChange(callback func(ConnectionState))
    
    // 媒体控制
    UpdateEncodingParams(params EncodingParams) error
    
    // 关闭
    Close() error
}
```

**WebRTC 配置：**

```go
type WebRTCConfig struct {
    ICEServers    []ICEServer
    EncodingParams EncodingParams
    DataChannelConfig DataChannelConfig
}

type ICEServer struct {
    URLs       []string
    Username   string
    Credential string
}

type EncodingParams struct {
    Width     int
    Height    int
    Framerate int
    Bitrate   int
}

type DataChannelConfig struct {
    ControlReliable   bool
    InputReliable     bool
    ClipboardReliable bool
    HeartbeatReliable bool
}
```

#### 7.3.2 会话管理接口

**会话管理器对外接口：**

```go
// 会话管理器接口
type SessionManager interface {
    // 会话生命周期
    CreateOrReplaceSession(userID string) (*Session, error)
    RestoreSession(sessionID, token string) (*Session, error)
    CloseSession(sessionID string) error
    
    // 会话状态
    GetSession(sessionID string) (*Session, error)
    GetActiveSession() *Session
    GetActiveSessionCount() int
    
    // 会话事件
    OnSessionEvent(callback func(*SessionEvent))
    
    // 输入事件处理
    HandleInputEvent(event *InputEvent) error
    
    // 剪贴板同步
    HandleClipboardEvent(event *ClipboardEvent) error
    
    // 健康检查
    IsHealthy() bool
}
```

**会话配置：**

```go
type SessionConfig struct {
    MaxSessions       int
    SessionTimeout    time.Duration
    AllowReconnect    bool
    ReconnectTimeout  time.Duration
    IdleTimeout       time.Duration
}
```

#### 7.3.3 监控采集接口

**监控采集器对外接口：**

```go
// 监控采集器接口
type MonitorCollector interface {
    // 启动/停止
    Start()
    Stop()
    
    // 采集指标
    CollectMetrics() []*MonitorMetric
    
    // 配置更新
    UpdateConfig(config *MonitorConfig)
    
    // 健康检查
    IsHealthy() bool
}

// 监控指标
type MonitorMetric struct {
    Name      string
    Value     float64
    Unit      string
    Timestamp time.Time
}

// 监控配置
type MonitorConfig struct {
    CollectInterval time.Duration
    ReportInterval  time.Duration
    MetricsEnabled  map[string]bool
}
```

#### 7.3.4 生命周期管理接口

**生命周期管理器对外接口：**

```go
// 生命周期管理器接口
type LifecycleManager interface {
    // 启动/停止
    Start() error
    Stop()
    
    // 健康检查
    HealthCheck() *HealthStatus
    
    // 升级管理
    CheckForUpgrade() (*UpgradeInfo, error)
    PerformUpgrade(info *UpgradeInfo) error
    Rollback() error
    
    // 版本管理
    GetCurrentVersion() string
    GetVersionHistory() []VersionInfo
    
    // 防卸载
    EnableAntiUninstall() error
    DisableAntiUninstall() error
}
```

**健康状态：**

```go
type HealthStatus struct {
    Status     string            // healthy, degraded, unhealthy
    Version    string
    Uptime     float64
    Components map[string]string // component -> status
}

type UpgradeInfo struct {
    CurrentVersion string
    LatestVersion  string
    DownloadURL    string
    Checksum       string
    ReleaseNotes   string
}
```

#### 7.3.5 Broker 客户端接口

**Broker 客户端对外接口：**

```go
// Broker 客户端接口
type BrokerClient interface {
    // 连接管理
    Connect() error
    Disconnect() error
    IsConnected() bool
    
    // Agent 注册
    Register(info *AgentInfo) (*RegisterResponse, error)
    
    // 心跳
    Heartbeat(req *HeartbeatRequest) (*HeartbeatResponse, error)
    
    // 配置
    GetConfig(version string) (*AgentConfig, error)
    
    // 会话事件
    ReportSessionEvent(event *SessionEvent) error
    
    // 监控数据
    ReportMonitorData(data *MonitorData) error
    
    // 指令确认
    ReportCommandResult(result *CommandResult) error
    
    // 错误上报
    ReportError(err *ErrorReport) error
    
    // 健康检查
    IsHealthy() bool
}
```

#### 7.3.6 组件间通信

**组件间依赖关系：**

```
┌─────────────────────────────────────────────────────────────────────┐
│                    Agent 组件依赖关系                                 │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────┐
│    Agent    │
│   (主进程)  │
└──────┬──────┘
       │
       ├──► WebRTCEngine
       │    ├──► GStreamerPipeline
       │    └──► DataChannelManager
       │
       ├──► SessionManager
       │    ├──► InputEventHandler
       │    └──► ClipboardSync
       │
       ├──► MonitorCollector
       │    ├──► CPUCollector
       │    ├──► MemoryCollector
       │    ├──► DiskCollector
       │    └──► NetworkCollector
       │
       ├──► BrokerClient
       │    └──► HTTPClient
       │
       └──► LifecycleManager
            ├──► ProcessGuardian
            ├──► CrashRecoveryManager
            ├──► AutoUpgradeManager
            └──► AntiUninstallManager
```

**组件间通信方式：**

| 组件 A | 组件 B | 通信方式 | 说明 |
|--------|--------|----------|------|
| Agent | WebRTCEngine | 直接调用 | 创建和管理 WebRTC 连接 |
| Agent | SessionManager | 直接调用 | 管理会话生命周期 |
| Agent | MonitorCollector | 直接调用 | 启动监控采集 |
| Agent | BrokerClient | 直接调用 | 与 Broker 通信 |
| Agent | LifecycleManager | 直接调用 | 管理生命周期 |
| WebRTCEngine | SessionManager | 回调 | 输入事件处理 |
| SessionManager | BrokerClient | 直接调用 | 上报会话事件 |
| MonitorCollector | BrokerClient | 直接调用 | 上报监控数据 |
| LifecycleManager | BrokerClient | 直接调用 | 上报升级状态 |

### 7.4 错误码规范

#### 7.4.1 错误码定义（已在 4.1 节定义）

**统一错误码：**

| 错误码 | HTTP 状态码 | 说明 |
|--------|-------------|------|
| 0 | 200 | 成功 |
| 1001 | 401 | Token 无效或已过期 |
| 1002 | 403 | 无权限 |
| 1003 | 404 | 资源不存在 |
| 1004 | 409 | 资源状态冲突 |
| 1005 | 429 | 请求频率超限 |
| 2001 | 400 | Desktop 不存在 |
| 2002 | 409 | Desktop 状态不允许此操作 |
| 5000 | 500 | 服务内部错误 |

**Agent 特定错误码：**

| 错误码 | HTTP 状态码 | 说明 |
|--------|-------------|------|
| 6001 | 400 | Agent 注册信息无效 |
| 6002 | 409 | Agent 已注册 |
| 6003 | 400 | 心跳数据无效 |
| 6004 | 400 | 监控数据无效 |
| 6005 | 400 | 会话事件无效 |
| 6006 | 400 | 指令结果无效 |
| 6007 | 400 | 错误报告无效 |

#### 7.4.2 错误消息格式

**错误响应格式：**

```json
{
  "code": 1001,
  "message": "Token 无效或已过期",
  "data": null
}
```

**错误响应字段：**

| 字段 | 类型 | 说明 |
|------|------|------|
| code | int | 错误码 |
| message | string | 错误描述 |
| data | object/null | 附加数据（可选） |

**带附加数据的错误响应：**

```json
{
  "code": 1005,
  "message": "Rate limit exceeded",
  "data": {
    "retryAfter": 60,
    "limit": 1,
    "remaining": 0
  }
}
```

#### 7.4.3 错误处理建议

**错误处理策略：**

| 错误码 | 处理建议 |
|--------|----------|
| 0 | 成功，继续处理 |
| 1001 | 重新注册获取新 Token |
| 1002 | 检查权限配置 |
| 1003 | 检查资源是否存在 |
| 1004 | 检查资源状态，等待状态变更后重试 |
| 1005 | 等待 retryAfter 秒后重试 |
| 2001 | 检查 desktopId 是否正确 |
| 2002 | 等待桌面就绪后重试 |
| 5000 | 指数退避重试，最大 3 次 |
| 6001 | 检查注册信息是否完整 |
| 6002 | 忽略，使用已有 Token |
| 6003 | 检查心跳数据格式 |
| 6004 | 检查监控数据格式 |
| 6005 | 检查会话事件格式 |
| 6006 | 检查指令结果格式 |
| 6007 | 检查错误报告格式 |

**错误处理代码：**

```go
// 错误处理
func (c *BrokerClient) handleError(resp *http.Response) error {
    // 解析错误响应
    var errResp ErrorResponse
    if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
        return fmt.Errorf("failed to decode error response: %w", err)
    }
    
    // 根据错误码处理
    switch errResp.Code {
    case 1001:
        // Token 无效，重新注册
        return c.reregister()
    case 1005:
        // 限流，等待后重试
        retryAfter := errResp.Data.RetryAfter
        time.Sleep(time.Duration(retryAfter) * time.Second)
        return ErrRetryLater
    case 5000:
        // 服务端错误，指数退避重试
        return ErrServerInternal
    default:
        // 其他错误，记录日志
        log.Printf("API error: code=%d, message=%s", errResp.Code, errResp.Message)
        return fmt.Errorf("API error: %d - %s", errResp.Code, errResp.Message)
    }
}
```

#### 7.4.4 错误日志记录

**错误日志格式：**

```json
{
  "timestamp": "2024-01-01T08:00:00Z",
  "level": "error",
  "component": "broker_client",
  "operation": "heartbeat",
  "errorCode": 5000,
  "errorMessage": "Service internal error",
  "httpStatus": 500,
  "requestPath": "/api/v1/agent/heartbeat",
  "retryCount": 1,
  "stackTrace": "..."
}
```

**错误日志代码：**

```go
// 记录错误日志
func (c *BrokerClient) logError(operation string, err error, resp *http.Response) {
    logEntry := &ErrorLogEntry{
        Timestamp:   time.Now(),
        Level:       "error",
        Component:   "broker_client",
        Operation:   operation,
        ErrorMessage: err.Error(),
    }
    
    if resp != nil {
        logEntry.HTTPStatus = resp.StatusCode
        logEntry.RequestPath = resp.Request.URL.Path
    }
    
    // 解析错误码
    var errResp ErrorResponse
    if errors.As(err, &errResp) {
        logEntry.ErrorCode = errResp.Code
    }
    
    // 写入日志
    log.Printf("Error: %+v", logEntry)
}
```

---

## 8. MVP 验证记录

> 本章记录 MVP 阶段的验证结果、与正式架构的偏差、以及调试过程中发现的关键经验。完整调试过程详见 `docs/mvp/2026-06-11-mvp-debug-record.md`。

### 8.1 MVP 验证范围与结果

| 验证项 | 状态 | 说明 |
|--------|------|------|
| WebSocket 信令建立 | ✅ 通过 | Agent 内建 WebSocket 服务 (:8080/ws)，CORS 允许所有来源 |
| SDP Offer/Answer 交换 | ✅ 通过 | Client 发 Offer，Agent 回 Answer（与原设计方向相反） |
| ICE 候选交换 | ✅ 通过 | Trickle ICE，候选逐条通过 WebSocket 转发 |
| H.264 视频流传输 | ✅ 通过 | x264enc constrained-baseline，30 FPS |
| Opus 音频流 | ⏳ 待验证 | Pipeline 已配置，音频通路待完整测试 |
| 鼠标移动 | ✅ 通过 | WebSocket 信令通道 + xdotool，带位置合并 |
| 鼠标按键 | ✅ 通过 | 左/右/中键，up/down 事件 |
| 滚轮 | ✅ 通过 | 上下滚动 |
| 键盘输入 | ✅ 通过 | 字母、数字、功能键、修饰键，含 Caps Lock 同步 |
| 剪贴板同步 | ⏳ MVP no-op | 消息格式已定义，功能未实现 |
| 分辨率调整 | ⏳ MVP no-op | 消息格式已定义，功能未实现 |
| DataChannel 双向 | ❌ 失败 | Pion Lite ICE 下 SCTP 单向，已改用 WebSocket |

### 8.2 MVP 与正式架构的偏差汇总

| 偏差项 | 正式架构设计 | MVP 实际实现 | 修正方向 |
|--------|------------|------------|---------|
| SDP 协商方向 | Agent 发 Offer | Client 发 Offer，Agent 回 Answer | 正式版统一为 Client 发 Offer |
| 输入事件传输 | DataChannel (control) | WebSocket 信令通道 | 修复 Pion DataChannel 后回归 DataChannel |
| 输入注入方式 | XTest CGo 直接调用 | xdotool (`exec.Command`) | 正式版迁移至 XTest CGo |
| GStreamer 输出 | appsink + rtph264pay | fdsink fd=1 + Agent 解析 NALU | 评估正式版是否沿用进程隔离方案 |
| 信令服务 | Broker Gateway Service | Agent 内建 WebSocket | 正式版接入 Broker Gateway |
| 认证 | JWT + Session Token | 无认证（CORS 全开放） | 正式版接入 Broker 认证 |
| 进程管理 | 生命周期管理模块 | supervisord / entrypoint.sh supervise | 正式版增强，K8s 原生管理 |
| 音频系统 | PulseAudio | PipeWire + WirePlumber | 正式版沿用 PipeWire 方案 |

### 8.3 MVP 调试关键经验

#### 8.3.1 Pion WebRTC Lite ICE 注意事项

| 问题 | 说明 |
|------|------|
| SCTP DataChannel 单向 | Lite ICE 模式下 Agent→浏览器方向正常，浏览器→Agent 方向可能失败 |
| MediaEngine 自定义 | 必须自定义 MediaEngine 只注册目标编解码器（H.264 + Opus），否则浏览器可能协商到 VP8 |
| ontrack MSID 合并 | Pion 为 video/audio 创建不同 MSID，Client 必须合并到同一个 MediaStream |
| NAT 1:1 IP | 跨网段时需设置 `NAT_1TO1_IP`，否则 ICE 候选者中的 IP 不可达 |

#### 8.3.2 GStreamer Pipeline 要点

| 要点 | 说明 |
|------|------|
| x264enc profile | 不能通过属性设置，必须用 caps filter 指定 `profile=constrained-baseline` |
| 输入格式 | x264enc 前必须强制 I420 格式（`videoconvert ! video/x-raw,format=I420 !`） |
| 光标捕获 | `ximagesrc show-pointer=true`（不是 show-cursor），需要 XFixes 扩展 |
| NALU 解析 | 必须同时支持 3 字节和 4 字节起始码，以 AUD 为界组 AU |
| 进程隔离 | GStreamer 作为独立进程运行（`fdsink fd=1`），满足 LGPL 合规要求 |
| 多线程问题 | 容器内 x264enc 多线程初始化可能失败，添加 `threads=1` |

#### 8.3.3 容器运行与部署要点

| 要点 | 说明 |
|------|------|
| Xvfb 单一管理 | Xvfb 只能由 entrypoint.sh / supervisord 启动，Agent 不再重复启动 |
| Lock 文件清理 | 容器重启前 Xvfb 不会优雅退出，`/tmp/.X{N}-lock` 残留需主动清理 |
| 进程管理 | supervisord 管理所有子进程，崩溃后自动重启（`autorestart=true`） |
| `--network host` | WSL2 + Podman 下 bridge 模式端口映射可能不通，`--network host` 更可靠 |
| GNOME 不兼容容器 | GNOME Shell 强依赖 systemd-logind，容器内无法运行。XFCE/KDE 无此限制 |
| PipeWire 替代 PulseAudio | PipeWire + WirePlumber + pipewire-pulse 替代 PulseAudio，权限问题更少 |
| 虚拟显示扩展 | Xvfb 需启用 COMPOSITE/DAMAGE/GLX/RANDR/RENDER/MIT-SHM/XFIXES/XTEST + iglx |
| Zombie 回收 | `syscall.Wait4(-1, nil, 0, nil)` goroutine 持续回收子进程 |

### 8.4 MVP 当前数据流全景

```
┌─────────────────────────────────────────────────────────────┐
│                       浏览器                                  │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  <video> ← MediaStream (video + audio tracks merged) │   │
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
│  │  supervisord → Xvfb + PipeWire + XFCE 桌面          │   │
│  └─────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### 8.5 MVP 待解决事项

| 优先级 | 事项 | 说明 |
|--------|------|------|
| P1 | Opus 音频流端到端验证 | Pipeline 已配置，需验证 PulseAudio/PipeWire → Opus → Client 完整通路 |
| P2 | DataChannel 双向修复 | 升级 Pion 或调整 ICE 配置，恢复 DataChannel 双向通信 |
| P2 | 生产镜像构建 | 优化 Dockerfile 多阶段构建，减小镜像体积 |
| P2 | 断线重连 | WebSocket/WebRTC 断线后的自动重连机制 |
| P2 | 清理 debug 日志 | 移除 H264 Frame 计数、Raw msg.Data 等调试日志 |
| P3 | 输入方式优化 | CGo 直接调用 XTest 扩展，替代 xdotool 进程创建开销 |
| P3 | GStreamer 动态编码器选择 | 实现 `nvh264enc > vaapih264enc > x264enc` 的自动降级逻辑 |
| P3 | GPU 透传测试 | NVIDIA GPU 环境测试 VirtualGL + nvh264enc |
