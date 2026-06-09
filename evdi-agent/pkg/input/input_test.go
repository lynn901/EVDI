package input

import (
	"testing"
)

func TestMouseMoveCommand(t *testing.T) {
	cmd := MouseMoveCmd(100, 200)
	if cmd.Path != "xdotool" {
		t.Errorf("path = %q, want xdotool", cmd.Path)
	}
}

func TestMouseButtonCommand(t *testing.T) {
	downCmd := MouseButtonCmd(1, "down")
	if downCmd.Path != "xdotool" {
		t.Errorf("path = %q, want xdotool", downCmd.Path)
	}
	upCmd := MouseButtonCmd(1, "up")
	if upCmd.Path != "xdotool" {
		t.Errorf("path = %q, want xdotool", upCmd.Path)
	}
}

func TestKeyDownUpCommand(t *testing.T) {
	downCmd := KeyCmd(65, "down", false, false, false)
	if downCmd.Path != "xdotool" {
		t.Errorf("path = %q, want xdotool", downCmd.Path)
	}
	upCmd := KeyCmd(65, "up", false, false, false)
	if upCmd.Path != "xdotool" {
		t.Errorf("path = %q, want xdotool", upCmd.Path)
	}
}

func TestKeycodeToXKeySym(t *testing.T) {
	tests := []struct {
		keycode int
		want    string
	}{
		{4, "a"},
		{29, "z"},
		{30, "1"},
		{39, "0"},
		{40, "Return"},
		{41, "Escape"},
		{44, "space"},
	}
	for _, tt := range tests {
		got := keycodeToXKeySym(tt.keycode)
		if got != tt.want {
			t.Errorf("keycodeToXKeySym(%d) = %q, want %q", tt.keycode, got, tt.want)
		}
	}
}