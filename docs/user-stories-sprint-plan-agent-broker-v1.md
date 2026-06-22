# 用户故事 & Sprint 规划：EVDI Agent (L2) + Broker (L3) V1

**团队**: 4人（2 Broker后端 · 1 Agent后端 · 1 运维/基础设施）
**节奏**: 2 周/Sprint，共 12 个 Sprint
**时间线**: 24 周（与客户端 Sprint 并行推进）

---

## 用户角色

| 角色 | 代号 | 描述 |
|------|------|------|
| 终端用户 | User | 通过浏览器或 Tauri 访问远程桌面 |
| IT 管理员 | Admin | 管理桌面实例、用户、策略 |
| 运维工程师 | Ops | 部署集群、监控告警、故障处理 |
| 桌面实例 | Agent | Agent 进程，向 Broker 上报状态和接收指令 |

---

## Sprint 1（W1-2）：基础设施 + Agent 重构

---

**US-AB01: Broker 项目脚手架**

**Description:** As a Developer, I want a Broker project scaffold with Go module structure, so that the team can start developing sub-services in parallel.

**Acceptance Criteria:**
1. Go module 初始化，项目结构按子服务拆分：`cmd/broker/`、`pkg/desktop/`、`pkg/gateway/`、`pkg/scheduler/`、`pkg/monitor/`、`pkg/event/`、`pkg/audit/`
2. 统一配置管理（Viper），支持 YAML 配置文件和环境变量覆盖
3. 统一日志框架（Zap），结构化 JSON 日志
4. 健康检查端点 `GET /healthz`，返回 200
5. Makefile 包含 `build`、`test`、`lint`、`run` 目标
6. CI 流水线配置（Go vet + test + build）

**Points:** 5 | **Assignee:** Broker-1

---

**US-AB02: PostgreSQL Schema 初始化**

**Description:** As a Developer, I want the complete database schema initialized, so that all Broker sub-services can start CRUD development immediately.

**Acceptance Criteria:**
1. 迁移脚本覆盖 02 文档第 11 章全部 11 张表：tenants、users、user_quotas、desktop_templates、resource_pools、desktop_instances、sessions、tenant_policies、desktop_policies、audit_logs（按月分区）、token_blacklist
2. 迁移框架使用 golang-migrate，支持 up/down
3. 种子数据脚本：默认 super_admin 用户、默认租户策略
4. 本地开发可用 Docker Compose 一键启动 PostgreSQL
5. 审计日志表按月分区，自动创建未来 3 个月分区

**Points:** 8 | **Assignee:** Broker-2

---

**US-AB03: NATS JetStream 部署与 Topic 初始化**

**Description:** As a Developer, I want NATS JetStream deployed with all required topics, so that Broker sub-services can communicate asynchronously.

**Acceptance Criteria:**
1. Docker Compose 中部署 NATS JetStream
2. 初始化全部 Topic：`schedule.request`、`schedule.result`、`alert.desktop.*`、`audit.desktop`、`audit.session`、`audit.kagent`
3. Topic 保留策略按 02 文档 4.8 节配置（schedule 1h、alert 24h、audit 7d）
4. 提供 Go 客户端封装（发布/订阅/Queue Group），可供所有子服务复用
5. 连接断开自动重连，指数退避

**Points:** 5 | **Assignee:** Broker-1

---

**US-AB04: Agent 项目模块化重构**

**Description:** As a Developer, I want the Agent codebase refactored into modular packages, so that new features (Broker communication, session management) can be added cleanly.

**Acceptance Criteria:**
1. 拆分现有代码到独立包：`pkg/webrtc/`（已有，保留）、`pkg/gstreamer/`（已有，保留）、`pkg/input/`（已有，保留）、新增 `pkg/session/`（会话管理）、`pkg/broker/`（Broker 通信）、`pkg/monitor/`（监控采集）
2. `cmd/agent/main.go` 重构为模块编排：初始化各包 → 启动 WebRTC → 连接 Broker → 进入主循环
3. Config 结构体扩展：新增 BrokerAddr、DesktopID、HeartbeatInterval 字段
4. 优雅关闭：SIGTERM → 通知 Broker → 等待当前 Session 断开（10s）→ 退出
5. 现有 MVP 功能（WebRTC + GStreamer + 输入）重构后行为不变

**Points:** 8 | **Assignee:** Agent

---

**US-AB05: Agent 生命周期管理模块**

**Description:** As an Agent, I want a lifecycle manager that handles startup sequencing and crash recovery, so that I can reliably initialize all subsystems.

**Acceptance Criteria:**
1. 启动顺序：Config → Xvfb 检测 → GStreamer 检测 → WebRTC 初始化 → Broker 注册 → 就绪
2. 每个阶段有超时控制（默认 30s），超时后重试或退出
3. GStreamer 进程崩溃自动重启（最多 3 次），崩溃后通知 Broker
4. WebRTC PeerConnection 异常断开后自动清理资源，等待新连接
5. 启动成功后日志输出 `Agent ready`，各子系统状态一目了然

**Points:** 5 | **Assignee:** Agent

---

**US-AB06: K8s 开发集群搭建**

**Description:** As an Ops, I want a K8s development cluster with Longhorn and Kube-OVN, so that Broker can deploy and test K8s integrations.

**Acceptance Criteria:**
1. K3s 或 Kind 单节点开发集群搭建脚本
2. Longhorn 部署并验证 PVC 创建/挂载
3. Kube-OVN 部署并验证 Subnet 创建/跨 Namespace 隔离
4. Docker Compose 部署：PostgreSQL + Redis + NATS + Coturn
5. Agent Dockerfile 更新：支持独立运行模式（无 K8s，直连 Docker Compose 的 Broker）

**Points:** 8 | **Assignee:** Ops

---

**Sprint 1 交付物**: 开发环境就绪
- ✅ Broker 项目脚手架可编译运行
- ✅ 数据库 Schema 初始化 + 种子数据
- ✅ NATS Topic 初始化 + Go 客户端封装
- ✅ Agent 模块化重构，现有功能不变
- ✅ 开发集群（K3s/Kind + Docker Compose）

