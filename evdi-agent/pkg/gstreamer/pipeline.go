package gstreamer

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/evdi/agent/pkg/config"
	"github.com/pion/webrtc/v4"
)

type Pipeline struct {
	videoTrack *webrtc.TrackLocalStaticSample
	audioTrack *webrtc.TrackLocalStaticSample
	stopChan   chan struct{}
	cfg        *config.Config
	cmd        *GStreamerCmd
}

type GStreamerSample struct {
	MediaType string          `json:"media_type"`
	Data      json.RawMessage `json:"data"`
	Duration  uint32          `json:"duration_ns"`
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
	cmd := NewGStreamerCmd(p.cfg)
	p.cmd = cmd

	// Start GStreamer as a subprocess and read H.264/Opus frames from stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start gstreamer subprocess: %w", err)
	}

	go p.readFrames()
	return nil
}

func (p *Pipeline) readFrames() {
	decoder := json.NewDecoder(p.cmd.Stdout())
	for {
		var sample GStreamerSample
		if err := decoder.Decode(&sample); err != nil {
			select {
			case <-p.stopChan:
				return
			default:
				log.Printf("GStreamer read error: %v", err)
				return
			}
		}

		switch sample.MediaType {
		case "video":
			p.videoTrack.WriteSampleMediaSample(webrtc.MediaSample{
				Data:     sample.Data,
				Duration: time.Duration(sample.Duration),
			})
		case "audio":
			p.audioTrack.WriteSampleMediaSample(webrtc.MediaSample{
				Data:     sample.Data,
				Duration: time.Duration(sample.Duration),
			})
		}
	}
}

func (p *Pipeline) Stop() {
	close(p.stopChan)
	if p.cmd != nil {
		p.cmd.Stop()
	}
}
