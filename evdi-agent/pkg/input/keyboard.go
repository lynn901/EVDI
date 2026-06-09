package input

import (
	"fmt"
	"os/exec"
)

func KeyCmd(keycode int, action string, shift, ctrl, alt bool) *exec.Cmd {
	modifiers := ""
	if ctrl {
		modifiers += "ctrl+"
	}
	if alt {
		modifiers += "alt+"
	}
	if shift {
		modifiers += "shift+"
	}

	keySym := keycodeToXKeySym(keycode)

	if action == "down" {
		if modifiers != "" {
			return exec.Command("xdotool", "keydown", modifiers+keySym)
		}
		return exec.Command("xdotool", "keydown", keySym)
	}
	if modifiers != "" {
		return exec.Command("xdotool", "keyup", modifiers+keySym)
	}
	return exec.Command("xdotool", "keyup", keySym)
}

func keycodeToXKeySym(keycode int) string {
	if keycode >= 4 && keycode <= 29 {
		return fmt.Sprintf("%c", 'a'+keycode-4)
	}
	if keycode >= 30 && keycode <= 38 {
		return fmt.Sprintf("%c", '1'+keycode-30)
	}
	if keycode == 39 {
		return "0"
	}
	switch keycode {
	case 40:
		return "Return"
	case 41:
		return "Escape"
	case 42:
		return "BackSpace"
	case 43:
		return "Tab"
	case 44:
		return "space"
	case 57:
		return "Caps_Lock"
	case 58:
		return "F1"
	case 79:
		return "Right"
	case 80:
		return "Left"
	case 81:
		return "Down"
	case 82:
		return "Up"
	}
	return fmt.Sprintf("0x%04x", keycode)
}