---

## Sprint 2（W3-4）：Broker Desktop 核心

---

**US-AB07: Desktop Service — 桌面 CRUD**

**Description:** As an Admin, I want to create, list, get, and delete desktop instances, so that I can manage desktop resources for my tenants.

**Acceptance Criteria:**
1. `POST /api/v1/desktops` — 创建桌面，state=Assigned，校验配额（用户+租户）
2. `GET /api/v1/desktops` — 查询桌面列表，支持分页、按 userId/tenantId/state 过滤
3. `GET /api/v1/desktops/{id}` — 查询桌面详情，含 agentReady、activeSessionId
4. `DELETE /api/v1/desktops/{id}` — 软删除桌面，仅 super_admin/tenant_admin 可调用
5. 统一响应格式：`{ code, message, data }`
6. 所有查询强制附加 `tenant_id` 过滤（从 JWT 提取）
7. UUID v4 应用层生成

**Points:** 8 | **Assignee:** Broker-1

---

**US-AB08: Desktop Service — 桌面生命周期**

**Description:** As a User, I want to start, stop, and restart my desktop, so that I can control when my desktop is running.

**Acceptance Criteria:**
1. `POST /api/v1/desktops/{id}/start` — Stopped → Starting，发布调度请求到 NATS
2. `POST /api/v1/desktops/{id}/stop` — Ready → Stopping，关闭活跃 Session 后停止
3. `POST /api/v1/desktops/{id}/restart` — Ready → Stopping → Starting
4. 状态转换严格按 02 文档第 6 章状态机，非法转换返回 2002
5. 操作审计日志异步写入（发布到 NATS `audit.desktop`）
6. 启动桌面时调用 K8s API 创建 VM/Pod（S5 阶段实现调度前，先用固定节点）

**Points:** 8 | **Assignee:** Broker-2

---

**US-AB09: Desktop Service — 用户与租户管理**

**Description:** As an Admin, I want to create tenants and users with proper RBAC, so that I can organize desktop access by organization.

**Acceptance Criteria:**
1. `POST /api/v1/tenants` — 创建租户，自动创建 K8s Namespace + ResourceQuota
2. `POST /api/v1/users` — 创建用户，自动初始化默认配额
3. `POST /api/v1/auth/login` — 登录，返回 JWT（RS256），含 sub/tenant_id/roles/client_type
4. `POST /api/v1/auth/refresh` — Token 续签，过期前 5 分钟可刷新
5. `POST /api/v1/auth/logout` — 登出，Token 加入 Redis 黑名单
6. RBAC 校验：super_admin 全权限、tenant_admin 本租户、user 本人资源

**Points:** 13 | **Assignee:** Broker-1

---

**US-AB10: Desktop State 状态机引擎**

**Description:** As a Developer, I want a reusable desktop state machine engine, so that state transitions are consistent and auditable across all Broker sub-services.

**Acceptance Criteria:**
1. 状态机定义：Assigned → Provisioning → Starting → Initializing → Ready → Stopping → Stopped，异常分支 Error → Recovering
2. 每次状态转换写入 `desktop_instances.updated_at`，通过乐观锁防并发覆盖
3. 非法转换返回错误码 2002，附带当前状态和期望操作
4. 状态变更事件发布到 NATS（`desktop.state_changed`），供 Monitor/Event 消费
5. Session 与 Desktop UsageState 一致性保障：Connected → Occupied、Closed → Inactive（事务原子更新）

**Points:** 5 | **Assignee:** Broker-2

---

**US-AB11: Redis 部署与 Token 黑名单**

**Description:** As a Developer, I want Redis deployed with a Token blacklist module, so that logged-out tokens are immediately invalidated.

**Acceptance Criteria:**
1. Docker Compose 部署 Redis 7.x，持久化 AOF
2. Go Redis 客户端封装，支持 Pub/Sub 和 TTL 操作
3. Token 黑名单：登出时写入 `token:blacklist:{jti}` TTL = Token 剩余有效期
4. 每次请求校验 Token 时检查黑名单
5. Redis 不可用时降级：跳过黑名单校验，记录 Warning 日志

**Points:** 3 | **Assignee:** Broker-1

---

**Sprint 2 交付物**: Broker 核心 API 可用
- ✅ 桌面 CRUD + 生命周期 + 状态机
- ✅ 用户/租户管理 + JWT 认证
- ✅ Token 黑名单
- ✅ 审计日志异步写入框架

---

## Sprint 3（W5-6）：Agent-Broker 联调

---

**US-AB12: Agent — Broker gRPC 通信（注册 + 心跳）**

**Description:** As an Agent, I want to register with Broker and send periodic heartbeats, so that Broker knows my desktop is alive and healthy.

**Acceptance Criteria:**
1. gRPC proto 定义：AgentService.Register、AgentService.Heartbeat、AgentService.ReportError
2. Agent 启动时调用 Register，传递 desktopId、agentVersion、hostname、ip、osType、gpuInfo
3. 每 15 秒调用 Heartbeat，上报 ready 四项状态 + system 资源使用率 + network 质量
4. 心跳响应携带 nextHeartbeatIntervalSec 和 configVersion，Agent 据此动态调整
5. Broker 不可用时指数退避重连（1s/2s/4s/8s/16s 上限）
6. gRPC 连接使用 TLS（生产）或 insecure（开发）

**Points:** 13 | **Assignee:** Agent

---

**US-AB13: Broker — Agent 注册与心跳端点**

**Description:** As a Broker, I want to receive Agent registration and heartbeats, so that I can track desktop health and update desktop state accordingly.

