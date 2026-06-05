# 云桌面 Broker 控制平面架构设计

## 1. 文档概述

### 1.1 编写目的

本文档用于定义云桌面 Broker 的架构定位、职责边界、核心领域模型以及生命周期管理机制，为后续系统研发、接口设计、数据库设计及运维体系建设提供统一设计依据。

### 1.2 设计目标

构建面向 Kubernetes + KubeVirt 云桌面平台的统一业务控制平面，实现：

* 云桌面生命周期管理
* 用户会话管理
* 资源调度决策
* 连接编排管理
* 多租户资源隔离
* 自动化运维与 AI Agent 集成

---

# 2. Broker 架构定位

## 2.1 系统定位

Broker 是云桌面平台的业务控制平面（Business Control Plane）。

Broker 负责将 Kubernetes、KubeVirt、Longhorn、Kube-OVN 等基础设施能力抽象为统一的云桌面服务能力，对外提供标准化的桌面管理接口。

Broker 不直接管理底层资源运行，而是负责业务对象管理与资源编排决策。

---

## 2.2 架构原则

### 控制面与数据面分离

Broker 仅负责业务控制逻辑。

媒体流、存储流和计算资源运行由数据面组件负责。

### 业务模型与基础设施解耦

Broker 管理：

* Desktop
* Session
* Tenant
* User

而非：

* VM
* Pod
* PVC

### 云原生设计

所有资源均通过 Kubernetes API 驱动。

### 无状态水平扩展

Broker 支持多副本部署，信令推送通过 Redis Pub/Sub 同步。

### AI Agent 原生兼容

所有智能运维能力通过标准 API 接入 Broker。

---

# 3. 职责边界

## 3.1 Broker 负责

### 身份与权限管理

负责：

* 用户认证
* 单点登录（SSO）
* LDAP / AD 集成
* OIDC/OAuth2
* RBAC 权限管理
* 多租户隔离

---

### Desktop 生命周期管理

负责：

* 创建桌面
* 启动桌面
* 停止桌面
* 重启桌面
* 销毁桌面
* 自动回收

---

### Session 生命周期管理

负责：

* Session 创建
* Session 断线重连
* Session 回收
* Session 审计

---

### 调度决策

负责：

* 资源池选择
* 集群选择
* 节点选择
* GPU 资源分配策略
* 调度策略执行

---

### 连接编排

负责：

* Token 签发
* ICE 信息分发
* TURN 信息下发
* Session 鉴权

---

### 审计与运维入口

负责：

* 用户审计
* 会话审计
* 操作审计
* 告警事件聚合

---

## 3.2 Broker 不负责

### 媒体处理

不负责：
* 音频编码
* WebRTC 媒体流传输
* 屏幕采集

由媒体层组件负责。

---

### 虚拟化运行

不负责：

* VM 启动
* Pod 启动
* 存储挂载
* 网络配置

由 Kubernetes、KubeVirt、Longhorn、Kube-OVN 实现。

---

### 指标采集

不负责：

* Metrics 采集
* Metrics 存储
* 告警规则计算

由 Prometheus 体系负责。

---

# 4. Broker 子服务架构

## 4.1 子服务拆分原则

Broker 控制平面按以下原则拆分子服务：

**按变更频率拆分**：调度算法、连接编排逻辑迭代频繁，独立后可单独升级不影响核心桌面管理。

**按扩展维度拆分**：Gateway Service 持有 WebSocket 长连接，扩展方式与无状态 REST 服务不同，需独立水平扩展。

**按故障隔离拆分**：Event Center 处理异步告警链路，故障时不应影响核心桌面管理；Audit Service 写入量大，不应争抢核心服务资源。

**不过度拆分**：用户、租户、配额、策略管理变更频率低，合并进 Desktop Service，不单独拆分。

---

## 4.2 子服务清单

```
Broker 控制平面
    ├── Desktop Service     桌面 & 会话生命周期管理
    ├── Scheduler Service   资源调度决策
    ├── Gateway Service     连接编排（Token / ICE / TURN / WebSocket 信令）
    ├── Monitor Service     业务指标聚合、Agent 心跳、状态巡检
    ├── Event Center        告警事件处理（通知 / 工单 / 自愈 / Kagent）
    └── Audit Service       审计日志写入与查询
```

---

## 4.3 各子服务职责详述

### Desktop Service

核心业务服务，Broker 的主要对外接口。

负责：
- 桌面生命周期管理（创建、启动、停止、重启、销毁）
- 会话生命周期管理（创建、断线重连、关闭）
- 用户、租户、配额、策略管理
- 调度请求发起（发布消息到 NATS，等待 Scheduler 回写）
- 多设备互斥控制
- 对外暴露 REST API（`/api/v1/`）

---

### Scheduler Service

资源调度决策服务，不对外暴露 API。

负责：
- 订阅 NATS 调度请求消息
- 资源池选择（CPU Pool / GPU Pool / Dedicated Pool）
- 集群与节点选择
- GPU 资源分配策略
- 调度结果回写 Desktop Service（REST 回调）
- 调度失败通知 Desktop Service

---

### Gateway Service

连接编排服务，持有所有客户端 WebSocket 长连接。

负责：
- JWT 签发与校验
- Session Token 签发
- TURN Server 凭证下发
- WebSocket 信令通道管理（SDP / ICE 转发）
- Redis Pub/Sub 多副本 WebSocket 推送同步
- 客户端心跳保活

---

### Monitor Service

业务监控服务，弥补 Prometheus 无法感知业务语义的不足。

负责：
- 接收 Desktop Agent 主动 Push 的业务指标
- Agent 心跳管理（检测超时，触发告警）
- Desktop State 与 K8s 实际状态一致性巡检
- 业务维度 Prometheus metrics 暴露（桌面在线数、Session 并发数、GPU 使用率等）
- 发现异常后发布事件到 NATS → Event Center

---

### Event Center

异步告警事件处理中枢，作为 Alertmanager 的统一下游。

负责：
- 接收 Alertmanager Webhook 推送
- 接收 Monitor Service / Desktop Service 发布的内部事件
- 按策略路由事件到以下处置链路：
  - **通知**：钉钉 / 企业微信 / 邮件
  - **工单**：自动创建运维工单
  - **自愈**：
    - Broker 内置自愈规则（规则驱动，重试机制见第 11 章）
    - 超过最大重试次数 / Fatal Error → 升级 Kagent 处置

---

### Audit Service

审计日志服务，独立处理高写入量的审计数据。

负责：
- 订阅 NATS 审计消息，异步写入 PostgreSQL
- 提供审计日志查询接口
- 日志分区管理（按月分区，见第 10 章）
- Kagent 处置报告写入

---

## 4.4 通信模式

### 同步 REST

适用于：需要立即获得结果的调用。

```
客户端 → Gateway Service → Desktop Service（查询桌面是否 Ready）
Scheduler Service → Desktop Service（调度结果回写）
Event Center → Broker MCP Server → Broker API（Kagent 处置动作执行）
```

### 异步 NATS JetStream

适用于：状态变更事件、不需要立即响应的操作。

```
Desktop Service   → NATS → Scheduler Service（调度请求）
Desktop Service   → NATS → Audit Service（操作审计）
Desktop Service   → NATS → Event Center（桌面异常事件）
Monitor Service   → NATS → Event Center（Agent 超时、状态不一致事件）
Gateway Service   → NATS → Audit Service（Session 审计）
```

**选择原则：**

| 场景 | 通信方式 | 理由 |
|------|----------|------|
| 用户请求需要立即返回结果 | REST | 同步等待，用户感知延迟 |
| 调度结果回写 | REST 回调 | Scheduler 完成后主动通知，Desktop Service 更新状态 |
| 状态变更事件通知 | NATS | 发布者不关心消费者处理结果 |
| 审计日志写入 | NATS | 异步写入，不阻塞核心链路 |
| 告警事件处理 | NATS | 异步处理，故障隔离 |

---

## 4.5 服务间完整调用关系

```
客户端（Tauri / Web）
        ↓ HTTPS / WSS
      Ingress（TLS 终结 + 限流）
        ↓
  ┌─────────────────────────────────────┐
  │           REST                      │
  ├──→ Desktop Service ─────────────────┤
  │         │ NATS                      │
  │         ├──→ Scheduler Service      │
  │         │       │ REST 回调         │
  │         │       └──→ Desktop Service│
  │         │                           │
  │         ├──→ Audit Service（NATS）  │
  │         └──→ Event Center（NATS）   │
  │                   │                 │
  │              ┌────┴────┐            │
  │           通知  工单   自愈          │
  │                        │            │
  │               内置自愈规则           │
  │                        │ 升级        │
  │                     Kagent          │
  │                                     │
  ├──→ Gateway Service ─────────────────┤
  │         │ REST                      │
  │         ├──→ Desktop Service        │
  │         │       （验证桌面状态）     │
  │         ├──→ NATS → Audit Service   │
  │         └──→ Redis Pub/Sub          │
  │                   （多副本 WS 同步） │
  └─────────────────────────────────────┘

  Desktop Agent
        ↓ Push
  Monitor Service
        ↓ NATS
  Event Center
```

---

## 4.6 关键链路时序

### 创建桌面

```
客户端                Desktop Service         NATS          Scheduler Service
  |                         |                  |                    |
  |-- POST /api/v1/desktops→|                  |                    |
  |                         |-- 创建 Desktop   |                    |
  |                         |   state=Assigned |                    |
  |<-- 200 { desktop_id }   |                  |                    |
  |                         |-- PUBLISH ------->|                    |
  |                         |   schedule.request|-- 订阅消息 ------->|
  |                         |                  |                    |-- 选择资源池
  |                         |                  |                    |-- 选择节点
  |                         |<-- REST 回调 -----|--------------------| 
  |                         |   调度结果        |                    |
  |                         |-- 更新 Desktop   |                    |
  |                         |   state=Provisioning                  |
  |                         |-- 调用 K8s API 创建 VM/Pod            |
  |                         |-- PUBLISH audit → Audit Service       |
```

---

### 用户连接桌面

```
客户端          Gateway Service       Desktop Service      Redis
  |                   |                     |               |
  |-- POST /sessions →|                     |               |
  |                   |-- REST: 验证桌面 -->|               |
  |                   |<-- Desktop Ready ---|               |
  |                   |-- 签发 Session Token|               |
  |                   |-- 查询 TURN 凭证    |               |
  |<-- 200 { token, turnCredential, signalUrl }             |
  |                   |                     |               |
  |-- WS 握手 ------->|                     |               |
  |                   |-- SUBSCRIBE ------->|               |
  |<-- WS 建连成功    |   session:{id}      |               |
  |                   |                     |               |
  |-- WS: offer ----->|-- 转发 → Media Gateway              |
  |<-- WS: answer ----|<-- 转发 ← Media Gateway             |
  |  （ICE 协商完成）  |                     |               |
  |                   |-- REST: Session → Connected → Desktop Service
  |                   |-- PUBLISH audit → Audit Service     |
```

---

### 告警自愈链路

```
Monitor Service     NATS          Event Center      Desktop Service     Kagent
      |               |                |                  |               |
      |（Agent 心跳超时）              |                  |               |
      |-- PUBLISH ---->|               |                  |               |
      |  alert.desktop |-- 推送 ------->|                  |               |
      |                |               |-- 内置自愈规则    |               |
      |                |               |-- REST: restart ->|               |
      |                |               |<-- 重启中 --------|               |
      |                |               |（等待重试结果）   |               |
      |                |               |                  |               |
      |（若超过最大重试次数 / Fatal Error）                 |               |
      |                |               |-- 调用 Kagent Agent Invoke API -->|
      |                |               |                  |               |-- 根因分析
      |                |               |                  |               |-- 处置决策
      |                |               |                  |<-- REST -------|
      |                |               |                  |  （执行处置）  |
      |                |               |-- PUBLISH audit → Audit Service   |
```

---

### Agent 心跳上报

```
Desktop Agent       Monitor Service      NATS         Event Center
      |                   |               |                |
      |-- POST /heartbeat→|               |               |
      |   {               |               |               |
      |     desktopId,    |-- 更新心跳时间 |               |
      |     agent: true,  |-- 检查状态一致性              |
      |     desktopService,|              |               |
      |     captureService,|              |               |
      |     loginReady    |               |               |
      |   }               |               |               |
      |<-- 200            |               |               |
      |                   |               |               |
      |  （60s 无心跳）    |               |               |
      |                   |-- PUBLISH ---->|               |
      |                   |  alert.desktop |-- 推送 ------->|
      |                   |  heartbeat    |               |-- 触发自愈链路
      |                   |  timeout      |               |
```

