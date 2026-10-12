package shirei

import "testing"

func TestDimensionQueriesSettleOnlyForReadAxis(t *testing.T) {
	for _, tc := range []struct {
		name    string
		axis    int
		content bool
		read    func() float32
	}{
		{"outer-width", 0, false, GetResolvedWidth},
		{"outer-height", 1, false, GetResolvedHeight},
		{"content-width", 0, true, GetContentWidth},
		{"content-height", 1, true, GetContentHeight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := ui
			bindUI(NewUI())
			defer bindUI(previous)
			ui.Host.WindowSize = Vec2{400, 300}
			padding := Vec4{10, 20, 30, 40}
			var child ContainerId
			var value float32
			frame := func() {
				ModAttrs(NoAnimate)
				Container(Attrs(Viewport, func(a *AttrSet) { a.Padding = padding }), func() {
					value = tc.read()
					size := Vec2{12, 12}
					size[tc.axis] = value / 2
					child = Element(Attrs(Float(0, 0), FixSizeVec(size), Background(210, 80, 40, 1)))
				})
			}
			check := func(label string, passes int64) {
				t.Helper()
				before := ui.FrameNumber
				out := RunFrameFn(frame)
				if got := ui.FrameNumber - before; got != passes {
					t.Fatalf("%s: passes=%d want %d", label, got, passes)
				}
				want := ui.Host.WindowSize[tc.axis]
				if tc.content {
					want -= PadSize(padding)[tc.axis]
				}
				if value != want {
					t.Fatalf("%s: query=%v want %v", label, value, want)
				}
				if size := GetResolvedRectOf(child).Size[tc.axis]; size != want/2 {
					t.Fatalf("%s: child size=%v want %v", label, size, want/2)
				}
				found := false
				for _, s := range out.Surfaces {
					if s.Color1 == (Vec4{210, 80, 40, 1}) && s.Rect.Size[tc.axis] == want/2 {
						found = true
					}
				}
				if !found {
					t.Fatalf("%s: final output lacks correctly sized child", label)
				}
			}
			check("fresh query", 2)
			check("steady", 1)
			ui.Host.WindowSize[1-tc.axis] += 30
			check("other-axis resize", 1)
			ui.Host.WindowSize[tc.axis] += 40
			check("read-axis resize", 2)
			// Vertical padding affects only content height; horizontal only width.
			samePad, otherPad := PAD_LEFT, PAD_TOP
			if tc.axis == 1 {
				samePad, otherPad = otherPad, samePad
			}
			padding[otherPad] += 9
			check("other-axis padding", 1)
			padding[samePad] += 7
			passes := int64(1)
			if tc.content {
				passes = 2
			}
			check("read-axis padding", passes)
			if tc.content {
				ui.Host.WindowSize[tc.axis] += 11
				padding[samePad] += 11
				check("unchanged content dimension", 1)
			}
		})
	}
}

func TestDimensionQueriesAccumulateAndReset(t *testing.T) {
	previous := ui
	bindUI(NewUI())
	defer bindUI(previous)
	ui.Host.WindowSize = Vec2{400, 300}
	mode := "width"
	var parent, child ContainerId
	var size Vec2
	frame := func() {
		ModAttrs(NoAnimate)
		parent = Container(Attrs(Viewport), func() {
			size = Vec2{20, 20}
			switch mode {
			case "width":
				size[0] = GetResolvedWidth() / 2
			case "height":
				size[1] = GetResolvedHeight() / 2
			case "both":
				size = Vec2{GetResolvedWidth() / 2, GetResolvedHeight() / 2}
			case "reverse":
				h := GetResolvedHeight()
				size = Vec2{GetResolvedWidth() / 2, h / 2}
			case "outer-and-content":
				size = Vec2{GetResolvedWidth() / 2, GetContentHeight() / 2}
			}
			child = Element(Attrs(FixSizeVec(size)))
		})
	}
	check := func(passes int64) {
		t.Helper()
		before := ui.FrameNumber
		RunFrameFn(frame)
		if got := ui.FrameNumber - before; got != passes {
			t.Fatalf("mode %s: passes=%d want %d", mode, got, passes)
		}
		if got := GetResolvedRectOf(child).Size; got != size {
			t.Fatalf("child=%v want %v", got, size)
		}
	}
	check(2)
	mode = "height"
	ui.Host.WindowSize[0] += 20
	check(1) // The previous width query cannot leak into this pass.
	mode = "both"
	ui.Host.WindowSize[0] += 20
	check(2) // The later height query cannot overwrite the width query.
	mode = "reverse"
	ui.Host.WindowSize[1] += 20
	check(2)
	mode = "outer-and-content"
	ui.Host.WindowSize[0] += 20
	check(2)
	mode = "width"
	ui.Host.WindowSize[1] += 20
	_ = GetRenderDataOf(parent) // Between-frame inspection does not register a dependency.
	check(1)
}

func TestVectorQueriesTrackBothDimensions(t *testing.T) {
	for _, tc := range []struct {
		name string
		read func() Vec2
	}{
		{"resolved", GetResolvedSize},
		{"available", GetAvailableSize},
		{"content-rect", func() Vec2 { return GetContentRect().Size }},
		{"screen-rect", func() Vec2 { return GetScreenRect().Size }},
		{"render-data", func() Vec2 { return GetRenderData().ResolvedSize }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			previous := ui
			bindUI(NewUI())
			defer bindUI(previous)
			ui.Host.WindowSize = Vec2{200, 100}
			var size Vec2
			frame := func() {
				ModAttrs(NoAnimate)
				Container(Attrs(Viewport), func() { size = tc.read(); Element(Attrs(FixSize(size[0]/2, size[1]/2))) })
			}
			RunFrameFn(frame)
			for axis := range 2 {
				ui.Host.WindowSize[axis] += 20
				before := ui.FrameNumber
				RunFrameFn(frame)
				if ui.FrameNumber-before != 2 || size != ui.Host.WindowSize {
					t.Fatalf("axis %d: passes=%d size=%v", axis, ui.FrameNumber-before, size)
				}
			}
		})
	}
}

func TestContentDimensionQueriesIgnoreAnimationProgress(t *testing.T) {
	previous := ui
	bindUI(NewUI())
	defer bindUI(previous)
	ui.Host.WindowSize = Vec2{400, 300}
	ui.pinnedTimeDelta = true
	ui.timeDelta = 0.01
	width, pad := float32(100), float32(10)
	var id ContainerId
	frame := func() {
		id = Container(Attrs(AnimateOnly(AnimSize|AnimPad), FixSize(width, 100), Pad(pad)), func() {
			Element(Attrs(FixSize(GetContentWidth()/2, 10)))
		})
	}
	RunFrameFn(frame)
	width, pad = 200, 20
	before := ui.FrameNumber
	RunFrameFn(frame)
	if ui.FrameNumber-before != 2 {
		t.Fatal("changed content target did not settle")
	}
	for range 4 {
		before = ui.FrameNumber
		RunFrameFn(frame)
		if ui.FrameNumber-before != 1 {
			t.Fatal("animation progress requested another settle")
		}
		w := GetContentRectOf(id).Size[0]
		if w <= 80 || w >= 160 {
			t.Fatalf("content width %v is not between animation endpoints", w)
		}
	}
}
