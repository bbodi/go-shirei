//go:build windows && (amd64 || arm64)

// check-uia inspects a running accessibility-form through Windows UI Automation.
// Run the form with --manual before starting this independent client.
package main

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/windows"
)

type variant struct {
	VT          uint16
	reserved    [3]uint16
	Data, Extra uint64
}

var (
	ole             = windows.NewLazySystemDLL("ole32.dll")
	auto            = windows.NewLazySystemDLL("oleaut32.dll")
	user            = windows.NewLazySystemDLL("user32.dll")
	clearVariant    = auto.NewProc("VariantClear")
	stringLength    = auto.NewProc("SysStringLen")
	arrayElement    = auto.NewProc("SafeArrayGetElement")
	arrayDestroy    = auto.NewProc("SafeArrayDestroy")
	uia, walker     uintptr
	eventMu         sync.Mutex
	focusEvents     int
	propertyEvents  = map[int32]int{}
	unknownGUID     = guid("00000000-0000-0000-c000-000000000046")
	focusGUID       = guid("c270f6b5-5c69-4290-9745-7a7f97169468")
	propertyGUID    = guid("40cd37d4-c756-4b0c-8c6f-bddfeeb13b50")
	focusHandler    handler
	propertyHandler handler
)

type handler struct {
	table *[4]uintptr
	iid   windows.GUID
}