---

## 4.7 各子服务部署规格建议

| 子服务 | 最小副本数 | 扩展维度 | 资源建议（单副本） |
|--------|-----------|----------|-------------------|
| Desktop Service | 2 | 按 API QPS 水平扩展 | 2 Core / 4 GB |
| Scheduler Service | 2 | 按调度请求并发量扩展 | 2 Core / 2 GB |
| Gateway Service | 3 | 按 WebSocket 连接数扩展 | 2 Core / 4 GB |
| Monitor Service | 2 | 按 Agent 数量扩展 | 2 Core / 2 GB |
| Event Center | 2 | 按告警事件量扩展 | 1 Core / 2 GB |
| Audit Service | 2 | 按审计写入量扩展 | 2 Core / 4 GB |

Gateway Service 副本数建议不低于 3，保证 WebSocket 连接在单副本故障时仍有足够容量承接重连。

---

## 4.8 NATS Topic 规范

### 命名约定

```
{domain}.{entity}.{action}
```

示例：

| Topic | 说明 | 生产者 | 消费者 |
|-------|------|--------|--------|
| `schedule.request` | 调度请求 | Desktop Service | Scheduler Service |
| `schedule.result` | 调度结果回调 | Scheduler Service | Desktop Service |
| `alert.desktop.error` | 桌面异常告警 | Monitor Service / Desktop Service | Event Center |
| `alert.desktop.heartbeat_timeout` | Agent 心跳超时 | Monitor Service | Event Center |
| `audit.desktop` | 桌面操作审计 | Desktop Service | Audit Service |
| `audit.session` | 会话操作审计 | Gateway Service | Audit Service |
| `audit.kagent` | Kagent 处置审计 | Event Center | Audit Service |

### 持久化策略

所有 Topic 均使用 NATS JetStream 持久化，配置如下：

| Topic 类型 | 保留策略 | 最大保留时间 | 说明 |
|------------|----------|-------------|------|
| `schedule.*` | Limits | 1 小时 | 调度消息处理完即无价值 |
| `alert.*` | Limits | 24 小时 | 保留足够时间供重试消费 |
| `audit.*` | Limits | 7 天 | 保证 Audit Service 故障恢复后可补偿消费 |

### 消费者策略

所有消费者采用 **Queue Group** 模式，保证同一消息在多副本部署下只被处理一次：

```
audit.desktop → Audit Service Queue Group（多副本竞争消费，消息不重复处理）
alert.desktop.error → Event Center Queue Group
```

# 5. 核心领域模型

## 5.1 总体模型

```text
Tenant
│
├── User
│
├── Policy
│
├── ResourcePool
│
└── DesktopInstance
          │
          ├── Template
          │
          └── Session
```

---

## 5.2 Tenant

租户对象。

负责资源归属管理。

---

## 5.3 User

用户对象。

负责身份映射与权限管理。

---

## 5.4 DesktopTemplate

桌面模板。

定义：

* CPU
* Memory
* GPU
* Image
* Storage
* Network

等标准规格。

---

## 5.5 DesktopInstance

DesktopInstance 是平台交付给用户的长期资产（Asset）。

DesktopInstance 具有：

* 固定身份
* 固定资源
* 固定数据
* 长期生命周期

Broker 的核心管理对象为 DesktopInstance，而非 VM 或 Pod。

---

## 5.6 Session

Session 是用户与 Desktop 之间的一次连接。

Session 生命周期独立于 Desktop 生命周期。

同一 Desktop 可关联多个历史 Session。

---

## 5.7 ResourcePool

资源池对象。

Broker 调度资源池，而非直接调度节点。

支持：

* CPU Pool
* GPU Pool
* Dedicated Pool
* AI Pool

等资源类型。

---

## 5.8 Policy

策略对象，定义桌面的外设重定向、会话行为和安全管控规则。

### 绑定层级

Policy 支持两级绑定：

```
TenantPolicy（租户默认策略）
    ↓ 继承
DesktopPolicy（桌面级覆盖策略，仅可覆盖部分字段）
```

桌面级策略优先于租户级策略生效。桌面级只能在租户级允许的范围内调整，不能突破租户级设置的安全边界。

### 字段定义

**外设重定向策略**

| 字段 | 类型 | 覆盖层级 | 说明 |
|------|------|----------|------|
| `usbEnabled` | bool | 仅租户级 | USB 整体开关 |
| `usbAllowedClasses` | []string | 仅租户级 | USB 设备类型白名单，如 `["HID","Audio"]` |
| `usbBlockedClasses` | []string | 仅租户级 | USB 设备类型黑名单 |
| `clipboardPolicy` | string | 桌面级可覆盖 | `disabled` / `readonly` / `writeonly` / `readwrite` |
| `printerRedirection` | bool | 桌面级可覆盖 | 打印机重定向开关 |
| `audioInputEnabled` | bool | 桌面级可覆盖 | 麦克风重定向开关 |
| `audioOutputEnabled` | bool | 桌面级可覆盖 | 扬声器重定向开关 |
| `cameraRedirection` | bool | 桌面级可覆盖 | 摄像头重定向开关 |
| `smartCardRedirection` | bool | 仅租户级 | 智能卡重定向开关 |

**会话策略**

| 字段 | 类型 | 覆盖层级 | 说明 |
|------|------|----------|------|
| `disconnectTimeoutSec` | int | 桌面级可覆盖 | 断线重连超时，默认 30 秒 |
| `idleLockScreenSec` | int | 仅租户级 | 空闲锁屏时间，默认 600 秒 |
| `idleShutdownSec` | int | 桌面级可覆盖 | 空闲自动关机时间，0 表示不关机 |
| `maxSessionDurationSec` | int | 仅租户级 | 最大 Session 时长，0 表示不限制 |

**安全策略**

| 字段 | 类型 | 覆盖层级 | 说明 |
|------|------|----------|------|
| `screenshotDisabled` | bool | 仅租户级 | 禁止截屏 |
| `screenRecordDisabled` | bool | 仅租户级 | 禁止录屏 |
| `watermarkEnabled` | bool | 仅租户级 | 强制水印开关 |
| `watermarkTemplate` | string | 仅租户级 | 水印内容模板，支持 `{username}` `{datetime}` 变量 |
| `localDiskMapping` | bool | 桌面级可覆盖 | 本地磁盘映射开关 |
| `dragDropTransfer` | bool | 仅租户级 | 拖拽文件传输开关 |

---

## 5.9 UserQuota

用户配额对象，限制单用户在租户内的资源使用上限。

UserQuota 从属于 Tenant，约束单个 User 的资源使用边界。

| 字段 | 类型 | 说明 |
|------|------|------|
| `maxDesktops` | int | 最多可拥有桌面数量 |
| `maxCpu` | int | CPU 核数上限 |
| `maxMemoryGb` | int | 内存 GB 上限 |
| `maxGpu` | int | GPU 数量上限 |

---

## 5.10 领域模型 ER 关系

```text
Tenant ──────────────────────────────────────────────┐
  │                                                   │
  ├── TenantPolicy（租户级策略）                        │
  │                                                   │
  ├── ResourcePool（资源池）                            │
  │                                                   │
  └── User ──────────────────────────────────────────┤
        │                                             │
        ├── UserQuota（用户配额）                       │
        │                                             │
        └── DesktopInstance ──────────────────────────┘
                  │
                  ├── DesktopTemplate（规格模板）
                  │
                  ├── DesktopPolicy（桌面级覆盖策略，可选）
                  │       ↑ 继承并覆盖 TenantPolicy 部分字段
                  │
                  └── Session（会话，1:N）
```

**关键约束：**

- 一个 Tenant 有且仅有一个 TenantPolicy
- 一个 DesktopInstance 可选绑定一个 DesktopPolicy，未绑定时完全继承 TenantPolicy
- 一个 User 有且仅有一个 UserQuota，由 Tenant 管理员设置
- 一个 DesktopInstance 同时只有一个 Connected Session（多设备互斥）

---

# 6. Desktop 生命周期设计

## 6.1 DesktopState

DesktopState 描述桌面资源状态。

```text
Assigned
↓
Provisioning
↓
Starting
↓
Initializing
↓
Ready
↓
Stopping
↓
Stopped
```

异常分支：

```text
Error
↓
Recovering
```

---

## 6.2 状态说明

### Assigned

桌面已创建。

尚未启动资源。

---

### Provisioning

资源准备阶段。

例如：

* 创建存储
* 创建网络
* 分配资源

---

### Starting

开始启动 Pod 或 VM。

---

### Initializing

基础资源已启动。

桌面环境正在初始化。

例如：

* GuestOS 启动
* Agent 注册
* 用户配置加载
* WebRTC 服务启动

---

### Ready

桌面已具备用户接入能力。

Ready 的定义：

> 用户能够立即建立 Session 并开始使用桌面。

Ready 是业务状态，而非基础设施状态。

因此：

```text
VM Running ≠ Ready
Pod Running ≠ Ready
```

---

### Stopping

桌面停止中。

---

### Stopped

桌面已关闭。

数据仍然保留。

---

### Error

桌面运行异常。

---

### Recovering

平台自动修复中。

---

## 6.3 状态转换触发条件

| 当前状态 | 目标状态 | 触发条件 | 触发方 |
|----------|----------|----------|--------|
| — | Assigned | 用户或管理员调用创建桌面接口 | Desktop Service |
| Assigned | Provisioning | Scheduler 回写调度结果，开始创建存储/网络资源 | Scheduler Service |
| Provisioning | Starting | K8s PVC / 网络资源创建完成，开始启动 VM / Pod | Desktop Service |
| Starting | Initializing | K8s VM / Pod 进入 Running 状态 | Monitor Service（K8s Watch）|
| Initializing | Ready | Agent 上报所有就绪字段为 true | Monitor Service |
| Ready | Stopping | 用户/管理员调用停止接口，或 Policy 触发空闲自动关机 | Desktop Service / Event Center |
| Stopping | Stopped | K8s VM / Pod 完全停止 | Monitor Service（K8s Watch）|
| Stopped | Starting | 用户/管理员调用启动接口 | Desktop Service |
| Ready / Starting / Initializing | Error | Agent 心跳超时 / CrashLoopBackOff / PVC 挂载失败等异常 | Monitor Service |
| Error | Recovering | Broker 内置自愈规则触发，开始自动修复 | Event Center |
| Recovering | Ready | Agent 重新上报所有就绪字段为 true | Monitor Service |
| Recovering | Error | 超过最大重试次数或超时未恢复 | Event Center |

**说明：**

- `VM Running ≠ Ready`，`Pod Running ≠ Ready`：Ready 是业务状态，必须等待 Agent 上报完整就绪信息
- Stopped → Starting 需先经过 Provisioning（存储和网络资源可能需要重新挂载）
- Fatal Error 场景（如存储损坏、配额不足）直接进入 Error 终态，不进入 Recovering

---

# 7. Session 生命周期设计

## 7.1 SessionState

```text
Created
↓
Connecting
↓
Connected
↓
Disconnected
↓
Closed
```

---

## 7.2 状态说明

### Created

Broker 已创建 Session。

---

### Connecting

连接建立中。

---

### Connected

用户已接入桌面。

---

### Disconnected

连接异常断开。

允许使用原 Session ID 重连。

断线超时时间由 Policy 配置，默认 30 秒。

超时后 Session → Closed，UsageState → Inactive。

---

### Closed

会话结束。

---

## 7.3 多设备互斥规则

同一用户同一台桌面，同时只允许一个 Session 处于 Connected 状态。

新连接建立时，Broker 主动踢断旧 Session。

旧客户端收到 `SESSION_REPLACED` 事件推送。

---

# 8. UsageState 设计

## 8.1 设计目的

为了支持自动化运维、资源回收和运营分析，引入 UsageState。

UsageState 不属于资源状态。

属于业务状态。

---

## 8.2 状态定义

```text
Available
Occupied
Inactive
```

---

### Available

桌面运行中。

无活跃 Session。

---

### Occupied

存在活跃 Session。

用户正在使用桌面。

---

### Inactive

桌面运行中。

长时间无活跃 Session。

可触发：

* 自动关机
* 自动回收
* 资源迁移

等策略。

---

# 9. 连接编排流程

## 9.1 整体架构

连接编排采用以下路径：

