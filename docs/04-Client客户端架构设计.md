# 云桌面客户端（Client）架构设计

---

## 1. 文档概述

### 1.1 编写目的

本文档定义云桌面客户端（Client）的技术架构、模块设计、协议规范与安全策略，为研发团队提供实现依据。

读完本文档，研发人员应能够：

- 明确 Web 客户端与 Windows Native 客户端的功能边界与技术选型
- 理解认证流程、WebRTC 建连流程与断线重连机制
- 依据 DataChannel 控制协议定义开始实现键鼠输入与剪贴板同步
- 了解水印渲染方案与安全设计要求

### 1.2 设计目标

| 目标 | 说明 |
|------|------|
| 多平台覆盖 | Web 优先，Windows Native 次之，移动端后续规划 |
| 代码最大复用 | Web 与 Native 共享同一套 React + TypeScript 业务逻辑层 |
| 低延迟交互 | WebRTC 媒体传输 + DataChannel 控制，端到端延迟目标见第 10 章 |
| 安全可控 | Token 内存持久化，Native 特权功能最小化，CSP 防注入 |
| Policy 驱动 | 功能开关、水印配置、剪贴板策略均由 Broker 下发，Client 不硬编码业务规则 |
| 可演进 | 关键模块（渲染层、DataChannel 编码、水印）预留升级路径，不锁定当前实现 |

### 1.3 文档范围

本文档覆盖以下内容：

- Web 客户端与 Windows Native 客户端的平台架构与功能边界
- 技术栈选型及共享层模块设计
- 认证与会话流程（登录、Token 管理、静默续签、登出）
- WebRTC 连接建立、媒体渲染与音频处理
- DataChannel 控制协议（消息帧格式、键鼠输入、剪贴板同步）
- 断线重连机制
- 可见水印实现方案
- 安全设计（Token 存储、特权功能边界、CSP）

本文档不覆盖以下内容：

- 移动端（Android / iOS）客户端设计，详见后续移动端专项设计文档
- 盲水印（DWT-DCT 频域嵌入）实现，详见后续水印专项设计文档
- Broker 控制平面设计，详见《云桌面 Broker 控制平面架构设计》
- Agent 媒体引擎设计（详见《Agent 架构设计》）
- 服务端部署与运维

---

## 2. 客户端定位与设计原则

### 2.1 客户端定位

Client 是云桌面平台的用户接入层，负责：

- 与 Broker 完成认证与会话建立
- 通过 WebRTC 接收桌面视频流并渲染至本地屏幕
- 通过 DataChannel 将本地键鼠输入实时传输至云桌面
- 在渲染画面上叠加可见水印
- 响应 Broker 推送的 Policy 配置，动态启用或禁用功能模块

Client 不包含任何业务编排逻辑，不直接操作 Kubernetes / KubeVirt 资源，所有业务决策由 Broker 负责。

```
┌─────────────────────────────────────────┐
│              Client 职责边界             │
│                                         │
│  认证 → 建连 → 渲染 → 输入传输 → 水印   │
│                                         │
│  ✗ 不做桌面调度    ✗ 不做资源管理        │
│  ✗ 不做业务规则    ✗ 不做 Policy 生成   │
└─────────────────────────────────────────┘
```

### 2.2 设计原则

**Policy 驱动，Client 不硬编码业务规则**

所有功能开关（USB、剪贴板、水印、音频等）均由 Broker 随 Session 创建响应下发，Client 依据 Policy 字段决定是否初始化对应模块。Client 代码中不出现基于用户角色、租户类型的本地判断逻辑。

**共享优先，差异最小化**

Web 与 Native 共享同一套 React + TypeScript 业务逻辑层，差异仅封装在宿主适配层（浏览器原生 API vs Tauri `invoke`）。新功能优先在共享层实现，仅无法在 Web 实现的特权能力才下沉至 Rust 层。

**内存安全，最小持久化**

Token、Session 状态等敏感数据仅存运行时内存，不写入任何持久化介质。Native 客户端中 Token 由 Rust 侧持有，不暴露至 WebView JS 层。

**渐进式演进，预留升级路径**

关键实现预留演进空间，不锁定当前方案：
- DataChannel 编码：JSON → MessagePack / Protobuf
- Native 渲染：WebView Canvas → Rust wgpu 原生渲染
- 水印方案：可见水印 → 叠加盲水印

**与 Broker 设计对齐**

断线重连策略、Token 结构、信令事件类型、Policy 字段命名均以 Broker 设计文档为准，Client 不自行定义与 Broker 交互的数据结构。

---

## 3. 平台架构与功能边界

### 3.1 客户端形态总览

云桌面客户端分三种形态：

- **Web 客户端**：运行于现代浏览器，零安装，覆盖轻量接入场景
- **Windows Native 客户端**：基于 Tauri 2.x + Rust，支持 USB 重定向、多显示器、底层键盘 Hook 等特权功能，覆盖重度生产力场景
- **移动端客户端**：计划采用 Tauri 2.x（支持 Android / iOS），详见后续移动端专项设计文档，本文档不展开

三种形态共享同一套 React + TypeScript 业务逻辑层（认证、信令、WebRTC、DataChannel 协议），差异仅在宿主环境与特权能力层。

客户端与后端的接入关系如下：

```
Client（Web / Native）
        ↓ HTTPS / WSS
    Ingress（TLS 终结 + 限流）
        ↓
    Broker Gateway Service（信令编排、Token 校验、SDP/ICE 中转）
        ↓ ICE 协商完成后
    Agent（WebRTC 媒体流，P2P 或经 Coturn 中转）
```

Broker 负责信令编排，媒体流在 Client 与 Agent 之间直接传输（P2P）或经 Coturn 中转。

**交付优先级：** Web 客户端优先交付，Windows Native 客户端次之，移动端后续单独规划。

---

### 3.2 Web 客户端

**运行环境**

支持现代主流桌面浏览器，最低版本要求：

| 浏览器 | 最低版本 |
|--------|---------|
| Chrome / Chromium | 90+ |
| Microsoft Edge | 90+ |
| Firefox | 90+ |
| Safari | 15+ |

无需安装任何插件或扩展，用户通过 URL 直接访问。

**渲染方案**

WebRTC 视频流采用双层渲染架构：

```
Agent（在桌面实例内）
    ↓ WebRTC H.264 视频流
<video> 元素（负责硬件解码，不显示）
    ↓ 每帧 drawImage()
Canvas 元素（负责渲染 + 水印叠加 + 显示）
```

`<video>` 元素隐藏，仅用于触发浏览器原生硬件解码；Canvas 负责最终合成渲染，可见水印叠加在 Canvas 层，截图也会包含水印内容。

**输入处理**

键盘、鼠标事件通过 WebRTC DataChannel 传输至桌面端，不依赖任何浏览器插件。具体协议格式见第 7 章。

**能力边界**

Web 客户端不支持以下功能，这些功能为 Native 客户端独占：

- USB 设备重定向
- 物理多显示器扩展
- 底层键盘 Hook（不支持拦截系统级快捷键，如 Win 键、Alt+Tab）

