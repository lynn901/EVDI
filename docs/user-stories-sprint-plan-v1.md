# 用户故事 & Sprint 规划：EVDI 云桌面客户端 V1 MVP

**团队**: 3人（1前端·Web / 1 Rust 工程师 / 1全栈·共享层+Web）
**节奏**: 2 周/Sprint，共 6 个 Sprint
**时间线**: 12 周

---

## 用户角色

| 角色 | 代号 | 描述 |
|------|------|------|
| 日常办公用户 | User | 通过浏览器访问远程桌面 |
| 重度用户 | ProUser | 安装 Tauri 客户端，使用 USB/多屏等高级功能 |
| IT 管理员 | Admin | 管理桌面实例和用户策略 |

---

## Sprint 1-2: V1.0-alpha（Web 核心）

### Sprint 1（Week 1-2）：共享层 + 连接基础

---

**US-01: JWT 认证登录**

**Description:** As a User, I want to log in with my credentials, so that I can securely access my cloud desktops.

**Acceptance Criteria:**
1. 登录页提供用户名/密码输入框和登录按钮
2. 登录成功后获取 Access Token（30min）和 Refresh Token
3. Access Token 过期前 5 分钟自动静默刷新，用户无感知
4. Token 刷新失败时跳转回登录页，提示"会话已过期"
5. 登录失败显示明确错误信息（用户名错误/密码错误/网络错误）
6. Token 存储在内存中（非 localStorage），关闭标签页即失效

**Points:** 5 | **Assignee:** 全栈

---

**US-02: Broker API 通信层**

**Description:** As a Developer, I want a unified API client, so that Web and Tauri clients share the same communication logic.

**Acceptance Criteria:**
1. 封装 REST API 客户端（axios），自动附带 JWT Token
2. 封装 WebSocket 客户端，支持心跳保活和断线重连
3. 401 响应自动触发 Token 刷新，刷新失败跳转登录
4. API 模块独立于 UI 框架，可在 Web/Tauri/测试环境复用
5. TypeScript 类型完整覆盖所有 API 请求/响应

**Points:** 8 | **Assignee:** 全栈

---

**US-03: 桌面列表页**

**Description:** As a User, I want to see all my available desktops, so that I can choose which one to connect.

**Acceptance Criteria:**
1. 登录后进入桌面列表页，展示用户可用的桌面实例
2. 每个桌面卡片显示：名称、操作系统图标、运行状态（运行中/已停止）、最近连接时间
3. 运行中的桌面显示"连接"按钮，已停止的显示"启动"按钮
4. 列表支持分页（每页 20 条）和按名称搜索
5. 桌面状态每 10 秒自动刷新（WebSocket 推送或轮询）
6. 无桌面时显示空状态引导："请联系管理员分配桌面"

**Points:** 5 | **Assignee:** 前端

---

**US-04: WebRTC 信令协议实现**

**Description:** As a Developer, I want a reusable signaling protocol, so that both Web and Tauri clients can establish WebRTC connections through Broker.

**Acceptance Criteria:**
1. 实现 SDP Offer/Answer 交换协议
2. 实现 ICE Candidate 交换协议
3. 从 Broker 获取 TURN 服务器地址和短期凭证
4. 信令消息通过 WebSocket 传输，格式与 Agent 端一致
5. 连接超时（30秒）后自动取消并通知用户
6. 信令状态机可观测（连接中/已连接/断开/错误）

**Points:** 8 | **Assignee:** 全栈

---

### Sprint 2（Week 3-4）：远程桌面核心

---

**US-05: WebRTC 视频流渲染**

**Description:** As a User, I want to see the remote desktop screen in my browser, so that I can visually interact with my cloud desktop.

**Acceptance Criteria:**
1. 通过 RTCPeerConnection 接收 H.264 视频流
2. HTML5 Canvas 渲染视频帧，1080p 30fps 流畅无撕裂
3. 窗口大小变化时视频自动缩放适配（保持宽高比）
4. 支持鼠标点击 Canvas 聚焦，聚焦后才发送输入事件
5. 视频流断开时 Canvas 冻结最后一帧（不黑屏）
6. Canvas 渲染 CPU 占用 <15%（1080p 场景）

**Points:** 8 | **Assignee:** 前端

---

**US-06: Opus 音频流播放**

**Description:** As a User, I want to hear the remote desktop audio, so that I can attend meetings and watch videos.

**Acceptance Criteria:**
1. 通过 RTCPeerConnection 接收 Opus 音频流
2. 使用 Web Audio API 播放音频
3. 音画同步延迟 <50ms
4. 提供静音按钮
5. 浏览器标签页非激活状态时音频继续播放
6. 无音频流时不报错（某些桌面模板可能无音频）

