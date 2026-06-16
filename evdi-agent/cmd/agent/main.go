package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/evdi/agent/pkg/config"
	"github.com/evdi/agent/pkg/gstreamer"
	"github.com/evdi/agent/pkg/input"
	"github.com/evdi/agent/pkg/webrtc"
)

func init() {
	// Reap zombie child processes from xdotool/mousemove etc.
	// Without this, Start()'d processes accumulate as zombies and exhaust PID limits.
	go func() {
		for {
			syscall.Wait4(-1, nil, 0, nil)
		}
	}()
}

func main() {
	cfg := config.Load()
	log.Printf("EVDI Agent starting, WS port=%s, display=%s, video=%dx%d@%dfps, webrtc-ports=%d-%d, nat1to1=%s",
		cfg.WSPort, cfg.Display, cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS,
		cfg.WebRTCPortMin, cfg.WebRTCPortMax, cfg.NAT1To1IP)

	// Xvfb is managed by entrypoint.sh — agent only uses the DISPLAY env var.
	// Verify the display is accessible before proceeding.
	if os.Getenv("DISPLAY") == "" {
		log.Fatalf("DISPLAY env var not set — Xvfb must be started by entrypoint.sh")
	}
	log.Printf("Using display %s (started by entrypoint)", cfg.Display)

	// Mouse move coalescing: only execute the latest position
	mouseCh := make(chan webrtc.MouseMovePayload, 64)
	go func() {
		for p := range mouseCh {
			input.MouseMoveCmd(p.X, p.Y).Start()
			// Drain any queued-up positions, keep only the latest
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

	// 2. 创建信令服务器，每次连接创建新 Engine + GStreamer
	var pipelineMu sync.Mutex
	var currentPipeline *gstreamer.Pipeline

	sigServer := webrtc.NewSignalingServer(cfg,
		func(engine *webrtc.WebRTCEngine) {
			pipelineMu.Lock()
			defer pipelineMu.Unlock()
			if currentPipeline != nil {
				currentPipeline.Stop()
			}
			pipe := gstreamer.NewPipeline(cfg, engine.VideoTrack(), engine.AudioTrack())
			if err := pipe.Start(); err != nil {
				log.Printf("Failed to start GStreamer: %v", err)
				return
			}
			currentPipeline = pipe
			log.Printf("GStreamer pipeline started for new connection")

			engine.OnDataChannel(func(channel string, msg webrtc.DataChannelMessage) {
				handleInputMessage(msg, mouseCh)
			})
		},
		func() {
			pipelineMu.Lock()
			defer pipelineMu.Unlock()
			if currentPipeline != nil {
				currentPipeline.Stop()
				currentPipeline = nil
			}
			log.Printf("GStreamer pipeline stopped, connection closed")
		},
		func(msg webrtc.DataChannelMessage) {
			handleInputMessage(msg, mouseCh)
		},
	)

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

func handleInputMessage(msg webrtc.DataChannelMessage, mouseCh chan<- webrtc.MouseMovePayload) {
	switch msg.Type {
	case "input.mouse_move":
		var p webrtc.MouseMovePayload
		if err := json.Unmarshal(msg.Payload, &p); err == nil {
			select {
			case mouseCh <- p:
			default:
			}
		}
	case "input.mouse_button":
		var p webrtc.MouseButtonPayload
		if err := json.Unmarshal(msg.Payload, &p); err == nil {
			input.MouseMoveCmd(p.X, p.Y).Start()
			input.MouseButtonCmd(p.Button, p.Action).Start()
		}
	case "input.mouse_wheel":
		var p webrtc.MouseWheelPayload
		if err := json.Unmarshal(msg.Payload, &p); err != nil {
			log.Printf("[Input] mouse_wheel unmarshal error: %v", err)
		} else {
			for _, cmd := range input.MouseWheelCmd(p.DeltaX, p.DeltaY) {
				cmd.Start()
			}
		}
	case "input.key":
		var p webrtc.KeyPayload
		if err := json.Unmarshal(msg.Payload, &p); err == nil {
			log.Printf("[Input] key: keycode=%d action=%s shift=%v ctrl=%v alt=%v capsLock=%v", p.Keycode, p.Action, p.Shift, p.Ctrl, p.Alt, p.CapsLock)
			input.KeyCmd(p.Keycode, p.Action, p.Shift, p.Ctrl, p.Alt, p.CapsLock).Start()
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