**Acceptance Criteria:**
1. gRPC 服务实现：Register → 更新 desktop_instances.agent_ready，设置 agent=true
2. Heartbeat → 更新 agent_ready 全部四项字段，记录最后心跳时间
3. 心跳超时检测：60 秒无心跳 → 发布 `alert.desktop.heartbeat_timeout` 到 NATS
4. Ready 判定：agent+desktopService+captureService+loginReady 全 true → DesktopState Initializing → Ready
5. 注册接口幂等：Agent 重启后重复注册不创建重复记录
6. 心跳响应携带配置：编码参数、WebRTC 参数、Policy 快照

**Points:** 8 | **Assignee:** Broker-2

---

**US-AB14: Agent — 配置拉取与热更新**

**Description:** As an Agent, I want to pull configuration from Broker and apply changes without restart, so that I can adapt to policy changes dynamically.

**Acceptance Criteria:**
1. 心跳响应中 configVersion 变更时，Agent 拉取新配置
2. 配置项包含：capture（编码器/分辨率/帧率/码率）、webrtc（ICE 服务器/最大码率）、policy（剪贴板/音频/USB 策略）
3. 编码参数变更时，重启 GStreamer pipeline（先停后启，中断 <3s）
4. Policy 变更时，更新 DataChannel 输入过滤规则（如禁用剪贴板同步）
5. 配置版本本地缓存，启动时使用缓存，首条心跳后同步最新

**Points:** 5 | **Assignee:** Agent

---

**US-AB15: Broker — 配额管理接口**

**Description:** As an Admin, I want to set and check user and tenant quotas, so that resource usage is controlled at every level.

**Acceptance Criteria:**
1. `PUT /api/v1/users/{id}/quota` — 设置用户配额，不超过租户总量
2. `GET /api/v1/users/{id}/quota` — 查询用户配额，含实时 used* 字段
3. 创建桌面时校验：用户已用 < 配额、租户已用 < 总量、K8s ResourceQuota 充足
4. 配额超限返回错误码 1004
5. 租户配额变更时同步更新 K8s ResourceQuota

**Points:** 5 | **Assignee:** Broker-1

---

**US-AB16: Agent — 错误上报**

**Description:** As an Agent, I want to report errors to Broker, so that Broker can trigger self-healing or alert the operations team.

**Acceptance Criteria:**
1. `POST /api/v1/agent/error` — Agent 主动上报错误，含 errorCode、errorMessage、severity、recoverable
2. GStreamer pipeline 启动失败、GPU 检测失败、WebRTC 连接异常时自动上报
3. Broker 收到 Error 级别错误 → DesktopState → Error，发布事件到 NATS
4. Broker 收到 Fatal 级别错误 → DesktopState → Error（终态），触发告警
5. Agent 优雅关闭时上报 ready 全 false 的最后心跳

**Points:** 3 | **Assignee:** Agent

---

**Sprint 3 交付物**: Agent-Broker 通信链路打通
- ✅ Agent 注册到 Broker，心跳持续上报
- ✅ Broker 检测 Agent 就绪状态，更新 DesktopState
- ✅ 配额管理可用
- ✅ 错误上报链路通

---

## Sprint 4（W7-8）：会话 + 信令 → Alpha

---

**US-AB17: Gateway Service — WebSocket 信令通道**

**Description:** As a Client, I want to connect to Broker via WebSocket for SDP/ICE signaling, so that I can establish a WebRTC connection to my desktop.

**Acceptance Criteria:**
1. `WSS /api/v1/signal?token=<sessionToken>` — WebSocket 握手，验证 Session Token
2. 信令消息格式：`{ type: "offer"|"answer"|"ice"|"heartbeat", payload: {} }`
3. Offer/Answer 双向转发：Client ↔ Broker ↔ Agent
4. ICE Candidate 双向转发（Trickle ICE）
5. 心跳保活：30 秒间隔，超时断开
6. WebSocket 断开 → Session → Disconnected，启动 30s 超时计时器
7. Redis Pub/Sub 多副本同步：事件推送到 `session:{id}` channel

**Points:** 13 | **Assignee:** Broker-1

---

**US-AB18: Gateway Service — Session 创建与 Token 签发**

**Description:** As a Client, I want to create a session and receive a signal URL with ICE servers, so that I can connect to my desktop.

**Acceptance Criteria:**
1. `POST /api/v1/sessions` — 创建 Session，校验 DesktopState == Ready、多设备互斥
2. 签发 Session Token（JWT），含 sub/session_id/desktop_id/tenant_id，独立于 Access Token
3. 返回 signalUrl、iceServers（STUN + TURN 时效凭证）、policy（合并后生效策略）
4. TURN 凭证：调用 Coturn HMAC-SHA1 签名，username 格式 `{过期时间戳}:{用户标识}`
5. 多设备互斥：新 Session 踢旧 Session，推送 `SESSION_REPLACED` 事件
6. Desktop 非 Ready 状态返回 2002

**Points:** 8 | **Assignee:** Broker-2

---

**US-AB19: Agent — 会话管理模块**

**Description:** As an Agent, I want to manage client sessions with a connection state machine, so that I can handle connect, disconnect, and reconnect properly.

**Acceptance Criteria:**
1. 会话状态机：Idle → Connecting → Connected → Disconnected，断线后等待重连
2. WebRTC 连接建立后通知 Broker：Session → Connected，UsageState → Occupied
3. 连接断开后通知 Broker：Session → Disconnected
4. Broker 超时关闭 Session 时，Agent 收到指令后清理 WebRTC 资源
5. 同一桌面同时只允许一个 Connected Session（多设备互斥）
6. 断线后等待 Broker 的重连信令，30s 内可复用原 PeerConnection

**Points:** 8 | **Assignee:** Agent

---

**US-AB20: Agent — 信令协议适配（Broker 模式）**

**Description:** As an Agent, I want to receive signaling messages from Broker (not direct WebSocket), so that all signaling goes through the Broker Gateway.