**Points:** 3 | **Assignee:** 前端

---

**US-07: 键鼠输入传输（WebSocket 回退）**

**Description:** As a User, I want to control the remote desktop with my keyboard and mouse, so that I can interact with applications.

**Acceptance Criteria:**
1. 鼠标移动/点击/滚轮事件正确映射到远端
2. 键盘按键（含修饰键、功能键）正确映射到远端
3. 中文输入法正常工作（组合键正确传递）
4. 鼠标移动平滑无跳跃，延迟 <30ms（局域网 WebSocket）
5. 事件格式与 Agent 端 WebSocket 输入协议一致
6. Canvas 非聚焦状态下不发送输入事件

**Points:** 8 | **Assignee:** 全栈

---

**US-08: 客户端类型标识**

**Description:** As a System, I want to identify the client type, so that Broker and Agent can provide appropriate capabilities.

**Acceptance Criteria:**
1. Web 客户端连接时在信令消息中附带 `clientType: "web"`
2. Tauri 客户端连接时附带 `clientType: "tauri"`
3. Broker 根据 clientType 返回不同的 TURN 配置和功能策略
4. Agent 根据 clientType 决定启用的通道能力（如 USB 仅 tauri）

**Points:** 2 | **Assignee:** 全栈

---

**US-09: 连接流程端到端**

**Description:** As a User, I want a smooth connection flow, so that I can enter my desktop with one click.

**Acceptance Criteria:**
1. 点击"连接"→ 显示连接进度（获取凭证 → 信令协商 → 媒体连接）
2. 每个阶段有清晰的进度指示
3. 连接成功后自动进入全屏远程桌面
4. 连接失败时显示错误原因和重试按钮
5. 连接过程中可点击"取消"中断
6. 端到端连接时间 <10 秒（局域网）

**Points:** 5 | **Assignee:** 前端

---

**Sprint 1-2 交付物**: V1.0-alpha
- ✅ 浏览器打开 → 登录 → 桌面列表 → 点击连接 → 看到远程桌面 → 键鼠操作
- ✅ 共享 Broker API 层可复用

---

## Sprint 3-4: V1.0-beta（Web 完善 + Tauri 核心）

### Sprint 3（Week 5-6）：Web 体验增强

---

**US-10: 基础剪贴板同步**

**Description:** As a User, I want to copy text from my local computer and paste it into the remote desktop (and vice versa), so that I can transfer text without retyping.

**Acceptance Criteria:**
1. 本地 Ctrl+C → 远端 Ctrl+V 文本正确同步
2. 远端 Ctrl+C → 本地 Ctrl+V 文本正确同步
3. 首次使用剪贴板时弹出浏览器授权提示
4. 授权被拒绝时显示提示："请在浏览器设置中允许剪贴板访问"
5. 支持纯文本同步（不支持富文本/图片，V1 范围）
6. 剪贴板同步延迟 <500ms

**Points:** 5 | **Assignee:** 前端

---

**US-11: 全屏模式切换**

**Description:** As a User, I want to switch to fullscreen mode, so that I can focus on my remote desktop without distractions.

**Acceptance Criteria:**
1. 双击 Canvas 或 F11 进入全屏模式
2. 全屏模式下 Escape 退出全屏（先弹出确认"是否退出全屏"）
3. 全屏模式下工具栏自动隐藏
4. 全屏切换时视频流不中断
5. 浏览器不支持全屏 API 时隐藏全屏按钮
6. 窗口 ↔ 全屏切换 <200ms 无闪烁

**Points:** 3 | **Assignee:** 前端

---

**US-12: 智能浮动工具栏**

**Description:** As a User, I want a non-intrusive toolbar, so that I can access clipboard, fullscreen, and disconnect without leaving the desktop view.

**Acceptance Criteria:**
1. 鼠标移至屏幕顶部 20px 区域触发工具栏滑入
2. 工具栏包含：剪贴板开关、全屏切换、断开连接
3. 3 秒无操作工具栏自动滑出
4. 工具栏不遮挡桌面顶部内容（显示在画面上方叠加层）
5. 快捷键 Ctrl+Shift+T 手动触发/隐藏工具栏
6. 工具栏位置可拖拽调整（顶部/底部）

**Points:** 5 | **Assignee:** 前端

---

**US-13: 一键重连**

**Description:** As a User, I want the connection to automatically recover when my network blips, so that I don't lose my work.

