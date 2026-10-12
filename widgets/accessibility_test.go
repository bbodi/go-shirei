package widgets

import (
	"slices"
	"strings"
	"testing"

	. "go.hasen.dev/shirei"
)

func TestAccessibleFormSnapshot(t *testing.T) {
	initFontsOnce.Do(InitFontSubsystem)
	ResetInputSession()
	GetHost().WindowSize = Vec2{600, 500}
	GetInputState().MousePoint = offscreen
	checked := true
	volume := float32(40)
	password := "private-password"
	spoken := "Save document"
	show := true
	scope := new(int)
	view := func() {
		ModAttrs(NoAnimate)
		ContainerWithKey(scope, Attrs(Pad(12), Gap(8)), func() {
			NextAccessName("form")
			NextAccessRole("group")
			AssignAccess()
			Label("Preferences")
			if show {
				ContainerWithKey("save-slot", Attrs(), func() {
					NextAccessName("save")
					NextAccessLabel(spoken)
					Button(NoIcon, "Save")
				})
			}
			NextAccessName("check")
			CheckBox(&checked, "Enable sound")
			NextAccessName("volume")
			NextAccessLabel("Volume")
			Slider(&volume, SliderAttrs{Min: 0, Max: 100, Step: 10})
			NextAccessName("disabled")
			ButtonExt("Unavailable", ButtonAttrs{Disabled: true}, DefaultButtonLook())
			NextAccessName("password")
			PasswordInput(&password)
		})
	}
	var out FrameOutputData
	for range 4 {
		out = RunFrameFn(view)
	}
	before := slices.Clone(out.Access)
	if out.AccessChanged {
		t.Fatal("settled snapshot keeps changing")
	}
	save, _ := QueryContainer("save")
	form, _ := QueryContainer("form")
	if save.Label != spoken || save.ParentID != form.ID || save.Bounds.Size[0] <= 0 {
		t.Fatalf("button semantics: %+v", save)
	}
	check, _ := QueryContainer("check")
	if check.Label != "Enable sound" || !check.Checked {
		t.Fatalf("checkbox semantics: %+v", check)
	}
	slider, _ := QueryContainer("volume")
	if !slider.Numeric || slider.Number != 40 || slider.Min != 0 || slider.Max != 100 || slider.Step != 10 {
		t.Fatalf("slider semantics: %+v", slider)
	}
	disabled, _ := QueryContainer("disabled")
	if !disabled.Disabled || disabled.Focusable {
		t.Fatalf("disabled semantics: %+v", disabled)
	}
	secure, _ := QueryContainer("password")
	if !secure.Protected || !secure.Editable || secure.Value != "" {
		t.Fatalf("protected semantics: %+v", secure)
	}
	var static []string
	for _, n := range out.Access {
		if strings.Contains(n.Value+n.Label, password) {
			t.Fatal("password in exported snapshot")
		}
		if n.Role == "statictext" {
			static = append(static, n.Label)
		}
	}
	if !slices.Equal(static, []string{"Preferences"}) {
		t.Fatalf("static text duplicates control labels: %q", static)
	}
	if reply := HandleInputCommand("show password"); strings.Contains(reply, password) {
		t.Fatal("password in drive output")
	}
	oldHash := out.SurfacesHash
	spoken = "Store document"
	out = RunFrameFn(view)
	if !out.AccessChanged || out.SurfacesHash != oldHash {
		t.Fatal("spoken-label-only change must publish without changing paint")
	}
	after, _ := QueryContainer("save")
	if after.ID != save.ID || before[2].Label != "Save document" {
		t.Fatal("unstable identity or retained snapshot")
	}
	FocusImmediateOn(save.Container)
	out = RunFrameFn(view)
	var focused []uint64
	for _, n := range out.Access {
		if n.KeyboardFocused {
			focused = append(focused, n.ID)
		}
	}
	if !slices.Equal(focused, []uint64{save.ID}) {
		t.Fatalf("exact focus: %v", focused)
	}
	show = false
	out = RunFrameFn(view)
	for _, n := range out.Access {
		if n.ID == save.ID {
			t.Fatal("removed button in snapshot")
		}
	}
}

func TestAccessibleFormActions(t *testing.T) {
	initFontsOnce.Do(InitFontSubsystem)
	ResetInputSession()
	GetHost().WindowSize = Vec2{500, 400}
	scope := new(int)
	checked := false
	volume := float32(40)
	clicks, builds := 0, 0
	disabled, show := false, true
	view := func() {
		builds++
		ModAttrs(NoAnimate)
		ContainerWithKey(scope, Attrs(Pad(12), Gap(8)), func() {
			if show {
				ContainerWithKey("save-slot", Attrs(), func() {
					NextAccessName("save")
					if ButtonExt("Save", ButtonAttrs{Disabled: disabled}, DefaultButtonLook()) {
						clicks++
					}
				})
			}
			NextAccessName("check")
			CheckBox(&checked, "Sound")
			NextAccessName("volume")
			Slider(&volume, SliderAttrs{Min: 0, Max: 100, Step: 10})
			if clicks > 0 {
				ContainerWithKey("result", Attrs(FixSize(100, 20)), func() { _ = GetResolvedSize(); Label("Saved") })
			}
		})
	}
	for range 3 {
		RunFrameFn(view)
	}
	save, _ := QueryContainer("save")
	check, _ := QueryContainer("check")
	slider, _ := QueryContainer("volume")
	if save.Actions != AccessPress|AccessFocus || slider.Actions != AccessFocus|AccessIncrement|AccessDecrement|AccessSetValue {
		t.Fatalf("unsupported advertised actions: button=%v slider=%v", save.Actions, slider.Actions)
	}
	send := func(id uint64, kind AccessActionKind, value float32) {
		GetFrameInput().AccessAction = AccessAction{ID: id, Kind: kind, Value: value}
		RunFrameFn(view)
	}
	beforeBuilds := builds
	send(save.ID, AccessPress, 0)
	if builds-beforeBuilds < 2 {
		t.Fatal("test requires a settling frame")
	}
	RunFrameFn(view)
	if clicks != 1 {
		t.Fatalf("press repeats across settle/idle: %d", clicks)
	}
	send(check.ID, AccessPress, 0)
	RunFrameFn(view)
	if !checked {
		t.Fatal("checkbox did not toggle exactly once")
	}
	send(slider.ID, AccessIncrement, 0)
	if volume != 50 {
		t.Fatalf("increment: %g", volume)
	}
	send(slider.ID, AccessDecrement, 0)
	if volume != 40 {
		t.Fatalf("decrement: %g", volume)
	}
	send(slider.ID, AccessSetValue, 67)
	if volume != 70 {
		t.Fatalf("set-value bypassed step: %g", volume)
	}
	send(slider.ID, AccessSetValue, 500)
	if volume != 100 {
		t.Fatalf("set-value bypassed clamp: %g", volume)
	}
	send(slider.ID, AccessFocus, 0)
	if !IdHasFocus(slider.Container) {
		t.Fatal("targeted focus missing")
	}
	disabled = true
	// The request comes from the enabled snapshot; current disabled state wins.
	send(save.ID, AccessPress, 0)
	if clicks != 1 {
		t.Fatal("disabled button activated")
	}
	show = false
	RunFrameFn(view)
	send(save.ID, AccessPress, 0)
	send(^uint64(0), AccessPress, 0)
	if clicks != 1 {
		t.Fatal("removed/unknown button activated")
	}
	send(check.ID, AccessPress|AccessFocus, 0)
	if !checked {
		t.Fatal("multi-action request accepted")
	}
}
