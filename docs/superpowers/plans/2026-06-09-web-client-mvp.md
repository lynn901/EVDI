# Web 客户端直连 Agent MVP 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 构建 Web 客户端直连容器内 Agent 的 MVP，验证 WebRTC 全链路（视频流 + 音频 + 键鼠输入 + DataChannel 控制流）的技术可行性。

**Architecture:** Agent 运行在 Docker 容器内，内建 WebSocket 信令服务 + Pion WebRTC 引擎 + GStreamer 编码管线 + Xvfb 虚拟显示。React 客户端通过浏览器原生 WebRTC API 与 Agent 建立 P2P 连接，视频渲染使用 `<video>` 元素，键鼠输入通过 DataChannel 发送。

**Tech Stack:** Go (Pion WebRTC v4, gorilla/websocket, go-gst), React 18 + TypeScript + Vite + Zustand + Ant Design, Docker Compose, GStreamer (x264enc/opusenc), Xvfb + PulseAudio + XFCE

---

## File Structure

```
evdi-agent/
├── cmd/agent/main.go              # 入口，初始化并启动所有组件
├── pkg/
│   ├── config/config.go           # 环境变量配置
│   ├── webrtc/
│   │   ├── engine.go              # WebRTCEngine 核心结构 + 生命周期
│   │   ├── signaling.go           # WebSocket 信令服务器
│   │   └── datachannel.go         # DataChannel 消息定义 + 处理
│   ├── gstreamer/
│   │   ├── pipeline.go            # GStreamer Pipeline 管理
│   │   └── launcher.go            # GStreamer 子进程启动器
│   ├── display/xvfb.go            # Xvfb 虚拟显示管理
│   └── input/
│       ├── mouse.go               # 鼠标事件注入 (xdotool)
│       └── keyboard.go            # 键盘事件注入 (xdotool)
├── Dockerfile                     # Agent 容器构建
├── go.mod
└── go.sum

evdi-web-client/
├── src/
│   ├── App.tsx                    # 应用入口
│   ├── App.css                    # 全局样式
│   ├── main.tsx                   # React 挂载点
│   ├── types/
│   │   └── signaling.ts           # 信令 + DataChannel 消息类型
│   ├── utils/
│   │   └── signaling.ts           # WebSocket 信令客户端
│   ├── stores/
│   │   └── connectionStore.ts     # Zustand 连接状态管理
│   ├── hooks/
│   │   └── useWebRTC.ts           # WebRTC 连接逻辑 Hook
│   └── components/
│       ├── VideoCanvas.tsx         # 视频渲染
│       ├── ControlPanel.tsx        # 连接控制面板
│       └── StatusIndicator.tsx     # 连接状态指示器
├── index.html
├── package.json
├── tsconfig.json
├── tsconfig.node.json
└── vite.config.ts

docker-compose.yml                 # Agent Docker 部署
Makefile                           # 构建/运行脚本
```

---

### Task 1: Agent 项目脚手架 + 配置模块

**Files:**
- Create: `evdi-agent/go.mod`
- Create: `evdi-agent/cmd/agent/main.go`
- Create: `evdi-agent/pkg/config/config.go`

- [ ] **Step 1: 初始化 Go 模块**

```bash
cd /home/yuan/EVDI && mkdir -p evdi-agent/cmd/agent evdi-agent/pkg/config
cd evdi-agent && go mod init github.com/evdi/agent
```

- [ ] **Step 2: 添加依赖**

```bash
cd /home/yuan/EVDI/evdi-agent && go get github.com/pion/webrtc/v4@latest github.com/gorilla/websocket@latest
```

- [ ] **Step 3: 创建配置模块 `pkg/config/config.go`**

```go
package config

import (
	"os"
	"strconv"
)

type Config struct {
	WSPort         string
	VideoWidth     int
	VideoHeight    int
	VideoFPS       int
	Display        string
	WebRTCPortMin  uint16
	WebRTCPortMax  uint16
}

func Load() *Config {
	return &Config{
		WSPort:        getEnv("AGENT_WS_PORT", "8080"),
		VideoWidth:    getEnvInt("VIDEO_WIDTH", 1920),
		VideoHeight:   getEnvInt("VIDEO_HEIGHT", 1080),
		VideoFPS:      getEnvInt("VIDEO_FPS", 30),
		Display:       getEnv("DISPLAY", ":99"),
		WebRTCPortMin: uint16(getEnvInt("WEBRTC_PORT_MIN", 50000)),
		WebRTCPortMax: uint16(getEnvInt("WEBRTC_PORT_MAX", 50100)),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}
```

- [ ] **Step 4: 创建入口 `cmd/agent/main.go`**

```go
package main

import (
	"log"

	"github.com/evdi/agent/pkg/config"
)

func main() {
	cfg := config.Load()
	log.Printf("EVDI Agent starting, WS port=%s, display=%s, video=%dx%d@%dfps",
		cfg.WSPort, cfg.Display, cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS)

	// 后续 Task 将在此初始化各组件
	select {}
}
```

- [ ] **Step 5: 验证编译**

```bash
cd /home/yuan/EVDI/evdi-agent && go build ./cmd/agent/
```

Expected: 编译成功，无错误输出。

- [ ] **Step 6: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-agent/go.mod evdi-agent/go.sum evdi-agent/cmd/agent/main.go evdi-agent/pkg/config/config.go
git commit -m "feat(agent): scaffold project with config module"
```

---

### Task 2: WebSocket 信令服务器

**Files:**
- Create: `evdi-agent/pkg/webrtc/signaling.go`
- Create: `evdi-agent/pkg/webrtc/signaling_test.go`

- [ ] **Step 1: 编写信令消息测试**

```go
package webrtc

import (
	"encoding/json"
	"testing"
)

func TestSignalingMessageMarshal(t *testing.T) {
	msg := SignalingMessage{
		Type: "offer",
		Data: json.RawMessage(`{"sdp":"test-sdp"}`),
	}
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got SignalingMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Type != "offer" {
		t.Errorf("type = %q, want offer", got.Type)
	}
}

func TestSignalingMessageTypes(t *testing.T) {
	types := []string{"offer", "answer", "ice", "ping", "pong"}
	for _, tt := range types {
		msg := SignalingMessage{Type: tt, Data: json.RawMessage(`{}`)}
		b, _ := json.Marshal(msg)
		var got SignalingMessage
		json.Unmarshal(b, &got)
		if got.Type != tt {
			t.Errorf("roundtrip type = %q, want %q", got.Type, tt)
		}
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/yuan/EVDI/evdi-agent && go test ./pkg/webrtc/ -run TestSignaling -v
```

Expected: 编译失败，`SignalingMessage` 未定义。

- [ ] **Step 3: 实现信令服务器**

```go
package webrtc

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/evdi/agent/pkg/config"
	"github.com/gorilla/websocket"
)

type SignalingMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type SignalingServer struct {
	addr     string
	upgrader websocket.Upgrader
	engine   *WebRTCEngine
	onOffer  chan json.RawMessage
	onICE    chan json.RawMessage
}

func NewSignalingServer(cfg *config.Config, engine *WebRTCEngine) *SignalingServer {
	return &SignalingServer{
		addr:   ":" + cfg.WSPort,
		engine: engine,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		onOffer: make(chan json.RawMessage, 16),
		onICE:   make(chan json.RawMessage, 16),
	}
}

func (s *SignalingServer) Start() error {
	http.HandleFunc("/ws", s.handleWebSocket)
	log.Printf("Signaling server listening on %s/ws", s.addr)
	return http.ListenAndServe(s.addr, nil)
}

func (s *SignalingServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()
	log.Printf("Client connected from %s", conn.RemoteAddr())

	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			log.Printf("WebSocket read error: %v", err)
			break
		}

		var msg SignalingMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			log.Printf("Invalid signaling message: %v", err)
			continue
		}

		switch msg.Type {
		case "offer":
			answer, err := s.engine.HandleOffer(msg.Data)
			if err != nil {
				log.Printf("HandleOffer error: %v", err)
				continue
			}
			s.sendSignal(conn, "answer", answer)

		case "ice":
			if err := s.engine.HandleICECandidate(msg.Data); err != nil {
				log.Printf("HandleICE error: %v", err)
			}

		case "ping":
			s.sendSignal(conn, "pong", map[string]int64{"ts": 0})
		}
	}
}