**Acceptance Criteria:**
1. 信令来源从直连 WebSocket 切换为 Broker 转发（gRPC 流或 WebSocket）
2. 支持 Offer/Answer/ICE 消息通过 Broker 中转
3. 保持与 MVP 直连模式的兼容：可通过配置切换（直接模式/代理模式）
4. 代理模式下 Agent 不再暴露 WebSocket 端口，所有连接经 Broker
5. 信令延迟 <50ms（Agent ↔ Broker 局域网）

**Points:** 8 | **Assignee:** Agent

---

**US-AB21: Ingress + TLS + Coturn 部署**

**Description:** As an Ops, I want Ingress with TLS termination and Coturn TURN server, so that clients can securely connect from any network.

**Acceptance Criteria:**
1. Nginx Ingress Controller 部署，TLS 证书配置（支持 cert-manager 自动签发）
2. 路由规则：`/api/v1/` → Broker、`/api/v1/signal` → Broker WebSocket
3. Coturn 部署，配置 `use-auth-secret` 模式，与 Broker HMAC 凭证兼容
4. STUN/TURN 端口开放：3478（UDP/TCP）、5349（TLS）
5. WebRTC 媒体端口范围开放：50000-50100（UDP）

**Points:** 5 | **Assignee:** Ops

---

**Sprint 4 交付物**: V1.0-alpha 🎯
- ✅ 端到端信令通路：浏览器 → Ingress → Broker → Agent → 桌面画面
- ✅ Session 创建 → Token 签发 → WebSocket 信令 → WebRTC 连接
- ✅ TURN 穿透可用
- ✅ **验收标准**：浏览器打开 → 进入桌面 < 10 秒，局域网操作延迟 < 150ms

---

## Sprint 5（W9-10）：调度 + 资源管理

---

**US-AB22: Scheduler Service — 资源调度**

**Description:** As a Broker, I want to schedule desktops to appropriate resource pools and nodes, so that resources are used efficiently.

**Acceptance Criteria:**
1. 订阅 NATS `schedule.request`，接收调度请求（desktopId、templateId、resourcePoolId）
2. 调度策略：资源池选择 → 集群选择 → 节点选择（按资源余量打分，最高分优先）
3. 调度结果 REST 回调 Desktop Service，更新 DesktopState → Provisioning
4. 调度失败（无可用资源）通知 Desktop Service，DesktopState → Error
5. GPU 桌面优先调度到 GPU 节点
6. 调度延迟 <5s

**Points:** 13 | **Assignee:** Broker-1

---

**US-AB23: 资源池管理**

**Description:** As an Admin, I want to manage resource pools, so that I can organize compute resources by type and tenant.

**Acceptance Criteria:**
1. `POST /api/v1/resource-pools` — 创建资源池（CPU/GPU/Dedicated/AI），绑定 K8s Namespace
2. `GET /api/v1/resource-pools` — 查询资源池列表，含总量/已用量
3. `GET /api/v1/resource-pools/{id}` — 查询资源池详情，含节点列表
4. 资源池用量实时聚合：从 K8s ResourceQuota 读取已用值
5. 资源池与租户绑定：一个租户可使用多个资源池

**Points:** 5 | **Assignee:** Broker-2

---

**US-AB24: Desktop Template 管理**

**Description:** As an Admin, I want to manage desktop templates, so that I can define standard desktop configurations.

**Acceptance Criteria:**
1. `POST /api/v1/desktop-templates` — 创建模板（osType、CPU、Memory、GPU、Disk、Image）
2. `GET /api/v1/desktop-templates` — 查询模板列表
3. `PUT /api/v1/desktop-templates/{id}` — 更新模板
4. 创建桌面时必须指定 templateId，校验模板存在且未删除
5. 模板软删除，已有桌面不受影响

**Points:** 3 | **Assignee:** Broker-2

---

**US-AB25: Longhorn + Kube-OVN 集成**

**Description:** As an Ops, I want Longhorn for persistent storage and Kube-OVN for enterprise network, so that desktops have reliable storage and native LAN access.

**Acceptance Criteria:**
1. Longhorn 部署验证：PVC 创建 → 桌面 Pod 挂载 → 写入数据 → Pod 重建后数据保留
2. Kube-OVN VLAN 桥接验证：桌面 Pod 获取企业内网 IP，可 ping 通内网网关
3. Longhorn 数据本地化读配置：确保桌面 Pod 调度到持有副本的节点
4. Kube-OVN 多租户隔离验证：租户 A 的桌面无法访问租户 B 的桌面网络
5. 开机风暴测试：50 个桌面同时启动，3 分钟内全部 Ready

**Points:** 8 | **Assignee:** Ops

---

**Sprint 5 交付物**: 调度与资源管理就绪
- ✅ 桌面创建 → 自动调度 → 资源分配 → K8s 创建 VM/Pod
- ✅ 资源池和模板管理可用
- ✅ 存储和网络基础设施验证通过

---

## Sprint 6（W11-12）：策略 + TURN → Beta

---

**US-AB26: Policy 引擎 — 租户策略**

**Description:** As an Admin, I want to configure tenant-level policies, so that security and session rules are enforced across all desktops in my tenant.

**Acceptance Criteria:**
1. `POST /api/v1/tenants/{id}/policy` — 创建/更新租户策略（外设+会话+安全）
2. `GET /api/v1/tenants/{id}/policy` — 查询租户策略
3. 策略字段完整覆盖 02 文档 5.8 节全部字段
4. 策略变更后立即生效：下次 Session 创建时返回最新策略
5. 已有 Session 不受策略变更影响（连接时快照）

**Points:** 8 | **Assignee:** Broker-1

---

**US-AB27: Policy 引擎 — 桌面策略覆盖 + 合并**

**Description:** As an Admin, I want to override certain policies at the desktop level, so that specific desktops can have different settings from the tenant default.