```
Client
  ↓ HTTPS / WSS
Ingress（TLS 终结 + 限流）
  ↓ HTTP / WS
Broker（业务控制面）
  ↓ K8s API
Media Gateway（WebRTC 媒体面）
```

Broker 负责信令编排，不参与媒体流传输。

Tauri 客户端与 Web 客户端共用同一套 API，通过请求中的 `clientType` 字段区分。

---

## 9.2 完整建连时序

用户点击"连接桌面"到画面出现的全链路时序如下：

```
Client          Ingress         Broker          Media Gateway     Desktop Agent
  |                |               |                  |                 |
  |-- POST /api/v1/sessions ------>|                  |                 |
  |                |               |-- 验证 JWT        |                 |
  |                |               |-- 检查 DesktopState == Ready        |
  |                |               |-- 检查多设备互斥   |                 |
  |                |               |-- 创建 Session (Created)            |
  |                |               |-- 签发 Session Token               |
  |                |               |-- 查询 TURN 凭证  |                 |
  |<-- 200 { sessionToken, turnCredential, mediaGatewayUrl } ------------|
  |                |               |                  |                 |
  |-- WS 握手 ws://broker/signal?token=<sessionToken> |                 |
  |                |               |-- 验证 sessionToken                |
  |                |               |-- Session → Connecting             |
  |<-- WS 握手成功  |               |                  |                 |
  |                |               |                  |                 |
  |-- WS: { type: "offer", sdp: "..." } ------------>|                  |
  |                |               |-- 转发 SDP Offer ->|               |
  |                |               |<-- SDP Answer  ---|               |
  |<-- WS: { type: "answer", sdp: "..." } -----------|                  |
  |                |               |                  |                 |
  |-- WS: { type: "ice", candidate: "..." } -------->|                  |
  |                |               |-- 转发 ICE ------->|               |
  |<-- WS: { type: "ice", candidate: "..." } --------|                  |
  |                |               |                  |                 |
  |  （ICE 协商完成，P2P 或 TURN 中转媒体连接建立）    |                 |
  |                |               |                  |                 |
  |                |               |<-- Agent 上报 captureService=true--|
  |                |               |-- Session → Connected              |
  |                |               |-- UsageState → Occupied            |
  |<-- WS: { type: "session_state", state: "Connected" } ---------------|
  |                |               |                  |                 |
  （画面开始渲染）
```

---

## 9.3 断线重连时序

网络抖动或异常断开后，客户端使用原 Session ID 重连：

```
Client          Broker          Redis
  |               |               |
  （WebSocket 断开）               |
  |               |-- Session → Disconnected                 
  |               |-- 发布事件到 Redis: session:{id}:state = Disconnected
  |               |               |
  （等待重连，默认 30 秒超时）       |
  |               |               |
  |-- WS 重连 ws://broker/signal?token=<原 sessionToken>
  |               |-- 验证 sessionToken 有效期内
  |               |-- 查询 Session 状态 == Disconnected
  |               |-- Session → Connecting
  |<-- WS 握手成功 |               |
  |               |               |
  |-- WS: { type: "offer", sdp: "..." }（重新协商 WebRTC）
  |               |（重走 ICE 协商流程）
  |               |-- Session → Connected
  |               |-- UsageState → Occupied
  |<-- WS: { type: "session_state", state: "Connected" }
  |               |               |
  （画面恢复）
```

若超时（默认 30 秒，由 Policy 配置）前未重连：

```
Session → Closed
UsageState → Inactive
```

---

## 9.4 多设备互斥踢人时序

同一用户在第二台设备发起连接时：

```
Client-B        Broker          Redis           Client-A
  |               |               |                 |
  |-- POST /api/v1/sessions ----->|                 |
  |               |-- 检查该 Desktop 存在 Connected Session（Client-A）
  |               |-- 发布踢人事件: session:{old_id}:replaced
  |               |               |-- 推送事件 ------>|
  |               |               |                 |<-- WS: SESSION_REPLACED
  |               |               |                 |（Client-A 提示"已在其他设备登录"）
  |               |-- 关闭旧 Session → Closed        |
  |               |-- 创建新 Session → Created        |
  |<-- 200 { sessionToken, ... } |                  |
  |（Client-B 继续建连流程）       |                  |
```

---

## 9.5 WebSocket 信令通道

### 连接地址

```
wss://<broker-host>/api/v1/signal?token=<sessionToken>
```

握手阶段通过 URL Query 参数携带 Session Token，不使用 Authorization Header（WebSocket 握手不支持自定义 Header）。

### 消息格式

所有信令消息均为 JSON，格式如下：

```json
{
  "type": "<消息类型>",
  "payload": { }
}
```

### 服务端推送事件类型

| 事件类型 | 触发时机 | Payload 说明 |
|----------|----------|--------------|
| `session_state` | Session 状态变更 | `{ "state": "Connected" }` |
| `desktop_state` | Desktop 状态变更 | `{ "state": "Ready" \| "Error" \| "Recovering" }` |
| `session_replaced` | 被其他设备踢下线 | `{ "reason": "LOGIN_FROM_OTHER_DEVICE" }` |
| `ice` | 媒体网关下发 ICE 候选 | `{ "candidate": "..." }` |
| `answer` | 媒体网关回传 SDP Answer | `{ "sdp": "..." }` |
| `heartbeat` | 保活心跳（30 秒间隔） | `{ "ts": 1234567890 }` |

### 客户端上行消息类型

| 消息类型 | 说明 | Payload 说明 |
|----------|------|--------------|
| `offer` | 发送 SDP Offer | `{ "sdp": "..." }` |
| `ice` | 上报 ICE 候选 | `{ "candidate": "..." }` |
| `heartbeat_ack` | 心跳回包 | `{ "ts": 1234567890 }` |

---

## 9.6 Redis Pub/Sub 多副本同步

Broker 支持多副本水平扩展，WebSocket 连接分散在不同副本上。事件同步通过 Redis Pub/Sub 实现：

```
副本 A                    Redis                    副本 B
  |                         |                         |
  |（持有 Client 的 WS 连接）|                         |
  |                         |                         |（处理了 Desktop 状态变更）
  |                         |<-- PUBLISH session:{id} event --|
  |-- SUBSCRIBE session:{id}|                         |
  |<-- 收到事件             |                         |
  |-- 推送 WS 消息给 Client  |                         |
```

Channel 命名规则：

```
session:{session_id}        # 单 Session 事件
desktop:{desktop_id}        # 单 Desktop 事件
tenant:{tenant_id}          # 租户级广播（如维护通知）
```

---

## 9.7 TURN Server 凭证下发

Broker 在创建 Session 时向 Coturn 请求时效性凭证，随 Session 创建响应一并下发给客户端：

```json
{
  "turnCredential": {
    "urls": [
      "turn:turn.example.com:3478?transport=udp",
      "turn:turn.example.com:3478?transport=tcp",
      "turns:turn.example.com:5349?transport=tcp"
    ],
    "username": "1700000000:user_123",
    "credential": "<HMAC-SHA1 签名>",
    "ttl": 86400
  }
}
```

`username` 格式为 `{过期时间戳}:{用户标识}`，与 Coturn 的 `use-auth-secret` 模式兼容。

客户端使用该凭证自行完成 ICE Candidate Gathering，无需 Broker 代理。

---

## 9.8 ICE Candidate 交换流程

客户端完成 ICE Gathering 后，通过 WebSocket 信令通道与媒体网关交换候选：

```
Client                  Broker                  Media Gateway
  |                       |                           |
  |-- WS: offer { sdp }-->|-- 转发 offer ------------>|
  |                       |<-- answer { sdp } --------|
  |<-- WS: answer { sdp } |                           |
  |                       |                           |
  |-- WS: ice { cand-1 }->|-- 转发 ice -------------->|
  |-- WS: ice { cand-2 }->|-- 转发 ice -------------->|
  |                       |<-- ice { cand-1 } --------|
  |<-- WS: ice { cand-1 } |                           |
  |                       |<-- ice { cand-2 } --------|
  |<-- WS: ice { cand-2 } |                           |
  |                       |                           |
  （Trickle ICE，候选逐条交换，不等 gathering 完成）
```

采用 Trickle ICE 模式，候选逐条交换，减少建连延迟。

---

## 9.9 JWT 结构与 Payload 定义

### Access Token

用于 REST API 鉴权，有效期 30 分钟，客户端关闭即失效（不持久化 Refresh Token）。

Header：

```json
{
  "alg": "RS256",
  "typ": "JWT"
}
```

Payload：

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

| 字段 | 说明 |
|------|------|
| `sub` | 用户 ID |
| `tenant_id` | 租户 ID，用于多租户隔离 |
| `roles` | 权限角色列表 |
| `client_type` | `tauri` / `web`，用于差异化功能控制 |
| `iat` | 签发时间 |
| `exp` | 过期时间（30 分钟） |

### Session Token

用于 WebSocket 信令鉴权，与 Session 生命周期绑定，有效期与 Session 保持一致。

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

Session Token 独立于 Access Token，不随 Access Token 过期而失效，避免用户使用桌面过程中因 Token 过期被踢断。

---

## 9.10 Access Token 静默续签流程

Access Token 有效期 30 分钟，客户端在 Token 过期前 5 分钟发起静默续签：

```
Client                          Broker
  |                               |
  |（Token 剩余有效期 < 5 分钟）   |
  |-- POST /api/v1/auth/refresh   |
  |   Header: Authorization: Bearer <当前有效 Access Token>
  |                               |-- 验证当前 Token 有效
  |                               |-- 签发新 Access Token
  |<-- 200 { accessToken }        |
  |（替换内存中的 Token，用户无感知）|
```

客户端关闭后内存中的 Token 随之失效，重新打开需重新登录。

---

## 9.11 WebSocket 断线自动重连策略

客户端 WebSocket 意外断开时，采用指数退避重连：

```
第 1 次重连：等待 1 秒
第 2 次重连：等待 2 秒
第 3 次重连：等待 4 秒
第 4 次重连：等待 8 秒
第 5 次及以后：等待 16 秒（上限）
```

重连时携带原 Session Token，Broker 验证 Session 处于 Disconnected 状态后恢复连接。

若 Session 已 Closed（超过 Policy 配置的断线超时时间），客户端收到 `SESSION_EXPIRED` 错误，提示用户重新发起连接。

---

## 9.12 ICE 协商失败处理

ICE 协商失败分两种情况：

**Gathering 失败**（无法获取候选）

原因通常为 TURN Server 不可达。

处理：Broker 返回 `ICE_GATHER_FAILED` 错误，客户端提示"网络连接异常，请检查网络后重试"。

**连通性检查失败**（候选均不可达）

原因通常为防火墙屏蔽。

处理：媒体网关通知 Broker，Broker 将 Session → Error，推送 `session_state: Error` 事件，客户端提示"无法建立媒体连接，请联系管理员"。

---

# 10. Broker API 规范

## 10.1 规范约定

### 基础路径

所有接口统一前缀：

```
https://<broker-host>/api/v1
```

后续扩展 gRPC 时，业务对象模型与字段命名保持一致，仅传输层替换。

### 认证方式

REST 接口通过 HTTP Header 携带 JWT：

```
Authorization: Bearer <accessToken>
```

WebSocket 信令接口通过 URL Query 参数携带 Session Token：

```
wss://<broker-host>/api/v1/signal?token=<sessionToken>
```

### 统一响应格式

所有接口响应均遵循以下 envelope 结构：

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

### 错误码规范

| 错误码 | HTTP 状态码 | 说明 |
|--------|-------------|------|
| 0 | 200 | 成功 |
| 1001 | 401 | Token 无效或已过期 |
| 1002 | 403 | 无权限 |
| 1003 | 404 | 资源不存在 |
| 1004 | 409 | 资源状态冲突（如桌面未就绪） |
| 1005 | 429 | 请求频率超限 |
| 2001 | 400 | Desktop 不存在 |
| 2002 | 409 | Desktop 状态不允许此操作 |
| 2003 | 409 | Desktop 已有活跃 Session |
| 3001 | 400 | Session 不存在 |
| 3002 | 409 | Session 已关闭 |
| 5000 | 500 | 服务内部错误 |

### 分页约定

列表接口统一使用以下分页参数：

```
GET /api/v1/desktops?page=1&pageSize=20
```

响应 `data` 字段结构：

```json
{
  "items": [ ],
  "total": 100,
  "page": 1,
  "pageSize": 20
}
```

