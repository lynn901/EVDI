package input

import (
	"strconv"
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

func TestMouseWheelCmdVertical(t *testing.T) {
	tests := []struct {
		name      string
		deltaX    float64
		deltaY    float64
		wantCmds  int    // expected number of commands
		wantBtn   int    // expected button for first command (4=up, 5=down)
		wantClicks int   // expected --repeat count
	}{
		{"scroll up 1 line", 0, -1, 1, 4, 1},
		{"scroll down 1 line", 0, 1, 1, 5, 1},
		{"scroll up 3 lines", 0, -3, 1, 4, 3},
		{"scroll down 5 lines", 0, 5, 1, 5, 5},
		{"small fractional scroll down", 0, 0.5, 1, 5, 1},  // rounds up to min 1
		{"small fractional scroll up", 0, -0.3, 1, 4, 1},    // rounds up to min 1
		{"large scroll capped at 20", 0, -50, 1, 4, 20},     // capped at 20
		{"zero deltas", 0, 0, 0, 0, 0},                       // no commands
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds := MouseWheelCmd(tt.deltaX, tt.deltaY)
			if len(cmds) != tt.wantCmds {
				t.Fatalf("got %d commands, want %d", len(cmds), tt.wantCmds)
			}
			if tt.wantCmds == 0 {
				return
			}
			args := cmds[0].Args
			// args[0] = "xdotool", args[1] = "click", args[2] = "--repeat",
			// args[3] = count, args[4] = "--delay", args[5] = "0", args[6] = button
			if args[6] != strconv.Itoa(tt.wantBtn) {
				t.Errorf("button = %q, want %d", args[6], tt.wantBtn)
			}
			if args[3] != strconv.Itoa(tt.wantClicks) {
				t.Errorf("repeat count = %q, want %d", args[3], tt.wantClicks)
			}
		})
	}
}

func TestMouseWheelCmdHorizontal(t *testing.T) {
	tests := []struct {
		name      string
		deltaX    float64
		deltaY    float64
		wantCmds  int
		wantBtn   int    // 6=left, 7=right
		wantClicks int
	}{
		{"scroll right", 3, 0, 1, 7, 3},
		{"scroll left", -2, 0, 1, 6, 2},
		{"diagonal scroll", -3, 5, 2, 0, 0}, // 2 commands, check separately
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmds := MouseWheelCmd(tt.deltaX, tt.deltaY)
			if len(cmds) != tt.wantCmds {
				t.Fatalf("got %d commands, want %d", len(cmds), tt.wantCmds)
			}
			if tt.name == "diagonal scroll" {
				// First cmd is vertical (button 5 = down), second is horizontal (button 6 = left)
				if len(cmds) != 2 {
					return
				}
				// Verify vertical (first) and horizontal (second) exist
				if cmds[0].Args[6] != "5" || cmds[1].Args[6] != "6" {
					t.Errorf("diagonal: got buttons %s and %s, want 5 and 6", cmds[0].Args[6], cmds[1].Args[6])
				}
			}
		})
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