**Acceptance Criteria:**
1. `PUT /api/v1/desktops/{id}/policy` — 设置桌面级覆盖策略（仅可覆盖字段：clipboard/printer/audio/camera/diskMap/disconnectTimeout/idleShutdown）
2. `GET /api/v1/desktops/{id}/policy/effective` — 查询生效策略（租户+桌面合并）
3. 合并逻辑按 02 文档 11.12 节：desktop 非 NULL → 使用桌面级，NULL → 使用租户级
4. `DELETE /api/v1/desktops/{id}/policy` — 删除桌面策略，回退到租户级
5. 桌面级策略不能突破租户级安全边界（如租户禁 USB，桌面级不能开启）

**Points:** 5 | **Assignee:** Broker-2

---

**US-AB28: Agent — Policy 执行**

**Description:** As an Agent, I want to receive and enforce policies from Broker, so that security rules are applied at the desktop level.

**Acceptance Criteria:**
1. Session 建立时接收生效策略快照
2. 剪贴板策略执行：disabled → 不处理剪贴板事件、readonly → 仅远端到本地、writeonly → 仅本地到远端、readwrite → 双向
3. 音频策略执行：audioInputEnabled/audioOutputEnabled 控制 Opus 轨的启停
4. USB 策略：usbEnabled=false 时，Agent 不处理 USB DataChannel 消息
5. 策略变更时（通过心跳配置下发），下一个 Session 生效

**Points:** 5 | **Assignee:** Agent

---

**US-AB29: Agent — 自适应码率反馈**

**Description:** As an Agent, I want to receive network quality feedback from clients and adjust encoding parameters, so that the experience adapts to network conditions.

**Acceptance Criteria:**
1. 接收客户端通过 DataChannel/WebSocket 发送的码率反馈消息（丢包率/延迟/可用带宽）
2. 网络质量下降时降低编码参数：分辨率降级（1080p→720p）、帧率降级（30→15fps）、码率降低
3. 网络恢复时自动提升质量
4. 编码参数切换时 GStreamer pipeline 动态重配置（无需重启）
5. 参数变更间隔 ≥5s，避免频繁切换导致画面抖动

**Points:** 5 | **Assignee:** Agent

---

**Sprint 6 交付物**: V1.0-beta 🎯
- ✅ 完整策略引擎：租户策略 + 桌面覆盖 + Agent 执行
- ✅ 自适应码率
- ✅ **验收标准**：双端可用、管理控制台可完成桌面全生命周期管理、策略管控生效

---

## Sprint 7（W13-14）：监控 + 告警

---

**US-AB30: Monitor Service — Agent 心跳管理**

**Description:** As an Ops, I want Monitor Service to track Agent heartbeats and detect timeouts, so that I can be alerted when desktops become unhealthy.

**Acceptance Criteria:**
1. 接收 Agent 心跳（从 gRPC Heartbeat 或 REST 端点）
2. 60 秒无心跳 → 发布 `alert.desktop.heartbeat_timeout` 到 NATS
3. 心跳数据写入 `desktop_instances.agent_ready` JSONB 字段
4. 心跳超时检测支持多副本竞争消费（NATS Queue Group）
5. 心跳响应携带 nextHeartbeatIntervalSec 和 configVersion

**Points:** 5 | **Assignee:** Broker-1

---

**US-AB31: Monitor Service — K8s 状态一致性巡检**

**Description:** As an Ops, I want Monitor Service to detect inconsistencies between Broker state and K8s actual state, so that I can fix discrepancies before users notice.

**Acceptance Criteria:**
1. K8s Watch 监听 VM/Pod 状态变更事件
2. 实时比对：Broker DesktopState vs K8s 实际状态
3. 不一致场景处理：Starting+PodFailed → Error、Ready+PodTerminating → Error
4. 全量对账巡检：每 5 分钟遍历所有桌面，比对状态
5. 检测到不一致时发布 `alert.desktop.state_inconsistency` 到 NATS

**Points:** 8 | **Assignee:** Broker-2

---

**US-AB32: Monitor Service — 业务 Metrics 暴露**

**Description:** As an Ops, I want Monitor Service to expose Prometheus metrics, so that I can build dashboards and configure alerts.

**Acceptance Criteria:**
1. 暴露 `GET /metrics` 端点，Prometheus 格式
2. 桌面维度指标：evdi_desktop_total、evdi_desktop_state_count、evdi_desktop_error_total
3. Session 维度指标：evdi_session_active_count、evdi_session_total、evdi_session_connect_duration_seconds
4. Agent 维度指标：evdi_agent_heartbeat_timeout_total、evdi_agent_ready_count
5. 资源维度指标：evdi_resource_cpu_usage_ratio、evdi_resource_gpu_usage_ratio
6. 所有指标含 tenant 标签，支持按租户过滤

**Points:** 5 | **Assignee:** Broker-1

---

**US-AB33: Prometheus + Grafana 部署**

**Description:** As an Ops, I want Prometheus and Grafana deployed with pre-configured dashboards, so that I can monitor the platform at a glance.

**Acceptance Criteria:**
1. Prometheus Operator 部署，自动发现 Broker/Monitor Service 的 /metrics 端点
2. Grafana 部署，预配置 4 个 Dashboard：平台总览、桌面运维、Session 质量、资源池
3. Alertmanager 部署，配置核心告警规则（DesktopFatalError、SessionConnectFailureHigh 等）
4. 告警通知渠道：钉钉/企业微信 Webhook
5. 数据保留 30 天

**Points:** 5 | **Assignee:** Ops

---

**US-AB34: Agent — 监控采集模块**

**Description:** As an Agent, I want to collect and report system metrics to Broker, so that operations can monitor desktop health.

**Acceptance Criteria:**
1. 每 10 秒采集 CPU/内存/磁盘使用率（/proc/stat、/proc/meminfo、/proc/diskstats）
2. GPU 使用率采集（nvidia-smi，如可用）
3. 采集数据通过心跳上报给 Monitor Service
4. WebRTC Stats 采集：RTT、丢包率、帧率、码率
5. 采集失败不阻塞心跳（设为 0 值继续上报）

**Points:** 3 | **Assignee:** Agent

---