func guid(s string) windows.GUID {
	g, e := windows.GUIDFromString("{" + s + "}")
	if e != nil {
		panic(e)
	}
	return g
}
func ptr(p *uintptr) uintptr { return uintptr(unsafe.Pointer(p)) }
func call(p uintptr, slot int, args ...uintptr) uintptr {
	if p == 0 {
		panic("nil COM object")
	}
	table := *(*uintptr)(unsafe.Pointer(p))
	method := *(*uintptr)(unsafe.Pointer(table + uintptr(slot)*unsafe.Sizeof(p)))
	hr, _, _ := syscall.SyscallN(method, append([]uintptr{p}, args...)...)
	return uintptr(uint32(hr))
}
func check(stage string, hr uintptr) {
	if int32(hr) < 0 {
		panic(fmt.Sprintf("%s: HRESULT 0x%08x", stage, uint32(hr)))
	}
}
func release(p uintptr) {
	if p != 0 {
		call(p, 2)
	}
}
func property(p uintptr, id int32) variant {
	var v variant
	check(fmt.Sprintf("property %d", id), call(p, 10, uintptr(id), uintptr(unsafe.Pointer(&v))))
	return v
}
func text(p uintptr, id int32) string {
	v := property(p, id)
	defer clearVariant.Call(uintptr(unsafe.Pointer(&v)))
	if v.VT != 8 || v.Data == 0 {
		return ""
	}
	n, _, _ := stringLength.Call(uintptr(v.Data))
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(uintptr(v.Data))), int(n)))
}
func number(p uintptr, id int32) float64 {
	v := property(p, id)
	defer clearVariant.Call(uintptr(unsafe.Pointer(&v)))
	switch v.VT {
	case 3:
		return float64(int32(v.Data))
	case 5:
		return math.Float64frombits(v.Data)
	case 11:
		if uint16(v.Data) != 0 {
			return 1
		}
		return 0
	}
	panic(fmt.Sprintf("property %d has unexpected VARIANT type %d", id, v.VT))
}
func assert(ok bool, msg string) {
	if !ok {
		panic(msg)
	}
}
func waitFor(label string, fn func() bool) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if fn() {
			return
		}
		if time.Now().After(deadline) {
			panic("timeout: " + label)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
func walk(root uintptr, visit func(uintptr)) {
	var child uintptr
	check("first child", call(walker, 4, root, ptr(&child)))
	for child != 0 {
		visit(child)
		walk(child, visit)
		var next uintptr
		check("next sibling", call(walker, 6, child, ptr(&next)))
		release(child)
		child = next
	}
}
func getPattern(p uintptr, id int32, iid string) uintptr {
	g := guid(iid)
	var result uintptr
	check(fmt.Sprintf("pattern %d", id), call(p, 14, uintptr(id), uintptr(unsafe.Pointer(&g)), ptr(&result)))
	assert(result != 0, fmt.Sprintf("missing pattern %d", id))
	return result
}
func eventQuery(self, iid, out uintptr) uintptr {
	*(*uintptr)(unsafe.Pointer(out)) = 0
	h := (*handler)(unsafe.Pointer(self))
	wanted := *(*windows.GUID)(unsafe.Pointer(iid))
	if wanted != unknownGUID && wanted != h.iid {
		return 0x80004002
	}
	*(*uintptr)(unsafe.Pointer(out)) = self
	return 0
}
func eventRef(uintptr) uintptr             { return 1 } // Static handlers live for the checker process.
func onFocus(self, sender uintptr) uintptr { eventMu.Lock(); focusEvents++; eventMu.Unlock(); return 0 }
func onProperty(self, sender, id, value uintptr) uintptr {
	eventMu.Lock()
	propertyEvents[int32(id)]++
	eventMu.Unlock()
	return 0
}
func eventCount(id int32) int {
	eventMu.Lock()
	defer eventMu.Unlock()
	if id == 0 {
		return focusEvents
	}
	return propertyEvents[id]
}

func run() {
	fmt.Println("=== Windows UIA accessibility-form check ===")
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 0)
	check("CoInitializeEx(MTA)", hr)
	defer ole.NewProc("CoUninitialize").Call()
	cls := guid("ff48dba4-60ef-4201-aa87-54103eef594e")
	iid := guid("30cbe57d-d9d0-452a-ab13-7ac5ac4825ee")
	hr, _, _ = ole.NewProc("CoCreateInstance").Call(uintptr(unsafe.Pointer(&cls)), 0, 1, uintptr(unsafe.Pointer(&iid)), ptr(&uia))
	check("CoCreateInstance(CUIAutomation)", hr)
	defer release(uia)
	title, _ := syscall.UTF16PtrFromString("Shirei accessibility form")
	hwnd, _, _ := user.NewProc("FindWindowW").Call(0, uintptr(unsafe.Pointer(title)))
	assert(hwnd != 0, "form window not found; launch accessibility-form.exe --manual first")
	var root uintptr
	check("ElementFromHandle", call(uia, 6, hwnd, ptr(&root)))
	assert(root != 0, "no window UIA element")
	defer release(root)
	check("RawViewWalker", call(uia, 16, ptr(&walker)))
	assert(walker != 0, "no raw walker")
	defer release(walker)
	controls := map[string]uintptr{}
	walk(root, func(p uintptr) {
		name := text(p, 30005)
		framework := text(p, 30024)
		fmt.Printf("  role=%.0f name=%q framework=%q\n", number(p, 30003), name, framework)
		if framework == "Shirei" {
			switch name {
			case "Save preferences", "Enable sound", "Volume":
				call(p, 1)
				controls[name] = p
			}
		}
	})
	defer func() {
		for _, p := range controls {
			release(p)
		}
	}()
	for name, role := range map[string]float64{"Save preferences": 50000, "Enable sound": 50002, "Volume": 50015} {
		p := controls[name]
		assert(p != 0, "Shirei provider missing: "+name)
		assert(number(p, 30003) == role, "wrong control type: "+name)
		v := property(p, 30001)
		assert(v.VT == 0x2005, "bounds are not a double array: "+name)
		var bounds [4]float64
		for i := range bounds {
			index := int32(i)
			hr, _, _ := arrayElement.Call(uintptr(v.Data), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&bounds[i])))
			check("bounds array", hr)
		}
		clearVariant.Call(uintptr(unsafe.Pointer(&v)))
		assert(bounds[2] > 0 && bounds[3] > 0, "empty bounds: "+name)
		// ElementFromPoint takes a POINT by value. On both 64-bit Windows ABIs
		// its two LONGs occupy one integer argument.
		x, y := int32(bounds[0]+bounds[2]/2), int32(bounds[1]+bounds[3]/2)
		packed := uintptr(uint64(uint32(x)) | uint64(uint32(y))<<32)
		var hit uintptr
		check("ElementFromPoint", call(uia, 7, packed, ptr(&hit)))
		assert(hit != 0, "empty point result")
		assert(text(hit, 30005) == name, "hit test misses "+name)
		release(hit)
	}
	volume := controls["Volume"]
	save := controls["Save preferences"]
	sound := controls["Enable sound"]
	assert(number(volume, 30049) == 0 && number(volume, 30050) == 100, "slider range")
	fmt.Println("PASS: OS discovery, tree, labels, roles, physical bounds, hit testing and range")
	q, r := syscall.NewCallback(eventQuery), syscall.NewCallback(eventRef)
	focusHandler = handler{table: &[4]uintptr{q, r, r, syscall.NewCallback(onFocus)}, iid: focusGUID}
	propertyHandler = handler{table: &[4]uintptr{q, r, r, syscall.NewCallback(onProperty)}, iid: propertyGUID}
	check("AddFocusChangedEventHandler", call(uia, 39, 0, uintptr(unsafe.Pointer(&focusHandler))))
	defer call(uia, 40, uintptr(unsafe.Pointer(&focusHandler)))
	ids := []int32{30086, 30047}
	check("AddPropertyChangedEventHandler", call(uia, 34, root, 7, 0, uintptr(unsafe.Pointer(&propertyHandler)), uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids))))
	defer call(uia, 36, root, uintptr(unsafe.Pointer(&propertyHandler)))
	fmt.Println("Focus the form window if it is not already active.")
	// Establish a different focused node before checking the target's event.
	check("focus checkbox", call(sound, 3))
	waitFor("checkbox focus", func() bool { return number(sound, 30008) == 1 })
	beforeFocus := eventCount(0)
	check("focus Save", call(save, 3))
	waitFor("Save focus and native event", func() bool { return number(save, 30008) == 1 && eventCount(0) > beforeFocus })
	var focused uintptr
	check("GetFocusedElement", call(uia, 8, ptr(&focused)))
	assert(focused != 0, "no focused element")
	assert(text(focused, 30005) == "Save preferences", "focus resolves to wrong element")
	release(focused)
	fmt.Println("PASS: focus action, focused element/state and native event")
	invoke := getPattern(save, 10000, "fb377fbe-8ea6-46d5-9c73-6499642d3059")
	defer release(invoke)
	check("Invoke Save", call(invoke, 3))
	waitFor("save visible result", func() bool {
		found := false
		walk(root, func(p uintptr) {
			if strings.HasPrefix(text(p, 30005), "Saved 1 times.") {
				found = true
			}
		})
		return found
	})
	toggle := getPattern(sound, 10015, "94cf8058-9b8d-4ab9-8bfd-4cd0a33c8c70")
	defer release(toggle)
	oldToggle := number(sound, 30086)
	oldEvent := eventCount(30086)
	check("Toggle sound", call(toggle, 3))
	waitFor("toggle state and native event", func() bool { return number(sound, 30086) != oldToggle && eventCount(30086) > oldEvent })
	rangePattern := getPattern(volume, 10003, "59213f4f-7346-49e5-b120-80555987a148")
	defer release(rangePattern)
	table := *(*uintptr)(unsafe.Pointer(rangePattern))
	method := *(*uintptr)(unsafe.Pointer(table + 3*unsafe.Sizeof(table)))
	var setValue func(uintptr, float64) uintptr
	purego.RegisterFunc(&setValue, method)
	// Choose a target different from the current value so an event is required.
	target := 67.0
	expected := 70.0
	if number(volume, 30047) == 70 {
		target = 37
		expected = 40
	}
	oldEvent = eventCount(30047)
	check("SetValue", setValue(rangePattern, target))
	waitFor("range value and native event", func() bool { return number(volume, 30047) == expected && eventCount(30047) > oldEvent })
	fmt.Println("PASS: native Invoke/Toggle/RangeValue actions, events and visible save result")
	fmt.Println("PASS: Windows UIA form checks (speech is a separate NVDA/Narrator check)")
}
func main() {
	// Bound hangs inside incomplete compatibility-layer COM implementations.
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
			return
		case <-time.After(45 * time.Second):
			fmt.Fprintln(os.Stderr, "FAIL: UIA checker exceeded 45 seconds")
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
