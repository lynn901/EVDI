package webrtc

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/evdi/agent/pkg/config"
	pionwebrtc "github.com/pion/webrtc/v4"
	"github.com/gorilla/websocket"
)

// SignalingMessage is the envelope for all WebSocket signaling messages.
type SignalingMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// OnConnectFunc is called when a new WebRTC session is established,
// providing the engine's media tracks for the caller to wire up.
type OnConnectFunc func(engine *WebRTCEngine)

// OnDisconnectFunc is called when the WebRTC session is torn down.
type OnDisconnectFunc func()

// OnInputFunc is called when an input event arrives over WebSocket.
type OnInputFunc func(msg DataChannelMessage)

// SignalingServer provides a WebSocket endpoint at /ws for SDP offer/answer
// exchange and ICE candidate forwarding between the web client and the
// Agent's WebRTC engine. A new WebRTCEngine is created per connection.
type SignalingServer struct {
	addr         string
	upgrader     websocket.Upgrader
	cfg          *config.Config
	onConnect    OnConnectFunc
	onDisconnect OnDisconnectFunc
	onInput      OnInputFunc
}

// NewSignalingServer creates a new SignalingServer bound to the WebSocket port
// specified in cfg.
func NewSignalingServer(cfg *config.Config, onConnect OnConnectFunc, onDisconnect OnDisconnectFunc, onInput OnInputFunc) *SignalingServer {
	return &SignalingServer{
		addr:   ":" + cfg.WSPort,
		cfg:    cfg,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		onConnect:    onConnect,
		onDisconnect: onDisconnect,
		onInput:      onInput,
	}
}

// Start begins listening for WebSocket connections on /ws.
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

	// Create a fresh WebRTCEngine for this connection
	engine, err := NewWebRTCEngine(s.cfg)
	if err != nil {
		log.Printf("Failed to create WebRTC engine: %v", err)
		return
	}
	defer engine.Close()

	// Notify caller about the new engine (wire up GStreamer, etc.)
	if s.onConnect != nil {
		s.onConnect(engine)
	}
	if s.onDisconnect != nil {
		defer s.onDisconnect()
	}

	var writeMu sync.Mutex

	sendSafe := func(msgType string, data interface{}) {
		msg := SignalingMessage{
			Type: msgType,
			Data: mustMarshal(data),
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		if err := conn.WriteJSON(msg); err != nil {
			log.Printf("WebSocket write error: %v", err)
		}
	}

	// Register ICE candidate callback to forward to client
	engine.OnICECandidate(func(candidate *pionwebrtc.ICECandidate) {
		if candidate != nil {
			init := candidate.ToJSON()
			log.Printf("Sending ICE candidate to client: %s", init.Candidate)
			sendSafe("ice", init)
		}
	})

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
			log.Printf("Received offer from client")
			answer, err := engine.HandleOffer(msg.Data)
			if err != nil {
				log.Printf("HandleOffer error: %v", err)
				continue
			}
			log.Printf("Sending answer to client")
			sendSafe("answer", answer)

		case "ice":
			log.Printf("Received ICE candidate from client")
			if err := engine.HandleICECandidate(msg.Data); err != nil {
				log.Printf("HandleICE error: %v", err)
			}

		case "ping":
			sendSafe("pong", map[string]int64{"ts": 0})

		default:
			// Input events: input.mouse_move, input.mouse_button, input.mouse_wheel, input.key, etc.
			if s.onInput != nil {
				log.Printf("[Input] Raw msg.Data: %s", string(msg.Data))
				var dcMsg DataChannelMessage
				if err := json.Unmarshal(msg.Data, &dcMsg); err != nil {
					log.Printf("[Input] Unmarshal error: %v", err)
				} else if dcMsg.Type != "" {
					s.onInput(dcMsg)
				}
			}
		}
	}
}