所有功能开关均由 Broker 下发的 Policy 控制，客户端依据 Policy 字段决定是否启用对应功能，不在本地硬编码判断逻辑。

**部署形式**

静态资源构建后部署至 CDN，支持后续扩展为 PWA（离线缓存壳，非离线桌面模式）。

---

### 3.3 Native 客户端（Tauri）

**运行环境**

| 项目 | 规格 |
|------|------|
| 操作系统 | Windows 10 / Windows 11（x64） |
| 框架 | Tauri 2.x |
| 前端层 | React + TypeScript（与 Web 端共享） |
| 后端层 | Rust |
| 包体积目标 | < 15 MB |

**渲染方案**

分两个阶段演进：

- **近期（当前阶段）**：复用 Web 端 Canvas 渲染方案，在 Tauri WebView 内运行，与 Web 端代码完全共享，快速交付
- **远期**：可迁移至 Rust 侧 `wgpu` 原生渲染，绕过 WebView 渲染层，获得更低延迟与更高帧率，Canvas 方案可平滑迁移；具体迁移时机根据实际性能表现与业务需求决策，无预设触发条件

**特权功能**

以下功能由 Rust 后端实现，仅 Native 客户端支持：

| 功能 | 实现方式 | 说明 |
|------|---------|------|
| 底层键盘 Hook | Rust 系统级 Hook | 拦截 Win 键、Alt+Tab 等系统快捷键，转发至桌面 |
| USB 设备重定向 | USBIP 协议 | 将本地 USB 设备映射至云桌面 |
| 物理多显示器扩展 | IDD 间接显示驱动 + 多轨 PTS 同步推流 | 支持多显示器独立画面 |

上述特权功能的启用均受 Broker Policy 控制，`usbEnabled`、`clipboardPolicy` 等字段由 Broker 随 Session 创建响应下发，客户端据此决定是否初始化对应功能模块。

**代码共享边界**

```
┌─────────────────────────────────────┐
│     React + TypeScript 共享层       │
│  认证 / 信令 / WebRTC / DataChannel  │
│  UI 组件 / 业务状态管理               │
├─────────────────┬───────────────────┤
│   Web 宿主层    │  Tauri 宿主层      │
│  浏览器原生 API  │  Rust 特权 API    │
│                 │  键盘 Hook        │
│                 │  USBIP           │
│                 │  IDD 多显示器      │
└─────────────────┴───────────────────┘
```

**安装与更新**

支持 Tauri 内置 Updater 机制，客户端启动时静默检查版本，后台下载增量更新包，下次启动时自动应用，用户无需手动操作。

---

### 3.4 功能差异矩阵

| 功能 | Web 客户端 | Windows Native | 移动端（规划中） | Policy 控制字段 |
|------|-----------|---------------|----------------|----------------|
| 桌面连接（WebRTC） | ✓ | ✓ | 规划中 | — |
| 键鼠输入 | ✓ | ✓ | 规划中 | — |
| 音频输出 | ✓ | ✓ | 规划中 | `audioOutputEnabled` |
| 音频输入（麦克风） | ✓ | ✓ | 规划中 | `audioInputEnabled` |
| 剪贴板同步 | ✓ | ✓ | 规划中 | `clipboardPolicy` |
| 可见水印 | ✓ | ✓ | 规划中 | `watermarkEnabled` |
| 文件拖拽传输 | ✗（后期规划） | ✓ | ✗ | `dragDropTransfer` |
| 本地磁盘映射 | ✗ | ✓ | ✗ | `localDiskMapping` |
| USB 设备重定向 | ✗ | ✓ | ✗ | `usbEnabled` |
| 物理多显示器 | ✗ | ✓ | ✗ | — |
| 底层键盘 Hook | ✗ | ✓ | ✗ | — |
| 摄像头重定向 | ✓（浏览器 getUserMedia） | ✓ | 规划中 | `cameraRedirection` |
| 打印机重定向 | ✗ | ✓ | ✗ | `printerRedirection` |
| 智能卡重定向 | ✗ | ✓ | ✗ | `smartCardRedirection` |
| 静默自动更新 | ✗（浏览器刷新） | ✓ | 规划中 | — |

> **说明**：Policy 控制字段均由 Broker 随 Session 创建响应下发，客户端不在本地判断功能可用性，所有开关以 Broker 下发值为准。

---

## 4. 技术栈选型

### 4.1 Web 客户端技术栈

| 层次 | 选型 | 版本要求 |
|------|------|---------|
| 核心框架 | React + TypeScript | React 18+，TypeScript 5+ |
| 状态管理 | Zustand | 4+ |
| WebRTC | 浏览器原生 WebRTC API | 无额外依赖 |
| 构建工具 | Vite | 5+ |
| 样式方案 | Tailwind CSS | 3+ |

### 4.2 Native 客户端技术栈

| 层次 | 选型 | 说明 |
|------|------|------|
| 宿主框架 | Tauri 2.x | Rust 后端 + WebView 前端 |
| 异步运行时 | tokio | Rust 异步任务调度 |
| 自动更新 | tauri-plugin-updater | 静默增量更新 |
| 键盘 Hook | `rdev` 或 `windows-rs` | 系统级键盘事件拦截 |
| USB 重定向 | USBIP 驱动 + Rust 封装 | 本地 USB 设备映射至云桌面 |
| 多显示器 | Windows IDD 框架 + Rust 调用 | 间接显示驱动，多轨推流 |
| 远期渲染 | wgpu（Rust） | 原生 GPU 渲染，替代 WebView Canvas |

### 4.3 共享层设计

Web 与 Native 共享同一套 TypeScript 模块，差异能力通过 Tauri `invoke` 桥接至 Rust 侧：

```
┌─────────────────────────────────────────────────┐
│               共享 TypeScript 层                 │
│                                                  │
│  AuthClient        — 认证、Token 管理、静默续签   │
│  SignalingClient   — WebSocket 信令、ICE 协商     │
│  ControlChannel    — DataChannel 协议封装         │
│  SessionManager    — Session 状态、断线重连        │
│  WatermarkLayer    — Canvas 水印叠加渲染           │
├────────────────────┬────────────────────────────┤
│   Web 宿主层       │      Tauri 宿主层            │
│                    │                             │
│  浏览器原生 API     │  tauri.invoke()            │
│  getUserMedia      │  ├─ keyboard_hook_start     │
│  RTCPeerConnection │  ├─ usb_redirect_attach     │
│  DataChannel       │  ├─ display_extend_init     │
│                    │  └─ updater_check           │
└────────────────────┴────────────────────────────┘
```

各模块职责：

- **AuthClient**：管理 Access Token 生命周期，内存持久化，定时触发静默续签，封装登录 / 登出 / 刷新接口
- **SignalingClient**：维护与 Broker 的 WebSocket 长连接，处理 ICE Candidate 交换、Session 事件订阅
- **ControlChannel**：封装 DataChannel 消息的序列化 / 反序列化，对上层暴露键鼠事件、剪贴板等高层接口
- **SessionManager**：维护 Session 状态机，处理断线检测与重连逻辑（详见第 8 章）
- **WatermarkLayer**：在 Canvas 渲染层叠加可见水印，水印参数由 Broker Policy 下发（详见第 9 章）