**Acceptance Criteria:**
1. 网络中断时冻结最后一帧 + 半透明遮罩 + "正在重新连接..."
2. 自动重连最多 3 次，间隔 2s/4s/8s（指数退避）
3. 重连成功后遮罩消失，桌面状态完整保留
4. 重连过程中显示倒计时和重试次数
5. 3 次重连失败后显示"连接已断开"+ 手动重连按钮
6. 重连期间键鼠输入队列缓存，恢复后按序发送

**Points:** 8 | **Assignee:** 全栈

---

**US-14: 连接状态指示**

**Description:** As a User, I want to see my connection quality, so that I know when network issues affect my experience.

**Acceptance Criteria:**
1. 工具栏显示延迟数值（ms）和连接质量图标（🟢好/🟡一般/🔴差）
2. 延迟 <100ms 绿色，100-300ms 黄色，>300ms 红色
3. 数据来自 WebRTC getStats() 实时统计
4. 弱网时弹出提示："网络不稳定，画面质量可能下降"
5. 显示当前分辨率和帧率（可折叠）

**Points:** 3 | **Assignee:** 前端

---

### Sprint 4（Week 7-8）：Tauri 客户端基础

---

**US-15: Tauri 项目初始化 + Canvas 渲染**

**Description:** As a ProUser, I want to install a lightweight native app, so that I can access my desktops with better performance.

**Acceptance Criteria:**
1. Tauri v2 项目搭建，安装包 <15MB
2. 复用 Web 客户端的 React 组件和共享 API 层
3. Canvas 渲染与 Web 客户端行为一致
4. 原生窗口支持拖拽、缩放、最小化/最大化
5. 连接时 `clientType: "tauri"` 正确标识
6. 共享代码比例 ≥65%（目标）

**Points:** 8 | **Assignee:** Rust

---

**US-16: 系统托盘**

**Description:** As a ProUser, I want the app to run in the system tray, so that I can quickly reconnect without opening the full window.

**Acceptance Criteria:**
1. 关闭窗口时最小化到系统托盘（不退出）
2. 托盘菜单：最近桌面列表、打开主窗口、退出
3. 托盘图标显示连接状态（已连接/断开）
4. 双击托盘图标打开主窗口
5. 系统托盘可在设置中禁用（关闭即退出）

**Points:** 5 | **Assignee:** Rust

---

**US-17: Tauri 键鼠输入（DataChannel 优先）**

**Description:** As a ProUser, I want lower input latency in the native app, so that my remote desktop feels more responsive.

**Acceptance Criteria:**
1. DataChannel 可用时优先使用 DataChannel 传输键鼠事件
2. DataChannel 不可用时回退到 WebSocket（与 Web 客户端一致）
3. DataChannel 模式下键鼠延迟 <10ms（局域网）
4. DataChannel 断开时自动切换到 WebSocket，无输入丢失

**Points:** 8 | **Assignee:** 全栈 + Rust

---

**US-18: Tauri 剪贴板增强**

**Description:** As a ProUser, I want seamless clipboard sharing, so that I don't need to deal with browser permission prompts.

**Acceptance Criteria:**
1. Tauri 使用 Rust clipboard 库直接读写系统剪贴板
2. 无浏览器授权弹窗
3. 文本剪贴板双向同步，延迟 <200ms
4. 剪贴板策略可配置（单向/双向/禁用）

**Points:** 3 | **Assignee:** Rust

---

**Sprint 3-4 交付物**: V1.0-beta
- ✅ Web 客户端体验完善（剪贴板、全屏、工具栏、重连）
- ✅ Tauri 客户端基础可用（Canvas 渲染 + 系统托盘）

---

## Sprint 5: V1.0-rc（全功能）

### Sprint 5（Week 9-10）：Tauri 专业功能

---

**US-19: wgpu 加速渲染**

**Description:** As a ProUser, I want GPU-accelerated rendering, so that I get lower latency and smoother visuals.

**Acceptance Criteria:**
1. wgpu 渲染 H.264 解码帧到窗口 surface
2. 延迟 < Canvas 模式的 60%（目标 <25ms）
3. CPU 占用比 Canvas 降低 ≥30%
4. wgpu 初始化失败时自动回退到 Canvas，不闪退
5. 支持运行时切换渲染模式（设置→渲染→wgpu/Canvas）

**Points:** 13 | **Assignee:** Rust

> **注**: 此 Story 依赖 G2 实验验证通过。如 G2 失败，此 Story 替换为 "Canvas 渲染优化"（Points: 5）

---

**US-20: USB HID 重定向**

**Description:** As a ProUser, I want to use my local USB keyboard/mouse/gamepad with the remote desktop, so that I can interact with devices that need direct USB connection.

