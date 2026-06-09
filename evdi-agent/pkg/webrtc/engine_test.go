package webrtc

import (
	"testing"

	"github.com/evdi/agent/pkg/config"
)

func TestNewWebRTCEngine(t *testing.T) {
	cfg := &config.Config{WebRTCPortMin: 50000, WebRTCPortMax: 50100}
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
	cfg := &config.Config{WebRTCPortMin: 50000, WebRTCPortMax: 50100}
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
