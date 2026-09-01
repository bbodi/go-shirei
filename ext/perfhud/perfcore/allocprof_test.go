package perfcore

import (
	"bytes"
	"image"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// localFramePC returns a program counter inside this function, to stand in
// for app code in a fabricated stack.
func localFramePC() uintptr {
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	return pcs[0]
}

// entryPC is a program counter just inside fn, so a frame lookup lands in it.
func entryPC(fn any) uintptr { return reflect.ValueOf(fn).Pointer() + 1 }

// The bug this guards: image.NewRGBA is the leaf of every RGBA allocation in
// the process, so naming a row after it folds all of them into one useless
// line. The search has to walk past it to the caller that asked for the
// memory.
func TestAllocSiteWalksPastTheAllocatingHelper(t *testing.T) {
	for _, leaf := range []any{image.NewRGBA, strings.Repeat, bytes.Repeat} {
		var st [stackDepth]uintptr
		st[0] = entryPC(leaf)
		st[1] = localFramePC()

		name := allocSiteName(st)
		if !strings.Contains(name, "localFramePC") {
			t.Errorf("allocSiteName = %q, want the caller (localFramePC)", name)
		}
	}
}

func TestInternalFramesCoverTheAllocatingHelpers(t *testing.T) {
	// These packages allocate on someone else's behalf; naming a row after
	// them folds every caller in the process into one line.
	for _, name := range []string{
		"runtime.mallocgc", "image.NewRGBA", "image/draw.Draw",
		"bytes.growSlice", "strings.(*Builder).grow", "sync.(*Pool).Get",
		"sort.Slice", "reflect.New", "internal/poll.Read",
	} {
		if !isInternalFrame(name) {
			t.Errorf("%q should be treated as internal", name)
		}
	}
	for _, name := range []string{
		"main.buildDisplay", "go.hasen.dev/shirei.ShapeTextMax",
		"imaging.Resize", // a third-party package that merely starts with "image"
	} {
		if isInternalFrame(name) {
			t.Errorf("%q should not be treated as internal", name)
		}
	}
}

// An entry keeps the stacks folded into it, heaviest first, so the detail
// view can show where the bytes came from.
func TestDiffKeepsTheStacksBehindAnEntry(t *testing.T) {
	var a, b [stackDepth]uintptr
	a[0], b[0] = 0x1000, 0x2000

	prev := allocSnap{}
	cur := allocSnap{
		a: {allocBytes: 4096, allocObjects: 4, freeBytes: 1024, freeObjects: 1},
		b: {allocBytes: 8192, allocObjects: 8},
	}
	got := diffTopAllocs(prev, cur, 5)
	if len(got) == 0 {
		t.Fatal("no entries")
	}
	// Both stacks resolve to no symbol, so they fold into one entry.
	e := got[0]
	if len(e.Stacks) != 2 {
		t.Fatalf("entry keeps %d stacks, want 2", len(e.Stacks))
	}
	if e.Stacks[0].KB <= e.Stacks[1].KB {
		t.Error("stacks are not sorted heaviest first")
	}
	if e.Stacks[1].LiveKB != 3 {
		t.Errorf("live = %v KB, want 3 (alloc 4096 − free 1024)", e.Stacks[1].LiveKB)
	}
	if e.Stacks[1].Objects != 4 {
		t.Errorf("objects = %d, want 4", e.Stacks[1].Objects)
	}
}

// The report is what lands on the clipboard in a bug report, so it has to
// carry the numbers and the source positions on its own.
func TestReportCarriesTheDetails(t *testing.T) {
	var pcs [stackDepth]uintptr
	runtime.Callers(1, pcs[:])

	e := AllocEntry{
		Name: "pkg.Hot",
		KB:   2048,
		Stacks: []AllocStack{{
			KB: 2048, Objects: 12, LiveKB: 1024, LiveObjects: 6, PCs: pcs,
		}},
	}
	rep := e.Report()
	for _, want := range []string{
		"pkg.Hot", "2.0 MB", "MemProfileRate",
		"--- stack 1/1 ---", "live:", "allocprof_test.go",
	} {
		if !strings.Contains(rep, want) {
			t.Errorf("report is missing %q:\n%s", want, rep)
		}
	}
}

func TestAllocatorTableRowsAreClickable(t *testing.T) {
	saved := topAllocs
	defer func() { topAllocs = saved }()

	tbl := AllocatorTable()
	if tbl.Detail == nil {
		t.Fatal("the allocator table has no Detail: its rows cannot be clicked")
	}
	topAllocs = []AllocEntry{{Name: "pkg.Hot", KB: 10}}
	if !strings.Contains(tbl.Detail(0), "pkg.Hot") {
		t.Error("Detail(0) does not describe the first row")
	}
	// A scan can replace the list between the click and the lookup.
	if tbl.Detail(9) != "" {
		t.Error("Detail out of range should return empty, not panic or lie")
	}
}