### 时间格式

所有时间字段统一使用 ISO 8601 格式，UTC 时区：

```
"createdAt": "2024-01-01T08:00:00Z"
```

---

## 10.2 认证接口

### 登录

```
POST /api/v1/auth/login
```

**Request Body：**

```json
{
  "username": "alice",
  "password": "******",
  "clientType": "tauri"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `username` | string | 是 | 用户名 |
| `password` | string | 是 | 密码 |
| `clientType` | string | 是 | `tauri` / `web` |

**Response：**

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

---

### Token 刷新

```
POST /api/v1/auth/refresh
```

**Headers：**

```
Authorization: Bearer <当前有效 accessToken>
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "accessToken": "<新 JWT>",
    "expiresIn": 1800
  }
}
```

客户端应在 Token 过期前 5 分钟主动调用此接口完成静默续签。

---

### 登出

```
POST /api/v1/auth/logout
```

**Headers：**

```
Authorization: Bearer <accessToken>
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": null
}
```

登出后 Broker 将当前 Token 加入黑名单（存入 Redis，TTL 等于 Token 剩余有效期）。

---

## 10.3 Desktop 接口

### 创建桌面

```
POST /api/v1/desktops
```

**Request Body：**

```json
{
  "name": "alice-desktop",
  "templateId": "tpl_linux_4c8g",
  "resourcePoolId": "pool_gpu_01",
  "userId": "user_123",
  "tenantId": "tenant_abc"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 桌面名称 |
| `templateId` | string | 是 | 桌面模板 ID |
| `resourcePoolId` | string | 是 | 资源池 ID |
| `userId` | string | 是 | 归属用户 ID |
| `tenantId` | string | 是 | 归属租户 ID |

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "desktop_001",
    "name": "alice-desktop",
    "state": "Assigned",
    "usageState": "Available",
    "templateId": "tpl_linux_4c8g",
    "userId": "user_123",
    "tenantId": "tenant_abc",
    "createdAt": "2024-01-01T08:00:00Z"
  }
}
```

---

### 查询桌面列表

```
GET /api/v1/desktops?page=1&pageSize=20&userId=user_123&state=Ready
```

| 查询参数 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| `userId` | string | 否 | 按用户过滤 |
| `tenantId` | string | 否 | 按租户过滤 |
| `state` | string | 否 | 按 DesktopState 过滤 |
| `page` | int | 否 | 页码，默认 1 |
| `pageSize` | int | 否 | 每页条数，默认 20 |

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "id": "desktop_001",
        "name": "alice-desktop",
        "state": "Ready",
        "usageState": "Available",
        "templateId": "tpl_linux_4c8g",
        "userId": "user_123",
        "tenantId": "tenant_abc",
        "createdAt": "2024-01-01T08:00:00Z"
      }
    ],
    "total": 1,
    "page": 1,
    "pageSize": 20
  }
}
```

---

### 查询桌面详情

```
GET /api/v1/desktops/{desktopId}
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "desktop_001",
    "name": "alice-desktop",
    "state": "Ready",
    "usageState": "Occupied",
    "templateId": "tpl_linux_4c8g",
    "userId": "user_123",
    "tenantId": "tenant_abc",
    "activeSessionId": "sess_xyz",
    "agentReady": {
      "agent": true,
      "desktopService": true,
      "captureService": true,
      "loginReady": true
    },
    "createdAt": "2024-01-01T08:00:00Z",
    "updatedAt": "2024-01-01T09:00:00Z"
  }
}
```

---

### 启动桌面

```
POST /api/v1/desktops/{desktopId}/start
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "desktop_001",
    "state": "Starting"
  }
}
```

仅允许在 `Stopped` 状态下调用，否则返回错误码 `2002`。

---

### 停止桌面

```
POST /api/v1/desktops/{desktopId}/stop
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "desktop_001",
    "state": "Stopping"
  }
}
```

仅允许在 `Ready` 状态下调用。若存在活跃 Session，Broker 先关闭 Session 再停止桌面。

---

### 重启桌面

```
POST /api/v1/desktops/{desktopId}/restart
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "desktop_001",
    "state": "Stopping"
  }
}
```

---

### 销毁桌面

```
DELETE /api/v1/desktops/{desktopId}
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": null
}
```

销毁操作不可逆，将同时删除关联存储数据。仅管理员角色可调用。

---

## 10.4 Session 接口

### 创建 Session

```
POST /api/v1/sessions
```

**Request Body：**

```json
{
  "desktopId": "desktop_001",
  "clientType": "tauri"
}
```

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `desktopId` | string | 是 | 目标桌面 ID |
| `clientType` | string | 是 | `tauri` / `web` |

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "sessionId": "sess_xyz",
    "sessionToken": "<Session JWT>",
    "signalUrl": "wss://broker.example.com/api/v1/signal?token=<sessionToken>",
    "mediaGatewayUrl": "wss://media.example.com",
    "turnCredential": {
      "urls": [
        "turn:turn.example.com:3478?transport=udp",
        "turn:turn.example.com:3478?transport=tcp",
        "turns:turn.example.com:5349?transport=tcp"
      ],
      "username": "1700000000:user_123",
      "credential": "<HMAC-SHA1>",
      "ttl": 86400
    }
  }
}
```

若目标桌面存在活跃 Session（多设备互斥），Broker 自动踢断旧 Session 后创建新 Session。

若桌面 `state != Ready`，返回错误码 `2002`。

---

### 查询 Session 详情

```
GET /api/v1/sessions/{sessionId}
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "sess_xyz",
    "desktopId": "desktop_001",
    "userId": "user_123",
    "state": "Connected",
    "clientType": "tauri",
    "createdAt": "2024-01-01T08:00:00Z",
    "connectedAt": "2024-01-01T08:00:05Z",
    "disconnectedAt": null
  }
}
```

---

### 关闭 Session

```
POST /api/v1/sessions/{sessionId}/close
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": null
}
```

关闭后 Session → Closed，UsageState → Inactive，WebSocket 连接断开。

---

## 10.5 资源池接口

### 查询资源池列表

```
GET /api/v1/resource-pools?tenantId=tenant_abc
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "id": "pool_gpu_01",
        "name": "GPU 资源池 01",
        "type": "GPU",
        "totalCpu": 256,
        "totalMemoryGb": 1024,
        "totalGpu": 8,
        "usedCpu": 128,
        "usedMemoryGb": 512,
        "usedGpu": 4,
        "tenantId": "tenant_abc"
      }
    ],
    "total": 1,
    "page": 1,
    "pageSize": 20
  }
}
```

---

### 查询资源池详情

```
GET /api/v1/resource-pools/{poolId}
```

**Response：** 同列表单项结构，额外包含节点列表：

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "pool_gpu_01",
    "name": "GPU 资源池 01",
    "type": "GPU",
    "totalCpu": 256,
    "totalMemoryGb": 1024,
    "totalGpu": 8,
    "usedCpu": 128,
    "usedMemoryGb": 512,
    "usedGpu": 4,
    "tenantId": "tenant_abc",
    "nodes": [
      {
        "nodeId": "node-01",
        "status": "Ready",
        "cpu": 64,
        "memoryGb": 256,
        "gpu": 2
      }
    ]
  }
}
```

---

## 10.6 租户 & 用户接口

### 创建租户

```
POST /api/v1/tenants
```

**Request Body：**

```json
{
  "name": "研发部",
  "maxDesktops": 100,
  "maxCpu": 512,
  "maxMemoryGb": 2048,
  "maxGpu": 16
}
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "tenant_abc",
    "name": "研发部",
    "maxDesktops": 100,
    "createdAt": "2024-01-01T08:00:00Z"
  }
}
```

仅超级管理员可调用。

---

### 创建用户

```
POST /api/v1/users
```

**Request Body：**

```json
{
  "username": "alice",
  "password": "******",
  "tenantId": "tenant_abc",
  "roles": ["user"]
}
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "user_123",
    "username": "alice",
    "tenantId": "tenant_abc",
    "roles": ["user"],
    "createdAt": "2024-01-01T08:00:00Z"
  }
}
```

---

### 查询用户列表

```
GET /api/v1/users?tenantId=tenant_abc&page=1&pageSize=20
```

| 查询参数 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| `tenantId` | string | 否 | 按租户过滤 |
| `username` | string | 否 | 按用户名模糊搜索 |
| `page` | int | 否 | 页码，默认 1 |
| `pageSize` | int | 否 | 每页条数，默认 20 |

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "id": "user_123",
        "username": "alice",
        "tenantId": "tenant_abc",
        "roles": ["user"],
        "createdAt": "2024-01-01T08:00:00Z"
      }
    ],
    "total": 1,
    "page": 1,
    "pageSize": 20
  }
}
```

---

## 10.7 审计接口

### 查询审计日志

```
GET /api/v1/audit-logs?tenantId=tenant_abc&userId=user_123&startTime=2024-01-01T00:00:00Z&endTime=2024-01-02T00:00:00Z&page=1&pageSize=20
```

| 查询参数 | 类型 | 必填 | 说明 |
|----------|------|------|------|
| `tenantId` | string | 否 | 按租户过滤 |
| `userId` | string | 否 | 按用户过滤 |
| `desktopId` | string | 否 | 按桌面过滤 |
| `action` | string | 否 | 按操作类型过滤，如 `desktop.start` |
| `startTime` | string | 否 | 开始时间，ISO 8601 |
| `endTime` | string | 否 | 结束时间，ISO 8601 |
| `page` | int | 否 | 页码，默认 1 |
| `pageSize` | int | 否 | 每页条数，默认 20 |

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "id": "log_001",
        "userId": "user_123",
        "tenantId": "tenant_abc",
        "action": "desktop.start",
        "resourceType": "desktop",
        "resourceId": "desktop_001",
        "result": "success",
        "clientIp": "10.0.0.1",
        "clientType": "tauri",
        "createdAt": "2024-01-01T08:00:00Z"
      }
    ],
    "total": 1,
    "page": 1,
    "pageSize": 20
  }
}
```

审计日志只读，不提供删除接口。保留策略由运维配置，默认 180 天。

---

## 10.8 Policy 接口

### 创建租户策略

```
POST /api/v1/tenants/{tenantId}/policy
```

**Request Body：**

```json
{
  "usbEnabled": false,
  "usbAllowedClasses": ["HID", "Audio"],
  "usbBlockedClasses": [],
  "clipboardPolicy": "readonly",
  "printerRedirection": false,
  "audioInputEnabled": true,
  "audioOutputEnabled": true,
  "cameraRedirection": false,
  "smartCardRedirection": false,
  "disconnectTimeoutSec": 30,
  "idleLockScreenSec": 600,
  "idleShutdownSec": 3600,
  "maxSessionDurationSec": 0,
  "screenshotDisabled": true,
  "screenRecordDisabled": true,
  "watermarkEnabled": true,
  "watermarkTemplate": "{username} {datetime}",
  "localDiskMapping": false,
  "dragDropTransfer": false
}
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "policy_001",
    "tenantId": "tenant_abc",
    "level": "tenant",
    "createdAt": "2024-01-01T08:00:00Z"
  }
}
```

每个租户有且仅有一个租户级策略，重复调用为更新操作。

---

### 查询租户策略

```
GET /api/v1/tenants/{tenantId}/policy
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "policy_001",
    "tenantId": "tenant_abc",
    "level": "tenant",
    "usbEnabled": false,
    "usbAllowedClasses": ["HID", "Audio"],
    "clipboardPolicy": "readonly",
    "printerRedirection": false,
    "audioInputEnabled": true,
    "audioOutputEnabled": true,
    "cameraRedirection": false,
    "smartCardRedirection": false,
    "disconnectTimeoutSec": 30,
    "idleLockScreenSec": 600,
    "idleShutdownSec": 3600,
    "maxSessionDurationSec": 0,
    "screenshotDisabled": true,
    "screenRecordDisabled": true,
    "watermarkEnabled": true,
    "watermarkTemplate": "{username} {datetime}",
    "localDiskMapping": false,
    "dragDropTransfer": false,
    "createdAt": "2024-01-01T08:00:00Z",
    "updatedAt": "2024-01-01T08:00:00Z"
  }
}
```

---

### 设置桌面级覆盖策略

```
PUT /api/v1/desktops/{desktopId}/policy
```

桌面级策略仅允许覆盖以下字段，其余字段提交后忽略：

