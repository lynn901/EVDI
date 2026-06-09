package webrtc

import "encoding/json"

// DataChannelMessage is the envelope for all DataChannel messages.
type DataChannelMessage struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	Ts      int64           `json:"ts"`
	Seq     int             `json:"seq"`
	Payload json.RawMessage `json:"payload"`
}

// MouseMovePayload represents a mouse move event.
type MouseMovePayload struct {
	X         int `json:"x"`
	Y         int `json:"y"`
	DisplayID int `json:"display_id"`
}

// MouseButtonPayload represents a mouse button click/release event.
type MouseButtonPayload struct {
	Button int    `json:"button"`
	Action string `json:"action"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
}

// MouseWheelPayload represents a mouse scroll event.
type MouseWheelPayload struct {
	DeltaX int `json:"delta_x"`
	DeltaY int `json:"delta_y"`
	X      int `json:"x"`
	Y      int `json:"y"`
}

// KeyPayload represents a keyboard key event.
type KeyPayload struct {
	Keycode int    `json:"keycode"`
	Action  string `json:"action"`
	Shift   bool   `json:"shift"`
	Ctrl    bool   `json:"ctrl"`
	Alt     bool   `json:"alt"`
}

// ClipboardPayload represents clipboard data transfer.
type ClipboardPayload struct {
	Data     string `json:"data"`
	MimeType string `json:"mime_type"`
}

// ResizePayload represents a display resize event.
type ResizePayload struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// CtrlPingPayload is the payload for a control-channel ping.
type CtrlPingPayload struct {
	Ts int64 `json:"ts"`
}

// CtrlPongPayload is the payload for a control-channel pong response.
type CtrlPongPayload struct {
	Ts int64 `json:"ts"`
}