**Acceptance Criteria:**
1. 检测本地 USB HID 设备并列在"设备"面板中
2. 用户勾选设备后，设备输入通过 DataChannel 隧道转发到远端
3. 键盘/鼠标/游戏手柄三类 HID 设备至少各一种实测通过
4. 设备重定向延迟 <15ms（局域网）
5. 拔出设备时自动停止重定向，远端设备消失
6. 同一设备不能同时被本地和远端使用（互斥）

**Points:** 13 | **Assignee:** Rust

---

**US-21: 文件拖放传输**

**Description:** As a ProUser, I want to drag files from my local computer into the remote desktop, so that I can quickly transfer files.

**Acceptance Criteria:**
1. 文件从本地拖入 Tauri 窗口 → 通过 DataChannel 传输到远端
2. 传输进度条显示在工具栏中
3. 单文件 ≤100MB 可传输
4. 多文件拖放支持（按序传输）
5. 传输中断（断线）后可恢复
6. 远端文件保存到用户桌面或指定目录

**Points:** 8 | **Assignee:** Rust

---

**US-22: 系统键盘 Hook**

**Description:** As a ProUser, I want system shortcuts like Ctrl+Alt+Del to be sent to the remote desktop, so that I can perform admin tasks.

**Acceptance Criteria:**
1. 可配置的快捷键拦截列表（默认：Ctrl+Alt+Del, Win+L, Alt+Tab）
2. 被拦截的快捷键发送到远端而非本地系统
3. 拦截可在设置中禁用/自定义
4. 未被拦截的快捷键正常传递给本地系统
5. 安全模式下（如 UAC 弹窗）部分 Hook 可能失效，需文档说明

**Points:** 5 | **Assignee:** Rust

---

**US-23: 会话水印**

**Description:** As an Admin, I want to display a watermark on the remote desktop, so that I can trace screen captures to specific users.

**Acceptance Criteria:**
1. 透明水印叠加在远程桌面画面上
2. 水印内容：用户名 + 时间 + 会话 ID（可配置）
3. 水印透明度可调节（默认 10%，不干扰操作但截图可识别）
4. 水印位置可配置：居中/平铺/右下角
5. 水印策略由 Broker 下发，客户端不可关闭

**Points:** 3 | **Assignee:** 前端 + Rust

---

**US-24: 自适应码率反馈**

**Description:** As a User, I want the video quality to adapt to my network conditions, so that I get smooth experience even on slow connections.

**Acceptance Criteria:**
1. 客户端每 2 秒采集 WebRTC stats（丢包率/延迟/抖动/可用带宽）
2. 网络质量下降时通过 DataChannel/WebSocket 发送反馈消息给 Agent
3. Agent 收到反馈后调整编码参数（降低分辨率/帧率/码率）
4. 网络恢复后自动提升质量
5. 画质变化时显示短暂提示"网络波动，已降低画质"
6. 用户可手动锁定画质（设置→画质→自动/高/中/低）

**Points:** 5 | **Assignee:** 全栈

---

**Sprint 5 交付物**: V1.0-rc
- ✅ Tauri 专业功能完成（wgpu/USB/文件/Hook/水印）
- ✅ 自适应码率
- ✅ 功能冻结

---

## Sprint 6: V1.0（正式发布）

### Sprint 6（Week 11-12）：优化 + 发布

---

**US-25: 首次使用引导**

**Description:** As a new User, I want a guided setup, so that I can connect to my first desktop without reading documentation.

**Acceptance Criteria:**
1. 首次登录后显示 3 步引导：①选择桌面 → ②连接 → ③快捷键提示
2. 每步有简洁说明和动画示意
3. 引导完成后不再显示（可在设置中重新触发）
4. 已有桌面连接记录的用户跳过引导

**Points:** 3 | **Assignee:** 前端

---

**US-26: 性能优化**

**Description:** As a User, I want the client to be fast and light, so that it doesn't slow down my computer.

**Acceptance Criteria:**
1. Web 客户端首屏加载 <3 秒（3G 网络）
2. Web 客户端内存占用 <200MB（连接中）
3. Tauri 安装包 <15MB
4. Tauri 内存占用 <150MB（连接中）
5. 桌面列表页切换 <300ms
6. 连接建立时间 <8 秒（局域网，P95）

**Points:** 8 | **Assignee:** 全团队

---

**US-27: 跨浏览器兼容性**

**Description:** As a User, I want the web client to work on Chrome, Firefox, and Edge, so that I'm not locked into a specific browser.

**Acceptance Criteria:**
1. Chrome 120+ 全功能通过
2. Firefox 120+ 全功能通过
3. Edge 120+ 全功能通过（与 Chrome 共享 Chromium）
4. Safari 17+ 核心功能通过（剪贴板/全屏已知限制需标注）
5. 每个浏览器的已知限制文档化