- `clipboardPolicy`
- `printerRedirection`
- `audioInputEnabled`
- `audioOutputEnabled`
- `cameraRedirection`
- `localDiskMapping`
- `disconnectTimeoutSec`
- `idleShutdownSec`

**Request Body：**

```json
{
  "clipboardPolicy": "readwrite",
  "printerRedirection": true,
  "audioInputEnabled": true,
  "audioOutputEnabled": true,
  "cameraRedirection": true,
  "localDiskMapping": true,
  "disconnectTimeoutSec": 60,
  "idleShutdownSec": 0
}
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "policy_002",
    "desktopId": "desktop_001",
    "level": "desktop",
    "clipboardPolicy": "readwrite",
    "printerRedirection": true,
    "audioInputEnabled": true,
    "audioOutputEnabled": true,
    "cameraRedirection": true,
    "localDiskMapping": true,
    "disconnectTimeoutSec": 60,
    "idleShutdownSec": 0,
    "createdAt": "2024-01-01T08:00:00Z"
  }
}
```

---

### 查询桌面生效策略

```
GET /api/v1/desktops/{desktopId}/policy/effective
```

返回租户策略与桌面策略合并后的最终生效策略，供客户端和 Agent 使用。

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "desktopId": "desktop_001",
    "tenantId": "tenant_abc",
    "usbEnabled": false,
    "usbAllowedClasses": ["HID", "Audio"],
    "clipboardPolicy": "readwrite",
    "printerRedirection": true,
    "audioInputEnabled": true,
    "audioOutputEnabled": true,
    "cameraRedirection": true,
    "smartCardRedirection": false,
    "disconnectTimeoutSec": 60,
    "idleLockScreenSec": 600,
    "idleShutdownSec": 0,
    "maxSessionDurationSec": 0,
    "screenshotDisabled": true,
    "screenRecordDisabled": true,
    "watermarkEnabled": true,
    "watermarkTemplate": "{username} {datetime}",
    "localDiskMapping": true,
    "dragDropTransfer": false
  }
}
```

---

### 删除桌面级覆盖策略

```
DELETE /api/v1/desktops/{desktopId}/policy
```

删除后桌面完全继承租户级策略。

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": null
}
```

---

## 10.9 用户配额接口

### 设置用户配额

```
PUT /api/v1/users/{userId}/quota
```

**Request Body：**

```json
{
  "maxDesktops": 3,
  "maxCpu": 32,
  "maxMemoryGb": 128,
  "maxGpu": 2
}
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "userId": "user_123",
    "tenantId": "tenant_abc",
    "maxDesktops": 3,
    "maxCpu": 32,
    "maxMemoryGb": 128,
    "maxGpu": 2,
    "updatedAt": "2024-01-01T08:00:00Z"
  }
}
```

用户配额不能超过租户级配额上限，否则返回错误码 `1004`。

---

### 查询用户配额

```
GET /api/v1/users/{userId}/quota
```

