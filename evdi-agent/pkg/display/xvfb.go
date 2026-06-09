package display

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/evdi/agent/pkg/config"
)

type Xvfb struct {
	display string
	width   int
	height  int
	depth   int
	cmd     *exec.Cmd
}

func NewXvfb(cfg *config.Config) *Xvfb {
	return &Xvfb{
		display: cfg.Display,
		width:   cfg.VideoWidth,
		height:  cfg.VideoHeight,
		depth:   24,
	}
}

func (x *Xvfb) Command() *exec.Cmd {
	screenSpec := fmt.Sprintf("%dx%dx%d", x.width, x.height, x.depth)
	return exec.Command("Xvfb", x.display, "-screen", "0", screenSpec, "-nolisten", "tcp")
}

func (x *Xvfb) Start() error {
	x.cmd = x.Command()
	x.cmd.Env = append(os.Environ(), "DISPLAY="+x.display)
	if err := x.cmd.Start(); err != nil {
		return fmt.Errorf("start Xvfb: %w", err)
	}
	return nil
}

func (x *Xvfb) Stop() error {
	if x.cmd != nil && x.cmd.Process != nil {
		return x.cmd.Process.Signal(syscall.SIGTERM)
	}
	return nil
}