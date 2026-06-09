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