**Response：**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "userId": "user_123",
    "tenantId": "tenant_abc",
    "maxDesktops": 3,
    "usedDesktops": 1,
    "maxCpu": 32,
    "usedCpu": 8,
    "maxMemoryGb": 128,
    "usedMemoryGb": 32,
    "maxGpu": 2,
    "usedGpu": 0,
    "updatedAt": "2024-01-01T08:00:00Z"
  }
}
```

`used*` 字段为当前实时用量，由 Broker 聚合计算。

---

# 11. 数据库模型设计

## 11.1 设计约定

### 通用字段

所有业务表均包含以下通用字段：

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | varchar(36) | 主键，UUID v4 |
| `created_at` | timestamptz | 创建时间，UTC |
| `updated_at` | timestamptz | 最后更新时间，UTC |
| `deleted_at` | timestamptz | 软删除时间，NULL 表示未删除 |

### ID 生成策略

统一使用 UUID v4，由应用层生成，不依赖数据库自增。

理由：
- 多副本 Broker 无需协调 ID 生成
- 便于分布式追踪与日志关联
- 避免枚举攻击（相比自增 ID）

### 软删除约定

业务表统一使用软删除（`deleted_at IS NOT NULL`），不物理删除记录。

查询时所有业务接口默认过滤 `deleted_at IS NULL`。

审计日志表不支持软删除，只追加写入。

### 索引设计原则

- 所有外键字段建普通索引
- 高频过滤字段（`tenant_id`、`user_id`、`state`）建复合索引
- 审计日志表按 `created_at` 建分区索引

### 时间字段

所有时间字段统一使用 `timestamptz`（带时区的 timestamp），存储 UTC 时间。

---

## 11.2 tenants（租户表）

```sql
CREATE TABLE tenants (
    id              VARCHAR(36)     PRIMARY KEY,
    name            VARCHAR(128)    NOT NULL,
    max_desktops    INT             NOT NULL DEFAULT 100,
    max_cpu         INT             NOT NULL DEFAULT 512,
    max_memory_gb   INT             NOT NULL DEFAULT 2048,
    max_gpu         INT             NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_tenants_deleted_at ON tenants(deleted_at);
```

---

## 11.3 users（用户表）

```sql
CREATE TABLE users (
    id              VARCHAR(36)     PRIMARY KEY,
    tenant_id       VARCHAR(36)     NOT NULL REFERENCES tenants(id),
    username        VARCHAR(128)    NOT NULL,
    password_hash   VARCHAR(256),                      -- LDAP/OIDC 用户为 NULL
    auth_type       VARCHAR(32)     NOT NULL DEFAULT 'local', -- local / ldap / oidc
    roles           JSONB           NOT NULL DEFAULT '["user"]',
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ,

    CONSTRAINT uq_users_tenant_username UNIQUE (tenant_id, username)
);

CREATE INDEX idx_users_tenant_id     ON users(tenant_id);
CREATE INDEX idx_users_deleted_at    ON users(deleted_at);
```

---

## 11.4 user_quotas（用户配额表）

```sql
CREATE TABLE user_quotas (
    id              VARCHAR(36)     PRIMARY KEY,
    user_id         VARCHAR(36)     NOT NULL REFERENCES users(id),
    tenant_id       VARCHAR(36)     NOT NULL REFERENCES tenants(id),
    max_desktops    INT             NOT NULL DEFAULT 1,
    max_cpu         INT             NOT NULL DEFAULT 16,
    max_memory_gb   INT             NOT NULL DEFAULT 64,
    max_gpu         INT             NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_user_quotas_user_id UNIQUE (user_id)
);

CREATE INDEX idx_user_quotas_tenant_id ON user_quotas(tenant_id);
```

每个用户有且仅有一条配额记录，用户创建时由 Broker 自动初始化默认配额。

---

## 11.5 desktop_templates（桌面模板表）

```sql
CREATE TABLE desktop_templates (
    id              VARCHAR(36)     PRIMARY KEY,
    tenant_id       VARCHAR(36)     NOT NULL REFERENCES tenants(id),
    name            VARCHAR(128)    NOT NULL,
    os_type         VARCHAR(32)     NOT NULL,  -- linux / windows
    cpu             INT             NOT NULL,
    memory_gb       INT             NOT NULL,
    gpu             INT             NOT NULL DEFAULT 0,
    disk_gb         INT             NOT NULL,
    image           VARCHAR(256)    NOT NULL,  -- 镜像地址
    network_type    VARCHAR(32)     NOT NULL DEFAULT 'overlay',
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_desktop_templates_tenant_id  ON desktop_templates(tenant_id);
CREATE INDEX idx_desktop_templates_deleted_at ON desktop_templates(deleted_at);
```

---

## 11.6 resource_pools（资源池表）

```sql
CREATE TABLE resource_pools (
    id              VARCHAR(36)     PRIMARY KEY,
    tenant_id       VARCHAR(36)     NOT NULL REFERENCES tenants(id),
    name            VARCHAR(128)    NOT NULL,
    pool_type       VARCHAR(32)     NOT NULL,  -- CPU / GPU / Dedicated / AI
    total_cpu       INT             NOT NULL DEFAULT 0,
    total_memory_gb INT             NOT NULL DEFAULT 0,
    total_gpu       INT             NOT NULL DEFAULT 0,
    k8s_namespace   VARCHAR(128)    NOT NULL,  -- 对应 K8s Namespace
    node_selector   JSONB,                     -- 节点选择器，如 {"gpu": "true"}
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX idx_resource_pools_tenant_id  ON resource_pools(tenant_id);
CREATE INDEX idx_resource_pools_deleted_at ON resource_pools(deleted_at);
```

---

## 11.7 desktop_instances（桌面实例表）

```sql
CREATE TABLE desktop_instances (
    id                  VARCHAR(36)     PRIMARY KEY,
    tenant_id           VARCHAR(36)     NOT NULL REFERENCES tenants(id),
    user_id             VARCHAR(36)     NOT NULL REFERENCES users(id),
    template_id         VARCHAR(36)     NOT NULL REFERENCES desktop_templates(id),
    resource_pool_id    VARCHAR(36)     NOT NULL REFERENCES resource_pools(id),
    name                VARCHAR(128)    NOT NULL,
    desktop_state       VARCHAR(32)     NOT NULL DEFAULT 'Assigned',
    usage_state         VARCHAR(32)     NOT NULL DEFAULT 'Available',
    active_session_id   VARCHAR(36),               -- 当前活跃 Session ID，无则 NULL
    k8s_namespace       VARCHAR(128)    NOT NULL,
    k8s_resource_name   VARCHAR(128)    NOT NULL,  -- VM 或 Pod 名称
    agent_ready         JSONB,                     -- Agent 上报的就绪状态
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    deleted_at          TIMESTAMPTZ
);

CREATE INDEX idx_desktop_instances_tenant_id     ON desktop_instances(tenant_id);
CREATE INDEX idx_desktop_instances_user_id       ON desktop_instances(user_id);
CREATE INDEX idx_desktop_instances_desktop_state ON desktop_instances(desktop_state);
CREATE INDEX idx_desktop_instances_deleted_at    ON desktop_instances(deleted_at);
CREATE INDEX idx_desktop_instances_tenant_state  ON desktop_instances(tenant_id, desktop_state)
    WHERE deleted_at IS NULL;
```

`agent_ready` 示例：

```json
{
  "agent": true,
  "desktopService": true,
  "captureService": true,
  "loginReady": true,
  "reportedAt": "2024-01-01T08:00:00Z"
}
```

---

## 11.8 sessions（会话表）

```sql
CREATE TABLE sessions (
    id                  VARCHAR(36)     PRIMARY KEY,
    desktop_id          VARCHAR(36)     NOT NULL REFERENCES desktop_instances(id),
    user_id             VARCHAR(36)     NOT NULL REFERENCES users(id),
    tenant_id           VARCHAR(36)     NOT NULL REFERENCES tenants(id),
    session_state       VARCHAR(32)     NOT NULL DEFAULT 'Created',
    client_type         VARCHAR(32)     NOT NULL,  -- tauri / web
    client_ip           VARCHAR(64),
    session_token_hash  VARCHAR(256),              -- Session Token 哈希，用于校验
    connected_at        TIMESTAMPTZ,
    disconnected_at     TIMESTAMPTZ,
    closed_at           TIMESTAMPTZ,
    created_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sessions_desktop_id    ON sessions(desktop_id);
CREATE INDEX idx_sessions_user_id       ON sessions(user_id);
CREATE INDEX idx_sessions_tenant_id     ON sessions(tenant_id);
CREATE INDEX idx_sessions_session_state ON sessions(session_state);
CREATE INDEX idx_sessions_created_at    ON sessions(created_at);
```

Session 表不使用软删除，`closed_at` 非空即表示会话已结束，完整保留历史记录用于审计。

---

## 11.9 tenant_policies（租户策略表）

```sql
CREATE TABLE tenant_policies (
    id                      VARCHAR(36)     PRIMARY KEY,
    tenant_id               VARCHAR(36)     NOT NULL REFERENCES tenants(id),

    -- 外设重定向策略
    usb_enabled             BOOLEAN         NOT NULL DEFAULT FALSE,
    usb_allowed_classes     JSONB           NOT NULL DEFAULT '[]',
    usb_blocked_classes     JSONB           NOT NULL DEFAULT '[]',
    clipboard_policy        VARCHAR(32)     NOT NULL DEFAULT 'readonly',
    printer_redirection     BOOLEAN         NOT NULL DEFAULT FALSE,
    audio_input_enabled     BOOLEAN         NOT NULL DEFAULT TRUE,
    audio_output_enabled    BOOLEAN         NOT NULL DEFAULT TRUE,
    camera_redirection      BOOLEAN         NOT NULL DEFAULT FALSE,
    smart_card_redirection  BOOLEAN         NOT NULL DEFAULT FALSE,

    -- 会话策略
    disconnect_timeout_sec  INT             NOT NULL DEFAULT 30,
    idle_lock_screen_sec    INT             NOT NULL DEFAULT 600,
    idle_shutdown_sec       INT             NOT NULL DEFAULT 3600,
    max_session_duration_sec INT            NOT NULL DEFAULT 0,

    -- 安全策略
    screenshot_disabled     BOOLEAN         NOT NULL DEFAULT TRUE,
    screen_record_disabled  BOOLEAN         NOT NULL DEFAULT TRUE,
    watermark_enabled       BOOLEAN         NOT NULL DEFAULT TRUE,
    watermark_template      VARCHAR(256)    NOT NULL DEFAULT '{username} {datetime}',
    local_disk_mapping      BOOLEAN         NOT NULL DEFAULT FALSE,
    drag_drop_transfer      BOOLEAN         NOT NULL DEFAULT FALSE,

    created_at              TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_tenant_policies_tenant_id UNIQUE (tenant_id)
);
```

每个租户有且仅有一条策略记录，由 Broker 在租户创建时自动初始化默认值。

---

## 11.10 desktop_policies（桌面级覆盖策略表）

仅存储桌面级覆盖字段，未覆盖的字段继承租户策略。

```sql
CREATE TABLE desktop_policies (
    id                      VARCHAR(36)     PRIMARY KEY,
    desktop_id              VARCHAR(36)     NOT NULL REFERENCES desktop_instances(id),
    tenant_id               VARCHAR(36)     NOT NULL REFERENCES tenants(id),

    -- 以下字段允许桌面级覆盖，NULL 表示继承租户策略
    clipboard_policy        VARCHAR(32),
    printer_redirection     BOOLEAN,
    audio_input_enabled     BOOLEAN,
    audio_output_enabled    BOOLEAN,
    camera_redirection      BOOLEAN,
    local_disk_mapping      BOOLEAN,
    disconnect_timeout_sec  INT,
    idle_shutdown_sec       INT,

    created_at              TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_desktop_policies_desktop_id UNIQUE (desktop_id)
);

CREATE INDEX idx_desktop_policies_tenant_id ON desktop_policies(tenant_id);
```

---

## 11.11 audit_logs（审计日志表）

```sql
CREATE TABLE audit_logs (
    id              VARCHAR(36)     NOT NULL,
    tenant_id       VARCHAR(36)     NOT NULL,
    user_id         VARCHAR(36)     NOT NULL,
    action          VARCHAR(64)     NOT NULL,  -- 如 desktop.start / session.create
    resource_type   VARCHAR(32)     NOT NULL,  -- desktop / session / user / tenant
    resource_id     VARCHAR(36)     NOT NULL,
    result          VARCHAR(16)     NOT NULL,  -- success / failure
    error_code      INT,                       -- 失败时的错误码
    client_ip       VARCHAR(64),
    client_type     VARCHAR(32),               -- tauri / web
    extra           JSONB,                     -- 扩展信息，如请求参数摘要
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW(),

    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- 按月创建分区（示例）
CREATE TABLE audit_logs_2024_01
    PARTITION OF audit_logs
    FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');

CREATE TABLE audit_logs_2024_02
    PARTITION OF audit_logs
    FOR VALUES FROM ('2024-02-01') TO ('2024-03-01');

CREATE INDEX idx_audit_logs_tenant_id   ON audit_logs(tenant_id, created_at);
CREATE INDEX idx_audit_logs_user_id     ON audit_logs(user_id, created_at);
CREATE INDEX idx_audit_logs_resource    ON audit_logs(resource_type, resource_id);
```

审计日志按月分区，单分区数据量可控。默认保留 180 天，由运维定期 DROP 过期分区。

`action` 字段枚举值：

| action | 说明 |
|--------|------|
| `auth.login` | 用户登录 |
| `auth.logout` | 用户登出 |
| `desktop.create` | 创建桌面 |
| `desktop.start` | 启动桌面 |
| `desktop.stop` | 停止桌面 |
| `desktop.restart` | 重启桌面 |
| `desktop.destroy` | 销毁桌面 |
| `session.create` | 创建会话 |
| `session.close` | 关闭会话 |
| `policy.update` | 更新策略 |
| `quota.update` | 更新配额 |

---

## 11.12 Policy Merge 逻辑

Broker 在返回生效策略（`GET /api/v1/desktops/{desktopId}/policy/effective`）时执行以下合并逻辑：

```
1. 读取 tenant_policies 获取租户级基准策略
2. 读取 desktop_policies 获取桌面级覆盖字段
3. 对每个可覆盖字段：
   - desktop_policies 字段为 NULL → 使用租户级值
   - desktop_policies 字段非 NULL → 使用桌面级值
4. 仅租户级字段直接使用租户级值，忽略桌面级任何覆盖
```

伪代码：

```go
func MergePolicy(tenant TenantPolicy, desktop *DesktopPolicy) EffectivePolicy {
    effective := EffectivePolicy{
        // 仅租户级字段，直接继承
        UsbEnabled:            tenant.UsbEnabled,
        UsbAllowedClasses:     tenant.UsbAllowedClasses,
        SmartCardRedirection:  tenant.SmartCardRedirection,
        IdleLockScreenSec:     tenant.IdleLockScreenSec,
        MaxSessionDurationSec: tenant.MaxSessionDurationSec,
        ScreenshotDisabled:    tenant.ScreenshotDisabled,
        ScreenRecordDisabled:  tenant.ScreenRecordDisabled,
        WatermarkEnabled:      tenant.WatermarkEnabled,
        WatermarkTemplate:     tenant.WatermarkTemplate,
        DragDropTransfer:      tenant.DragDropTransfer,
    }

    // 可覆盖字段，桌面级非 NULL 时覆盖
    effective.ClipboardPolicy     = coalesce(desktop.ClipboardPolicy,    tenant.ClipboardPolicy)
    effective.PrinterRedirection  = coalesce(desktop.PrinterRedirection,  tenant.PrinterRedirection)
    effective.AudioInputEnabled   = coalesce(desktop.AudioInputEnabled,   tenant.AudioInputEnabled)
    effective.AudioOutputEnabled  = coalesce(desktop.AudioOutputEnabled,  tenant.AudioOutputEnabled)
    effective.CameraRedirection   = coalesce(desktop.CameraRedirection,   tenant.CameraRedirection)
    effective.LocalDiskMapping    = coalesce(desktop.LocalDiskMapping,    tenant.LocalDiskMapping)
    effective.DisconnectTimeoutSec = coalesce(desktop.DisconnectTimeoutSec, tenant.DisconnectTimeoutSec)
    effective.IdleShutdownSec     = coalesce(desktop.IdleShutdownSec,    tenant.IdleShutdownSec)

    return effective
}
```

---

## 11.13 Session 与 Desktop 状态一致性保障

Session 状态与 Desktop UsageState 需保持一致，通过以下机制保障：

**写入规则**

| 事件 | Session 变更 | Desktop UsageState 变更 |
|------|-------------|------------------------|
| Session 创建 | Created | — |
| WebSocket 建连成功 | Connected | Occupied |
| WebSocket 断开 | Disconnected | — |
| 断线超时 | Closed | Inactive |
| Session 主动关闭 | Closed | Inactive |
| 被踢下线 | Closed | Available |
| 新 Session Connected | — | Occupied |

**一致性保障**

状态变更通过数据库事务保证原子性：

```sql
BEGIN;
UPDATE sessions     SET session_state = 'Connected', connected_at = NOW() WHERE id = $1;
UPDATE desktop_instances SET usage_state = 'Occupied', active_session_id = $1 WHERE id = $2;
COMMIT;
```

Broker 多副本并发场景下，通过乐观锁（`updated_at` 版本校验）防止状态覆盖：

```sql
UPDATE desktop_instances
SET usage_state = 'Occupied', updated_at = NOW()
WHERE id = $1 AND updated_at = $2;  -- $2 为读取时的 updated_at
```

更新行数为 0 时触发重试。

---

## 11.14 审计日志分区策略

审计日志按月自动创建分区，建议通过 pg_cron 或外部任务提前创建下月分区：

```sql
-- 每月 25 日执行，创建下月分区
SELECT cron.schedule(
    'create-audit-log-partition',
    '0 0 25 * *',
    $$
    DO $$
    DECLARE
        next_month DATE := DATE_TRUNC('month', NOW() + INTERVAL '1 month');
        partition_name TEXT := 'audit_logs_' || TO_CHAR(next_month, 'YYYY_MM');
    BEGIN
        EXECUTE FORMAT(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF audit_logs FOR VALUES FROM (%L) TO (%L)',
            partition_name,
            next_month,
            next_month + INTERVAL '1 month'
        );
    END;
    $$
    $$
);
```

过期分区清理（保留 180 天）：

```sql
-- 每月 1 日执行，DROP 6 个月前的分区
DROP TABLE IF EXISTS audit_logs_YYYY_MM;
```

---

# 12. 错误处理与自愈机制

## 12.1 错误来源分类

Broker 感知的错误来源分三层：

| 层级 | 错误来源 | 示例 |
|------|----------|------|
| 基础设施层 | K8s / KubeVirt / Longhorn / Kube-OVN 异常 | VM CrashLoopBackOff、PVC 挂载失败、节点宕机 |
| 业务层 | Broker 内部逻辑错误、Agent 上报异常 | Agent 超时未注册、Session Token 校验失败 |
| 客户端层 | 客户端网络异常、ICE 协商失败 | WebSocket 断开、ICE Candidate 无法连通 |

基础设施层错误由 Broker 通过 K8s Watch 机制感知，业务层错误由 Broker 内部捕获，客户端层错误由媒体网关或 WebSocket 断线触发。

---

## 12.2 错误严重程度分级

| 级别 | 含义 | 处理策略 |
|------|------|----------|
| `Fatal` | 不可恢复，需人工介入 | 桌面进入 Error 终态，触发告警，等待运维处理 |
| `Error` | 可尝试自愈 | 桌面进入 Recovering，Broker 自动执行修复动作 |
| `Warning` | 异常但不影响当前使用 | 记录日志，上报 Alertmanager，不改变桌面状态 |

---

## 12.3 Desktop 错误触发条件

以下异常触发 `DesktopState → Error`：

| 异常事件 | 严重程度 | 说明 |
|----------|----------|------|
| VM / Pod 连续 CrashLoopBackOff 超过阈值 | Error | 默认阈值：3 次 |
| Agent 超时未注册 | Error | 默认超时：5 分钟 |
| Agent 心跳超时 | Error | 默认超时：60 秒 |
| PVC 挂载失败 | Error | 存储不可用 |
| 节点宕机导致 Pod 驱逐 | Error | 需重新调度 |
| K8s 资源创建失败（配额不足等） | Fatal | 无法自愈，需人工介入 |
| 存储数据损坏 | Fatal | 无法自愈，需人工介入 |
| 网络配置失败（IP 分配失败等） | Fatal | 无法自愈，需人工介入 |

---

## 12.4 自愈策略

`Error` 级别异常触发自愈流程：

```
DesktopState: Ready/Starting/Initializing
      ↓ 检测到 Error 级别异常
DesktopState: Error
      ↓ Broker 评估是否可自愈
      ├── 可自愈 → DesktopState: Recovering → 执行修复动作
      └── 不可自愈（Fatal）→ 保持 Error，触发告警
```

各类异常的自愈动作：

| 异常类型 | 自愈动作 |
|----------|----------|
| VM / Pod CrashLoopBackOff | 重启 VM / Pod（调用 K8s API delete Pod，由 KubeVirt 重建） |
| Agent 超时未注册 | 重启 VM / Pod，等待 Agent 重新注册 |
| Agent 心跳超时 | 先重启 Agent 进程（通过 K8s exec），失败则重启 VM / Pod |
| PVC 挂载失败 | 触发 Longhorn 卷修复，等待副本重建后重新挂载 |
| 节点宕机 Pod 驱逐 | 重新调度到健康节点（更新 VM 亲和性规则，触发迁移） |

---

## 12.5 自愈重试机制

自愈采用指数退避重试，避免频繁操作加剧故障：

```
第 1 次自愈：立即执行
第 2 次自愈：等待 30 秒
第 3 次自愈：等待 60 秒
超过最大重试次数：进入 Fatal Error，停止自愈，触发告警
```

默认最大重试次数：3 次（可通过运维配置调整）。

重试计数存储在 `desktop_instances.agent_ready` 的扩展字段中，Broker 重启后可续算：

```json
{
  "recoveryAttempts": 2,
  "lastRecoveryAt": "2024-01-01T08:05:00Z"
}
```

---

## 12.6 Recovering 退出条件

自愈动作执行后，Broker 持续监听 Agent 上报，判断退出条件：

**恢复成功 → Ready**

```
Agent 上报所有字段为 true：
{
  "agent": true,
  "desktopService": true,
  "captureService": true,
  "loginReady": true
}
→ DesktopState: Recovering → Ready
→ 推送 WS 事件: desktop_state: Ready
```

**恢复失败 → Error（Fatal）**

```
自愈等待超时（默认 10 分钟）或超过最大重试次数
→ DesktopState: Recovering → Error
→ 推送 WS 事件: desktop_state: Error
→ 触发 Alertmanager 告警
→ 等待运维人工介入
```

---

## 12.7 不可自愈场景（Fatal Error）

以下场景直接进入 Error 终态，不触发自愈：

| 场景 | 原因 |
|------|------|
| K8s 资源配额不足 | 自愈无法解决资源不足问题 |
| 存储数据损坏 | 自愈可能导致数据进一步丢失 |
| 网络 IP 分配失败 | 需运维介入检查 Kube-OVN 配置 |
| 镜像拉取失败 | 镜像不存在或仓库不可达 |
| 连续自愈超过最大重试次数 | 自愈无效，升级为人工处理 |

Fatal Error 触发后：

1. `DesktopState → Error`
2. 推送 WebSocket 事件 `desktop_state: Error` 给客户端
3. 客户端展示"桌面异常，请联系管理员"
4. 上报 Alertmanager，触发运维告警

---

## 12.8 Session 建连失败处理

| 失败原因 | 错误码 | 客户端处理 |
|----------|--------|-----------|
| Desktop 未就绪（state != Ready） | 2002 | 提示"桌面未就绪，请稍后重试" |
| Session Token 无效或过期 | 1001 | 重新调用创建 Session 接口获取新 Token |
| ICE Gathering 失败 | 3003 | 提示"网络连接异常，请检查网络" |
| ICE 连通性检查失败 | 3004 | 提示"无法建立媒体连接，请联系管理员" |
| TURN Server 不可达 | 3005 | 提示"媒体服务异常，请联系管理员" |
| 被其他设备踢下线 | — | 收到 `SESSION_REPLACED` 事件，提示"已在其他设备登录" |

---

## 12.9 Session 运行中断处理

WebSocket 断开时，Broker 需区分网络抖动与真实断线：

```
WebSocket 断开
      ↓
Session → Disconnected
      ↓
启动断线超时计时器（Policy.disconnectTimeoutSec，默认 30 秒）
      ↓
      ├── 30 秒内客户端重连 → Session → Connected（复用原 Session ID）
      └── 超时未重连 → Session → Closed，UsageState → Inactive
```

运行中断推送事件：

| 事件 | 触发时机 | 客户端展示 |
|------|----------|-----------|
| `session_state: Disconnected` | WebSocket 断开时 | "连接中断，正在重连..." |
| `session_state: Connected` | 重连成功时 | 恢复正常，无需提示 |
| `session_state: Closed` | 超时关闭时 | "连接已断开，请重新发起连接" |
| `desktop_state: Error` | 桌面异常时 | "桌面异常，请联系管理员" |

---

## 12.10 Session 错误客户端推送规范

所有错误事件通过 WebSocket 推送，格式统一：

```json
{
  "type": "error",
  "payload": {
    "code": 3004,
    "message": "ICE connectivity check failed",
    "level": "Fatal",
    "action": "CONTACT_ADMIN"
  }
}
```

`action` 字段指导客户端行为：

| action | 客户端行为 |
|--------|-----------|
| `RETRY` | 自动重试 |
| `RECONNECT` | 重新建立 Session |
| `RELOGIN` | 重新登录 |
| `CONTACT_ADMIN` | 提示联系管理员，不自动重试 |

---

## 12.11 Broker 副本故障

Broker 以多副本方式运行在 K8s 中，副本故障由 K8s 自动处理：

```
Broker 副本 A 故障
      ↓
K8s 检测到 Pod 不健康，自动重建副本 A
      ↓
客户端 WebSocket 连接断开（副本 A 持有的连接）
      ↓
客户端触发指数退避重连
      ↓
重连到副本 B 或新副本 A
      ↓
携带原 Session Token，Session 状态从 PostgreSQL 恢复
```

关键设计：Broker 无本地状态，所有 Session 状态存储在 PostgreSQL，副本重建后可无缝接管。

---

## 12.12 Redis 不可用降级策略

Redis 承担两个职责：WebSocket 多副本同步（Pub/Sub）和 Token 黑名单。

**Redis 不可用时的降级行为：**

| 功能 | 降级策略 |
|------|----------|
| WebSocket Pub/Sub | 降级为单副本推送（事件只推给持有该连接的副本），可能丢失跨副本推送 |
| Token 黑名单 | 降级为跳过黑名单校验，已登出 Token 在过期前仍可使用（最多 30 分钟窗口） |
| Session 缓存 | 降级为直接查询 PostgreSQL，延迟略增，功能不受影响 |

Redis 不可用时，Broker 触发 Warning 级别告警，不影响核心业务流程。

---

## 12.13 PostgreSQL 不可用降级策略

PostgreSQL 是 Broker 的核心数据存储，不可用时影响所有写操作：

**降级行为：**

```
PostgreSQL 不可用
      ↓
Broker 所有写操作失败，返回 500 错误
      ↓
已建立的 WebSocket 连接和媒体流不受影响（数据面独立）
      ↓
客户端新建 Session 请求失败，提示"服务暂时不可用，请稍后重试"
      ↓
触发 Fatal 级别告警，运维立即介入
```

建议 PostgreSQL 以 K8s StatefulSet 高可用模式部署（主从 + 自动 Failover），或使用云数据库托管服务。

---

## 12.14 K8s API 不可用处理

Broker 依赖 K8s API 执行桌面生命周期操作（启动、停止、重建）：

**K8s API 不可用时的行为：**

```
Broker 调用 K8s API 失败
      ↓
返回业务错误："基础设施暂时不可用，请稍后重试"（错误码 5001）
      ↓
桌面状态不变（避免状态与实际不一致）
      ↓
Broker 定期重试 Watch 重连（指数退避，最大间隔 60 秒）
      ↓
K8s API 恢复后，Watch 事件驱动状态同步
```

Broker 启动时全量 List K8s 资源，建立内存缓存，Watch 断开期间读操作可使用缓存数据，写操作失败返回错误。

---

## 12.15 告警规则与运维介入入口

### Alertmanager 告警规则

| 告警名称 | 触发条件 | 严重程度 | 通知方式 |
|----------|----------|----------|----------|
| `DesktopFatalError` | 桌面进入 Fatal Error 状态 | Critical | 即时通知（钉钉 / 企业微信 / PagerDuty） |
| `DesktopRecoveringTimeout` | Recovering 超过 10 分钟未恢复 | Critical | 即时通知 |
| `BrokerRedisUnavailable` | Redis 连接失败超过 30 秒 | Warning | 工作时间通知 |
| `BrokerPostgresUnavailable` | PostgreSQL 连接失败 | Critical | 即时通知 |
| `DesktopErrorRateHigh` | 5 分钟内 Error 桌面数超过阈值 | Warning | 工作时间通知 |
| `SessionConnectFailureHigh` | Session 建连失败率超过 10% | Warning | 工作时间通知 |

### 运维人工介入入口

运维收到告警后，通过以下入口处理：

**查看桌面详情**

```
GET /api/v1/desktops/{desktopId}
```

查看 `desktopState`、`agentReady`、`activeSessionId` 确认故障现场。

**强制重置桌面状态**（运维专用接口）

```
POST /api/v1/admin/desktops/{desktopId}/reset
```

将桌面强制重置为 `Stopped` 状态，清理 K8s 残留资源，供运维在自愈失败后手动干预。

**查看审计日志**

```
GET /api/v1/audit-logs?desktopId={desktopId}&startTime=...
```

追溯故障发生前后的操作记录。

**Kagent 辅助诊断**

告警触发后，Kagent 自动拉取相关指标和日志，生成故障诊断报告，运维可通过 Kagent 接口获取根因分析结果（详见第 12 章）。

---



# 13. Kagent 集成设计

## 13.1 集成定位

Kagent 是一个运行在 Kubernetes 中的开源 AI Agent 框架，由 Solo.io 发起并捐赠至 CNCF。Kagent 通过声明式 CRD 定义 Agent 和工具，支持 MCP 协议对接外部能力。

Broker 与 Kagent 的集成关系如下：

| 组件 | 职责 |
|------|------|
| Kagent | AI Agent 运行框架，负责 LLM 调用、工具编排、Human-in-the-loop |
| Broker MCP Server | Broker 侧实现，将 Broker API 能力以 MCP 协议暴露给 Kagent |
| Alertmanager | 告警触发源，通过 Webhook 通知触发链路 |
| Broker API | Kagent 执行处置动作的唯一入口 |

**核心约束：**

- Kagent 不直接访问 PostgreSQL，所有业务数据通过 Broker MCP Server → Broker API 获取
- Kagent 不直接调用 K8s API 操作桌面资源，所有处置动作通过 Broker API 代理执行
- Broker 是 Kagent 感知云桌面业务状态的唯一数据源

---

## 13.2 集成架构

```
Prometheus
    ↓ 告警规则触发
Alertmanager
    ↓ Webhook 推送
Webhook Bridge（轻量转发服务）
    ↓ 调用 Kagent Agent API
Kagent Agent
    ↓ MCP 工具调用
Broker MCP Server
    ↓ HTTP 请求
Broker API
    ↓ K8s API / PostgreSQL
基础设施层
```

**Webhook Bridge 说明：**

Kagent 本身没有内置 Alertmanager Webhook 接收器。需要实现一个轻量 Webhook Bridge 服务，接收 Alertmanager 推送，提取告警信息后调用 Kagent 的 Agent Invoke API，触发对应 Agent 执行诊断与处置。

Webhook Bridge 可作为 Broker 的一个内部模块实现，不需要独立部署。

---

## 13.3 Broker MCP Server 设计

Broker 需要实现一个 MCP Server，将 Broker API 能力暴露给 Kagent Agent 调用。

MCP Server 以 K8s Deployment 形式运行，通过 Kagent 的 `RemoteMCPServer` CRD 注册：

```yaml
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: broker-mcp-server
  namespace: kagent
spec:
  description: Broker MCP Server，暴露云桌面业务操作能力
  url: http://broker-mcp.vdi-system.svc.cluster.local:8090/mcp
  sseReadTimeout: 5m0s
  timeout: 30s
```

### Broker MCP Server 工具清单

**只读工具（Kagent 可自主调用）：**

| 工具名称 | 说明 | 对应 Broker API |
|----------|------|----------------|
| `get_desktop` | 获取桌面详情及 Agent Ready 状态 | `GET /api/v1/desktops/{id}` |
| `list_desktops` | 按状态查询桌面列表 | `GET /api/v1/desktops` |
| `get_session` | 获取会话详情 | `GET /api/v1/sessions/{id}` |
| `get_effective_policy` | 获取桌面生效策略 | `GET /api/v1/desktops/{id}/policy/effective` |
| `get_user_quota` | 获取用户配额及用量 | `GET /api/v1/users/{id}/quota` |
| `list_audit_logs` | 查询审计日志 | `GET /api/v1/audit-logs` |
| `get_resource_pool` | 获取资源池详情及用量 | `GET /api/v1/resource-pools/{id}` |

**可写工具（需配合 Human-in-the-loop 机制）：**

| 工具名称 | 风险等级 | 说明 | 对应 Broker API |
|----------|----------|------|----------------|
| `restart_desktop` | 低危 | 重启桌面，数据不丢失 | `POST /api/v1/desktops/{id}/restart` |
| `close_session` | 低危 | 关闭指定会话 | `POST /api/v1/sessions/{id}/close` |
| `stop_desktop` | 中危 | 停止桌面，中断用户会话 | `POST /api/v1/desktops/{id}/stop` |
| `reset_desktop` | 高危 | 强制重置桌面状态 | `POST /api/v1/admin/desktops/{id}/reset` |
| `write_audit_log` | 低危 | 写入 Kagent 诊断报告 | `POST /api/v1/audit-logs` |

---

## 13.4 告警触发链路

Alertmanager 触发告警后，Webhook Bridge 将告警转换为 Kagent Agent 调用：

```
1. Alertmanager 推送 Webhook 到 Broker 内置 Webhook Bridge
2. Webhook Bridge 解析告警，提取 desktop_id、tenant_id、alertname
3. Webhook Bridge 调用 Kagent Agent Invoke API：
   POST https://kagent/api/v1/agents/vdi-ops-agent/invoke
   {
     "input": "告警触发：桌面 desktop_001 进入 Fatal Error 状态，请诊断并处置",
     "context": {
       "desktop_id": "desktop_001",
       "tenant_id": "tenant_abc",
       "alert_name": "DesktopFatalError",
       "fired_at": "2024-01-01T08:00:00Z"
     }
   }
4. Kagent Agent 接收任务，开始调用 Broker MCP Server 工具执行诊断
```

Alertmanager 路由配置示例：

```yaml
routes:
  - match:
      severity: critical
    receiver: kagent-webhook
  - match:
      severity: warning
    receiver: kagent-webhook

receivers:
  - name: kagent-webhook
    webhook_configs:
      - url: http://broker.vdi-system.svc.cluster.local:8080/api/v1/internal/alert-webhook
        send_resolved: true
```

---

## 13.5 Kagent Agent 定义

在 Kagent 中声明 VDI 运维 Agent，指定 systemMessage 和允许使用的工具：

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: vdi-ops-agent
  namespace: kagent
spec:
  description: 云桌面智能运维 Agent，负责桌面故障诊断与自动处置
  modelConfig: default-model-config
  systemMessage: |-
    你是云桌面平台的运维 Agent。
    当收到告警时，请按以下步骤处理：
    1. 调用 get_desktop 获取桌面当前状态和 Agent Ready 信息
    2. 调用 list_audit_logs 查看近 1 小时操作记录
    3. 根据桌面状态和日志分析根因
    4. 根据根因选择处置动作：
       - Agent 心跳超时 / CrashLoopBackOff → restart_desktop
       - 存在活跃异常 Session → close_session
       - 连续重启无效 → reset_desktop（需人工审批）
    5. 执行处置动作后，调用 get_desktop 确认状态恢复
    6. 调用 write_audit_log 写入诊断报告

    注意事项：
    - stop_desktop 和 reset_desktop 为高危操作，执行前必须等待人工审批
    - 如果无法判断根因，不要盲目执行写操作，应输出诊断报告等待人工决策
    - 所有操作结果必须写入审计日志
  tools:
    - type: McpServer
      mcpServer:
        toolServer: broker-mcp-server
```

---

## 13.6 Human-in-the-loop 对接

Kagent 原生支持 Tool Approval Gates 机制。对于高危工具，Broker MCP Server 在工具定义中声明需要审批，Kagent 在调用前暂停并等待人工确认。

Broker MCP Server 将 `stop_desktop` 和 `reset_desktop` 标记为需要审批的工具。

审批通知通过 Kagent 的 Human-in-the-loop 回调推送到运维渠道（钉钉 / 企业微信），运维确认后 Kagent 继续执行，拒绝后记录结果不执行。

Broker 侧无需额外实现审批流逻辑，直接复用 Kagent 原生机制。

---

## 13.7 处置结果回写审计日志

Kagent Agent 执行处置动作后，调用 `write_audit_log` 工具将诊断报告写入 Broker 审计日志，格式如下：

```json
{
  "action": "kagent.auto_heal",
  "resourceType": "desktop",
  "resourceId": "desktop_001",
  "result": "success",
  "extra": {
    "alertName": "DesktopFatalError",
    "rootCause": "Agent heartbeat timeout, suspected OOMKill",
    "actionsExecuted": ["restart_desktop"],
    "approvalRequired": false,
    "outcome": "recovered",
    "durationSec": 90
  }
}
```

审计日志写入后，运维可通过 `GET /api/v1/audit-logs?action=kagent.auto_heal` 查询所有 Kagent 处置记录，形成完整的自愈操作审计链路。

---

# 14. 多租户隔离机制

## 14.1 隔离层次

多租户隔离分四个层次，从基础设施到业务层逐级保障：

```
业务层隔离        Broker RBAC + tenant_id 过滤
    ↓
数据层隔离        PostgreSQL 行级租户字段过滤
    ↓
网络层隔离        Kube-OVN Subnet + ACL
    ↓
计算层隔离        K8s Namespace + ResourceQuota
```

---

## 14.2 计算层隔离（K8s Namespace）

每个租户对应一个独立的 K8s Namespace，桌面资源（VM / Pod / PVC）均在租户 Namespace 内创建：

```
vdi-tenant-{tenant_id}    # 租户专属 Namespace
```

通过 K8s ResourceQuota 限制租户资源用量上限：

```yaml
apiVersion: v1
kind: ResourceQuota
metadata:
  name: tenant-quota
  namespace: vdi-tenant-tenant_abc
spec:
  hard:
    requests.cpu: "512"
    requests.memory: 2048Gi
    count/virtualmachines.kubevirt.io: "100"
    requests.nvidia.com/gpu: "16"
```

ResourceQuota 由 Desktop Service 在租户创建时自动创建，配额值来源于 `tenants` 表的 `max_*` 字段。

---

## 14.3 网络层隔离（Kube-OVN）

每个租户分配独立的 Kube-OVN Subnet，租户桌面之间二层网络天然隔离：

```
租户 A Subnet: 10.100.1.0/24
租户 B Subnet: 10.100.2.0/24
```

跨租户访问通过 ACL 策略显式拒绝：

```yaml
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: vdi-tenant-tenant_abc
spec:
  cidrBlock: 10.100.1.0/24
  namespaces:
    - vdi-tenant-tenant_abc
  acls:
    - direction: to-lport
      priority: 1000
      match: "ip4.src == 10.100.2.0/24"   # 拒绝来自其他租户的流量
      action: drop
```

---

## 14.4 数据层隔离（PostgreSQL）

所有业务表均包含 `tenant_id` 字段，Broker 所有查询强制附加 `tenant_id` 过滤条件：

```sql
-- 查询桌面时强制附加租户过滤
SELECT * FROM desktop_instances
WHERE tenant_id = $1   -- 从 JWT 中提取，不信任客户端传参
AND deleted_at IS NULL;
```

`tenant_id` 从 JWT Payload 中提取，不接受客户端传参覆盖，防止越权访问。

---

## 14.5 业务层隔离（Broker RBAC）

Broker 定义三级角色：

| 角色 | 说明 | 可访问范围 |
|------|------|-----------|
| `super_admin` | 超级管理员 | 所有租户 |
| `tenant_admin` | 租户管理员 | 本租户内所有资源 |
| `user` | 普通用户 | 本人名下资源 |

角色权限矩阵：

| 操作 | super_admin | tenant_admin | user |
|------|-------------|--------------|------|
| 创建租户 | ✓ | ✗ | ✗ |
| 创建用户 | ✓ | ✓ | ✗ |
| 设置用户配额 | ✓ | ✓ | ✗ |
| 设置租户策略 | ✓ | ✓ | ✗ |
| 创建桌面 | ✓ | ✓ | ✗ |
| 查看本人桌面 | ✓ | ✓ | ✓ |
| 连接本人桌面 | ✓ | ✓ | ✓ |
| 停止/重启他人桌面 | ✓ | ✓ | ✗ |
| 查看审计日志 | ✓ | ✓（本租户）| ✗ |

---

## 14.6 租户资源配额联动

租户配额、用户配额、K8s ResourceQuota 三者联动，形成完整的资源管控链：

```
tenants.max_cpu = 512 核
    ↓ Desktop Service 同步
K8s ResourceQuota.requests.cpu = 512
    ↓ 约束
user_quotas.max_cpu ≤ 512（单用户不超过租户总量）
    ↓ 约束
DesktopTemplate.cpu（单台桌面不超过用户配额）
```

创建桌面时，Desktop Service 按以下顺序校验：

```
1. 用户当前已用 CPU < user_quotas.max_cpu
2. 租户当前已用 CPU < tenants.max_cpu
3. K8s ResourceQuota 剩余量充足
4. 目标 ResourcePool 剩余量充足
```

任一校验失败返回 `1004` 错误。

---

# 15. 状态组合关系

Broker 内部对象模型：

```text
DesktopInstance
├── DesktopState
├── UsageState
└── SessionList
```

示例：

用户正在办公：

```text
DesktopState = Ready
UsageState = Occupied
SessionState = Connected
```

用户断开连接：

```text
DesktopState = Ready
UsageState = Inactive
SessionState = Closed
```

桌面已停止：

```text
DesktopState = Stopped
Session = None
```

---

# 16. Agent Ready 机制

Broker 不直接依赖 VM 或 Pod 状态判断桌面可用性。

通过 Desktop Agent 上报状态。

Agent 上报内容：

```json
{
  "agent": true,
  "desktopService": true,
  "captureService": true,
  "loginReady": true
}
```

当所有条件满足时：

```text
DesktopState = Ready
```

---

# 17. 总结

## 17.1 架构回顾

Broker 是云桌面平台的业务控制平面，由六个子服务构成：

| 子服务 | 核心职责 |
|--------|----------|
| Desktop Service | 桌面 & 会话生命周期管理，对外暴露 REST API |
| Scheduler Service | 资源调度决策，异步处理调度请求 |
| Gateway Service | 连接编排，管理 WebSocket 信令通道 |
| Monitor Service | Agent 心跳管理，业务指标聚合，状态巡检 |
| Event Center | 告警事件处理，驱动通知 / 工单 / 自愈 / Kagent |
| Audit Service | 审计日志异步写入与查询 |

---

## 17.2 核心设计决策汇总

| 决策项 | 选型 | 理由 |
|--------|------|------|
| 业务对象 | Desktop / Session / Tenant / User | 与基础设施解耦，不管理 VM / Pod |
| API 协议 | REST `/api/v1/`，预留 gRPC 扩展 | 标准化，新人友好 |
| 信令通道 | WebSocket + Redis Pub/Sub | 实时推送，支持多副本水平扩展 |
| 消息队列 | NATS JetStream | 轻量，K8s 生态友好，支持持久化 |
| Token 机制 | JWT，Access 30min，关闭即失效 | 安全边界清晰，无需持久化 Refresh Token |
| 断线重连 | 原 Session ID 重连，超时由 Policy 配置 | 网络抖动无感知恢复 |
| 多设备互斥 | 新连接踢旧连接，推送 SESSION_REPLACED | 企业 VDI 标准行为 |
| 策略绑定 | 租户级 + 桌面级两层，桌面级部分字段可覆盖 | 安全边界统一管控，业务场景灵活配置 |
| 自愈升级 | 内置规则 → 超限升级 Kagent / Fatal 直接 Kagent | 规则驱动优先，AI 兜底 |
| 多租户隔离 | K8s Namespace + Kube-OVN Subnet + PostgreSQL 行过滤 + RBAC | 四层隔离，纵深防御 |
| 数据库 | PostgreSQL，软删除，按月分区审计日志 | 成熟稳定，分区策略控制单表规模 |

---

## 17.3 三层状态模型

Broker 通过三层状态模型统一管理云桌面生命周期：

```
DesktopState    描述资源状态：Assigned / Provisioning / Starting /
                Initializing / Ready / Stopping / Stopped / Error / Recovering

SessionState    描述连接状态：Created / Connecting / Connected /
                Disconnected / Closed

UsageState      描述业务使用状态：Available / Occupied / Inactive
```

三层状态组合驱动自动化运维策略（空闲关机、资源回收、告警自愈）。

---

## 17.4 待专项设计的模块

以下模块在本文档中有所涉及，但需单独输出详细设计文档：

| 模块 | 说明 |
|------|------|
| Event Center | 告警路由规则、工单集成、通知渠道配置 |
| Kagent 集成 | Agent CRD 定义、Playbook 设计、Tool Approval 配置 |
| Monitor Service | Agent 心跳协议、业务 metrics 指标体系、巡检规则 |
| Desktop Agent | Agent 上报协议、captureService / desktopService 启动机制 |
| Session 质量指标 | 延迟 / 帧率 / 丢包率采集与上报方案 |
| 监控展示方案 | Grafana Dashboard 设计或自研监控 UI 方案 |
