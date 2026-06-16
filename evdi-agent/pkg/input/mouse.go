package input

import (
	"fmt"
	"math"
	"os/exec"
)

func MouseMoveCmd(x, y int) *exec.Cmd {
	return exec.Command("xdotool", "mousemove", fmt.Sprintf("%d", x), fmt.Sprintf("%d", y))
}

func MouseButtonCmd(button int, action string) *exec.Cmd {
	if action == "down" {
		return exec.Command("xdotool", "mousedown", fmt.Sprintf("%d", button))
	}
	return exec.Command("xdotool", "mouseup", fmt.Sprintf("%d", button))
}

// MouseWheelCmd builds xdotool commands to simulate mouse scroll events.
// Browser WheelEvent deltas are normalized to "lines" before reaching here:
//   - deltaY > 0 → scroll down  (xdotool button 5)
//   - deltaY < 0 → scroll up    (xdotool button 4)
//   - deltaX > 0 → scroll right (xdotool button 7)
//   - deltaX < 0 → scroll left  (xdotool button 6)
//
// The absolute value determines the number of scroll clicks.
// One "line" of scroll ≈ one xdotool click, which matches standard X11 behavior.
func MouseWheelCmd(deltaX, deltaY float64) []*exec.Cmd {
	var cmds []*exec.Cmd

	// Vertical scroll: button 4 = up, button 5 = down
	if deltaY != 0 {
		button := 5 // down
		if deltaY < 0 {
			button = 4 // up
		}
		clicks := int(math.Abs(deltaY))
		if clicks < 1 {
			clicks = 1 // minimum one click for any non-zero delta
		}
		if clicks > 20 {
			clicks = 20 // cap to prevent runaway scrolling
		}
		args := []string{"click", "--repeat", fmt.Sprintf("%d", clicks), "--delay", "0", fmt.Sprintf("%d", button)}
		cmds = append(cmds, exec.Command("xdotool", args...))
	}

	// Horizontal scroll: button 6 = left, button 7 = right
	if deltaX != 0 {
		button := 7 // right
		if deltaX < 0 {
			button = 6 // left
		}
		clicks := int(math.Abs(deltaX))
		if clicks < 1 {
			clicks = 1
		}
		if clicks > 20 {
			clicks = 20
		}
		args := []string{"click", "--repeat", fmt.Sprintf("%d", clicks), "--delay", "0", fmt.Sprintf("%d", button)}
		cmds = append(cmds, exec.Command("xdotool", args...))
	}

	return cmds
}