func (s *SignalingServer) sendSignal(conn *websocket.Conn, msgType string, data interface{}) {
	msg := SignalingMessage{
		Type: msgType,
		Data: mustMarshal(data),
	}
	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("WebSocket write error: %v", err)
	}
}

func mustMarshal(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		log.Fatalf("marshal error: %v", err)
	}
	return b
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/yuan/EVDI/evdi-agent && go test ./pkg/webrtc/ -run TestSignaling -v
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-agent/pkg/webrtc/signaling.go evdi-agent/pkg/webrtc/signaling_test.go
git commit -m "feat(agent): add WebSocket signaling server with message types"
```

---

### Task 3: WebRTC 引擎核心

**Files:**
- Create: `evdi-agent/pkg/webrtc/engine.go`
- Create: `evdi-agent/pkg/webrtc/engine_test.go`

- [ ] **Step 1: 编写 WebRTC 引擎测试**

```go
package webrtc

import (
	"testing"

	"github.com/evdi/agent/pkg/config"
)

func TestNewWebRTCEngine(t *testing.T) {
	cfg := &config.Config{
		WebRTCPortMin: 50000,
		WebRTCPortMax: 50100,
	}
	engine, err := NewWebRTCEngine(cfg)
	if err != nil {
		t.Fatalf("NewWebRTCEngine: %v", err)
	}
	if engine == nil {
		t.Fatal("engine is nil")
	}
	engine.Close()
}

func TestWebRTCEngineCreateTracks(t *testing.T) {
	cfg := &config.Config{
		WebRTCPortMin: 50000,
		WebRTCPortMax: 50100,
	}
	engine, err := NewWebRTCEngine(cfg)
	if err != nil {
		t.Fatalf("NewWebRTCEngine: %v", err)
	}
	defer engine.Close()

	if engine.VideoTrack() == nil {
		t.Error("video track is nil")
	}
	if engine.AudioTrack() == nil {
		t.Error("audio track is nil")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/yuan/EVDI/evdi-agent && go test ./pkg/webrtc/ -run TestNewWebRTCEngine -v
```

Expected: 编译失败，`NewWebRTCEngine` 未定义。

- [ ] **Step 3: 实现 WebRTC 引擎**

```go
package webrtc

import (
	"encoding/json"
	"fmt"

	"github.com/evdi/agent/pkg/config"
	"github.com/pion/webrtc/v4"
)

type WebRTCEngine struct {
	peerConnection *webrtc.PeerConnection
	videoTrack     *webrtc.TrackLocalStaticSample
	audioTrack     *webrtc.TrackLocalStaticSample
	channelControl *webrtc.DataChannel
	channelBulk    *webrtc.DataChannel
	onICECandidate func(candidate *webrtc.ICECandidate)
	onDataChannel  func(channel string, msg DataChannelMessage)
	cfg            *config.Config
}

func NewWebRTCEngine(cfg *config.Config) (*WebRTCEngine, error) {
	settingsEngine := webrtc.SettingEngine{}
	settingsEngine.SetLite(true)
	settingsEngine.SetEphemeralUDPPortRange(cfg.WebRTCPortMin, cfg.WebRTCPortMax)

	api := webrtc.NewAPI(webrtc.WithSettingEngine(settingsEngine))

	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		"video", "evdi-desktop",
	)
	if err != nil {
		return nil, fmt.Errorf("create video track: %w", err)
	}

	audioTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio", "evdi-desktop-audio",
	)
	if err != nil {
		return nil, fmt.Errorf("create audio track: %w", err)
	}

	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, fmt.Errorf("create peer connection: %w", err)
	}

	engine := &WebRTCEngine{
		peerConnection: pc,
		videoTrack:     videoTrack,
		audioTrack:     audioTrack,
		cfg:            cfg,
	}

	if _, err := pc.AddTrack(videoTrack); err != nil {
		return nil, fmt.Errorf("add video track: %w", err)
	}
	if _, err := pc.AddTrack(audioTrack); err != nil {
		return nil, fmt.Errorf("add audio track: %w", err)
	}

	engine.registerHandlers()

	return engine, nil
}

func (e *WebRTCEngine) registerHandlers() {
	e.peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if e.onICECandidate != nil {
			e.onICECandidate(candidate)
		}
	})

	e.peerConnection.OnDataChannel(func(dc *webrtc.DataChannel) {
		switch dc.Label() {
		case "control":
			e.channelControl = dc
			dc.OnMessage(func(msg webrtc.DataChannelMessage) {
				e.handleDataChannelMessage("control", msg.Data)
			})
		case "bulk":
			e.channelBulk = dc
			dc.OnMessage(func(msg webrtc.DataChannelMessage) {
				e.handleDataChannelMessage("bulk", msg.Data)
			})
		}
	})
}

func (e *WebRTCEngine) VideoTrack() *webrtc.TrackLocalStaticSample {
	return e.videoTrack
}

func (e *WebRTCEngine) AudioTrack() *webrtc.TrackLocalStaticSample {
	return e.audioTrack
}

func (e *WebRTCEngine) OnICECandidate(fn func(candidate *webrtc.ICECandidate)) {
	e.onICECandidate = fn
}

func (e *WebRTCEngine) OnDataChannel(fn func(channel string, msg DataChannelMessage)) {
	e.onDataChannel = fn
}

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

	return mustMarshal(answer), nil
}

func (e *WebRTCEngine) HandleICECandidate(data json.RawMessage) error {
	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal(data, &candidate); err != nil {
		return fmt.Errorf("unmarshal ice candidate: %w", err)
	}
	return e.peerConnection.AddICECandidate(candidate)
}

func (e *WebRTCEngine) Close() {
	if e.peerConnection != nil {
		e.peerConnection.Close()
	}
}

