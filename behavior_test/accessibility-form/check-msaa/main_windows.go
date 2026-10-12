//go:build windows && (amd64 || arm64)

// check-msaa inspects a fresh accessibility-form --manual through oleacc.dll.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type variant struct {
	VT          uint16
	reserved    [3]uint16
	Data, Extra uint64
}
type message struct {
	Window  uintptr
	Message uint32
	padding uint32
	W, L    uintptr
	Time    uint32
	X, Y    int32
	Private uint32
}
type winEvent struct {
	event         uint32
	object, child int32
}

var (
	ole          = windows.NewLazySystemDLL("ole32.dll")
	auto         = windows.NewLazySystemDLL("oleaut32.dll")
	acc          = windows.NewLazySystemDLL("oleacc.dll")
	user         = windows.NewLazySystemDLL("user32.dll")
	iid          = windows.GUID{Data1: 0x618736e0, Data2: 0x3c3d, Data3: 0x11cf, Data4: [8]byte{0x81, 0x0c, 0, 0xaa, 0, 0x38, 0x9b, 0x71}}
	clearVariant = auto.NewProc("VariantClear")
	freeString   = auto.NewProc("SysFreeString")
	stringLength = auto.NewProc("SysStringLen")
	fromWindow   = acc.NewProc("AccessibleObjectFromWindow")
	children     = acc.NewProc("AccessibleChildren")
	fromEvent    = acc.NewProc("AccessibleObjectFromEvent")
	peek         = user.NewProc("PeekMessageW")
	dispatch     = user.NewProc("DispatchMessageW")
	events       []winEvent
	window       uintptr
	self         = variant{VT: 3}
)

