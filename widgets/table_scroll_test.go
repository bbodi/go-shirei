package widgets

import (
	"testing"

	"go.hasen.dev/shirei"

	. "go.hasen.dev/shirei"
)

func TestTableScrollOffsetPerOwner(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()

	scope := new(int)
	rows := make([]int, 80)
	for i := range rows {
		rows[i] = i
	}
	cols := []TableColumn[int]{
		{Label: "N", Cell: func(n int) { Label("x") }},
	}

	var a, b f32
	a = 400
	owner := &a

	frame := func() {
		shirei.GetHost().WindowSize = Vec2{400, 240}
		RunFrameFn(func() {
			ModAttrs(func(attrs *AttrSet) { attrs.Animations = 0 })
			ContainerWithKey(scope, Attrs(Viewport, FixSize(400, 240)), func() {
				TableExt(nil, TableAttrs[int]{
					RowHeight:    20,
					ScrollOffset: owner,
				}, cols, rows, func(n int) any { return n })
			})
		})
	}

	for range 8 {
		frame()
	}
	if Absf32(a-400) > 2 {
		t.Fatalf("owner A restore: scroll=%.1f want 400", a)
	}

	owner = &b
	for range 8 {
		frame()
	}
	if Absf32(b) > 2 {
		t.Fatalf("owner B should start at 0, got %.1f", b)
	}
	if Absf32(a-400) > 2 {
		t.Fatalf("owner A slot changed while B was showing: %.1f", a)
	}

	owner = &a
	for range 8 {
		frame()
	}
	if Absf32(a-400) > 2 {
		t.Fatalf("back to A: scroll=%.1f want 400", a)
	}
}
