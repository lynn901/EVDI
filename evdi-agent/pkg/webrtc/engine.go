package webrtc

import (
	"encoding/json"
	"fmt"

	"github.com/evdi/agent/pkg/config"
	"github.com/pion/webrtc/v4"
)

// WebRTCEngine manages a single WebRTC peer connection with media tracks
// and DataChannel handling.
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

// NewWebRTCEngine creates a new WebRTCEngine with Lite ICE mode and the
// configured ephemeral UDP port range. It adds H.264 video and Opus audio
// tracks and registers DataChannel handlers.
func NewWebRTCEngine(cfg *config.Config) (*WebRTCEngine, error) {
	settingsEngine := webrtc.SettingEngine{}
	settingsEngine.SetLite(true)
	if err := settingsEngine.SetEphemeralUDPPortRange(cfg.WebRTCPortMin, cfg.WebRTCPortMax); err != nil {
		return nil, fmt.Errorf("set ephemeral UDP port range: %w", err)
	}

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
		pc.Close()
		return nil, fmt.Errorf("add video track: %w", err)
	}
	if _, err := pc.AddTrack(audioTrack); err != nil {
		pc.Close()
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

// VideoTrack returns the H.264 video track.
func (e *WebRTCEngine) VideoTrack() *webrtc.TrackLocalStaticSample { return e.videoTrack }

// AudioTrack returns the Opus audio track.
func (e *WebRTCEngine) AudioTrack() *webrtc.TrackLocalStaticSample { return e.audioTrack }

// OnICECandidate sets the callback for ICE candidate events.
func (e *WebRTCEngine) OnICECandidate(fn func(candidate *webrtc.ICECandidate)) {
	e.onICECandidate = fn
}

// OnDataChannel sets the callback for incoming DataChannel messages.
func (e *WebRTCEngine) OnDataChannel(fn func(channel string, msg DataChannelMessage)) {
	e.onDataChannel = fn
}

// HandleOffer processes an SDP offer and returns the SDP answer.
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

// HandleICECandidate adds a remote ICE candidate to the peer connection.
func (e *WebRTCEngine) HandleICECandidate(data json.RawMessage) error {
	var candidate webrtc.ICECandidateInit
	if err := json.Unmarshal(data, &candidate); err != nil {
		return fmt.Errorf("unmarshal ice candidate: %w", err)
	}
	return e.peerConnection.AddICECandidate(candidate)
}

// Close shuts down the peer connection.
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

// mustMarshal marshals v to JSON, returning nil on error.
func mustMarshal(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
