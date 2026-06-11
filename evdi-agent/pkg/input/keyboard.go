package input

import (
	"fmt"
	"os/exec"
)

// KeyCmd builds an xdotool command for a key event.
// Modifiers (shift/ctrl/alt) are sent as separate keydown/keyup events
// to avoid xdotool's combined keysym parsing issues.
func KeyCmd(keycode int, action string, shift, ctrl, alt bool) *exec.Cmd {
	keySym := keyCodeToXKeySym(keycode)

	if action == "down" {
		// For keydown with modifiers, use xdotool key --delay 0 which handles
		// the press sequence correctly (modifiers first, then key)
		if shift || ctrl || alt {
			args := []string{"key", "--delay", "0", "--clearmodifiers"}
			if ctrl {
				args = append(args, "ctrl+"+keySym)
			} else if alt {
				args = append(args, "alt+"+keySym)
			} else if shift {
				args = append(args, "shift+"+keySym)
			}
			return exec.Command("xdotool", args...)
		}
		return exec.Command("xdotool", "keydown", keySym)
	}

	// keyup - just release the key
	return exec.Command("xdotool", "keyup", keySym)
}

// keyCodeToXKeySym maps browser e.keyCode values to X11 KeySym names.
// Browser keyCode values follow the DOM Level 3 / Windows Virtual Key Code standard.
func keyCodeToXKeySym(keycode int) string {
	// Letters: A=65 ... Z=90
	if keycode >= 65 && keycode <= 90 {
		return fmt.Sprintf("%c", keycode)
	}
	// Digits: 0=48 ... 9=57
	if keycode >= 48 && keycode <= 57 {
		return fmt.Sprintf("%c", keycode)
	}

	switch keycode {
	// Function keys
	case 112:
		return "F1"
	case 113:
		return "F2"
	case 114:
		return "F3"
	case 115:
		return "F4"
	case 116:
		return "F5"
	case 117:
		return "F6"
	case 118:
		return "F7"
	case 119:
		return "F8"
	case 120:
		return "F9"
	case 121:
		return "F10"
	case 122:
		return "F11"
	case 123:
		return "F12"

	// Navigation
	case 37:
		return "Left"
	case 38:
		return "Up"
	case 39:
		return "Right"
	case 40:
		return "Down"
	case 33:
		return "Page_Up"
	case 34:
		return "Page_Down"
	case 35:
		return "End"
	case 36:
		return "Home"
	case 45:
		return "Insert"
	case 46:
		return "Delete"

	// Editing
	case 8:
		return "BackSpace"
	case 9:
		return "Tab"
	case 13:
		return "Return"
	case 32:
		return "space"
	case 27:
		return "Escape"

	// Lock keys
	case 20:
		return "Caps_Lock"
	case 144:
		return "Num_Lock"
	case 145:
		return "Scroll_Lock"

	// Punctuation and symbols
	case 186:
		return "semicolon"
	case 187:
		return "equal"
	case 188:
		return "comma"
	case 189:
		return "minus"
	case 190:
		return "period"
	case 191:
		return "slash"
	case 192:
		return "grave"
	case 219:
		return "bracketleft"
	case 220:
		return "backslash"
	case 221:
		return "bracketright"
	case 222:
		return "apostrophe"

	// Modifiers (for standalone press/release tracking)
	case 16:
		return "Shift_L"
	case 17:
		return "Control_L"
	case 18:
		return "Alt_L"

	// Space bar
	case 91:
		return "Super_L"

	// Numpad
	case 96:
		return "KP_0"
	case 97:
		return "KP_1"
	case 98:
		return "KP_2"
	case 99:
		return "KP_3"
	case 100:
		return "KP_4"
	case 101:
		return "KP_5"
	case 102:
		return "KP_6"
	case 103:
		return "KP_7"
	case 104:
		return "KP_8"
	case 105:
		return "KP_9"
	case 106:
		return "KP_Multiply"
	case 107:
		return "KP_Add"
	case 109:
		return "KP_Subtract"
	case 110:
		return "KP_Decimal"
	case 111:
		return "KP_Divide"
	}

	return fmt.Sprintf("0x%04x", keycode)
}