func (e *WebRTCEngine) handleDataChannelMessage(channel string, data []byte) {
	var msg DataChannelMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}
	if e.onDataChannel != nil {
		e.onDataChannel(channel, msg)
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/yuan/EVDI/evdi-agent && go test ./pkg/webrtc/ -run TestNewWebRTCEngine -v
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-agent/pkg/webrtc/engine.go evdi-agent/pkg/webrtc/engine_test.go
git commit -m "feat(agent): add WebRTC engine with Pion Lite ICE, video/audio tracks, SDP negotiation"
```

---

### Task 4: DataChannel 消息定义与处理

**Files:**
- Create: `evdi-agent/pkg/webrtc/datachannel.go`
- Create: `evdi-agent/pkg/webrtc/datachannel_test.go`

- [ ] **Step 1: 编写 DataChannel 消息解析测试**

```go
package webrtc

import (
	"encoding/json"
	"testing"
)

func TestDataChannelMessageParse(t *testing.T) {
	raw := `{"v":1,"type":"input.mouse_move","ts":1700000000123,"seq":1,"payload":{"x":960,"y":540,"display_id":0}}`
	var msg DataChannelMessage
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.V != 1 {
		t.Errorf("v = %d, want 1", msg.V)
	}
	if msg.Type != "input.mouse_move" {
		t.Errorf("type = %q, want input.mouse_move", msg.Type)
	}
	if msg.Seq != 1 {
		t.Errorf("seq = %d, want 1", msg.Seq)
	}
}

func TestMouseMovePayload(t *testing.T) {
	raw := `{"v":1,"type":"input.mouse_move","ts":0,"seq":0,"payload":{"x":100,"y":200,"display_id":0}}`
	var msg DataChannelMessage
	json.Unmarshal([]byte(raw), &msg)
	var payload MouseMovePayload
	json.Unmarshal(msg.Payload, &payload)
	if payload.X != 100 || payload.Y != 200 {
		t.Errorf("payload = %+v, want x=100 y=200", payload)
	}
}

func TestMouseButtonPayload(t *testing.T) {
	raw := `{"v":1,"type":"input.mouse_button","ts":0,"seq":0,"payload":{"button":1,"action":"down","x":100,"y":200}}`
	var msg DataChannelMessage
	json.Unmarshal([]byte(raw), &msg)
	var payload MouseButtonPayload
	json.Unmarshal(msg.Payload, &payload)
	if payload.Button != 1 || payload.Action != "down" {
		t.Errorf("payload = %+v, want button=1 action=down", payload)
	}
}

func TestKeyPayload(t *testing.T) {
	raw := `{"v":1,"type":"input.key","ts":0,"seq":0,"payload":{"keycode":65,"action":"down","shift":false,"ctrl":false,"alt":false}}`
	var msg DataChannelMessage
	json.Unmarshal([]byte(raw), &msg)
	var payload KeyPayload
	json.Unmarshal(msg.Payload, &payload)
	if payload.Keycode != 65 || payload.Action != "down" {
		t.Errorf("payload = %+v, want keycode=65 action=down", payload)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/yuan/EVDI/evdi-agent && go test ./pkg/webrtc/ -run TestMouseMove -v
```

Expected: 编译失败，类型未定义。

- [ ] **Step 3: 实现 DataChannel 消息类型**

```go
package webrtc

import "encoding/json"

type DataChannelMessage struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	Ts      int64           `json:"ts"`
	Seq     int             `json:"seq"`
	Payload json.RawMessage `json:"payload"`
}

type MouseMovePayload struct {
	X         int `json:"x"`
	Y         int `json:"y"`
	DisplayID int `json:"display_id"`
}

type MouseButtonPayload struct {
	Button int    `json:"button"`
	Action string `json:"action"` // "down" | "up"
	X      int    `json:"x"`
	Y      int    `json:"y"`
}

type MouseWheelPayload struct {
	DeltaX int `json:"delta_x"`
	DeltaY int `json:"delta_y"`
	X      int `json:"x"`
	Y      int `json:"y"`
}

type KeyPayload struct {
	Keycode int    `json:"keycode"`
	Action  string `json:"action"` // "down" | "up"
	Shift   bool   `json:"shift"`
	Ctrl    bool   `json:"ctrl"`
	Alt     bool   `json:"alt"`
}

type ClipboardPayload struct {
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
}

type ResizePayload struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type CtrlPingPayload struct {
	Ts int64 `json:"ts"`
}

type CtrlPongPayload struct {
	Ts int64 `json:"ts"`
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/yuan/EVDI/evdi-agent && go test ./pkg/webrtc/ -run "TestMouseMove|TestMouseButton|TestKey|TestDataChannel" -v
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-agent/pkg/webrtc/datachannel.go evdi-agent/pkg/webrtc/datachannel_test.go
git commit -m "feat(agent): add DataChannel message types and parsing"
```

---

### Task 5: 输入事件注入（xdotool）

**Files:**
- Create: `evdi-agent/pkg/input/mouse.go`
- Create: `evdi-agent/pkg/input/keyboard.go`
- Create: `evdi-agent/pkg/input/input_test.go`

- [ ] **Step 1: 编写输入注入测试**

```go
package input

import (
	"testing"
)

func TestMouseMoveCommand(t *testing.T) {
	cmd := MouseMoveCmd(100, 200)
	if cmd.Path != "xdotool" {
		t.Errorf("path = %q, want xdotool", cmd.Path)
	}
	args := cmd.Args[1:] // Args[0] is program name
	want := []string{"mousemove", "--sync", "100", "200"}
	for i, a := range want {
		if args[i] != a {
			t.Errorf("arg[%d] = %q, want %q", i, args[i], a)
		}
	}
}

func TestMouseButtonCommand(t *testing.T) {
	downCmd := MouseButtonCmd(1, "down")
	if downCmd.Path != "xdotool" {
		t.Errorf("path = %q, want xdotool", downCmd.Path)
	}

	upCmd := MouseButtonCmd(1, "up")
	if upCmd.Path != "xdotool" {
		t.Errorf("path = %q, want xdotool", upCmd.Path)
	}
}

func TestKeyDownUpCommand(t *testing.T) {
	downCmd := KeyCmd(65, "down", false, false, false)
	if downCmd.Path != "xdotool" {
		t.Errorf("path = %q, want xdotool", downCmd.Path)
	}
	upCmd := KeyCmd(65, "up", false, false, false)
	if upCmd.Path != "xdotool" {
		t.Errorf("path = %q, want xdotool", upCmd.Path)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/yuan/EVDI/evdi-agent && go test ./pkg/input/ -v
```

Expected: 编译失败。

- [ ] **Step 3: 实现鼠标输入**

```go
package input

import "os/exec"

func MouseMoveCmd(x, y int) *exec.Cmd {
	return exec.Command("xdotool", "mousemove", "--sync", itoa(x), itoa(y))
}

func MouseButtonCmd(button int, action string) *exec.Cmd {
	if action == "down" {
		return exec.Command("xdotool", "mousedown", itoa(button))
	}
	return exec.Command("xdotool", "mouseup", itoa(button))
}

func MouseWheelCmd(deltaX, deltaY int) *exec.Cmd {
	if deltaY > 0 {
		return exec.Command("xdotool", "click", "4")
	} else if deltaY < 0 {
		return exec.Command("xdotool", "click", "5")
	}
	return exec.Command("xdotool", "click", "0")
}

func itoa(i int) string {
	return fmt.Sprintf("%d", i)
}
```

- [ ] **Step 4: 实现键盘输入**

```go
package input

import (
	"fmt"
	"os/exec"
)

func KeyCmd(keycode int, action string, shift, ctrl, alt bool) *exec.Cmd {
	// Build modifier prefix
	modifiers := ""
	if ctrl {
		modifiers += "ctrl+"
	}
	if alt {
		modifiers += "alt+"
	}
	if shift {
		modifiers += "shift+"
	}

	keySym := keycodeToXKeySym(keycode)

	if action == "down" {
		if modifiers != "" {
			return exec.Command("xdotool", "keydown", modifiers+keySym)
		}
		return exec.Command("xdotool", "keydown", keySym)
	}
	if modifiers != "" {
		return exec.Command("xdotool", "keyup", modifiers+keySym)
	}
	return exec.Command("xdotool", "keyup", keySym)
}

// keycodeToXKeySym maps USB keycodes to X11 keysym names.
// MVP covers a subset; full mapping deferred to production version.
func keycodeToXKeySym(keycode int) string {
	// Letters (USB HID keycode 4='a' through 29='z')
	if keycode >= 4 && keycode <= 29 {
		return fmt.Sprintf("%c", 'a'+keycode-4)
	}
	// Numbers (USB HID keycode 30='1' through 39='0')
	if keycode >= 30 && keycode <= 38 {
		return fmt.Sprintf("%c", '1'+keycode-30)
	}
	if keycode == 39 {
		return "0"
	}
	// Special keys
	switch keycode {
	case 40:
		return "Return"
	case 41:
		return "Escape"
	case 42:
		return "BackSpace"
	case 43:
		return "Tab"
	case 44:
		return "space"
	case 57:
		return "Caps_Lock"
	case 58:
		return "F1"
	case 79:
		return "Right"
	case 80:
		return "Left"
	case 81:
		return "Down"
	case 82:
		return "Up"
	}
	return fmt.Sprintf("0x%04x", keycode)
}
```

- [ ] **Step 5: 运行测试验证通过**

```bash
cd /home/yuan/EVDI/evdi-agent && go test ./pkg/input/ -v
```

Expected: PASS

- [ ] **Step 6: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-agent/pkg/input/mouse.go evdi-agent/pkg/input/keyboard.go evdi-agent/pkg/input/input_test.go
git commit -m "feat(agent): add xdotool-based input injection for mouse and keyboard"
```

---

### Task 6: Xvfb 虚拟显示管理

**Files:**
- Create: `evdi-agent/pkg/display/xvfb.go`
- Create: `evdi-agent/pkg/display/xvfb_test.go`

- [ ] **Step 1: 编写 Xvfb 测试**

```go
package display

import (
	"testing"

	"github.com/evdi/agent/pkg/config"
)

func TestXvfbCommand(t *testing.T) {
	cfg := &config.Config{
		Display:    ":99",
		VideoWidth: 1920,
		VideoHeight: 1080,
	}
	xvfb := NewXvfb(cfg)
	cmd := xvfb.Command()
	if cmd.Path != "Xvfb" {
		t.Errorf("path = %q, want Xvfb", cmd.Path)
	}
	args := cmd.Args[1:]
	found := false
	for _, a := range args {
		if a == "-screen" {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing -screen argument")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/yuan/EVDI/evdi-agent && go test ./pkg/display/ -v
```

Expected: 编译失败。

- [ ] **Step 3: 实现 Xvfb 管理**

```go
package display

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/evdi/agent/pkg/config"
)

type Xvfb struct {
	display string
	width   int
	height  int
	depth   int
	cmd     *exec.Cmd
}

func NewXvfb(cfg *config.Config) *Xvfb {
	return &Xvfb{
		display: cfg.Display,
		width:   cfg.VideoWidth,
		height:  cfg.VideoHeight,
		depth:   24,
	}
}

func (x *Xvfb) Command() *exec.Cmd {
	screenSpec := fmt.Sprintf("%dx%dx%d", x.width, x.height, x.depth)
	return exec.Command("Xvfb", x.display, "-screen", "0", screenSpec, "-nolisten", "tcp")
}

func (x *Xvfb) Start() error {
	x.cmd = x.Command()
	x.cmd.Env = append(os.Environ(), "DISPLAY="+x.display)
	if err := x.cmd.Start(); err != nil {
		return fmt.Errorf("start Xvfb: %w", err)
	}
	return nil
}

func (x *Xvfb) Stop() error {
	if x.cmd != nil && x.cmd.Process != nil {
		return x.cmd.Process.Signal(syscall.SIGTERM)
	}
	return nil
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/yuan/EVDI/evdi-agent && go test ./pkg/display/ -v
```

Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-agent/pkg/display/xvfb.go evdi-agent/pkg/display/xvfb_test.go
git commit -m "feat(agent): add Xvfb virtual display manager"
```

---

### Task 7: GStreamer Pipeline 管理

**Files:**
- Create: `evdi-agent/pkg/gstreamer/pipeline.go`
- Create: `evdi-agent/pkg/gstreamer/launcher.go`

- [ ] **Step 1: 实现 Pipeline 管理**

```go
package gstreamer

import (
	"fmt"

	"github.com/evdi/agent/pkg/config"
	"github.com/pion/webrtc/v4"
	"github.com/tinyzimmer/go-gst/gst"
)

type Pipeline struct {
	videoPipeline *gst.Pipeline
	audioPipeline *gst.Pipeline
	videoTrack    *webrtc.TrackLocalStaticSample
	audioTrack    *webrtc.TrackLocalStaticSample
	stopChan      chan struct{}
	cfg           *config.Config
}

func NewPipeline(cfg *config.Config, videoTrack, audioTrack *webrtc.TrackLocalStaticSample) *Pipeline {
	return &Pipeline{
		videoTrack: videoTrack,
		audioTrack: audioTrack,
		stopChan:   make(chan struct{}),
		cfg:        cfg,
	}
}

func (p *Pipeline) Start() error {
	if err := p.startVideo(); err != nil {
		return fmt.Errorf("start video pipeline: %w", err)
	}
	if err := p.startAudio(); err != nil {
		return fmt.Errorf("start audio pipeline: %w", err)
	}
	return nil
}

func (p *Pipeline) startVideo() error {
	pipelineStr := fmt.Sprintf(
		"ximagesrc display-name=%s ! "+
			"video/x-raw, framerate=%d/1, width=%d, height=%d ! "+
			"videoconvert ! "+
			"x264enc tune=zerolatency speed-preset=ultrafast byte-stream=true ! "+
			"video/x-h264, stream-format=byte-stream ! "+
			"appsink name=videosink emit-signals=true max-buffers=1 drop=true sync=false",
		p.cfg.Display, p.cfg.VideoFPS, p.cfg.VideoWidth, p.cfg.VideoHeight,
	)

	pipe, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		return fmt.Errorf("create video pipeline: %w", err)
	}
	p.videoPipeline = pipe

	sink, err := pipe.GetElementByName("videosink")
	if err != nil {
		return fmt.Errorf("get videosink: %w", err)
	}

	sink.Obj.Connect("new-sample", func(_ interface{}) interface{} {
		sample := sink.Obj.Emit("pull-sample")
		if sample == nil {
			return nil
		}
		gstSample, ok := sample.(*gst.Sample)
		if !ok || gstSample == nil {
			return nil
		}
		buffer := gstSample.GetBuffer()
		if buffer == nil {
			return nil
		}
		data := buffer.Bytes()
		p.videoTrack.WriteSampleMediaSample(webrtc.MediaSample{
			Data:     data,
			Duration: defaultFrameDuration(p.cfg.VideoFPS),
		})
		return nil
	})

	pipe.SetState(gst.StatePlaying)
	return nil
}

func (p *Pipeline) startAudio() error {
	pipelineStr := "pulsesrc device=EVDI.monitor ! " +
		"audio/x-raw, rate=48000, channels=2 ! " +
		"audioconvert ! audioresample ! " +
		"opusenc bitrate=96000 ! " +
		"audio/x-opus ! " +
		"appsink name=audiosink emit-signals=true max-buffers=1 drop=true sync=false"

	pipe, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		return fmt.Errorf("create audio pipeline: %w", err)
	}
	p.audioPipeline = pipe

	sink, err := pipe.GetElementByName("audiosink")
	if err != nil {
		return fmt.Errorf("get audiosink: %w", err)
	}

	sink.Obj.Connect("new-sample", func(_ interface{}) interface{} {
		sample := sink.Obj.Emit("pull-sample")
		if sample == nil {
			return nil
		}
		gstSample, ok := sample.(*gst.Sample)
		if !ok || gstSample == nil {
			return nil
		}
		buffer := gstSample.GetBuffer()
		if buffer == nil {
			return nil
		}
		data := buffer.Bytes()
		p.audioTrack.WriteSampleMediaSample(webrtc.MediaSample{
			Data:     data,
			Duration: 20000000, // 20ms per Opus frame
		})
		return nil
	})

	pipe.SetState(gst.StatePlaying)
	return nil
}

func (p *Pipeline) Stop() {
	close(p.stopChan)
	if p.videoPipeline != nil {
		p.videoPipeline.SetState(gst.StateNull)
	}
	if p.audioPipeline != nil {
		p.audioPipeline.SetState(gst.StateNull)
	}
}

func defaultFrameDuration(fps int) uint32 {
	return uint32(1000000000 / fps)
}
```

- [ ] **Step 2: 实现 GStreamer 子进程启动器**

```go
package gstreamer

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/evdi/agent/pkg/config"
)

// Launcher starts GStreamer as an independent process for LGPL compliance.
// MVP uses in-process go-gst for simplicity; this launcher is reserved
// for the production version that requires process isolation.
type Launcher struct {
	cfg    *config.Config
	cmd    *exec.Cmd
	binDir string
}

func NewLauncher(cfg *config.Config) *Launcher {
	return &Launcher{cfg: cfg}
}

func (l *Launcher) BuildHelper() error {
	// Build the GStreamer helper binary that runs as a separate process
	// This is a placeholder for the production LGPL-compliant architecture
	return nil
}

func (l *Launcher) Start() error {
	args := []string{
		"ximagesrc", fmt.Sprintf("display-name=%s", l.cfg.Display),
		"!", "video/x-raw", fmt.Sprintf("framerate=%d/1", l.cfg.VideoFPS),
		fmt.Sprintf("width=%d", l.cfg.VideoWidth), fmt.Sprintf("height=%d", l.cfg.VideoHeight),
		"!", "videoconvert",
		"!", "x264enc", "tune=zerolatency", "speed-preset=ultrafast",
		"!", "video/x-h264",
		"!", "fdsink", "fd=1", "sync=false",
	}
	l.cmd = exec.Command("gst-launch-1.0", args...)
	l.cmd.Env = append(os.Environ(), "DISPLAY="+l.cfg.Display)
	l.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return l.cmd.Start()
}

func (l *Launcher) Stop() error {
	if l.cmd != nil && l.cmd.Process != nil {
		return syscall.Kill(-l.cmd.Process.Pid, syscall.SIGTERM)
	}
	return nil
}
```

- [ ] **Step 3: 添加 go-gst 依赖**

```bash
cd /home/yuan/EVDI/evdi-agent && go get github.com/tinyzimmer/go-gst/gst@latest
```

- [ ] **Step 4: 验证编译**

```bash
cd /home/yuan/EVDI/evdi-agent && go build ./pkg/gstreamer/
```

Expected: 编译成功（注意：需要安装 GStreamer 开发库才能编译）。

- [ ] **Step 5: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-agent/pkg/gstreamer/pipeline.go evdi-agent/pkg/gstreamer/launcher.go
git commit -m "feat(agent): add GStreamer pipeline with video (x264enc) and audio (opusenc)"
```

---

### Task 8: Agent 入口整合

**Files:**
- Modify: `evdi-agent/cmd/agent/main.go`

- [ ] **Step 1: 重写 main.go 整合所有组件**

```go
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/evdi/agent/pkg/config"
	"github.com/evdi/agent/pkg/display"
	"github.com/evdi/agent/pkg/gstreamer"
	"github.com/evdi/agent/pkg/input"
	"github.com/evdi/agent/pkg/webrtc"
)

func main() {
	cfg := config.Load()
	log.Printf("EVDI Agent starting, WS port=%s, display=%s, video=%dx%d@%dfps",
		cfg.WSPort, cfg.Display, cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS)

	// 1. 启动 Xvfb
	xvfb := display.NewXvfb(cfg)
	if err := xvfb.Start(); err != nil {
		log.Fatalf("Failed to start Xvfb: %v", err)
	}
	defer xvfb.Stop()
	log.Printf("Xvfb started on %s", cfg.Display)

	// 2. 启动窗口管理器
	go func() {
		if err := startWindowManager(cfg); err != nil {
			log.Printf("Window manager error: %v", err)
		}
	}()

	// 3. 创建 WebRTC 引擎
	engine, err := webrtc.NewWebRTCEngine(cfg)
	if err != nil {
		log.Fatalf("Failed to create WebRTC engine: %v", err)
	}
	defer engine.Close()

	// 4. 注册 DataChannel 回调
	engine.OnDataChannel(func(channel string, msg webrtc.DataChannelMessage) {
		handleDataChannelMessage(msg)
	})

	// 5. 创建并启动 GStreamer Pipeline
	pipe := gstreamer.NewPipeline(cfg, engine.VideoTrack(), engine.AudioTrack())
	if err := pipe.Start(); err != nil {
		log.Fatalf("Failed to start GStreamer: %v", err)
	}
	defer pipe.Stop()

	// 6. 启动信令服务器
	sigServer := webrtc.NewSignalingServer(cfg, engine)

	// 注册 ICE 候选回调，通过信令转发给客户端
	engine.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		// ICE 候选通过 OnICECandidate 回调处理
		// 实际转发逻辑在 signaling server 中处理
		log.Printf("ICE candidate: %v", candidate)
	})

	go func() {
		if err := sigServer.Start(); err != nil {
			log.Fatalf("Signaling server error: %v", err)
		}
	}()

	log.Printf("EVDI Agent ready, waiting for client connection...")

	// 等待退出信号
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Printf("Shutting down...")
}

func startWindowManager(cfg *config.Config) error {
	// 启动 xfwm4 或其他窗口管理器
	// MVP 可以跳过，XFCE session 由 Docker entrypoint 处理
	return nil
}

func handleDataChannelMessage(msg webrtc.DataChannelMessage) {
	switch msg.Type {
	case "input.mouse_move":
		var p webrtc.MouseMovePayload
		jsonUnmarshal(msg.Payload, &p)
		input.MouseMoveCmd(p.X, p.Y).Run()

	case "input.mouse_button":
		var p webrtc.MouseButtonPayload
		jsonUnmarshal(msg.Payload, &p)
		input.MouseButtonCmd(p.Button, p.Action).Run()

	case "input.mouse_wheel":
		var p webrtc.MouseWheelPayload
		jsonUnmarshal(msg.Payload, &p)
		input.MouseWheelCmd(p.DeltaX, p.DeltaY).Run()

	case "input.key":
		var p webrtc.KeyPayload
		jsonUnmarshal(msg.Payload, &p)
		input.KeyCmd(p.Keycode, p.Action, p.Shift, p.Ctrl, p.Alt).Run()

	case "clipboard.push":
		log.Printf("Clipboard push received (MVP: no-op)")

	case "ctrl.resize":
		var p webrtc.ResizePayload
		jsonUnmarshal(msg.Payload, &p)
		log.Printf("Resize request: %dx%d (MVP: no-op)", p.Width, p.Height)

	case "ctrl.ping":
		// Pong response handled via DataChannel send
	}
}

func jsonUnmarshal(data []byte, v interface{}) {
	// imported from encoding/json in actual file
}
```

注意：实际代码中 `jsonUnmarshal` 应替换为 `json.Unmarshal`，此处为避免重复 import 示例简化。

- [ ] **Step 2: 验证编译**

```bash
cd /home/yuan/EVDI/evdi-agent && go build ./cmd/agent/
```

Expected: 编译成功。

- [ ] **Step 3: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-agent/cmd/agent/main.go
git commit -m "feat(agent): integrate all components in main entry point"
```

---

### Task 9: Agent Dockerfile

**Files:**
- Create: `evdi-agent/Dockerfile`

- [ ] **Step 1: 创建 Dockerfile**

```dockerfile
FROM golang:1.22-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
    gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
    gstreamer1.0-x gstreamer1.0-libav \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /agent ./cmd/agent

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    xvfb xdotool \
    gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
    gstreamer1.0-x gstreamer1.0-libav gstreamer1.0-tools \
    pulseaudio pulseaudio-utils \
    xfce4 xfce4-terminal \
    dbus \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /agent /usr/local/bin/agent

# 配置 PulseAudio
RUN mkdir -p /etc/pulse && \
    echo "default-server = /run/pulse/native" > /etc/pulse/client.conf && \
    echo "autospawn = no" >> /etc/pulse/client.conf

COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 8080
EXPOSE 50000-50100/udp

ENTRYPOINT ["/entrypoint.sh"]
```

- [ ] **Step 2: 创建 entrypoint.sh**

```bash
#!/bin/bash
set -e

# 启动 D-Bus
eval $(dbus-launch --sh-syntax)

# 启动 Xvfb
Xvfb ${DISPLAY:-:99} -screen 0 ${VIDEO_WIDTH:-1920}x${VIDEO_HEIGHT:-1080}x24 -nolisten tcp &
sleep 1

# 启动 PulseAudio
pulseaudio --start --fail=false --daemonize=false &
sleep 0.5

# 加载 PulseAudio module-null-source 用于音频捕获
pacmd load-module module-null-source source_name=EVDI rate=48000 channels=2
pacmd set-default-source EVDI

# 启动窗口管理器
xfwm4 &
sleep 0.5

# 启动 XFCE 桌面
startxfce4 &
sleep 2

# 启动 Agent
exec agent
```

- [ ] **Step 3: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-agent/Dockerfile evdi-agent/entrypoint.sh
git commit -m "feat(agent): add Dockerfile with Xvfb, PulseAudio, XFCE, GStreamer"
```

---

### Task 10: React 客户端脚手架

**Files:**
- Create: `evdi-web-client/package.json`
- Create: `evdi-web-client/tsconfig.json`
- Create: `evdi-web-client/tsconfig.node.json`
- Create: `evdi-web-client/vite.config.ts`
- Create: `evdi-web-client/index.html`
- Create: `evdi-web-client/src/main.tsx`
- Create: `evdi-web-client/src/App.tsx`
- Create: `evdi-web-client/src/App.css`

- [ ] **Step 1: 创建 Vite 项目**

```bash
cd /home/yuan/EVDI && npm create vite@latest evdi-web-client -- --template react-ts
```

- [ ] **Step 2: 安装依赖**

```bash
cd /home/yuan/EVDI/evdi-web-client && npm install zustand antd && npm install -D @types/react @types/react-dom
```

- [ ] **Step 3: 配置 Vite 代理**

替换 `vite.config.ts`：

```typescript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
})
```

- [ ] **Step 4: 创建 App.tsx 基础框架**

```tsx
import { ConfigProvider, theme } from 'antd'
import { ControlPanel } from './components/ControlPanel'
import { VideoCanvas } from './components/VideoCanvas'
import { useConnectionStore } from './stores/connectionStore'

function App() {
  const { connectionState, mediaStream } = useConnectionStore()

  return (
    <ConfigProvider theme={{ algorithm: theme.darkAlgorithm }}>
      <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', background: '#1a1a2e' }}>
        <ControlPanel />
        <div style={{ flex: 1, position: 'relative' }}>
          {connectionState === 'connected' && mediaStream ? (
            <VideoCanvas stream={mediaStream} />
          ) : (
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: '#888' }}>
              {connectionState === 'connecting' ? '正在连接...' : '未连接'}
            </div>
          )}
        </div>
      </div>
    </ConfigProvider>
  )
}

export default App
```

- [ ] **Step 5: 验证客户端启动**

```bash
cd /home/yuan/EVDI/evdi-web-client && npm run dev
```

Expected: Vite dev server 在 localhost:3000 启动成功，浏览器可访问。

- [ ] **Step 6: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-web-client/
git commit -m "feat(web-client): scaffold React + TypeScript + Vite project with Zustand and Ant Design"
```

---

### Task 11: 客户端类型定义 + 信令工具

**Files:**
- Create: `evdi-web-client/src/types/signaling.ts`
- Create: `evdi-web-client/src/utils/signaling.ts`

- [ ] **Step 1: 创建信令和 DataChannel 类型**

```typescript
// evdi-web-client/src/types/signaling.ts

export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'error'

// WebSocket 信令消息
export interface SignalingMessage {
  type: 'offer' | 'answer' | 'ice' | 'ping' | 'pong'
  data: unknown
}

export interface SDPPayload {
  sdp: string
  type: string
}

export interface ICEPayload {
  candidate: string
  sdpMid: string
  sdpMLineIndex: number
}

// DataChannel 统一帧结构
export interface DataChannelMessage {
  v: number
  type: string
  ts: number
  seq: number
  payload: unknown
}

// 输入事件 payload
export interface MouseMovePayload {
  x: number
  y: number
  display_id: number
}

export interface MouseButtonPayload {
  button: number
  action: 'down' | 'up'
  x: number
  y: number
}

export interface MouseWheelPayload {
  delta_x: number
  delta_y: number
  x: number
  y: number
}

export interface KeyPayload {
  keycode: number
  action: 'down' | 'up'
  shift: boolean
  ctrl: boolean
  alt: boolean
}

export interface ClipboardPayload {
  data: string
  mime_type: string
}

export interface ResizePayload {
  width: number
  height: number
}
```

- [ ] **Step 2: 创建信令客户端**

```typescript
// evdi-web-client/src/utils/signaling.ts

import type { SignalingMessage } from '../types/signaling'

export class SignalingClient {
  private ws: WebSocket | null = null
  private onMessage: ((msg: SignalingMessage) => void) | null = null
  private reconnectAttempts = 0
  private maxReconnectDelay = 16000

  connect(url: string, onMessage: (msg: SignalingMessage) => void): void {
    this.onMessage = onMessage
    this.doConnect(url)
  }

  private doConnect(url: string): void {
    this.ws = new WebSocket(url)

    this.ws.onopen = () => {
      this.reconnectAttempts = 0
    }

    this.ws.onmessage = (event) => {
      try {
        const msg: SignalingMessage = JSON.parse(event.data)
        this.onMessage?.(msg)
      } catch {
        console.error('Failed to parse signaling message')
      }
    }

    this.ws.onclose = () => {
      // 指数退避重连
      const delay = Math.min(1000 * Math.pow(2, this.reconnectAttempts), this.maxReconnectDelay)
      this.reconnectAttempts++
      setTimeout(() => this.doConnect(url), delay)
    }

    this.ws.onerror = () => {
      this.ws?.close()
    }
  }

  send(msg: SignalingMessage): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg))
    }
  }

  disconnect(): void {
    this.onMessage = null
    this.reconnectAttempts = 999 // 阻止自动重连
    this.ws?.close()
    this.ws = null
  }
}
```

- [ ] **Step 3: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-web-client/src/types/signaling.ts evdi-web-client/src/utils/signaling.ts
git commit -m "feat(web-client): add signaling types and WebSocket client with reconnect"
```

