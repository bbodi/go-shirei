//go:build !darwin || ios

package main

import (
	"fmt"
	. "go.hasen.dev/shirei"
)

// Other backends exercise the portable action channel with the same form.
func driveAccess(step int) error {
	send := func(name string, kind AccessActionKind, value float32) {
		n, _ := QueryContainer(name)
		GetFrameInput().AccessAction = AccessAction{ID: n.ID, Kind: kind, Value: value}
	}
	switch step {
	case 1:
		send("save", AccessPress, 0)
	case 2:
		if clicks != 1 {
			return fmt.Errorf("press count %d", clicks)
		}
		send("sound", AccessPress, 0)
	case 3:
		if !sound {
			return fmt.Errorf("checkbox does not toggle")
		}
		send("volume", AccessIncrement, 0)
	case 4:
		if volume != 50 {
			return fmt.Errorf("increment: %g", volume)
		}
		send("volume", AccessSetValue, 67)
	case 5:
		if volume != 70 {
			return fmt.Errorf("set-value: %g", volume)
		}
		saveLabel = "Store preferences"
	case 6:
		disabled = true
	case 7:
		send("save", AccessPress, 0)
	case 8:
		if clicks != 1 {
			return fmt.Errorf("disabled button activates")
		}
		showSave = false
	}
	return nil
}
