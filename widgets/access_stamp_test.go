package widgets

import (
	"fmt"
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

func TestStockWidgetsStampAccess(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()
	GetHost().WindowSize = Vec2{600, 400}

	on := true
	choice := 1
	buf := "hello"
	sw := false
	vol := float32(0.25)
	scope := new(int)
	view := func() {
		ContainerWithKey(scope, Attrs(Gap(8), Pad(8)), func() {
			NextAccessName("save")
			Button(NoIcon, "Save")
			NextAccessName("ts")
			CheckBox(&on, "Show timestamps")
			NextAccessName("search")
			TextInput(&buf)
			NextAccessName("opts")
			OptionGroup(&choice, func() {
				NextAccessName("opt_a")
				OptionButton("A", 1)
				NextAccessName("opt_b")
				OptionButton("B", 2)
			})
			NextAccessName("pwr")
			ToggleSwitch(&sw)
			NextAccessName("open")
			MenuItem(NoIcon, "Open")
			NextAccessName("vol")
			Slider(&vol, SliderAttrs{Min: 0, Max: 1})
			NextAccessName("load")
			ProgressBarExt(0.4, ProgressBarAttrs{})
			NextAccessName("view")
			SegmentedControl(&choice, func() {
				NextAccessName("seg_a")
				SegmentedCell("A", 1)
				NextAccessName("seg_b")
				SegmentedCell("B", 2)
			})
			NextAccessName("file")
			MenuButton(NoIcon, "File", func() {})
			NextAccessName("dir")
			DirectoryBrowse(&buf)
			NextAccessName("find")
			FileSelector(FileSelectorAttrs{Width: 200, MaxRows: 2})
		})
	}
	RunFrameFn(view)

	btn, ok := QueryContainer("save")
	if !ok || btn.Role != "button" {
		t.Fatalf("button: %+v ok=%v", btn, ok)
	}
	cb, ok := QueryContainer("ts")
	if !ok || cb.Role != "checkbox" || !cb.Checked {
		t.Fatalf("checkbox: %+v ok=%v", cb, ok)
	}
	tf, ok := QueryContainer("search")
	if !ok || tf.Role != "text" || tf.Value != "hello" {
		t.Fatalf("text: %+v ok=%v", tf, ok)
	}
	og, ok := QueryContainer("opts")
	if !ok || og.Role != "radiogroup" {
		t.Fatalf("option group: %+v ok=%v", og, ok)
	}
	ra, ok := QueryContainer("opt_a")
	if !ok || ra.Role != "radio" || !ra.Checked {
		t.Fatalf("radio a: %+v ok=%v", ra, ok)
	}
	rb, ok := QueryContainer("opt_b")
	if !ok || rb.Role != "radio" || rb.Checked {
		t.Fatalf("radio b: %+v ok=%v", rb, ok)
	}
	tg, ok := QueryContainer("pwr")
	if !ok || tg.Role != "switch" || tg.Checked {
		t.Fatalf("switch: %+v ok=%v", tg, ok)
	}
	mi, ok := QueryContainer("open")
	if !ok || mi.Role != "menuitem" {
		t.Fatalf("menuitem: %+v ok=%v", mi, ok)
	}
	sl, ok := QueryContainer("vol")
	if !ok || sl.Role != "slider" || sl.Value != "0.25" {
		t.Fatalf("slider: %+v ok=%v", sl, ok)
	}
	pb, ok := QueryContainer("load")
	if !ok || pb.Role != "progressbar" || pb.Value != "0.4" {
		t.Fatalf("progressbar: %+v ok=%v", pb, ok)
	}
	vg, ok := QueryContainer("view")
	if !ok || vg.Role != "radiogroup" {
		t.Fatalf("segmented group: %+v ok=%v", vg, ok)
	}
	sa, ok := QueryContainer("seg_a")
	if !ok || sa.Role != "radio" || !sa.Checked {
		t.Fatalf("seg a: %+v ok=%v", sa, ok)
	}
	mb, ok := QueryContainer("file")
	if !ok || mb.Role != "menu" {
		t.Fatalf("menu: %+v ok=%v", mb, ok)
	}
	dir, ok := QueryContainer("dir")
	if !ok || dir.Role != "group" {
		t.Fatalf("directory browse: %+v ok=%v", dir, ok)
	}
	fs, ok := QueryContainer("find")
	if !ok || fs.Role != "group" {
		t.Fatalf("file selector: %+v ok=%v", fs, ok)
	}
}

func TestInputCommandClickCheckbox(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()
	GetHost().WindowSize = Vec2{600, 400}

	on := false
	scope := new(int)
	view := func() {
		ContainerWithKey(scope, Attrs(Pad(8)), func() {
			NextAccessName("ts")
			CheckBox(&on, "Show")
		})
	}
	RunFrameFn(view)
	n, ok := QueryContainer("ts")
	if !ok {
		t.Fatal("no ts")
	}
	cx := n.Rect.Origin[0] + n.Rect.Size[0]/2
	cy := n.Rect.Origin[1] + n.Rect.Size[1]/2
	if HandleInputCommand(fmt.Sprintf("move %g %g", cx, cy)) != "" {
		t.Fatal("move")
	}
	RunFrameFn(view)
	if HandleInputCommand("mouse_down") != "" {
		t.Fatal("mouse_down")
	}
	RunFrameFn(view)
	if HandleInputCommand("mouse_up") != "" {
		t.Fatal("mouse_up")
	}
	RunFrameFn(view)
	if !on {
		t.Fatal("checkbox did not toggle")
	}
}