### 4.4 选型理由

**为什么用 Zustand 而不是 Redux**

WebRTC 连接状态、ICE 状态、Session 状态更新频繁且异步，Redux 的 Action → Reducer → Store 模式样板代码重，在高频状态更新场景下维护成本高。Zustand 直接暴露 store 的 set 方法，状态更新路径短，更适合 WebRTC 这类事件驱动场景。

**为什么用 Vite**

Tauri 官方推荐构建工具，HMR 热更新快，与 Tauri CLI 集成无缝，构建产物轻量，无需额外配置即可输出适合 WebView 加载的静态资源。

**为什么用 Tauri 而不是 Electron**

| 维度 | Tauri | Electron |
|------|-------|---------|
| 包体积 | < 15 MB | > 100 MB |
| 内存占用 | 低（复用系统 WebView） | 高（内置 Chromium） |
| 特权能力 | Rust 后端，可直接调用系统 API | Node.js，能力受限 |
| 安全模型 | 细粒度 capability 权限控制 | 相对宽松 |
| 协议 | MIT / Apache-2.0，闭源无障碍 | MIT，同样可闭源 |

Tauri 在包体积、内存占用和系统级特权能力（键盘 Hook、USBIP、IDD）上均优于 Electron，与项目需求高度契合。

---

## 5. 认证与会话流程

### 5.1 登录流程

用户通过用户名 + 密码登录，Client 向 Broker 发起认证请求：

```
POST /api/v1/auth/login
{
  "username": "user@example.com",
  "password": "...",
  "clientType": "web" | "tauri"
}
```

`clientType` 字段用于 Broker 差异化下发 Policy：`tauri` 类型可获得 USB 重定向、多显示器等特权功能的 Policy 授权，`web` 类型仅获得基础功能 Policy。

登录成功响应：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "accessToken": "<JWT>",
    "expiresIn": 1800,
    "user": {
      "id": "user_123",
      "username": "alice",
      "tenantId": "tenant_abc",
      "roles": ["user"]
    }
  }
}
```

Client 从 `data` 中提取 `accessToken` 存入内存，`user` 对象用于初始化 UI（用户名显示、权限判断等），无需额外调用用户信息接口。

完整登录流程：

```
Client                              Broker
  |                                   |
  |-- POST /api/v1/auth/login ------->|
  |                                   |-- 验证用户名密码
  |                                   |-- 校验租户状态（是否到期 / 黑名单）
  |                                   |-- 签发 Access Token（RS256，30min）
  |<-- 200 { accessToken, expiresIn } |
  |                                   |
  |（Access Token 存入内存，启动续签定时器）
```

**Token 存储原则：** Access Token 仅存储于运行时内存，不写入 `localStorage`、`sessionStorage`、系统 Keychain 或任何持久化介质，客户端关闭后 Token 随进程销毁，重新打开需重新登录。

---

### 5.2 Token 管理

客户端维护两类 Token，职责严格分离：

| Token 类型 | 用途 | 有效期 | 存储位置 |
|-----------|------|--------|---------|
| Access Token | REST API 鉴权（`Authorization: Bearer`） | 30 分钟 | 内存 |
| Session Token | WebSocket 信令鉴权 | 与 Session 生命周期一致 | 内存 |

Session Token 由 Broker 在桌面会话创建时下发，独立于 Access Token，不随 Access Token 过期而失效，确保用户使用桌面过程中不会因 Token 过期被中断。

**JWT Payload 结构：**

Access Token：
```json
{
  "sub": "user_123",
  "tenant_id": "tenant_abc",
  "roles": ["user"],
  "client_type": "tauri",
  "iat": 1700000000,
  "exp": 1700001800
}
```

Session Token：
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

---

### 5.3 Access Token 静默续签

Access Token 过期前 5 分钟，`AuthClient` 内置定时器自动触发续签，用户无感知：

```
Client                              Broker
  |                                   |
  |（剩余有效期 < 5 分钟，定时器触发）  |
  |-- POST /api/v1/auth/refresh ----->|
  |   Authorization: Bearer <当前 Token>
  |                                   |-- 验证当前 Token 有效
  |                                   |-- 签发新 Access Token
  |<-- 200 { accessToken } -----------|
  |（替换内存中旧 Token，重置定时器）   |
```

**续签失败重试策略：**

网络抖动导致续签失败时，`AuthClient` 采用指数退避重试：

| 重试次数 | 等待时间 | 动作 |
|---------|---------|------|
| 第 1 次失败 | 等待 2s | 自动重试 |
| 第 2 次失败 | 等待 4s | 自动重试 |
| 第 3 次失败 | 等待 8s | 自动重试 |
| 第 3 次仍失败 | — | 弹出"登录已过期"提示，跳转登录页，清除内存 Token |

由于续签在过期前 5 分钟触发，重试总耗时（14s）远小于剩余有效期，正常网络抖动场景下用户无感知。

---

### 5.4 登出流程

**主动登出**

用户手动点击登出，Client 执行以下步骤：

```
1. POST /api/v1/auth/logout（通知 Broker 使当前 Token 失效）
2. 清除内存中的 Access Token 与 Session Token
3. 关闭 WebSocket 信令连接
4. 关闭 WebRTC PeerConnection
5. 跳转至登录页
```

**被动强制下线**

Broker 通过 WebSocket 推送强制下线事件，Client 收到后立即执行登出流程（步骤 2-5），并根据事件类型向用户展示对应提示：

| 事件类型 | 触发场景 | 客户端提示文案 |
|---------|---------|-------------|
| `auth.admin_kicked` | 管理员在控制台手动踢出 | "您已被管理员强制下线" |
| `auth.tenant_expired` | 租户套餐到期 | "账户服务已到期，请联系管理员" |
| `auth.tenant_blacklisted` | 租户被列入黑名单 | "账户访问受限，请联系管理员" |
| `session.terminated` | 会话被系统终止 | "桌面会话已结束" |

所有强制下线事件处理完成后，Client 清除全部内存状态，跳转至登录页，不保留任何会话残留数据。

---

## 6. 连接建立与媒体渲染

### 6.1 建连总体流程

用户选择桌面并发起连接后，完整建连流程分为三个阶段：

```
阶段一：会话创建（Client ↔ Broker REST）
阶段二：ICE 协商（Client ↔ Broker WebSocket ↔ Agent）
阶段三：媒体建流（Client ↔ Agent，P2P 或经 Coturn 中转）
```

完整时序：

```
Client                        Broker                      Agent
  |                              |                            |
  |-- POST /api/v1/sessions ---->|                            |
  |   （请求创建桌面会话）         |                            |
  |                              |-- 分配桌面实例               |
  |-- Agent 位于桌面实例内 -----|                            |
  |                              |-- 签发 Session Token        |
  |<-- 200 {                     |                            |
  |     sessionToken,            |                            |
  |     sessionId,               |                            |
  |     wsUrl,                   |                            |
  |     iceServers,              |                            |
  |     policy                   |                            |
  |   } -----------------------  |                            |
  |                              |                            |
  |== 建立 WebSocket（携带 Session Token）===================> |
  |                              |                            |
  |-- SDP Offer （via WS）------>|                            |
  |                              |-- 转发 SDP Offer ---------->|
  |                              |<-- SDP Answer -------------|
  |<-- SDP Answer（via WS）------|                            |
  |                              |                            |
  |-- ICE Candidate（via WS）--->|-- 转发 Candidate ---------->|
  |<-- ICE Candidate（via WS）---|<-- 转发 Candidate ----------|
  |                              |                            |
  |========= WebRTC PeerConnection 建立完成 ==================>|
  |<========= 视频流 + 音频流 + DataChannel ==================|
