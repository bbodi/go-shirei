//go:build darwin && !ios

package main

import (
	"fmt"
	"math"
	"strings"
	"structs"

	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"
	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/cocoabackend"
)

func selector(s string) objc.SEL { return objc.RegisterName(s) }
func nativeString(object objc.ID, method string) string {
	value := object.Send(selector(method))
	if value == 0 {
		return ""
	}
	return cstrings.NSStringToString(value)
}
func nativeFind(object objc.ID, name string) objc.ID {
	if strings.HasPrefix(nativeString(object, "accessibilityIdentifier"), name+"#") {
		return object
	}
	children := object.Send(selector("accessibilityChildren"))
	for i, n := uint(0), objc.Send[uint](children, selector("count")); i < n; i++ {
		if found := nativeFind(children.Send(selector("objectAtIndex:"), i), name); found != 0 {
			return found
		}
	}
	return 0
}

type point struct {
	_    structs.HostLayout
	X, Y float64
}
type size struct {
	_    structs.HostLayout
	W, H float64
}
type rect struct {
	_      structs.HostLayout
	Origin point
	Size   size
}

var retainedSave objc.ID

func driveAccess(step int) error {
	context := GetHost().EscapeHatchBackendContext.(cocoabackend.Context)
	window := objc.ID(uintptr(context.NSWindow()))
	view := window.Send(selector("contentView"))
	save, soundElement, slider := nativeFind(view, "save"), nativeFind(view, "sound"), nativeFind(view, "volume")
	fail := func(message string) error { return fmt.Errorf("step %d: %s", step, message) }
	switch step {
	case 0:
		if save == 0 || soundElement == 0 || slider == 0 {
			return fail("missing native controls")
		}
		if nativeString(save, "accessibilityLabel") != saveLabel || nativeString(soundElement, "accessibilityLabel") != "Enable sound" {
			return fail("incorrect spoken labels")
		}
		if nativeString(save, "accessibilityRole") != "AXButton" || nativeString(slider, "accessibilityRole") != "AXSlider" {
			return fail("incorrect roles")
		}
		if objc.Send[bool](save, selector("isAccessibilitySelectorAllowed:"), selector("setAccessibilityValue:")) {
			return fail("button advertises writable value")
		}
		for _, attribute := range []string{"AXRole", "AXTitle", "AXDescription", "AXValue"} {
			name := objc.ID(objc.GetClass("NSString")).Send(selector("stringWithUTF8String:"), attribute)
			if objc.Send[bool](save, selector("accessibilityIsAttributeSettable:"), name) {
				return fail("button exposes a writable " + attribute)
			}
		}
		retainedSave = save.Send(selector("retain"))
	case 1:
		if !objc.Send[bool](save, selector("accessibilityPerformPress")) {
			return fail("native press rejected")
		}
	case 2:
		if clicks != 1 || save != retainedSave {
			return fail("press count or native identity changed")
		}
		if !objc.Send[bool](soundElement, selector("accessibilityPerformPress")) {
			return fail("checkbox press rejected")
		}
	case 3:
		if !sound || objc.Send[int](soundElement.Send(selector("accessibilityValue")), selector("intValue")) != 1 {
			return fail("checkbox state not published")
		}
		if !objc.Send[bool](slider, selector("accessibilityPerformIncrement")) {
			return fail("increment rejected")
		}
		slider.Send(selector("setAccessibilityFocused:"), true)
	case 4:
		if volume != 50 || !objc.Send[bool](slider, selector("isAccessibilityFocused")) || view.Send(selector("accessibilityFocusedUIElement")) != slider {
			return fail("queued increment/focus missing")
		}
		number := objc.ID(objc.GetClass("NSNumber")).Send(selector("numberWithDouble:"), float64(67))
		slider.Send(selector("setAccessibilityValue:"), number)
	case 5:
		if volume != 70 {
			return fail("native set-value bypasses widget step")
		}
		saveLabel = "Store preferences"
	case 6:
		if save != retainedSave || nativeString(save, "accessibilityLabel") != saveLabel {
			return fail("label-only update missing")
		}
		disabled = true
	case 7:
		if objc.Send[bool](save, selector("isAccessibilityEnabled")) || objc.Send[bool](save, selector("accessibilityPerformPress")) {
			return fail("disabled control accepts action")
		}
		showSave = false
	case 8:
		if save != 0 || objc.Send[bool](retainedSave, selector("accessibilityPerformPress")) {
			return fail("removed element remains active")
		}
		retainedSave.Send(selector("release"))
		retainedSave = 0
	case 9:
		before := objc.Send[rect](slider, selector("accessibilityFrame"))
		frame := objc.Send[rect](window, selector("frame"))
		moved := point{X: frame.Origin.X + 32, Y: frame.Origin.Y + 24}
		window.Send(selector("setFrameOrigin:"), moved)
		after := objc.Send[rect](slider, selector("accessibilityFrame"))
		window.Send(selector("setFrameOrigin:"), frame.Origin)
		if math.Abs(after.Origin.X-before.Origin.X-32) > 0.1 || math.Abs(after.Origin.Y-before.Origin.Y-24) > 0.1 {
			return fail("accessibility frame does not follow window")
		}
		center := point{X: before.Origin.X + before.Size.W/2, Y: before.Origin.Y + before.Size.H/2}
		if view.Send(selector("accessibilityHitTest:"), center) != slider {
			return fail("native hit test misses slider")
		}
	}
	return nil
}
