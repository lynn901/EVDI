package gstreamer

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/evdi/agent/pkg/config"
	"github.com/hraban/opus"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type Pipeline struct {
	videoTrack *webrtc.TrackLocalStaticSample
	audioTrack *webrtc.TrackLocalStaticSample
	audioCmd   *AudioGStreamerCmd
	opusEnc    *opus.Encoder
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
	// Start video capture
	cmd := NewGStreamerCmd(p.cfg)
	p.cmd = cmd

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start gstreamer subprocess: %w", err)
	}

	go p.readH264Stream()

	// Start audio capture + Opus encoding (non-fatal if audio fails)
	if p.audioTrack != nil {
		if err := p.startAudio(); err != nil {
			log.Printf("[Audio] Failed to start audio pipeline (video continues): %v", err)
		}
	}

	return nil
}

func (p *Pipeline) startAudio() error {
	audioCmd := NewAudioGStreamerCmd(p.cfg)
	if err := audioCmd.Start(); err != nil {
		return fmt.Errorf("start audio subprocess: %w", err)
	}
	p.audioCmd = audioCmd

	enc, err := opus.NewEncoder(48000, 2, opus.AppVoIP)
	if err != nil {
		audioCmd.Stop()
		return fmt.Errorf("create opus encoder: %w", err)
	}
	p.opusEnc = enc

	go p.readAudioStream()
	log.Printf("[Audio] Pipeline started successfully")
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

// readAudioStream reads PCM data from the audio GStreamer subprocess,
// encodes each 20ms frame to Opus, and writes to the WebRTC audio track.
func (p *Pipeline) readAudioStream() {
	// 20ms @ 48kHz stereo S16LE = 960 samples * 2 channels * 2 bytes = 3840 bytes
	const frameSize = 960           // samples per channel per frame
	const channels = 2
	const bytesPerSample = 2        // S16LE
	const frameBytes = frameSize * channels * bytesPerSample
	const frameDuration = time.Second / 50 // 20ms

	pcmBuf := make([]byte, frameBytes)
	opusBuf := make([]byte, 4000) // Opus frame max ~4000 bytes
	frameCount := 0

	for {
		select {
		case <-p.stopChan:
			return
		default:
		}

		_, err := io.ReadFull(p.audioCmd.Stdout(), pcmBuf)
		if err != nil {
			select {
			case <-p.stopChan:
				return
			default:
				log.Printf("[Audio] PCM read error: %v", err)
				return
			}
		}

		// Convert S16LE []byte to []int16 for Opus encoder
		pcmSamples := bytesToSamples(pcmBuf)

		n, err := p.opusEnc.Encode(pcmSamples, opusBuf)
		if err != nil {
			log.Printf("[Audio] Opus encode error: %v", err)
			continue
		}

		frameCount++
		if writeErr := p.audioTrack.WriteSample(media.Sample{
			Data:     opusBuf[:n],
			Duration: frameDuration,
		}); writeErr != nil {
			log.Printf("[Audio] WriteSample error: %v", writeErr)
		}

		if frameCount <= 5 || frameCount%250 == 0 {
			log.Printf("[Audio] Frame #%d: opus size=%d", frameCount, n)
		}
	}
}

// bytesToSamples converts S16LE (signed 16-bit little-endian) byte slice to int16 slice.
func bytesToSamples(b []byte) []int16 {
	samples := make([]int16, len(b)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return samples
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
	if p.audioCmd != nil {
		p.audioCmd.Stop()
	}
}
