package gstreamer

import (
	"fmt"
	"log"
	"time"

	"github.com/evdi/agent/pkg/config"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type Pipeline struct {
	videoTrack *webrtc.TrackLocalStaticSample
	audioTrack *webrtc.TrackLocalStaticSample
	stopChan   chan struct{}
	cfg        *config.Config
	cmd        *GStreamerCmd
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

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start gstreamer subprocess: %w", err)
	}

	go p.readH264Stream()
	return nil
}

func (p *Pipeline) readH264Stream() {
	buf := make([]byte, 1024*1024)
	var pending []byte
	frameDuration := time.Second / time.Duration(p.cfg.VideoFPS)
	frameCount := 0

	// Buffer one complete access unit (all NALUs between AUD delimiters)
	var auBuf []byte

	for {
		n, err := p.cmd.Stdout().Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)

			for {
				start := findNALUStart(pending, 0)
				if start < 0 {
					break
				}
				end := findNALUStart(pending, start+3)
				if end < 0 {
					break
				}

				nalu := pending[start:end]
				pending = pending[end:]

				if len(nalu) < 4 {
					continue
				}

				scLen := 4
				if nalu[0] != 0 || nalu[1] != 0 || nalu[2] != 0 || nalu[3] != 1 {
					scLen = 3
				}
				naluType := nalu[scLen] & 0x1F

				// AUD (type 9) marks the start of a new access unit.
				// When we see it, flush the previous AU as one sample.
				if naluType == 9 {
					if len(auBuf) > 0 {
						frameCount++
						if writeErr := p.videoTrack.WriteSample(media.Sample{
							Data:     auBuf,
							Duration: frameDuration,
						}); writeErr != nil {
							log.Printf("WriteSample error: %v", writeErr)
						}
						if frameCount <= 5 || frameCount%30 == 0 {
							log.Printf("[H264] Frame #%d: size=%d", frameCount, len(auBuf))
						}
					}
					auBuf = nil
				}

				auBuf = append(auBuf, nalu...)
			}
		}

		if err != nil {
			// Flush remaining AU
			if len(auBuf) > 0 {
				p.videoTrack.WriteSample(media.Sample{Data: auBuf, Duration: frameDuration})
			}
			select {
			case <-p.stopChan:
				return
			default:
				log.Printf("GStreamer read error: %v", err)
				return
			}
		}
	}
}

// findNALUStart finds the next H.264 NAL unit start code in data[start:].
// Matches both 4-byte (00 00 00 01) and 3-byte (00 00 01) start codes.
func findNALUStart(data []byte, start int) int {
	for i := start; i+2 < len(data); i++ {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			// Check for 4-byte start code
			if i > 0 && data[i-1] == 0 {
				return i - 1
			}
			return i
		}
	}
	return -1
}

func (p *Pipeline) Stop() {
	close(p.stopChan)
	if p.cmd != nil {
		p.cmd.Stop()
	}
}
