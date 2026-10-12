package shirei

import (
	"bytes"
	"testing"
)

func TestSettlePassKeepsHitOrderAndClipping(t *testing.T) {
	for _, clip := range []bool{false, true} {
		name := "overflow"
		if clip {
			name = "clipped"
		}
		t.Run(name, func(t *testing.T) {
			previous := ui
			bindUI(NewUI())
			defer bindUI(previous)
			ui.Host.WindowSize = Vec2{200, 100}
			ui.Host.WindowScale = 1
			ui.Host.Input.MousePoint = Vec2{30, 30}
			ui.Host.Input.Touches[0] = TouchInfo{Active: true, Id: 7, Pos: Vec2{30, 30}}
			var frontHover, frontTouch, backHover, backTouch bool
			frame := func() {
				ModAttrs(NoAnimate)
				Container(Attrs(Viewport), func() {
					_ = GetResolvedSize()
					// The parent's box is offscreen. Only its unclipped child can reenter.
					Container(AttrSet{Floats: true, Float: Vec2{250, 0}, MinSize: Vec2{20, 20}, MaxSize: Vec2{20, 20}, Clip: clip, Z: 10}, func() {
						Container(Attrs(Float(-240, 10), FixSize(60, 60), BorderWidth(1), BorderColor(0, 0, 0, 1)), func() {
							frontHover, frontTouch = IsHoveredDirectly(), IsTouchedDirectly()
							if frontHover {
								ModAttrs(Background(120, 80, 40, 1))
							} else {
								ModAttrs(Background(0, 80, 40, 1))
							}
						})
					})
					// Source order differs from paint order.
					Container(Attrs(Float(10, 10), FixSize(60, 60)), func() {
						backHover, backTouch = IsHoveredDirectly(), IsTouchedDirectly()
						if backHover {
							ModAttrs(Background(210, 80, 40, 1))
						} else {
							ModAttrs(Background(0, 0, 50, 1))
						}
					})
					// A topmost transparent hit layer must not steal hover or touch.
					Element(AttrSet{Floats: true, Float: Vec2{10, 10}, MinSize: Vec2{60, 60}, MaxSize: Vec2{60, 60}, Z: 20, ClickThrough: true})
				})
			}
			before := ui.FrameNumber
			out := RunFrameFn(frame)
			if ui.FrameNumber-before != 2 {
				t.Fatal("fixture did not settle")
			}
			if frontHover != !clip || frontTouch != !clip || backHover != clip || backTouch != clip {
				t.Fatalf("clip=%v: front hover/touch=%v/%v back=%v/%v", clip, frontHover, frontTouch, backHover, backTouch)
			}
			hash := out.SurfacesHash
			var first SoftRenderer
			pixels := append([]byte(nil), first.Render(out.Surfaces, out.GlyphRuns, 200, 100, 1).Pix...)
			before = ui.FrameNumber
			out = RunFrameFn(frame)
			if ui.FrameNumber-before != 1 {
				t.Fatal("steady frame did not converge")
			}
			var next SoftRenderer
			if hash != out.SurfacesHash || !bytes.Equal(pixels, next.Render(out.Surfaces, out.GlyphRuns, 200, 100, 1).Pix) {
				t.Fatal("settled output differs from the subsequent steady frame")
			}
		})
	}
}

func TestSettlePassPublishesOnlyFinalPaint(t *testing.T) {
	previous := ui
	bindUI(NewUI())
	defer bindUI(previous)
	ui.Host.WindowSize = Vec2{300, 100}
	ui.Host.WindowScale = 1
	seed := RunFrameFn(func() { Element(Attrs(FixSize(10, 10), Background(300, 80, 50, 1))) })
	previousHash := seed.SurfacesHash
	// Isolated keys let the test observe shadow generation without altering caches.
	discarded := ShadowMapKey{w: 63, h: 27, r: 11, a: 117}
	final := ShadowMapKey{w: 173, h: 27, r: 11, a: 117}
	if res.imageKeys[discarded] != 0 || res.imageKeys[final] != 0 {
		t.Fatal("shadow fixture keys already registered")
	}
	defer func() {
		for _, key := range []ShadowMapKey{discarded, final} {
			if id := res.imageKeys[key]; id != 0 {
				freeImage(id)
			}
		}
	}()
	passes := 0
	out := RunFrameFn(func() {
		passes++
		if got := computeSurfacesHash(LastFrameSurfaces(), nil); got != previousHash {
			t.Fatal("an incomplete pass replaced the last produced output")
		}
		w := float32(63)
		if passes == 1 {
			RequestStabilize()
		} else {
			w = 173
		}
		Element(AttrSet{MinSize: Vec2{w, 27}, MaxSize: Vec2{w, 27}, Background: Vec4{210, 80, 40, 1}, Shadow: Shadow{Blur: 1.1, Alpha: 117.5 / 255}})
	})
	if passes != 2 {
		t.Fatalf("passes=%d", passes)
	}
	if res.imageKeys[discarded] != 0 {
		t.Fatal("generated a shadow for discarded paint")
	}
	if res.imageKeys[final] == 0 {
		t.Fatal("final shadow was not generated")
	}
	if len(out.Surfaces) == 0 || computeSurfacesHash(LastFrameSurfaces(), nil) != out.SurfacesHash {
		t.Fatal("final surfaces were not published")
	}
	// Even an unresolved dependency at the pass limit must publish final output.
	passes = 0
	out = RunFrameFn(func() { passes++; RequestStabilize(); Element(Attrs(FixSize(30, 30), Background(0, 0, 20, 1))) })
	if passes != 2 || out.SurfacesHash == previousHash || len(out.Surfaces) == 0 {
		t.Fatal("pass limit discarded the final output")
	}
}