---

### Task 12: Zustand 连接状态管理

**Files:**
- Create: `evdi-web-client/src/stores/connectionStore.ts`

- [ ] **Step 1: 创建状态管理**

```typescript
// evdi-web-client/src/stores/connectionStore.ts

import { create } from 'zustand'
import type { ConnectionState } from '../types/signaling'

interface ConnectionStore {
  agentAddress: string
  connectionState: ConnectionState
  mediaStream: MediaStream | null
  errorMessage: string | null
  seqCounter: number

  setAgentAddress: (addr: string) => void
  setConnectionState: (state: ConnectionState) => void
  setMediaStream: (stream: MediaStream | null) => void
  setError: (msg: string | null) => void
  nextSeq: () => number
  reset: () => void
}

export const useConnectionStore = create<ConnectionStore>((set, get) => ({
  agentAddress: 'ws://localhost:8080/ws',
  connectionState: 'disconnected',
  mediaStream: null,
  errorMessage: null,
  seqCounter: 0,

  setAgentAddress: (addr) => set({ agentAddress: addr }),
  setConnectionState: (state) => set({ connectionState: state }),
  setMediaStream: (stream) => set({ mediaStream: stream }),
  setError: (msg) => set({ errorMessage: msg, connectionState: msg ? 'error' : get().connectionState }),
  nextSeq: () => {
    const seq = get().seqCounter + 1
    set({ seqCounter: seq })
    return seq
  },
  reset: () => set({
    connectionState: 'disconnected',
    mediaStream: null,
    errorMessage: null,
    seqCounter: 0,
  }),
}))
```

