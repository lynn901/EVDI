package webrtc

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/evdi/agent/pkg/config"
	pionwebrtc "github.com/pion/webrtc/v4"
	"github.com/gorilla/websocket"
)

// SignalingMessage is the envelope for all WebSocket signaling messages.
type SignalingMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// SignalingServer provides a WebSocket endpoint at /ws for SDP offer/answer
// exchange and ICE candidate forwarding between the web client and the
// Agent's WebRTC engine.
type SignalingServer struct {
	addr     string
	upgrader websocket.Upgrader
	engine   *WebRTCEngine
}

// NewSignalingServer creates a new SignalingServer bound to the WebSocket port
// specified in cfg.
func NewSignalingServer(cfg *config.Config, engine *WebRTCEngine) *SignalingServer {
	return &SignalingServer{
		addr:   ":" + cfg.WSPort,
		engine: engine,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
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

	// Register ICE candidate callback to forward to client
	s.engine.OnICECandidate(func(candidate *pionwebrtc.ICECandidate) {
		if candidate != nil {
			init := candidate.ToJSON()
			s.sendSignal(conn, "ice", init)
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
