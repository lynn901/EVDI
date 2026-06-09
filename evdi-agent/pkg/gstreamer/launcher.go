package gstreamer

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"

	"github.com/evdi/agent/pkg/config"
)

type GStreamerCmd struct {
	cfg    *config.Config
	cmd    *exec.Cmd
	stdout io.ReadCloser
}

func NewGStreamerCmd(cfg *config.Config) *GStreamerCmd {
	return &GStreamerCmd{cfg: cfg}
}

func (g *GStreamerCmd) Start() error {
	// Video pipeline: ximagesrc -> x264enc -> stdout
	// Audio pipeline: pulsesrc -> opusenc -> stdout
	// For MVP, run both as a single gst-launch pipeline with fdsink
	pipelineStr := fmt.Sprintf(
		"ximagesrc display-name=%s ! "+
			"video/x-raw, framerate=%d/1, width=%d, height=%d ! "+
			"videoconvert ! "+
			"x264enc tune=zerolatency speed-preset=ultrafast byte-stream=true ! "+
			"video/x-h264, stream-format=byte-stream ! "+
			"fdsink fd=1 sync=false",
		g.cfg.Display, g.cfg.VideoFPS, g.cfg.VideoWidth, g.cfg.VideoHeight,
	)

	g.cmd = exec.Command("gst-launch-1.0", "-v", pipelineStr)
	g.cmd.Env = append(os.Environ(), "DISPLAY="+g.cfg.Display)
	g.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var err error
	g.stdout, err = g.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := g.cmd.Start(); err != nil {
		return fmt.Errorf("start gst-launch: %w", err)
	}
	return nil
}

func (g *GStreamerCmd) Stdout() io.ReadCloser {
	return g.stdout
}

func (g *GStreamerCmd) Stop() error {
	if g.cmd != nil && g.cmd.Process != nil {
		return syscall.Kill(-g.cmd.Process.Pid, syscall.SIGTERM)
	}
	return nil
}