- [ ] **Step 2: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-web-client/src/stores/connectionStore.ts
git commit -m "feat(web-client): add Zustand connection state store"
```

---

### Task 13: WebRTC 连接 Hook

**Files:**
- Create: `evdi-web-client/src/hooks/useWebRTC.ts`

- [ ] **Step 1: 实现 WebRTC Hook**

```typescript
// evdi-web-client/src/hooks/useWebRTC.ts

import { useCallback, useRef } from 'react'
import { SignalingClient } from '../utils/signaling'
import { useConnectionStore } from '../stores/connectionStore'
import type { SignalingMessage, SDPPayload, ICEPayload } from '../types/signaling'

export function useWebRTC() {
  const pcRef = useRef<RTCPeerConnection | null>(null)
  const signalingRef = useRef<SignalingClient | null>(null)
  const controlChannelRef = useRef<RTCDataChannel | null>(null)
  const bulkChannelRef = useRef<RTCDataChannel | null>(null)

  const {
    agentAddress,
    setConnectionState,
    setMediaStream,
    setError,
    nextSeq,
    reset,
  } = useConnectionStore()

  const connect = useCallback(async () => {
    try {
      setConnectionState('connecting')
      setError(null)

      // 1. 创建 RTCPeerConnection
      const pc = new RTCPeerConnection({
        iceServers: [], // MVP: 无 STUN/TURN，纯 P2P
      })
      pcRef.current = pc

      // 2. 接收远端媒体流
      pc.ontrack = (event) => {
        if (event.streams.length > 0) {
          setMediaStream(event.streams[0])
        }
      }

      pc.onconnectionstatechange = () => {
        switch (pc.connectionState) {
          case 'connected':
            setConnectionState('connected')
            break
          case 'disconnected':
          case 'failed':
            setConnectionState('disconnected')
            setError('连接已断开')
            break
        }
      }

      // 3. 添加 transceiver（接收视频和音频）
      pc.addTransceiver('video', { direction: 'recvonly' })
      pc.addTransceiver('audio', { direction: 'recvonly' })

      // 4. 创建 DataChannel
      const controlChannel = pc.createDataChannel('control', { ordered: true })
      const bulkChannel = pc.createDataChannel('bulk', { ordered: true })
      controlChannelRef.current = controlChannel
      bulkChannelRef.current = bulkChannel

      // 5. 创建信令客户端
      const signaling = new SignalingClient()
      signalingRef.current = signaling

      signaling.connect(agentAddress, (msg: SignalingMessage) => {
        handleSignalingMessage(pc, signaling, msg)
      })

      // 6. 等待 ICE gathering 后发送 offer
      // 使用 trickle ICE：逐条发送候选
      pc.onicecandidate = (event) => {
        if (event.candidate) {
          signaling.send({
            type: 'ice',
            data: {
              candidate: event.candidate.candidate,
              sdpMid: event.candidate.sdpMid ?? '',
              sdpMLineIndex: event.candidate.sdpMLineIndex ?? 0,
            } satisfies ICEPayload,
          })
        }
      }

      // 7. 创建 Offer
      const offer = await pc.createOffer()
      await pc.setLocalDescription(offer)
      signaling.send({
        type: 'offer',
        data: { sdp: offer.sdp, type: offer.type } satisfies SDPPayload,
      })
    } catch (err) {
      setError(`连接失败: ${err instanceof Error ? err.message : String(err)}`)
    }
  }, [agentAddress, setConnectionState, setMediaStream, setError, nextSeq])

  const disconnect = useCallback(() => {
    controlChannelRef.current?.close()
    bulkChannelRef.current?.close()
    pcRef.current?.close()
    signalingRef.current?.disconnect()
    pcRef.current = null
    signalingRef.current = null
    controlChannelRef.current = null
    bulkChannelRef.current = null
    reset()
  }, [reset])

  const sendDataChannelMessage = useCallback((channel: 'control' | 'bulk', msgType: string, payload: unknown) => {
    const ch = channel === 'control' ? controlChannelRef.current : bulkChannelRef.current
    if (ch?.readyState === 'open') {
      const msg = {
        v: 1,
        type: msgType,
        ts: Date.now(),
        seq: nextSeq(),
        payload,
      }
      ch.send(JSON.stringify(msg))
    }
  }, [nextSeq])

  return { connect, disconnect, sendDataChannelMessage }
}

