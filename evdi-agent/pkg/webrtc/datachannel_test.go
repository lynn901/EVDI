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
}

func TestMouseMovePayload(t *testing.T) {
	raw := `{"v":1,"type":"input.mouse_move","ts":0,"seq":0,"payload":{"x":100,"y":200,"display_id":0}}`
	var msg DataChannelMessage
	json.Unmarshal([]byte(raw), &msg)
	var p MouseMovePayload
	json.Unmarshal(msg.Payload, &p)
	if p.X != 100 || p.Y != 200 {
		t.Errorf("payload = %+v, want x=100 y=200", p)
	}
}

func TestMouseButtonPayload(t *testing.T) {
	raw := `{"v":1,"type":"input.mouse_button","ts":0,"seq":0,"payload":{"button":1,"action":"down","x":100,"y":200}}`
	var msg DataChannelMessage
	json.Unmarshal([]byte(raw), &msg)
	var p MouseButtonPayload
	json.Unmarshal(msg.Payload, &p)
	if p.Button != 1 || p.Action != "down" {
		t.Errorf("payload = %+v, want button=1 action=down", p)
	}
}

func TestKeyPayload(t *testing.T) {
	raw := `{"v":1,"type":"input.key","ts":0,"seq":0,"payload":{"keycode":65,"action":"down","shift":false,"ctrl":false,"alt":false}}`
	var msg DataChannelMessage
	json.Unmarshal([]byte(raw), &msg)
	var p KeyPayload
	json.Unmarshal(msg.Payload, &p)
	if p.Keycode != 65 || p.Action != "down" {
		t.Errorf("payload = %+v, want keycode=65 action=down", p)
	}
}
