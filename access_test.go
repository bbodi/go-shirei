package shirei

import "testing"

func accessFrame(fn FrameFn) {
	ui.Host.WindowSize = Vec2{400, 300}
	RunFrameFn(fn)
}

func TestAccessQueryByNamePath(t *testing.T) {
	ResetInputSession()
	view := func() {
		NextAccessName("top_bar")
		Container(Attrs(Row, FixSize(400, 40)), func() {
			AssignAccess()
			NextAccessName("files_browser")
			Container(Attrs(FixSize(80, 30)), func() {
				AssignAccess()
			})
		})
		Container(Attrs(FixSize(200, 20)), func() {
			NextAccessName("files_browser")
			Container(Attrs(FixSize(80, 20)), func() {
				AssignAccess()
			})
		})
	}
	accessFrame(view)

	n, ok := QueryContainer("top_bar files_browser")
	if !ok {
		t.Fatal("expected top_bar files_browser")
	}
	if n.Path != "top_bar files_browser" {
		t.Fatalf("Path=%q", n.Path)
	}
	if n.Rect.Size[0] == 0 || n.Rect.Size[1] == 0 {
		t.Fatalf("empty rect: %+v", n.Rect)
	}

	all := QueryContainers("files_browser")
	if len(all) != 2 {
		t.Fatalf("files_browser hits=%d want 2", len(all))
	}
	if all[1].Path != "files_browser" {
		t.Fatalf("second path=%q (unnamed wrapper should drop)", all[1].Path)
	}

	if _, ok := QueryContainer("nope"); ok {
		t.Fatal("unexpected match")
	}
}

func TestAccessUnnamedPathUsesDash(t *testing.T) {
	ResetInputSession()
	accessFrame(func() {
		NextAccessName("top_bar")
		Container(Attrs(Row, FixSize(400, 40)), func() {
			AssignAccess()
			Container(Attrs(FixSize(80, 30)), func() {
				NextAccessRole("button")
				AssignAccess()
			})
		})
		Container(Attrs(FixSize(10, 10)), func() {
			NextAccessRole("button")
			AssignAccess()
		})
	})
	var child, stray string
	for _, n := range ui.access {
		switch n.Path {
		case "top_bar -":
			child = n.Path
		case "-":
			stray = n.Path
		}
	}
	if child != "top_bar -" {
		t.Fatal("unnamed child under top_bar: want Path \"top_bar -\"")
	}
	if stray != "-" {
		t.Fatal("unnamed node with no named ancestor: want Path \"-\"")
	}
	hits := QueryContainers("top_bar")
	if len(hits) != 1 || hits[0].Path != "top_bar" {
		t.Fatalf("query top_bar = %v (unnamed child must not match)", hits)
	}
}

func TestAccessTreeRebuiltEachFrame(t *testing.T) {
	ResetInputSession()
	show := true
	view := func() {
		if show {
			NextAccessName("only")
			Container(Attrs(FixSize(10, 10)), func() { AssignAccess() })
		}
	}
	accessFrame(view)
	if _, ok := QueryContainer("only"); !ok {
		t.Fatal("expected only on first frame")
	}
	show = false
	accessFrame(view)
	if _, ok := QueryContainer("only"); ok {
		t.Fatal("stale access node survived a frame where it was not assigned")
	}
}

func TestAccessLeftoverDoesNotStealNextFrame(t *testing.T) {
	ResetInputSession()
	accessFrame(func() { NextAccessName("orphan") })
	accessFrame(func() {
		Container(Attrs(FixSize(10, 10)), func() {
			NextAccessRole("button")
			AssignAccess()
		})
	})
	if _, ok := QueryContainer("orphan"); ok {
		t.Fatal("leftover name applied on the next frame")
	}
}

func TestAccessQueryFocused(t *testing.T) {
	ResetInputSession()
	var id ContainerId
	accessFrame(func() {
		id = Container(Attrs(Focusable, FixSize(20, 20)), func() {
			NextAccessName("field")
			AssignAccess()
		})
		FocusImmediateOn(id)
	})
	n, ok := QueryContainer("field")
	if !ok || n.Name != "field" {
		t.Fatalf("field = %+v ok=%v", n, ok)
	}
	ids := FocusIDs()
	if len(ids) == 0 || ids[0] != n.ID {
		t.Fatalf("FocusIDs = %v want leaf %d", ids, n.ID)
	}
	if !n.Focused {
		t.Fatal("Focused flag unset")
	}
}

func TestAccessQueryHovered(t *testing.T) {
	ResetInputSession()
	view := func() {
		NextAccessName("hit")
		Container(Attrs(FixSize(40, 40)), func() { AssignAccess() })
	}
	accessFrame(view)
	n, ok := QueryContainer("hit")
	if !ok {
		t.Fatal("expected hit")
	}
	ui.Host.Input.MousePoint = Vec2{
		n.Rect.Origin[0] + n.Rect.Size[0]/2,
		n.Rect.Origin[1] + n.Rect.Size[1]/2,
	}
	accessFrame(view)
	h, ok := QueryContainer("hit")
	if !ok || h.Name != "hit" {
		t.Fatalf("hit = %+v ok=%v", h, ok)
	}
	ids := HoverIDs()
	if len(ids) == 0 || ids[0] != h.ID {
		t.Fatalf("HoverIDs = %v want leaf %d", ids, h.ID)
	}
	if !h.Hovered {
		t.Fatal("Hovered flag unset")
	}
}