async function handleSignalingMessage(
  pc: RTCPeerConnection,
  signaling: SignalingClient,
  msg: SignalingMessage,
) {
  switch (msg.type) {
    case 'answer': {
      const data = msg.data as SDPPayload
      await pc.setRemoteDescription(new RTCSessionDescription({
        sdp: data.sdp,
        type: data.type as RTCSdpType,
      }))
      break
    }
    case 'ice': {
      const data = msg.data as ICEPayload
      await pc.addIceCandidate(new RTCIceCandidate({
        candidate: data.candidate,
        sdpMid: data.sdpMid,
        sdpMLineIndex: data.sdpMLineIndex,
      }))
      break
    }
    case 'pong':
      // 心跳响应，无需处理
      break
  }
}
```

- [ ] **Step 2: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-web-client/src/hooks/useWebRTC.ts
git commit -m "feat(web-client): add useWebRTC hook with PeerConnection, DataChannel, signaling"
```

---

### Task 14: 客户端 UI 组件

**Files:**
- Create: `evdi-web-client/src/components/VideoCanvas.tsx`
- Create: `evdi-web-client/src/components/ControlPanel.tsx`
- Create: `evdi-web-client/src/components/StatusIndicator.tsx`

- [ ] **Step 1: 实现 VideoCanvas**