**Sprint 7 交付物**: 监控告警体系就绪
- ✅ Monitor Service 心跳 + 巡检 + Metrics
- ✅ Prometheus + Grafana 可观测
- ✅ Agent 监控数据上报

---

## Sprint 8（W15-16）：自愈 + 事件

---

**US-AB35: Event Center — 告警路由**

**Description:** As an Ops, I want Event Center to route alerts to appropriate handlers, so that the right action is taken automatically.

**Acceptance Criteria:**
1. 接收 NATS 事件（alert.desktop.*）和 Alertmanager Webhook
2. 路由规则：Fatal → 通知 + Kagent、Error → 自愈 + 通知、Warning → 仅审计
3. 通知渠道：钉钉/企业微信/邮件（可配置）
4. 事件格式统一：eventId、eventType、severity、source、resourceType、resourceId、payload
5. 多副本 Queue Group 竞争消费，同一事件只处理一次

**Points:** 8 | **Assignee:** Broker-1

---

**US-AB36: Event Center — 内置自愈规则**

**Description:** As an Ops, I want Event Center to automatically heal common desktop errors, so that I don't need to manually restart desktops at 3 AM.

**Acceptance Criteria:**
1. CrashLoopBackOff → 重启 VM/Pod（K8s API delete Pod）
2. Agent 心跳超时 → 先尝试重启 Agent 进程（K8s exec），失败则重启 VM/Pod
3. 自愈指数退避：立即 → 30s → 60s，最大重试 3 次
4. 超过重试次数 → DesktopState → Error（Fatal），升级到 Kagent
5. 自愈结果写入审计日志（action: self_heal）

**Points:** 8 | **Assignee:** Broker-2

---

**US-AB37: Event Center — Kagent Bridge**

**Description:** As an Ops, I want Event Center to escalate unresolvable errors to Kagent, so that AI-powered diagnosis can handle complex issues.

**Acceptance Criteria:**
1. 自愈失败或 Fatal 事件 → 调用 Kagent Agent Invoke API
2. 构造告警上下文：desktopId、tenantId、alertName、firedAt
3. Kagent 处置结果回写审计日志（action: kagent.auto_heal）
4. 高危操作（stop_desktop、reset_desktop）需 Human-in-the-loop 审批
5. Kagent 不可用时降级：仅通知 + 审计，不执行处置

**Points:** 5 | **Assignee:** Broker-1

---

**US-AB38: Kagent MCP Server（只读工具）**

**Description:** As a Kagent, I want read-only access to Broker data, so that I can diagnose desktop issues without risk of unintended side effects.

**Acceptance Criteria:**
1. MCP Server 实现只读工具：get_desktop、list_desktops、get_session、get_effective_policy、get_user_quota、list_audit_logs、get_resource_pool
2. 工具注册为 Kagent RemoteMCPServer CRD
3. 所有数据通过 Broker API 获取，MCP Server 不直接访问 DB
4. 认证：MCP Server 使用 ServiceAccount 调用 Broker 内部 API
5. SSE 模式，超时 5 分钟

**Points:** 5 | **Assignee:** Broker-2

---

**Sprint 8 交付物**: 自愈闭环可工作
- ✅ 告警路由 + 通知 + 自愈 + Kagent 升级
- ✅ MCP Server 只读工具就绪

---

## Sprint 9（W17-18）：审计 + 安全

---

**US-AB39: Audit Service — 异步日志写入**

**Description:** As an Admin, I want all operations logged for compliance, so that I can trace any action back to a specific user.

**Acceptance Criteria:**
1. 订阅 NATS `audit.*` Topic，异步写入 PostgreSQL audit_logs 表
2. 日志字段完整：id、userId、tenantId、action、resourceType、resourceId、result、clientIp、clientType、extra
3. 写入延迟 <1s（P99）
4. 按月自动创建分区（pg_cron 或 Broker 定时任务）
5. 过期分区清理（保留 180 天）

**Points:** 5 | **Assignee:** Broker-1

---

**US-AB40: Audit Service — 日志查询接口**

**Description:** As an Admin, I want to search audit logs, so that I can investigate security incidents.

**Acceptance Criteria:**
1. `GET /api/v1/audit-logs` — 支持按 tenantId、userId、desktopId、action、startTime、endTime 过滤
2. 分页查询，默认按 created_at 降序
3. 查询响应 <2s（含索引优化）
4. tenant_admin 只能查看本租户日志
5. 日志只读，不提供删除接口

**Points:** 3 | **Assignee:** Broker-1

---

**US-AB41: 安全策略执行 — 水印 + 截屏/录屏禁用**

**Description:** As an Admin, I want watermark and screen capture prevention enforced, so that data leakage can be traced and deterred.

**Acceptance Criteria:**
1. Client 端水印渲染：根据 Policy.watermark 配置（enabled、fields、style、position）
2. 水印内容：{username} {datetime} 等变量替换
3. 水印叠加在远程桌面画面上，透明度可配置（默认 15%）
4. screenshotDisabled=true → Client 端禁用截屏快捷键
5. screenRecordDisabled=true → Client 端检测录屏并警告
6. Agent 端水印方案：频域盲水印预留接口（V2 实现）

**Points:** 5 | **Assignee:** Broker-2

---

**US-AB42: 认证集成 — LDAP/AD**

**Description:** As an Admin, I want to integrate with our corporate LDAP/AD, so that users can login with their existing credentials.

**Acceptance Criteria:**
1. Broker 配置 LDAP 连接：host、port、baseDN、bindDN、bindPassword
2. 登录时查询 LDAP 验证密码，本地不存密码
3. LDAP 用户首次登录自动创建本地 User 记录（authType=ldap）
4. LDAP 组 → Broker 角色映射（可配置）
5. LDAP 不可用时降级到本地认证（authType=local 的用户）

**Points:** 5 | **Assignee:** Broker-2

---

**Sprint 9 交付物**: 审计与安全就绪
- ✅ 审计日志完整写入和查询
- ✅ 安全策略执行（水印、截屏禁用）
- ✅ LDAP/AD 认证集成

