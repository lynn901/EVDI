package input

import (
	"fmt"
	"os/exec"
)

func MouseMoveCmd(x, y int) *exec.Cmd {
	return exec.Command("xdotool", "mousemove", "--sync", fmt.Sprintf("%d", x), fmt.Sprintf("%d", y))
}

func MouseButtonCmd(button int, action string) *exec.Cmd {
	if action == "down" {
		return exec.Command("xdotool", "mousedown", fmt.Sprintf("%d", button))
	}
	return exec.Command("xdotool", "mouseup", fmt.Sprintf("%d", button))
}

func MouseWheelCmd(deltaX, deltaY int) *exec.Cmd {
	if deltaY > 0 {
		return exec.Command("xdotool", "click", "4")
	} else if deltaY < 0 {
		return exec.Command("xdotool", "click", "5")
	}
	return exec.Command("xdotool", "click", "0")
}