func ptr(p *uintptr) uintptr { return uintptr(unsafe.Pointer(p)) }
func call(p uintptr, slot int, args ...uintptr) uintptr {
	if p == 0 {
		panic("nil accessible object")
	}
	table := *(*uintptr)(unsafe.Pointer(p))
	method := *(*uintptr)(unsafe.Pointer(table + uintptr(slot)*unsafe.Sizeof(p)))
	r, _, _ := syscall.SyscallN(method, append([]uintptr{p}, args...)...)
	return uintptr(uint32(r))
}
func check(stage string, hr uintptr) {
	if int32(hr) < 0 {
		panic(fmt.Sprintf("%s: HRESULT 0x%08x", stage, uint32(hr)))
	}
}
func assert(ok bool, s string) {
	if !ok {
		panic(s)
	}
}
func release(p uintptr) {
	if p != 0 {
		call(p, 2)
	}
}
func bstr(p uintptr) string {
	if p == 0 {
		return ""
	}
	n, _, _ := stringLength.Call(p)
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), int(n)))
}
func text(p uintptr, slot int) string {
	var s uintptr
	check(fmt.Sprintf("IAccessible slot %d", slot), call(p, slot, uintptr(unsafe.Pointer(&self)), ptr(&s)))
	defer freeString.Call(s)
	return bstr(s)
}
func number(p uintptr, slot int) int32 {
	var v variant
	check(fmt.Sprintf("IAccessible slot %d", slot), call(p, slot, uintptr(unsafe.Pointer(&self)), uintptr(unsafe.Pointer(&v))))
	defer clearVariant.Call(uintptr(unsafe.Pointer(&v)))
	assert(v.VT == 3, "expected integer VARIANT")
	return int32(v.Data)
}
func asAccessible(disp uintptr) uintptr {
	var p uintptr
	check("QI(IAccessible)", call(disp, 0, uintptr(unsafe.Pointer(&iid)), ptr(&p)))
	return p
}
func walk(p uintptr, visit func(uintptr), depth int) {
	assert(depth < 64, "cyclic or excessively deep accessibility tree")
	var count int32
	check("child count", call(p, 8, uintptr(unsafe.Pointer(&count))))
	assert(count >= 0 && count < 4096, "unexpected child count")
	if count == 0 {
		return
	}
	values := make([]variant, count)
	var obtained int32
	hr, _, _ := children.Call(p, 0, uintptr(count), uintptr(unsafe.Pointer(&values[0])), uintptr(unsafe.Pointer(&obtained)))
	check("AccessibleChildren", hr)
	for i := int32(0); i < obtained; i++ {
		v := &values[i]
		assert(v.VT == 9, "Shirei child is not a full accessible object")
		child := asAccessible(uintptr(v.Data))
		clearVariant.Call(uintptr(unsafe.Pointer(v)))
		visit(child)
		walk(child, visit, depth+1)
		release(child)
	}
}
func pump() {
	var msg message
	for {
		r, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 1)
		if r == 0 {
			break
		}
		dispatch.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
func waitFor(name string, fn func() bool) {
	until := time.Now().Add(5 * time.Second)
	for {
		pump()
		if fn() {
			return
		}
		if time.Now().After(until) {
			panic("timeout: " + name)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func onEvent(h, event, hwnd, object, child, thread, at uintptr) uintptr {
	if hwnd == window {
		events = append(events, winEvent{uint32(event), int32(object), int32(child)})
	}
	return 0
}
func sawEvent(start int, event uint32, name string) bool {
	for _, e := range events[start:] {
		if e.event != event {
			continue
		}
		var p uintptr
		var v variant
		hr, _, _ := fromEvent.Call(window, uintptr(uint32(e.object)), uintptr(uint32(e.child)), ptr(&p), uintptr(unsafe.Pointer(&v)))
		if int32(hr) < 0 || p == 0 {
			continue
		}
		var s uintptr
		hr = call(p, 10, uintptr(unsafe.Pointer(&v)), ptr(&s))
		actual := bstr(s)
		freeString.Call(s)
		clearVariant.Call(uintptr(unsafe.Pointer(&v)))
		release(p)
		if int32(hr) >= 0 && actual == name {
			return true
		}
	}
	return false
}
func run() {
	fmt.Println("=== Windows MSAA accessibility-form check ===")
	aware := user.NewProc("SetProcessDpiAwarenessContext")
	if aware.Find() == nil {
		aware.Call(^uintptr(3))
	} else {
		user.NewProc("SetProcessDPIAware").Call()
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 0)
	check("CoInitializeEx", hr)
	defer ole.NewProc("CoUninitialize").Call()
	title, _ := syscall.UTF16PtrFromString("Shirei accessibility form")
	window, _, _ = user.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(title)))
	assert(window != 0, "launch a fresh accessibility-form.exe --manual first")
	var root uintptr
	hr, _, _ = fromWindow.Call(window, 0xfffffffc, uintptr(unsafe.Pointer(&iid)), ptr(&root))
	check("AccessibleObjectFromWindow(OBJID_CLIENT)", hr)
	assert(root != 0, "missing client object")
	defer release(root)
	controls := map[string]uintptr{}
	walk(root, func(p uintptr) {
		name := text(p, 10)
		fmt.Printf("  role=%d name=%q\n", number(p, 13), name)
		switch name {
		case "Save preferences", "Enable sound", "Volume":
			call(p, 1)
			controls[name] = p
		}
	}, 0)
	defer func() {
		for _, p := range controls {
			release(p)
		}
	}()
	for name, role := range map[string]int32{"Save preferences": 43, "Enable sound": 44, "Volume": 51} {
		p := controls[name]
		assert(p != 0, "missing accessible: "+name)
		assert(number(p, 13) == role, "wrong role: "+name)
		var x, y, w, h int32
		check("accLocation", call(p, 22, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y)), uintptr(unsafe.Pointer(&w)), uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(&self))))
		assert(w > 0 && h > 0, "empty bounds: "+name)
		fmt.Printf("  bounds %q: %d,%d %dx%d\n", name, x, y, w, h)
		point := uintptr(uint64(uint32(x+w/2)) | uint64(uint32(y+h/2))<<32)
		var hit uintptr
		var child variant
		hr, _, _ := acc.NewProc("AccessibleObjectFromPoint").Call(point, ptr(&hit), uintptr(unsafe.Pointer(&child)))
		check("AccessibleObjectFromPoint", hr)
		var s uintptr
		check("point name", call(hit, 10, uintptr(unsafe.Pointer(&child)), ptr(&s)))
		actual := bstr(s)
		freeString.Call(s)
		clearVariant.Call(uintptr(unsafe.Pointer(&child)))
		release(hit)
		assert(actual == name, "hit test misses "+name+": "+actual)
		var parent uintptr
		check("accParent", call(p, 7, ptr(&parent)))
		assert(parent != 0, "missing parent")
		release(parent)
	}
	fmt.Println("PASS: external MSAA discovery, hierarchy, names, roles, bounds and hit testing")
	var pid uint32
	user.NewProc("GetWindowThreadProcessId").Call(window, uintptr(unsafe.Pointer(&pid)))
	hook, _, _ := user.NewProc("SetWinEventHook").Call(0x8000, 0x8017, 0, syscall.NewCallback(onEvent), uintptr(pid), 0, 0)
	assert(hook != 0, "SetWinEventHook failed")
	defer user.NewProc("UnhookWinEvent").Call(hook)
	save, sound, volume := controls["Save preferences"], controls["Enable sound"], controls["Volume"]
	fmt.Println("Focus the form window if it is not already active.")
	check("focus sound", call(sound, 21, 1, uintptr(unsafe.Pointer(&self))))
	waitFor("sound focus", func() bool { return number(sound, 14)&4 != 0 })
	start := len(events)
	check("focus Save", call(save, 21, 1, uintptr(unsafe.Pointer(&self))))
	waitFor("Save focus and event", func() bool { return number(save, 14)&4 != 0 && sawEvent(start, 0x8005, "Save preferences") })
	var focused variant
	check("accFocus", call(root, 18, uintptr(unsafe.Pointer(&focused))))
	assert(focused.VT == 9, "accFocus does not identify a child")
	fp := asAccessible(uintptr(focused.Data))
	assert(text(fp, 10) == "Save preferences", "wrong focused object")
	release(fp)
	clearVariant.Call(uintptr(unsafe.Pointer(&focused)))
	fmt.Println("PASS: native focus action, focused state/object and resolvable WinEvent")
	check("press Save", call(save, 25, uintptr(unsafe.Pointer(&self))))
	waitFor("visible Save result", func() bool {
		found := false
		walk(root, func(p uintptr) {
			if strings.HasPrefix(text(p, 10), "Saved 1 times.") {
				found = true
			}
		}, 0)
		return found
	})
	old := number(sound, 14) & 16
	start = len(events)
	check("toggle sound", call(sound, 25, uintptr(unsafe.Pointer(&self))))
	waitFor("checked state and event", func() bool { return number(sound, 14)&16 != old && sawEvent(start, 0x800a, "Enable sound") })
	target, want := "67", "70"
	if text(volume, 11) == "70" {
		target, want = "37", "40"
	}
	u, _ := syscall.UTF16PtrFromString(target)
	value, _, _ := auto.NewProc("SysAllocString").Call(uintptr(unsafe.Pointer(u)))
	start = len(events)
	check("set Volume", call(volume, 27, uintptr(unsafe.Pointer(&self)), value))
	freeString.Call(value)
	waitFor("slider value and event", func() bool { return text(volume, 11) == want && sawEvent(start, 0x800e, "Volume") })
	fmt.Println("PASS: default actions, checkbox state, slider value and WinEvents")
	fmt.Println("PASS: external MSAA form checks (speech is a separate NVDA check)")
}
func main() {
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
			return
		case <-time.After(45 * time.Second):
			fmt.Fprintln(os.Stderr, "FAIL: MSAA checker exceeded 45 seconds")
			os.Exit(1)
		}
	}()
	defer close(done)
	defer func() {
		if e := recover(); e != nil {
			fmt.Fprintln(os.Stderr, "FAIL:", e)
			os.Exit(1)
		}
	}()
	run()
}
