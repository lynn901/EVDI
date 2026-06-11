package gstreamer

import (
	"fmt"
	"io"
	"log"
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
	pipelineStr := fmt.Sprintf(
		"ximagesrc display-name=%s use-damage=false show-pointer=true startx=0 starty=0 endx=%d endy=%d ! "+
			"video/x-raw,framerate=%d/1 ! "+
			"videoconvert ! video/x-raw,format=I420 ! "+
			"x264enc tune=zerolatency speed-preset=ultrafast byte-stream=true threads=1 ! "+
			"video/x-h264,stream-format=byte-stream,profile=constrained-baseline ! "+
			"fdsink fd=1 sync=false",
		g.cfg.Display, g.cfg.VideoWidth-1, g.cfg.VideoHeight-1, g.cfg.VideoFPS,
	)

	g.cmd = exec.Command("sh", "-c", "gst-launch-1.0 -v "+pipelineStr)
	g.cmd.Env = append(os.Environ(), "DISPLAY="+g.cfg.Display)
	log.Printf("[GStreamer] Starting pipeline: %s", pipelineStr)
	g.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var err error
	g.stdout, err = g.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("create stdout pipe: %w", err)
	}

	stderr, err := g.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("create stderr pipe: %w", err)
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				log.Printf("[GStreamer] %s", string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()

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