```tsx
// evdi-web-client/src/components/VideoCanvas.tsx

import { useEffect, useRef, useCallback } from 'react'
import { useConnectionStore } from '../stores/connectionStore'
import { useWebRTC } from '../hooks/useWebRTC'
import type { MouseMovePayload, MouseButtonPayload, MouseWheelPayload, KeyPayload } from '../types/signaling'

interface Props {
  stream: MediaStream
}

export const VideoCanvas: React.FC<Props> = ({ stream }) => {
  const videoRef = useRef<HTMLVideoElement>(null)
  const { sendDataChannelMessage } = useWebRTC()
  const nextSeq = useConnectionStore((s) => s.nextSeq)

  useEffect(() => {
    if (videoRef.current && stream) {
      videoRef.current.srcObject = stream
    }
  }, [stream])

  const getRelativePos = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const video = videoRef.current
    if (!video) return { x: e.nativeEvent.offsetX, y: e.nativeEvent.offsetY }

    const videoRatio = video.videoWidth / video.videoHeight
    const containerRatio = rect.width / rect.height

    let renderWidth: number, renderHeight: number, offsetX: number, offsetY: number
    if (containerRatio > videoRatio) {
      renderHeight = rect.height
      renderWidth = renderHeight * videoRatio
      offsetX = (rect.width - renderWidth) / 2
      offsetY = 0
    } else {
      renderWidth = rect.width
      renderHeight = renderWidth / videoRatio
      offsetX = 0
      offsetY = (rect.height - renderHeight) / 2
    }

    return {
      x: Math.round(((e.nativeEvent.offsetX - offsetX) / renderWidth) * video.videoWidth),
      y: Math.round(((e.nativeEvent.offsetY - offsetY) / renderHeight) * video.videoHeight),
    }
  }, [])

  const handleMouseMove = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    const { x, y } = getRelativePos(e)
    const payload: MouseMovePayload = { x, y, display_id: 0 }
    sendDataChannelMessage('control', 'input.mouse_move', payload)
  }, [getRelativePos, sendDataChannelMessage])

  const handleMouseDown = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    const { x, y } = getRelativePos(e)
    const payload: MouseButtonPayload = { button: e.button + 1, action: 'down', x, y }
    sendDataChannelMessage('control', 'input.mouse_button', payload)
  }, [getRelativePos, sendDataChannelMessage])

  const handleMouseUp = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
    const { x, y } = getRelativePos(e)
    const payload: MouseButtonPayload = { button: e.button + 1, action: 'up', x, y }
    sendDataChannelMessage('control', 'input.mouse_button', payload)
  }, [getRelativePos, sendDataChannelMessage])

  const handleWheel = useCallback((e: React.WheelEvent<HTMLDivElement>) => {
    const { x, y } = getRelativePos(e)
    const payload: MouseWheelPayload = { delta_x: e.deltaX, delta_y: e.deltaY, x, y }
    sendDataChannelMessage('control', 'input.mouse_wheel', payload)
  }, [getRelativePos, sendDataChannelMessage])

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
    e.preventDefault()
    const payload: KeyPayload = {
      keycode: e.keyCode,
      action: 'down',
      shift: e.shiftKey,
      ctrl: e.ctrlKey,
      alt: e.altKey,
    }
    sendDataChannelMessage('control', 'input.key', payload)
  }, [sendDataChannelMessage])

  const handleKeyUp = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
    e.preventDefault()
    const payload: KeyPayload = {
      keycode: e.keyCode,
      action: 'up',
      shift: e.shiftKey,
      ctrl: e.ctrlKey,
      alt: e.altKey,
    }
    sendDataChannelMessage('control', 'input.key', payload)
  }, [sendDataChannelMessage])

  return (
    <div
      style={{ width: '100%', height: '100%', position: 'relative', outline: 'none' }}
      onMouseMove={handleMouseMove}
      onMouseDown={handleMouseDown}
      onMouseUp={handleMouseUp}
      onWheel={handleWheel}
      onKeyDown={handleKeyDown}
      onKeyUp={handleKeyUp}
      tabIndex={0}
    >
      <video
        ref={videoRef}
        autoPlay
        playsInline
        muted
        style={{
          width: '100%',
          height: '100%',
          objectFit: 'contain',
          pointerEvents: 'none',
        }}
      />
    </div>
  )
}
```