---

## Sprint 10（W19-20）：Windows VM + Kagent

---

**US-AB43: KubeVirt Windows VM 桌面**

**Description:** As an Admin, I want to offer Windows desktops, so that users who need Windows applications are supported.

**Acceptance Criteria:**
1. Windows 10/11 VM 模板制作：KubeVirt VirtualMachine CRD + Windows virtio 驱动
2. GPU 直通配置：KubeVirt CRD 声明 host-passthrough，Agent 检测到 NVENC
3. Windows Agent 安装：Agent 二进制 + Windows Service 注册
4. Windows 输入处理：SendInput API 替代 xdotool
5. 桌面创建时 osType=windows → Scheduler 选择支持 KubeVirt 的节点
6. Windows VM 冷启动 <120 秒

**Points:** 13 | **Assignee:** Ops + Agent

---

**US-AB44: Kagent MCP Server（可写工具 + Human-in-the-loop）**

**Description:** As a Kagent, I want to execute remediation actions on Broker, so that I can automatically fix desktop issues.

**Acceptance Criteria:**
1. 可写工具：restart_desktop、close_session、stop_desktop、reset_desktop、write_audit_log
2. 风险分级：restart/close → 低危（自动执行）、stop → 中危（需审批）、reset → 高危（需审批）
3. Kagent Agent 定义：vdi-ops-agent CRD，systemMessage 含诊断处置流程
4. Human-in-the-loop：高危工具执行前暂停，等待钉钉/企微审批
5. 处置结果回写审计日志

**Points:** 8 | **Assignee:** Broker-1

---

**US-AB45: Session 断线重连完整链路**

**Description:** As a User, I want my session to automatically reconnect when the network blips, so that I don't lose my work.

**Acceptance Criteria:**
1. WebSocket 断开 → Broker Session → Disconnected，启动 30s 超时
2. 客户端 30s 内重连 → Broker 验证原 Session Token → Session → Connected
3. 重连时重新协商 WebRTC（新 Offer/Answer/ICE）
4. Agent 侧：原 PeerConnection 保留 30s，超时后释放
5. 超时未重连 → Session → Closed，UsageState → Inactive
6. 指数退避重连：1s/2s/4s/8s/16s

**Points:** 5 | **Assignee:** Broker-2

---

**Sprint 10 交付物**: Windows 桌面 + Kagent 可写
- ✅ Windows VM 桌面可用
- ✅ Kagent 自动诊断+处置
- ✅ 断线重连完整

---

## Sprint 11（W21-22）：性能 + 稳定性 → RC

---

**US-AB46: Broker 性能优化**

**Description:** As a Developer, I want Broker to meet performance targets, so that the platform can handle 500+ concurrent sessions.

**Acceptance Criteria:**
1. REST API P99 延迟 <200ms（不含 K8s API 调用）
2. Session 创建 P99 <500ms
3. WebSocket 消息推送延迟 <100ms
4. 并发 Session 支持 ≥500（单集群）
5. Agent 心跳处理吞吐 ≥5000/s
6. 瓶颈分析：数据库连接池、Redis 连接池、NATS 批量发布优化

**Points:** 8 | **Assignee:** Broker-1 + Broker-2

---

**US-AB47: Agent 弱网优化**

**Description:** As a User, I want smooth desktop experience even on poor network, so that I can work from anywhere.

**Acceptance Criteria:**
1. 2% 丢包场景：操作延迟比局域网增加 ≤30%
2. 跨运营商（电信↔联通）：延迟 ≤竞品 +20%
3. 自适应码率响应时间 <3s（检测到质量下降 → 编码参数调整生效）
4. TURN 中转场景：延迟增加 ≤50ms
5. 网络恢复后画质恢复 <5s

**Points:** 8 | **Assignee:** Agent

---

**US-AB48: 压力测试 + 稳定性验证**

**Description:** As an Ops, I want to verify the platform handles peak load, so that I'm confident in production readiness.

**Acceptance Criteria:**
1. 50 桌面同时冷启动，3 分钟内全部 Ready
2. 200 并发 Session 同时建连，成功率 ≥95%
3. Broker 单副本故障，客户端 10s 内自动重连到其他副本
4. PostgreSQL 主从切换，Broker 30s 内恢复服务
5. 24 小时稳定性测试：无内存泄漏、无 goroutine 泄漏、无连接泄漏

**Points:** 5 | **Assignee:** Ops

---

**Sprint 11 交付物**: V1.0-rc 🎯
- ✅ 性能达标（KR1-KR8）
- ✅ 弱网体验可接受
- ✅ 压力测试通过

---

## Sprint 12（W23-24）：生产就绪 → V1.0

---

**US-AB49: 安全加固**

**Description:** As an Admin, I want the platform to be production-secure, so that our corporate data is protected.

**Acceptance Criteria:**
1. 所有 REST API 强制 HTTPS，WebSocket 强制 WSS
2. WebRTC 连接强制 DTLS 加密
3. JWT RS256 密钥轮换方案（支持双密钥过渡期）
4. CSP 头部配置正确
5. SQL 注入防护：所有查询参数化
6. 安全扫描：Trivy 容器镜像扫描无 Critical 漏洞

**Points:** 5 | **Assignee:** Broker-1

---

**US-AB50: 现有客户迁移工具**

**Description:** As an Admin, I want a migration tool to move existing desktops to the new architecture, so that we can upgrade without disruption.

**Acceptance Criteria:**
1. 迁移脚本：导出旧系统桌面配置 → 导入 Broker 数据库
2. 策略映射：旧系统安全策略 → Broker TenantPolicy
3. 用户映射：旧系统用户 → Broker User（含 LDAP 映射）
4. 灰度迁移：支持按租户逐步迁移，新旧系统并行期
5. 回滚方案：迁移失败可回退到旧系统

**Points:** 8 | **Assignee:** Broker-2 + Ops

---

**US-AB51: 生产环境部署 + 文档**

