package main

import (
	"encoding/json"
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

	// 2. 创建 WebRTC 引擎
	engine, err := webrtc.NewWebRTCEngine(cfg)
	if err != nil {
		log.Fatalf("Failed to create WebRTC engine: %v", err)
	}
	defer engine.Close()

	// 3. 注册 DataChannel 回调 - 将输入事件转发到 xdotool
	engine.OnDataChannel(func(channel string, msg webrtc.DataChannelMessage) {
		handleDataChannelMessage(msg)
	})

	// 4. 启动信令服务器（在连接建立后再启动 GStreamer）
	sigServer := webrtc.NewSignalingServer(cfg, engine)

	// 监听 PeerConnection 连接状态
	go func() {
		// TODO: 在正式版本中，应在连接建立后启动 GStreamer
		// MVP 中直接启动
		pipe := gstreamer.NewPipeline(cfg, engine.VideoTrack(), engine.AudioTrack())
		if err := pipe.Start(); err != nil {
			log.Printf("Failed to start GStreamer: %v", err)
			return
		}
		defer pipe.Stop()
	}()

	go func() {
		if err := sigServer.Start(); err != nil {
			log.Fatalf("Signaling server error: %v", err)
		}
	}()

	log.Printf("EVDI Agent ready, waiting for client connection...")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Printf("Shutting down...")
}

func handleDataChannelMessage(msg webrtc.DataChannelMessage) {
	switch msg.Type {
	case "input.mouse_move":
		var p webrtc.MouseMovePayload
		if err := json.Unmarshal(msg.Payload, &p); err == nil {
			input.MouseMoveCmd(p.X, p.Y).Run()
		}
	case "input.mouse_button":
		var p webrtc.MouseButtonPayload
		if err := json.Unmarshal(msg.Payload, &p); err == nil {
			input.MouseButtonCmd(p.Button, p.Action).Run()
		}
	case "input.mouse_wheel":
		var p webrtc.MouseWheelPayload
		if err := json.Unmarshal(msg.Payload, &p); err == nil {
			input.MouseWheelCmd(p.DeltaX, p.DeltaY).Run()
		}
	case "input.key":
		var p webrtc.KeyPayload
		if err := json.Unmarshal(msg.Payload, &p); err == nil {
			input.KeyCmd(p.Keycode, p.Action, p.Shift, p.Ctrl, p.Alt).Run()
		}
	case "clipboard.push":
		log.Printf("Clipboard push received (MVP: no-op)")
	case "ctrl.resize":
		var p webrtc.ResizePayload
		if err := json.Unmarshal(msg.Payload, &p); err == nil {
			log.Printf("Resize request: %dx%d (MVP: no-op)", p.Width, p.Height)
		}
	}
}