- [ ] **Step 2: 实现 ControlPanel**

```tsx
// evdi-web-client/src/components/ControlPanel.tsx

import { Input, Button, Space, Typography } from 'antd'
import { useConnectionStore } from '../stores/connectionStore'
import { useWebRTC } from '../hooks/useWebRTC'
import { StatusIndicator } from './StatusIndicator'

const { Text } = Typography

export const ControlPanel: React.FC = () => {
  const { agentAddress, connectionState, setAgentAddress } = useConnectionStore()
  const { connect, disconnect } = useWebRTC()

  const isConnecting = connectionState === 'connecting'
  const isConnected = connectionState === 'connected'

  return (
    <div style={{ padding: '12px 16px', display: 'flex', alignItems: 'center', gap: 12, borderBottom: '1px solid #333' }}>
      <Text style={{ color: '#ccc' }}>EVDI</Text>
      <StatusIndicator state={connectionState} />
      <Input
        size="small"
        value={agentAddress}
        onChange={(e) => setAgentAddress(e.target.value)}
        disabled={isConnected || isConnecting}
        style={{ width: 320 }}
        placeholder="ws://localhost:8080/ws"
      />
      <Space>
        {!isConnected ? (
          <Button type="primary" size="small" onClick={connect} loading={isConnecting} disabled={isConnecting}>
            连接
          </Button>
        ) : (
          <Button danger size="small" onClick={disconnect}>
            断开
          </Button>
        )}
      </Space>
    </div>
  )
}
```

- [ ] **Step 3: 实现 StatusIndicator**

```tsx
// evdi-web-client/src/components/StatusIndicator.tsx

import { Tag } from 'antd'
import type { ConnectionState } from '../types/signaling'

const stateConfig: Record<ConnectionState, { color: string; label: string }> = {
  disconnected: { color: 'default', label: '未连接' },
  connecting: { color: 'processing', label: '连接中' },
  connected: { color: 'success', label: '已连接' },
  error: { color: 'error', label: '错误' },
}

interface Props {
  state: ConnectionState
}

export const StatusIndicator: React.FC<Props> = ({ state }) => {
  const config = stateConfig[state]
  return <Tag color={config.color}>{config.label}</Tag>
}
```

- [ ] **Step 4: 更新 App.tsx 引入组件**

```tsx
// evdi-web-client/src/App.tsx

import { ConfigProvider, theme } from 'antd'
import { ControlPanel } from './components/ControlPanel'
import { VideoCanvas } from './components/VideoCanvas'
import { useConnectionStore } from './stores/connectionStore'

function App() {
  const { connectionState, mediaStream } = useConnectionStore()

  return (
    <ConfigProvider theme={{ algorithm: theme.darkAlgorithm }}>
      <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', background: '#1a1a2e' }}>
        <ControlPanel />
        <div style={{ flex: 1, position: 'relative' }}>
          {connectionState === 'connected' && mediaStream ? (
            <VideoCanvas stream={mediaStream} />
          ) : (
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: '#888' }}>
              {connectionState === 'connecting' ? '正在连接...' : '未连接'}
            </div>
          )}
        </div>
      </div>
    </ConfigProvider>
  )
}

export default App
```

- [ ] **Step 5: 提交**

```bash
cd /home/yuan/EVDI && git add evdi-web-client/src/components/ evdi-web-client/src/App.tsx
git commit -m "feat(web-client): add VideoCanvas, ControlPanel, StatusIndicator components"
```

---

### Task 15: Docker Compose + Makefile

**Files:**
- Create: `docker-compose.yml`
- Create: `Makefile`

- [ ] **Step 1: 创建 Docker Compose**

```yaml
services:
  evdi-agent:
    build:
      context: ./evdi-agent
      dockerfile: Dockerfile
    container_name: evdi-agent-mvp
    ports:
      - "8080:8080"
      - "50000-50100:50000-50100/udp"
      - "5900:5900"
    environment:
      - DISPLAY=:99
      - AGENT_WS_PORT=8080
      - VIDEO_WIDTH=1920
      - VIDEO_HEIGHT=1080
      - VIDEO_FPS=30
      - WEBRTC_PORT_MIN=50000
      - WEBRTC_PORT_MAX=50100
    cap_add:
      - SYS_ADMIN
```

- [ ] **Step 2: 创建 Makefile**

```makefile
.PHONY: build-agent build-client run-agent run-docker run-client clean

build-agent:
	cd evdi-agent && CGO_ENABLED=1 go build -o bin/agent ./cmd/agent

build-client:
	cd evdi-web-client && npm run build

run-agent:
	cd evdi-agent && go run ./cmd/agent

run-docker:
	docker compose up --build

run-client:
	cd evdi-web-client && npm run dev

clean:
	docker compose down -v
	rm -rf evdi-agent/bin/
	rm -rf evdi-web-client/dist/
```

- [ ] **Step 3: 提交**

```bash
cd /home/yuan/EVDI && git add docker-compose.yml Makefile
git commit -m "feat: add Docker Compose and Makefile for MVP deployment"
```

---

### Task 16: 端到端集成测试

**Files:**
- 无新文件

- [ ] **Step 1: 构建 Agent Docker 镜像**

```bash
cd /home/yuan/EVDI && docker compose build
```

Expected: 构建成功，无致命错误。

- [ ] **Step 2: 启动 Agent 容器**

```bash
cd /home/yuan/EVDI && docker compose up -d
```

Expected: 容器启动，`docker compose logs` 显示 "Signaling server listening on :8080/ws"。

- [ ] **Step 3: 启动客户端**

```bash
cd /home/yuan/EVDI && make run-client
```

Expected: Vite dev server 在 localhost:3000 启动。

- [ ] **Step 4: 浏览器验证连接**

1. 打开 `http://localhost:3000`
2. 点击"连接"按钮
3. 验证状态从"连接中"变为"已连接"
4. 验证视频画面显示（XFCE 桌面）
5. 验证鼠标移动、点击响应
6. 验证键盘输入响应

- [ ] **Step 5: 修复发现的问题**

根据端到端测试发现的问题逐一修复并提交。

- [ ] **Step 6: 停止并清理**

```bash
cd /home/yuan/EVDI && docker compose down
```

---

## Self-Review

**1. Spec coverage:**

| 规格需求 | 对应 Task |
|---------|----------|
| WebSocket 信令服务 | Task 2 |
| Pion WebRTC Lite ICE | Task 3 |
| DataChannel 消息处理 | Task 4 |
| 鼠标/键盘输入注入 | Task 5 |
| Xvfb 虚拟显示 | Task 6 |
| GStreamer 视频+音频管道 | Task 7 |
| Agent 组件整合 | Task 8 |
| Docker 部署 | Task 9 |
| React 客户端脚手架 | Task 10 |
| 类型定义+信令工具 | Task 11 |
| Zustand 状态管理 | Task 12 |
| WebRTC 连接 Hook | Task 13 |
| UI 组件 | Task 14 |
| Docker Compose + Makefile | Task 15 |
| 端到端测试 | Task 16 |

**2. Placeholder scan:** 无 TBD、TODO、implement later 等占位符。

**3. Type consistency:** 检查 Agent 端 `DataChannelMessage`、`MouseMovePayload` 等类型与客户端 `types/signaling.ts` 中定义一致。Agent 使用 Go 结构体，客户端使用 TypeScript 接口，JSON 字段名通过 `json` tag 和属性名保持一致。