```

---

### 6.2 WebRTC 连接建立

**ICE 协商通道**

ICE Candidate 交换复用 Broker WebSocket 信令通道，Broker 在 Client 与 Agent 之间转发 SDP 和 Candidate。PeerConnection 建立后媒体流直接在 Client 与 Agent 之间传输（P2P）或经 Coturn 中转，不经过 Broker。

**ICE 服务器配置**

`iceServers` 由 Broker 在会话创建响应中下发，Client 不硬编码任何 ICE 服务器地址：

```json
{
  "iceServers": [
    { "urls": "stun:stun.example.com:3478" },
    {
      "urls": "turn:turn.example.com:3478",
      "username": "user_123",
      "credential": "tmp_credential"
    }
  ]
}
```

**SDP 媒体方向**

Client 发起 SDP Offer 时，各媒体轨道方向如下：

| 媒体轨道 | 方向 | 说明 |
|---------|------|------|
| 视频（桌面画面） | `recvonly` | Client 只接收桌面推送的视频流 |
| 音频输出（扬声器） | `recvonly` | Client 只接收桌面音频 |
| 音频输入（麦克风） | `sendrecv` | 双向，Client 上行麦克风音频；`audioInputEnabled` 为 false 时不添加此轨道 |
| DataChannel `control` | — | Client 发起创建，label 为 `control`（全小写） |
| DataChannel `bulk` | — | Client 发起创建，label 为 `bulk`（全小写） |

DataChannel 由 Client 在创建 Offer 时主动发起，Agent 在 Answer 中确认。

**连接失败降级策略**

WebRTC 建连采用自动降级机制，按以下顺序尝试：

```
1. P2P 直连（ICE Host / Srflx Candidate）
        ↓ 超时或失败（默认 10s）
2. TURN 中转（Relay Candidate，经 Coturn）
        ↓ 超时或失败（默认 10s）
3. 建连彻底失败 → 向用户展示错误提示，提供"重试"按钮
```

降级过程对用户透明，仅在彻底失败时展示错误。TURN 凭证由 Broker 随会话创建下发，Client 无需额外请求。

**超时计时起点**：10s 超时从 WebSocket 连接建立成功时开始计算，而非从 SDP Offer 发出时算起。

**ICE 连接状态机**

`SignalingClient` 监听 `RTCPeerConnection` 的 `iceConnectionState` 变化，驱动 UI 状态展示：

| ICE 状态 | 含义 | UI 表现 |
|---------|------|--------|
| `checking` | 正在协商 | 展示"连接中..."加载态 |
| `connected` | P2P 直连成功 | 进入桌面画面 |
| `completed` | ICE 协商完全完成 | 维持桌面画面 |
| `failed` | 协商失败，触发降级 | 静默降级至 TURN |
| `disconnected` | 连接短暂中断 | 触发断线重连流程（见第 8 章） |

---

### 6.3 WebSocket 信令消息格式

WebSocket 连接地址与 Session Token 携带方式：

```
wss://<broker-host>/api/v1/signal?token=<sessionToken>
```

Session Token 通过 URL Query 参数传递，不使用 `Authorization` Header（WebSocket 握手阶段不支持自定义 Header）。

所有信令消息均为 JSON，统一帧结构：

```json
{
  "type": "<消息类型>",
  "payload": {}
}
```

**Broker 服务端推送事件（下行）**

| `type` | 触发时机 | `payload` 结构 |
|--------|---------|--------------|
| `session_state` | Session 状态变更 | `{ "state": "Connected" \| "Disconnected" \| "Closed" }` |
| `desktop_state` | Desktop 状态变更 | `{ "state": "Ready" \| "Error" \| "Recovering" }` |
| `session_replaced` | 被其他设备踢下线 | `{ "reason": "LOGIN_FROM_OTHER_DEVICE" }` |
| `ice` | Agent 下发 ICE Candidate（经 Broker 转发） | `{ "candidate": "..." }` |
| `answer` | Agent 回传 SDP Answer（经 Broker 转发） | `{ "sdp": "..." }` |
| `heartbeat` | Broker 保活心跳（30 秒间隔） | `{ "ts": 1234567890 }` |
| `error` | 错误事件 | `{ "code": 3004, "message": "...", "level": "Fatal", "action": "RECONNECT" }` |

**Client 上行消息（上行）**

| `type` | 说明 | `payload` 结构 |
|--------|------|--------------|
| `offer` | 发送 SDP Offer | `{ "sdp": "..." }` |
| `ice` | 上报 ICE Candidate | `{ "candidate": "..." }` |
| `heartbeat_ack` | 响应 Broker 心跳 | `{ "ts": 1234567890 }` |

**完整建连信令交互示例**

```
Client                          Broker                      Agent
  |                               |                            |
  |-- WS 连接 /api/v1/signal      |                            |
  |   ?token=<sessionToken> ----->|                            |
  |<-- WS 握手成功 ---------------|                            |
  |                               |                            |
  |-- { type: "offer",            |                            |
  |    payload: { sdp: "..." } }->|                            |
  |                               |-- 转发 SDP Offer ---------->|
  |                               |<-- SDP Answer -------------|
  |<-- { type: "answer",          |                            |
  |     payload: { sdp: "..." } } |                            |
  |                               |                            |
  |-- { type: "ice",              |                            |
  |    payload: { candidate } } ->|-- 转发 Candidate ---------->|
  |<-- { type: "ice",             |<-- 转发 Candidate ----------|
  |     payload: { candidate } }  |                            |
  |                               |                            |
  （WebRTC PeerConnection 建立完成）
```

---

### 6.4 视频渲染方案

**渲染架构**

WebRTC 视频轨道（H.264）采用双层渲染架构：

```
Agent（在桌面实例内，Pion WebRTC 输出）
      ↓ WebRTC H.264 视频流
  <video> 元素（隐藏，负责硬件解码）
      ↓ requestAnimationFrame() 每帧 drawImage()
  <canvas> 元素（负责合成渲染 + 水印叠加 + 用户可见）
