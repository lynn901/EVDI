package display

import (
	"testing"

	"github.com/evdi/agent/pkg/config"
)

func TestXvfbCommand(t *testing.T) {
	cfg := &config.Config{
		Display:    ":99",
		VideoWidth: 1920,
		VideoHeight: 1080,
	}
	xvfb := NewXvfb(cfg)
	cmd := xvfb.Command()
	if cmd.Path != "Xvfb" {
		t.Errorf("path = %q, want Xvfb", cmd.Path)
	}
	args := cmd.Args[1:]
	found := false
	for _, a := range args {
		if a == "-screen" {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing -screen argument")
	}
}

func TestXvfbScreenSpec(t *testing.T) {
	cfg := &config.Config{
		Display:    ":99",
		VideoWidth: 1920,
		VideoHeight: 1080,
	}
	xvfb := NewXvfb(cfg)
	cmd := xvfb.Command()
	// Args[0] is program name, check for screen spec
	found := false
	for _, a := range cmd.Args {
		if a == "1920x1080x24" {
			found = true
			break
		}
	}
	if !found {
		t.Error("missing screen spec 1920x1080x24 in command args")
	}
}