**Points:** 5 | **Assignee:** 前端

---

**US-28: 安全加固**

**Description:** As an Admin, I want the client to be secure, so that our corporate data is protected.

**Acceptance Criteria:**
1. Token 不存储在 localStorage/sessionStorage
2. WebRTC 连接强制 DTLS 加密
3. 信令通道强制 WSS（WebSocket Secure）
4. 连接超时自动断开（可配置，默认 30 分钟无操作）
5. XSS 防护：所有用户输入经过转义
6. CSP 头部正确配置

**Points:** 5 | **Assignee:** 全栈

---

**US-29: 用户文档**

**Description:** As a User, I want clear documentation, so that I can troubleshoot common issues myself.

**Acceptance Criteria:**
1. 快速入门指南（1 页，3 步上手）
2. 常见问题 FAQ（至少 10 条）
3. 快捷键参考卡
4. 浏览器兼容性说明
5. Tauri 安装指南（Win/Mac/Linux）

**Points:** 3 | **Assignee:** 全团队

---

**Sprint 6 交付物**: V1.0 正式版
- ✅ 性能达标
- ✅ 跨浏览器兼容
- ✅ 安全加固
- ✅ 文档完善

---

## Sprint 总览

| Sprint | 周次 | 主题 | 交付物 | Story Points |
|--------|------|------|--------|-------------|
| S1 | W1-2 | 共享层 + 连接基础 | 认证 + API层 + 桌面列表 + 信令 | 26 |
| S2 | W3-4 | 远程桌面核心 | 视频渲染 + 音频 + 输入 + 连接流程 | 26 |
| S3 | W5-6 | Web 体验增强 | 剪贴板 + 全屏 + 工具栏 + 重连 + 状态 | 24 |
| S4 | W7-8 | Tauri 客户端基础 | Tauri初始化 + 托盘 + DataChannel + 剪贴板 | 24 |
| S5 | W9-10 | Tauri 专业功能 | wgpu + USB + 文件 + Hook + 水印 + 码率 | 47 |
| S6 | W11-12 | 优化 + 发布 | 引导 + 性能 + 兼容 + 安全 + 文档 | 24 |
| | | | **总计** | **171** |

### 团队负载分配

| 成员 | S1 | S2 | S3 | S4 | S5 | S6 |
|------|-----|-----|-----|-----|-----|-----|
| 前端(Web) | US-03 (5) | US-05,06,09 (16) | US-10,11,12,14 (16) | US-14增强 (3) | US-23 (3) | US-25,26,27 (16) |
| Rust | 实验4,5 (—) | 实验7,9 (—) | 实验4完成 (—) | US-15,16,18 (16) | US-19,20,21,22 (39) | US-26优化 (3) |
| 全栈 | US-01,02,04 (21) | US-07,08 (10) | US-13 (8) | US-17 (8) | US-24 (5) | US-26,28 (13) |

> **注**: Rust 工程师在 S1-S3 期间并行执行发现计划的 G2/G3 实验，S4 开始正式开发 Tauri 功能。

### 风险缓冲

| 风险 | 缓冲方式 |
|------|----------|
| G3(DataChannel)失败 | US-17 保留 WebSocket 回退，S4 不阻塞 |
| G2(wgpu)失败 | US-19 替换为 "Canvas 优化" (5pts)，S5 减少 8pts |
| Rust 人才不足 | S5 的 US-20(USB) 可延后到 V1.1，降低 S5 负载 |
| S5 负载过重(47pts) | US-20(USB) 和 US-21(文件) 可拆分到 V1.1 |

---

## Backlog（V2+ 候选）

| 优先级 | 功能 | 用户故事草案 |
|--------|------|-------------|
| P1 | 多桌面切换器 | As a User, I want to switch between multiple desktops with a visual overview |
| P1 | USB 大容量存储 | As a ProUser, I want to redirect my USB flash drive to the remote desktop |
| P2 | 打印重定向 | As a User, I want to print from the remote desktop to my local printer |
| P2 | 统一工作空间门户 | As a User, I want a dashboard with apps, files, and desktops in one place |
| P2 | 完整零信任安全 | As an Admin, I want device posture check before allowing connections |
| P3 | 智能卡重定向 | As a ProUser, I want to use my smart card reader with the remote desktop |
| P3 | AI 桌面助手 | As a User, I want to use natural language to control my desktop |
| — | 离线 PWA | 已淘汰 |

---

*本文档基于 PRD-cloud-desktop-client-v1.md 和 Discovery Plan 产出。*