```

- `<video>` 元素设置 `display: none`，仅触发浏览器 / WebView 原生硬件解码（充分利用 GPU 解码能力）
- `<canvas>` 负责最终合成渲染，可见水印叠加在 Canvas 层，截图 / 录屏均包含水印内容
- 使用 `requestAnimationFrame` 驱动帧循环，与显示器刷新率同步，避免撕裂

**分辨率适配**

Canvas 尺寸跟随容器自适应，支持 DPI 感知缩放：

```typescript
const dpr = window.devicePixelRatio || 1;
canvas.width = container.clientWidth * dpr;
canvas.height = container.clientHeight * dpr;
ctx.scale(dpr, dpr);
```

**Native 端渲染演进**

- **近期**：复用上述 Web Canvas 方案，在 Tauri WebView 内运行，与 Web 端代码完全共享
- **远期**：可迁移至 Rust 侧 `wgpu` 原生渲染，绕过 WebView 渲染层；具体迁移时机根据实际性能表现与业务需求决策，无预设触发条件

---

### 6.5 音频处理

**音频输出（扬声器）**

桌面音频通过 WebRTC 音频轨道传输，浏览器原生播放，无需额外处理。默认静音，用户主动解除静音后开始播放，避免页面首次加载自动播放被浏览器拦截。

**音频输入（麦克风）**

麦克风采集通过 `getUserMedia` 获取本地音频流，加入 WebRTC PeerConnection 作为上行音频轨道传输至桌面端。

启用前需向用户申请麦克风权限，权限被拒时展示引导提示，不静默失败。

**回声消除（AEC）**

优先使用浏览器内置 AEC，通过 `getUserMedia` 约束开启：

```typescript
const stream = await navigator.mediaDevices.getUserMedia({
  audio: {
    echoCancellation: true,
    noiseSuppression: true,
    autoGainControl: true,
    sampleRate: 48000
  }
});
```

浏览器内置 AEC 覆盖主流场景（Chrome / Edge 效果最佳）。Safari 支持有限，后续可按需引入 WebRTC 原生 AEC 处理管线作为补充。

**音频功能开关**

音频输入 / 输出均受 Broker Policy 控制：

| Policy 字段 | 默认值 | 说明 |
|------------|--------|------|
| `audioOutputEnabled` | `true` | 控制音频输出是否可用 |
| `audioInputEnabled` | `false` | 控制麦克风是否可用，默认关闭 |

Policy 为 `false` 时，Client 不初始化对应音频轨道，UI 入口置灰。

---

## 7. DataChannel 控制协议

### 7.1 协议设计原则

- **JSON 编码**：当前阶段使用 JSON 序列化，可读性高，便于调试；后期如出现性能瓶颈可整体迁移至 MessagePack 或 Protobuf，消息结构不变
- **双通道分离**：`control` 通道承载延迟敏感消息，`bulk` 通道承载大数据消息，两条通道互不阻塞
- **Fire-and-forget**：所有消息均不需要应用层 ACK，传输可靠性由 DataChannel 底层 SCTP 保证
- **统一消息帧**：所有消息共享同一帧结构，类型由 `type` 字段区分，便于统一解析入口
- **版本字段预留**：消息帧包含 `v` 版本字段，为后续协议升级保留空间

---

### 7.2 消息帧格式

所有 DataChannel 消息均采用以下统一 JSON 帧结构：

```json
{
  "v": 1,
  "type": "<消息类型>",
  "ts": 1700000000123,
  "seq": 1024,
  "payload": { }
}
```

字段说明：

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `v` | number | 是 | 协议版本号，当前为 `1` |
| `type` | string | 是 | 消息类型，见 7.3 节 |
| `ts` | number | 是 | 客户端发送时间戳，Unix 毫秒，用于端到端延迟统计 |
| `seq` | number | 是 | 单调递增序列号，每条通道独立计数，用于乱序检测与日志追踪 |
| `payload` | object | 是 | 消息体，结构因 `type` 而异，见各节定义 |

---

### 7.3 消息类型定义

**DataChannel 通道分配**

| 通道名 | 模式 | 承载消息类型 |
|--------|------|------------|
| `control` | 有序可靠 | 键盘输入、鼠标事件、心跳、功能控制指令 |
| `bulk` | 有序可靠 | 剪贴板同步、文件传输（后期） |

**消息类型总览**

| `type` 值 | 通道 | 方向 | 说明 |
|-----------|------|------|------|
| `input.mouse_move` | control | Client → Desktop | 鼠标移动 |
| `input.mouse_button` | control | Client → Desktop | 鼠标按键按下 / 释放 |
| `input.mouse_wheel` | control | Client → Desktop | 滚轮事件 |
| `input.key` | control | Client → Desktop | 键盘按键按下 / 释放 |
| `clipboard.push` | bulk | Client ↔ Desktop | 推送剪贴板内容（双向） |
| `clipboard.request` | bulk | Client → Desktop | 请求桌面当前剪贴板内容 |
| `ctrl.ping` | control | Client → Desktop | 心跳探测 |
| `ctrl.pong` | control | Desktop → Client | 心跳响应 |
| `ctrl.resize` | control | Client → Desktop | 通知桌面调整分辨率 |
| `input.touch` | control | Client → Desktop | 触摸事件（移动端，详见移动端专项设计） |

---

### 7.4 键鼠输入事件

**鼠标移动 `input.mouse_move`**

```json
{
  "v": 1,
  "type": "input.mouse_move",
  "ts": 1700000000123,
  "seq": 1024,
  "payload": {
    "x": 960,
    "y": 540,
    "display_id": 0
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `x` / `y` | number | 桌面坐标系下的绝对坐标，单位像素 |
| `display_id` | number | 多显示器场景下的目标显示器 ID，单显示器固定为 `0` |

**鼠标按键 `input.mouse_button`**

```json
{
  "v": 1,
  "type": "input.mouse_button",
  "ts": 1700000000123,
  "seq": 1025,
  "payload": {
    "button": "left",
    "action": "down",
    "x": 960,
    "y": 540,
    "display_id": 0
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `button` | string | `left` / `right` / `middle` |
| `action` | string | `down` / `up` |
| `x` / `y` | number | 点击时鼠标位置 |

**滚轮事件 `input.mouse_wheel`**

```json
{
  "v": 1,
  "type": "input.mouse_wheel",
  "ts": 1700000000123,
  "seq": 1026,
  "payload": {
    "delta_x": 0,
    "delta_y": -120,
    "x": 960,
    "y": 540
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `delta_x` / `delta_y` | number | 滚动量，像素单位，负值向上 / 向左 |

**键盘事件 `input.key`**

```json
{
  "v": 1,
  "type": "input.key",
  "ts": 1700000000123,
  "seq": 1027,
  "payload": {
    "code": "KeyA",
    "action": "down",
    "modifiers": ["ctrl", "shift"]
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `code` | string | 物理按键码，使用 W3C KeyboardEvent.code 规范，与语言 / 布局无关 |
| `action` | string | `down` / `up` |
| `modifiers` | string[] | 当前激活的修饰键列表：`ctrl` / `shift` / `alt` / `meta` |

> **说明**：使用 `code` 而非 `key`，确保跨键盘布局（QWERTY / AZERTY / 中文输入法）行为一致，桌面端负责将物理按键码映射为操作系统输入事件。

**心跳探测 `ctrl.ping` / `ctrl.pong`**

Client 每 5 秒发送一次 `ctrl.ping`，桌面端回复 `ctrl.pong`：

```json
// Client → Desktop
{
  "v": 1,
  "type": "ctrl.ping",
  "ts": 1700000000123,
  "seq": 100,
  "payload": { "ts": 1700000000123 }
}

// Desktop → Client
{
  "v": 1,
  "type": "ctrl.pong",
  "ts": 1700000000456,
  "seq": 100,
  "payload": { "ts": 1700000000123 }
}
```

`payload.ts` 回传 Client 发送时的时间戳，Client 可据此计算 DataChannel 往返延迟。连续 3 次（15 秒）未收到 `pong` 响应，`SessionManager` 判定连接异常并触发断线重连。

**分辨率调整 `ctrl.resize`**

```json
{
  "v": 1,
  "type": "ctrl.resize",
  "ts": 1700000000123,
  "seq": 1028,
  "payload": {
    "width": 1920,
    "height": 1080,
    "dpr": 2,
    "display_id": 0
  }
}
```

Client 窗口尺寸变化时发送，通知桌面端同步调整分辨率。`dpr` 为设备像素比，用于 HiDPI 场景下的清晰度适配。

---

### 7.5 剪贴板同步

剪贴板消息走 `bulk` 通道，支持纯文本与富文本（HTML）两种格式，优先使用富文本，降级至纯文本。

**推送剪贴板内容 `clipboard.push`**

Client → Desktop 与 Desktop → Client 方向共用同一消息结构：

```json
{
  "v": 1,
  "type": "clipboard.push",
  "ts": 1700000000123,
  "seq": 1,
  "payload": {
    "formats": ["text/plain", "text/html"],
    "data": {
      "text/plain": "Hello World",
      "text/html": "<b>Hello</b> World"
    }
  }
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `formats` | string[] | 本次推送包含的格式列表 |
| `data` | object | 各格式对应的内容，key 为 MIME 类型 |

**请求剪贴板内容 `clipboard.request`**

Client 主动请求桌面当前剪贴板：

```json
{
  "v": 1,
  "type": "clipboard.request",
  "ts": 1700000000123,
  "seq": 2,
  "payload": {}
}
```

桌面端收到后，回复一条 `clipboard.push` 消息。

**剪贴板安全控制**

剪贴板同步受 Broker Policy 字段 `clipboardPolicy` 控制：

| `clipboardPolicy` 值 | 说明 |
|---------------------|------|
| `readwrite` | Client ↔ Desktop 双向同步（默认） |
| `readonly` | 仅允许桌面 → Client 方向（Client 只读，不可推送） |
| `writeonly` | 仅允许 Client → 桌面方向（Client 只写，不可拉取） |
| `disabled` | 禁用剪贴板同步 |

Client 在初始化 `ControlChannel` 时读取 Policy 值，据此决定是否监听本地剪贴板变化事件及是否响应桌面端推送。

---

## 8. 断线重连机制

### 8.1 断线检测

Client 通过两个独立机制检测断线，任一触发即启动重连流程：

**机制一：WebSocket 连接事件**

监听 WebSocket `onclose` / `onerror` 事件，连接意外断开时立即触发重连。

**机制二：DataChannel 心跳超时**

`ctrl.ping` 每 5 秒发送一次，桌面端回复 `ctrl.pong`。Client 连续 3 次（15 秒）未收到 `pong` 响应，判定连接异常，主动关闭 WebSocket 并触发重连。

两种机制互为补充：WebSocket 层断线可被 `onclose` 即时感知；网络"假连接"（TCP 连接存活但数据不通）由心跳超时兜底检测。

同时，Broker 侧 WebSocket 断开后会推送 `session_state: Disconnected` 事件，Client UI 立即展示"连接中断，正在重连…"提示，不等待重连成功。

---

### 8.2 重连策略

**WebSocket 指数退避重连**

与 Broker 文档保持一致，Client 采用以下指数退避策略重建 WebSocket 连接：

| 重连次数 | 等待时间 | 动作 |
|---------|---------|------|
| 第 1 次 | 1s | 携带原 Session Token 重连 |
| 第 2 次 | 2s | 携带原 Session Token 重连 |
| 第 3 次 | 4s | 携带原 Session Token 重连 |
| 第 4 次 | 8s | 携带原 Session Token 重连 |
| 第 5 次及以后 | 16s（上限） | 携带原 Session Token 重连 |

重连期间 UI 持续展示"连接中断，正在重连…"及当前重连倒计时，用户可手动点击"立即重试"跳过等待。

**完整重连时序**

```
Client                              Broker
  |                                   |
  （WebSocket 断开 / 心跳超时）         |
  |                                   |-- Session → Disconnected
  |                                   |-- 启动 30s 断线超时计时器
  |                                   |
  （指数退避等待）                      |
  |                                   |
  |-- WS 重连 /api/v1/signal?token=<原 Session Token>
  |                                   |-- 验证 Session Token 有效
  |                                   |-- 查询 Session 状态 == Disconnected
  |                                   |-- Session → Connecting
  |<-- WS 握手成功 -------------------|
  |                                   |
  |-- WS: { type: "offer", sdp: "..." }（重新发起 ICE 协商）
  |                                   |-- 转发至 Agent
  |<-- WS: { type: "answer", sdp: "..." }
  |                                   |
  （重走 ICE 协商，重建 WebRTC PeerConnection）
  |                                   |-- Session → Connected
  |<-- WS: session_state: Connected --|
  |                                   |
  （画面恢复，UI 提示消失）
```

**Session 超时处理**

若断线超过 Policy 配置的超时时间（默认 30 秒，可由租户级 Policy `disconnectTimeoutSec` 覆盖），Broker 将 Session 置为 Closed，Client 重连时收到 `SESSION_EXPIRED` 错误：

```
Client 收到 SESSION_EXPIRED
      ↓
停止重连
      ↓
UI 展示"连接已断开，请重新发起连接"
      ↓
用户点击确认 → 跳转至桌面列表页（不清除 Access Token，无需重新登录）
```

**Broker 副本故障场景**

Broker 副本故障时，K8s 自动重建副本，Client 感知到 WebSocket 断开后走相同的指数退避重连流程，重连至新副本后携带原 Session Token，Broker 从 PostgreSQL 恢复 Session 状态，Client 无需感知副本切换。

---

### 8.3 状态恢复

重连成功（WebSocket 握手 + ICE 协商完成）后，Client 执行以下状态恢复步骤：

```
1. 重置 DataChannel（关闭旧通道，重建 control / bulk 两条通道）
2. 重置心跳定时器（清除旧计时器，重新启动 5s ping 周期）
3. 恢复 Canvas 渲染（新 PeerConnection 视频轨道绑定至 <video> 元素）
4. 重新发送 ctrl.resize（通知桌面端当前窗口尺寸，避免分辨率错位）
5. 请求剪贴板同步（发送 clipboard.request，恢复剪贴板状态）
6. UI 恢复正常态（隐藏重连提示）
```

**错误兜底处理**

Broker 通过 WebSocket 推送 `error` 事件，携带 `action` 字段指导 Client 行为：

| `action` 值 | Client 行为 |
|------------|-----------|
| `RETRY` | 自动重试当前操作 |
| `RECONNECT` | 重新走断线重连流程 |
| `RELOGIN` | 清除内存 Token，跳转登录页 |
| `CONTACT_ADMIN` | 展示"请联系管理员"提示，不自动重试 |

**多设备互斥场景**

同一用户在第二台设备发起连接时，Broker 推送 `session_replaced` 事件至当前 Client，Client 收到后：

```
1. 停止重连（非网络问题，重连无意义）
2. 展示"您已在其他设备登录"提示
3. 清除内存中的 Session Token
4. 跳转至桌面列表页
```

---

## 9. 水印实现方案

### 9.1 设计目标

- **泄密追溯**：水印包含用户身份信息，截图外泄后可快速定位来源
- **视觉威慑**：水印常驻显示，提醒用户当前操作处于受监控环境
- **灵活配置**：水印内容、样式、位置均由 Broker Policy 控制，支持租户级差异化配置
- **截图覆盖**：水印叠加在 Canvas 渲染层，截图 / 录屏均包含水印，不可通过截图工具绕过

本阶段仅实现客户端可见水印。盲水印（DWT-DCT 频域嵌入）作为后续阶段独立专项设计，本文档不展开。

---

### 9.2 可见水印实现

**渲染位置**

水印在 Canvas 合成阶段叠加，位于视频画面之上，作为独立图层渲染：

```
<video>（解码层，隐藏）
      ↓ drawImage()
Canvas 图层 1：视频画面
Canvas 图层 2：水印（每帧叠加）
      ↓
用户可见画面
```

每帧渲染时均重绘水印层，确保水印不可通过暂停画面或截取单帧方式规避。

**渲染实现**

`WatermarkLayer` 模块在 Canvas `requestAnimationFrame` 回调中执行水印绘制，与视频帧渲染同步：

```typescript
function renderFrame() {
  // 1. 清空 Canvas
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  // 2. 绘制视频帧
  ctx.drawImage(videoEl, 0, 0, canvas.width, canvas.height);
  // 3. 叠加水印
  WatermarkLayer.render(ctx, canvas.width, canvas.height, policy.watermark);
  // 4. 下一帧
  requestAnimationFrame(renderFrame);
}
```

**性能优化**

水印文字内容在 Policy 加载时预渲染为离屏 `OffscreenCanvas`，每帧直接 `drawImage` 复用，避免每帧重复计算文字布局与样式：

```typescript
// Policy 加载时预渲染，仅执行一次
const offscreen = WatermarkLayer.prerender(policy.watermark);

// 每帧复用
ctx.drawImage(offscreen, x, y);
```

---

### 9.3 水印内容与样式规范

**水印内容字段**

水印展示内容由 Broker Policy `watermark.fields` 配置，支持以下字段按需组合：

| 字段标识 | 展示内容 | 示例 |
|---------|---------|------|
| `username` | 用户名 | `张三` |
| `user_id` | 用户 ID | `user_123` |
| `desktop_id` | 桌面 ID | `desktop_001` |
| `client_ip` | 客户端 IP 地址 | `192.168.1.100` |
| `timestamp` | 当前时间戳 | `2026-06-06 10:30:00` |

`timestamp` 字段每分钟刷新一次，刷新时重新预渲染离屏 Canvas。

**水印样式 Policy 定义**

```json
{
  "watermark": {
    "enabled": true,
    "fields": ["username", "desktop_id", "client_ip", "timestamp"],
    "style": {
      "mode": "tile",
      "opacity": 0.15,
      "angle": -30,
      "fontSize": 14,
      "color": "#000000",
      "fontFamily": "sans-serif"
    },
    "position": {
      "type": "corner",
      "corner": "bottom-right",
      "offsetX": 20,
      "offsetY": 20
    }
  }
}
```

**样式字段说明**

| 字段 | 类型 | 说明 |
|------|------|------|
| `enabled` | boolean | 是否启用水印，`false` 时 `WatermarkLayer` 不执行任何渲染 |
| `fields` | string[] | 展示的内容字段列表，按顺序拼接为水印文本 |
| `style.mode` | string | 展示模式：`tile`（对角线平铺）/ `corner`（固定角落） |
| `style.opacity` | number | 透明度，范围 `0.0 ~ 1.0`，建议值 `0.1 ~ 0.2` |
| `style.angle` | number | 文字倾斜角度（度），仅 `tile` 模式生效，建议 `-30 ~ -45` |
| `style.fontSize` | number | 字号，单位 px |
| `style.color` | string | 文字颜色，十六进制色值 |
| `style.fontFamily` | string | 字体，默认 `sans-serif` |
| `position.type` | string | 位置类型：`tile` 模式固定为平铺，`corner` 模式需指定角落 |
| `position.corner` | string | 角落位置：`top-left` / `top-right` / `bottom-left` / `bottom-right` |
| `position.offsetX/Y` | number | 角落偏移量，单位 px |

**两种展示模式示意**

`tile`（对角线平铺）：
```
张三 desktop_001              张三 desktop_001
        张三 desktop_001              张三 desktop_001
                张三 desktop_001              张三 desktop_001
```

`corner`（固定角落）：
```
┌─────────────────────────────┐
│                             │
│         桌面画面             │
│                             │
│              张三 desktop_001│
│         192.168.1.100       │
│      2026-06-06 10:30:00    │
└─────────────────────────────┘
```

**Client IP 获取**

`client_ip` 字段由 Broker 在 Session 创建时从请求来源 IP 获取，随 Session Policy 下发至 Client，Client 直接使用下发值渲染，不在本地自行获取。

---

## 10. 性能目标

> 本章指标当前为 TBD，待性能测试阶段根据实测数据填入。以下为设计参考方向，不作为当前验收标准。

### 10.1 端到端延迟

端到端延迟定义为：用户产生键鼠输入 → DataChannel 传输 → 桌面处理 → 视频编码 → WebRTC 传输 → Client 解码渲染的完整链路延迟。

| 网络环境 | 目标延迟 | 说明 |
|---------|---------|------|
| 局域网（< 1ms RTT） | TBD | 理想环境基线 |
| 城域网（< 10ms RTT） | TBD | 主要使用场景 |
| 广域网（< 50ms RTT） | TBD | 远程接入场景 |

### 10.2 帧率目标

| 场景 | 目标帧率 | 说明 |
|------|---------|------|
| 办公场景（文档、浏览器） | TBD | 低动态画面 |
| 开发场景（IDE、终端） | TBD | 中动态画面 |
| 图形场景（视频、设计工具） | TBD | 高动态画面 |

### 10.3 带宽消耗基线

| 场景 | 目标带宽 | 编码参数 |
|------|---------|---------|
| 1080p 办公 | TBD | H.264，码率 TBD |
| 1080p 图形 | TBD | H.264，码率 TBD |
| 4K 办公 | TBD | H.264，码率 TBD |

### 10.4 性能测试计划

性能指标将在以下阶段采集：

- **阶段一**：Web 客户端单机压测，采集局域网基线数据
- **阶段二**：Native 客户端对比测试，与 Web 端渲染性能对比
- **阶段三**：多并发用户压测，采集带宽与服务端负载数据

测试完成后本章指标更新为实测值，并标注测试环境与测试工具。

---

## 11. 安全设计

### 11.1 Token 安全存储

**存储原则**

Access Token 与 Session Token 均仅存储于运行时内存，不写入任何持久化介质：

| 存储位置 | 是否允许 | 说明 |
|---------|---------|------|
| 运行时内存 | ✓ | 唯一允许的存储位置 |
| `localStorage` / `sessionStorage` | ✗ | 可被同源 JS 读取，存在 XSS 风险 |
| 系统 Keychain / Credential Store | ✗ | Token 短期有效（30 分钟），持久化无必要 |
| 磁盘文件 | ✗ | 明文落盘风险 |

客户端关闭后 Token 随进程销毁，重新打开需重新登录。

**Native 客户端：Rust 侧代持 Token**

Tauri Native 客户端中，Token 由 Rust 后端持有，不暴露给 WebView JS 层。所有需要鉴权的 REST API 请求通过 `tauri.invoke` 代理发出，JS 层只传递请求参数，不接触 Token 字符串：

```
WebView JS 层                    Rust 后端
     |                               |
     |-- tauri.invoke("api_request", |
     |   { method, path, body }) --->|
     |                               |-- 从 Rust 内存读取 Token
     |                               |-- 发起 HTTP 请求（携带 Authorization Header）
     |                               |-- 返回响应数据
     |<-- { data } -----------------|
```

这样即使 WebView 层遭受 XSS 攻击，攻击者也无法直接获取 Token 字符串。

**Web 客户端**

Web 客户端 Token 存储于 JS 模块级变量（`AuthClient` 私有状态），不挂载到 `window` 对象，减少全局访问面：

```typescript
// AuthClient 内部，不对外暴露 token 变量
let accessToken: string | null = null;
let sessionToken: string | null = null;

// 对外只暴露方法，不暴露 token 本身
export const AuthClient = {
  getAuthHeader: () => ({ Authorization: `Bearer ${accessToken}` }),
  refresh: async () => { /* ... */ },
  logout: () => { accessToken = null; sessionToken = null; }
};
```

---

### 11.2 Native 特权功能安全边界

**键盘 Hook 作用域限制**

底层键盘 Hook 仅在 VDI 窗口处于前台焦点时激活，窗口失焦时自动停用，避免捕获用户在其他应用中的输入（如密码、银行账号等）：

```
VDI 窗口获得焦点 → Rust 侧启动键盘 Hook
VDI 窗口失去焦点 → Rust 侧立即停用键盘 Hook
```

Rust 侧通过 `rdev` 监听窗口焦点事件驱动 Hook 生命周期，Hook 作用域严格限定在 VDI 窗口内，不全局拦截系统键盘事件。

**USB 设备类型过滤**

USB 重定向设备范围由 Broker Policy 控制，按 USB 设备类型（Class）过滤：

| Policy 字段 | 类型 | 说明 |
|------------|------|------|
| `usbEnabled` | bool | USB 重定向总开关，`false` 时完全禁用 |
| `usbAllowedClasses` | string[] | 允许的 USB 设备类型白名单，如 `["HID", "Audio"]` |
| `usbBlockedClasses` | string[] | 禁止的 USB 设备类型黑名单 |

Client 通过 USB 设备的 Class Code（从设备描述符读取）与 `usbAllowedClasses` / `usbBlockedClasses` 比对，决定是否允许重定向：

| 设备类型标识 | USB Class Code | 典型设备 |
|-------------|---------------|---------|
| `HID` | 0x03 | 键盘、鼠标、手写板 |
| `Audio` | 0x01 | 音频设备、麦克风 |
| `MassStorage` | 0x08 | U 盘、移动硬盘 |
| `SmartCard` | 0x0B | 智能卡读卡器 |
| `Printer` | 0x07 | 打印机 |
| `Video` | 0x0E | 摄像头 |

过滤规则：
- `usbEnabled = false` → 禁止所有 USB 设备重定向
- `usbAllowedClasses` 非空 → 仅允许列表中的设备类型
- `usbBlockedClasses` 非空 → 禁止列表中的设备类型
- 两者同时存在时，先检查白名单再检查黑名单

白名单由管理员在 Broker 控制台配置，Client 不允许本地修改。

**IDD 多显示器驱动安全**

IDD（Indirect Display Driver）驱动需满足以下安全要求：

- 驱动须通过 Windows WHQL（Windows Hardware Quality Labs）签名，未签名驱动拒绝安装
- 驱动安装需管理员权限，Client 安装包内置驱动，安装时一次性申请提权
- 运行期间驱动以最低必要权限运行，不开放额外系统接口

---

### 11.3 内容安全策略（CSP）

Web 客户端服务端响应头配置以下 CSP，防止 XSS 注入影响 WebRTC 与 DataChannel：

```
Content-Security-Policy:
  default-src 'self';
  connect-src 'self' wss://*.example.com https://*.example.com;
  media-src 'self' blob:;
  script-src 'self';
  style-src 'self' 'unsafe-inline';
  img-src 'self' data:;
  frame-ancestors 'none';
  base-uri 'self';
  form-action 'self'
```

各指令说明：

| 指令 | 值 | 作用 |
|------|-----|------|
| `default-src` | `'self'` | 默认仅允许同源资源 |
| `connect-src` | `'self' wss://*.example.com` | WebSocket 与 REST API 仅允许连接受信域名，上线前替换通配符为实际域名 |
| `media-src` | `'self' blob:` | 允许 WebRTC 媒体流（`blob:` URL） |
| `script-src` | `'self'` | 禁止内联脚本与 `eval`，防止代码注入 |
| `style-src` | `'self' 'unsafe-inline'` | 允许内联样式（React 组件需要），后续可迁移至 CSS Modules 去除 `unsafe-inline` |
| `frame-ancestors` | `'none'` | 禁止被任何页面以 iframe 嵌套，防止 Clickjacking |
| `base-uri` | `'self'` | 防止 `<base>` 标签劫持相对路径 |
| `form-action` | `'self'` | 限制表单提交目标，防止数据外泄 |

> **注意**：`connect-src` 中的 `*.example.com` 为占位符，上线前需替换为实际 Broker 与 Agent（通过桌面实例域名或 Broker 代理）的连接域名。

---