**Description:** As an Ops, I want production deployment automation and documentation, so that the platform can be reliably deployed and maintained.

**Acceptance Criteria:**
1. Helm Chart：Broker 6 子服务 + PostgreSQL + Redis + NATS 一键部署
2. 备份恢复方案：PostgreSQL 定时备份 + PVC 快照
3. 运维手册：部署、扩缩容、故障排查、升级流程
4. 用户手册：管理员操作指南、终端用户使用指南
5. API 文档：Swagger/OpenAPI 自动生成

**Points:** 8 | **Assignee:** Ops

---

**Sprint 12 交付物**: V1.0 正式发布 🎯
- ✅ 安全加固通过
- ✅ 迁移工具可用
- ✅ 生产部署自动化
- ✅ 文档完善

---

## Sprint 总览

| Sprint | 周次 | 主题 | Agent Story Pts | Broker Story Pts | Ops Story Pts | 总计 |
|--------|------|------|:-:|:-:|:-:|:-:|
| S1 | W1-2 | 基础设施+Agent重构 | 13 | 18 | 8 | 39 |
| S2 | W3-4 | Broker Desktop核心 | — | 34 | 3 | 37 |
| S3 | W5-6 | Agent-Broker联调 | 21 | 13 | — | 34 |
| S4 | W7-8 | 会话+信令→Alpha | 16 | 21 | 5 | 42 |
| S5 | W9-10 | 调度+资源管理 | — | 21 | 8 | 29 |
| S6 | W11-12 | 策略+TURN→Beta | 10 | 13 | — | 23 |
| S7 | W13-14 | 监控+告警 | 3 | 18 | 5 | 26 |
| S8 | W15-16 | 自愈+事件 | — | 26 | — | 26 |
| S9 | W17-18 | 审计+安全 | — | 18 | — | 18 |
| S10 | W19-20 | Windows+Kagent | 0* | 13 | 13 | 26 |
| S11 | W21-22 | 性能+稳定性→RC | 8 | 8 | 5 | 21 |
| S12 | W23-24 | 生产就绪→V1.0 | — | 13 | 8 | 21 |
| | | | **71** | **238** | **58** | **367** |

> *Agent 在 S10 的工作量包含在 US-AB43 中（Windows 输入处理），与 Ops 协作

### 团队负载分配

| 成员 | S1 | S2 | S3 | S4 | S5 | S6 | S7 | S8 | S9 | S10 | S11 | S12 |
|------|-----|-----|-----|-----|-----|-----|-----|-----|-----|------|------|------|
| **Broker-1** | 5+5=10 | 8 | 5 | 13 | 13 | 8 | 5+5=10 | 8+5=13 | 5+3=8 | 8 | 8 | 5 |
| **Broker-2** | 8 | 8+5=13 | 8 | 8 | 5+3=8 | 5 | 8 | 8 | 5+5=10 | 5 | 8 | 8 |
| **Agent** | 8+5=13 | — | 13+5+3=21 | 8+8=16 | — | 5+5=10 | 3 | — | — | 13* | 8 | — |
| **Ops** | 8 | 3 | — | 5 | 8 | — | 5 | — | — | 13 | 5 | 8 |

---

### 里程碑对齐

| 里程碑 | 时间 | Agent/Broker 交付 | 客户端交付 |
|--------|------|-------------------|-----------|
| **Alpha** | W8 | 端到端信令通路 + WebRTC + 心跳 | Web 核心可用 |
| **Beta** | W16 | 完整 Broker + 策略 + 自适应码率 | 双端+管理控制台 |
| **RC** | W22 | 全功能 + 性能达标 + Windows | 全功能+安全合规 |
| **V1.0** | W24 | 生产就绪 + 迁移工具 + 文档 | 正式发布 |

---

### 风险缓冲

| 风险 | 影响 | 缓冲方式 |
|------|------|----------|
| KubeVirt Windows VM 不稳定 | S10 延期 | Windows 支持降级为 P1，V1 先聚焦 Linux 容器桌面 |
| gRPC Agent-Broker 通信延迟高 | S3 联调延期 | 心跳保留 HTTP REST 回退，gRPC 仅用于高频场景 |
| S4 Story Point 过高（42pts）| Alpha 延期 | US-AB20（信令适配）可简化为 MVP 直连模式先跑通，代理模式延后到 S5 |
| Kagent MCP Server 实现复杂 | S8-S10 延期 | V1 先实现内置自愈规则（无 Kagent），Kagent 作为 V1.5 增值 |
| S1 Agent 重构引入回归 | MVP 功能中断 | 重构前先写集成测试，确保 WebRTC+GStreamer+输入链路不变 |
| 弱网优化不达标 | RC 延期 | 评估 WebRTC+私有优化双栈方案（发现计划 G1 结果决定） |

---

## Backlog（V2+ 候选）

| 优先级 | 功能 | 用户故事草案 |
|--------|------|-------------|
| P1 | 多轨 PTS 同步推流 | As an Agent, I want to push multiple video tracks for multi-monitor setups |
| P1 | 频域盲水印 | As an Admin, I want invisible watermarks embedded in the video stream for traceability |
| P1 | OIDC/OAuth2 认证 | As an Admin, I want to integrate with our corporate SSO via OIDC |
| P2 | 打印重定向 | As a User, I want to print from the remote desktop to my local printer |
| P2 | 智能卡重定向 | As a User, I want to use my smart card reader with the remote desktop |
| P2 | 工单系统集成 | As an Ops, I want Event Center to automatically create tickets in our ITSM |
| P3 | 国产化适配（麒麟/统信） | As an Admin, I want desktops running on domestic OS |
| P3 | AI 桌面助手 | As a User, I want to use natural language to control my desktop |

---

*本文档基于 PRD-EVDI-cloud-desktop-v1.md、02-Broker控制平面架构设计.md、03-Agent架构设计.md 产出，与 user-stories-sprint-plan-v1.md（客户端）并行执行。*
