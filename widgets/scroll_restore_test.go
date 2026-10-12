package widgets

// Restoring a scroll position onto a freshly (re)built VirtualListView — the
// haystack tab-switch flow — uses ScrollToIndex so the list owns height walks.
// The restore must stick on the first presented frames (width-unknown settle).

import (
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

func TestVirtualListRestoreFirstFrame(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()

	scope := new(int)
	listKey := new(int)
	const itemCount = 60
	const itemHeight = 20 // content 1200 in a 200-tall viewport: max scroll 1000
	const restoreIndex = 25

	showList := false
	var firstVis int
	rendered := map[int]bool{}

	frame := func() FrameOutputData {
		rendered = map[int]bool{}
		shirei.GetHost().WindowSize = Vec2{400, 200}
		return RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.Animations = 0 })
			ContainerWithKey(scope, Attrs(Viewport), func() {
				if showList {
					VirtualListViewExt(listKey, VirtualListAttrs{
						ItemCount:  itemCount,
						ItemKey:    func(i int) any { return i },
						ItemHeight: func(i int, w f32) f32 { return itemHeight },
						ItemView: func(i int, w f32) {
							rendered[i] = true
						},
						OutFirstVisible: &firstVis,
					})
				}
			})
		})
	}

	frame() // the list's tab is not active; nothing to restore onto yet

	// activate the tab: post the restore and build the list, as activateTab does
	WithFrameLock(func() {
		VirtualListView_ScrollToIndex(listKey, restoreIndex)
	})
	showList = true

	out := frame()
	if !out.NextFrameRequested {
		// May settle immediately when width is known; not required.
	}

	// Settle height/width
	for range 6 {
		frame()
	}
	if !rendered[restoreIndex] {
		t.Fatalf("after ScrollToIndex(%d) row not visible: rendered=%v firstVis=%d",
			restoreIndex, rendered, firstVis)
	}
	if firstVis != restoreIndex {
		// Clamped if tail is short — with 60 items of 20px in 200px view,
		// index 25 should pin exactly.
		t.Fatalf("firstVis=%d want %d", firstVis, restoreIndex)
	}
}

func TestVirtualListScrollTo(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()

	scope := new(int)
	listKey := new(int)
	const itemCount = 60
	const itemHeight f32 = 20
	const restoreY f32 = 25 * itemHeight // same as index 25 at the top
	var scrollY f32
	var firstVis int
	rendered := map[int]bool{}

	frame := func() {
		rendered = map[int]bool{}
		shirei.GetHost().WindowSize = Vec2{400, 200}
		RunFrameFn(func() {
			ModAttrs(func(a *AttrSet) { a.Animations = 0 })
			ContainerWithKey(scope, Attrs(Viewport), func() {
				VirtualListViewExt(listKey, VirtualListAttrs{
					ItemCount: itemCount,
					ItemKey:   func(i int) any { return i },
					ItemHeight: func(i int, w f32) f32 {
						return itemHeight
					},
					ItemView: func(i int, w f32) {
						rendered[i] = true
					},
					OutScrollOffset: &scrollY,
					OutFirstVisible: &firstVis,
				})
			})
		})
	}

	frame()
	WithFrameLock(func() {
		VirtualListView_ScrollTo(listKey, restoreY)
	})
	for range 6 {
		frame()
	}
	if firstVis != 25 {
		t.Fatalf("ScrollTo(%.0f): firstVis=%d want 25 (scrollY=%.1f rendered=%v)",
			restoreY, firstVis, scrollY, rendered)
	}
	if Absf32(scrollY-restoreY) > 1 {
		t.Fatalf("ScrollTo(%.0f): scrollY=%.1f", restoreY, scrollY)
	}
}
