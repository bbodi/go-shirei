//go:build !js

package atspi

import (
	"bufio"
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"go.hasen.dev/shirei"
)

// This test uses a real private bus and a separate client connection. The input
// is a whole form snapshot; requests cross D-Bus before entering the action queue.
func TestFormOverDBus(t *testing.T) {
	address := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	stopBus := startBus(t, address)
	t.Setenv("AT_SPI_BUS_ADDRESS", address)
	client, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	signals := make(chan *dbus.Signal, 256)
	client.Signal(signals)
	if err = client.AddMatchSignal(dbus.WithMatchInterface(prefix + "Event.Object")); err != nil {
		t.Fatal(err)
	}
	if err = client.AddMatchSignal(dbus.WithMatchInterface(prefix + "Cache")); err != nil {
		t.Fatal(err)
	}
	b := Start("Transport form", func() {})
	defer b.Close()
	r := shirei.Rect{Origin: shirei.Vec2{20, 30}, Size: shirei.Vec2{100, 40}}
	nodes := []shirei.AccessNode{
		{ID: 1, AccessAttrs: shirei.AccessAttrs{Role: "button", Label: "Save", Name: "save"}, Bounds: r, Rect: r, Focusable: true, Actions: shirei.AccessPress | shirei.AccessFocus, PaintOrder: 1},
		{ID: 2, AccessAttrs: shirei.AccessAttrs{Role: "checkbox", Label: "Enable sound"}, Actions: shirei.AccessPress, Bounds: r, Rect: r, PaintOrder: 2},
		{ID: 3, AccessAttrs: shirei.AccessAttrs{Role: "slider", Label: "Volume", Numeric: true, Number: 40, Max: 100, Step: 10}, Actions: shirei.AccessIncrement | shirei.AccessDecrement | shirei.AccessSetValue, Bounds: r, Rect: r},
		{ID: 4, AccessAttrs: shirei.AccessAttrs{Role: "text", Label: "Password", Protected: true, Value: "secret"}},
		{ID: 5, AccessAttrs: shirei.AccessAttrs{Role: "statictext", Label: "Hidden secret", Hidden: true}},
		{ID: 6, AccessAttrs: shirei.AccessAttrs{Role: "button", Label: "Disabled", Disabled: true}, Actions: shirei.AccessPress},
	}
	b.Publish(nodes, true, shirei.Vec2{400, 300}, true)
	var bus string
	wait(t, func() bool { b.mu.RLock(); defer b.mu.RUnlock(); bus = b.bus; return bus != "" && len(b.nodes) == 7 })
	call := func(p dbus.ObjectPath, method string, args ...any) *dbus.Call {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return client.Object(bus, p).CallWithContext(ctx, method, 0, args...)
	}
	prop := func(p dbus.ObjectPath, iface, name string) dbus.Variant {
		var v dbus.Variant
		if err := call(p, "org.freedesktop.DBus.Properties.Get", prefix+iface, name).Store(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	var items []CacheItem
	if err := call(cachePath, prefix+"Cache.GetItems").Store(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 7 || items[2].Name != "Save" || items[2].Parent.Path != windowPath || items[2].Role != 43 {
		t.Fatalf("bad cache: %+v", items)
	}
	for _, item := range items {
		if item.Name == "Hidden secret" {
			t.Fatal("hidden node exposed")
		}
	}
	if got := prop(nodePath(1), "Accessible", "AccessibleId").Value(); got != "save" {
		t.Fatalf("query identifier: %v", got)
	}
	var children []Reference
	if err := call(windowPath, prefix+"Accessible.GetChildren").Store(&children); err != nil {
		t.Fatal(err)
	}
	if len(children) != 5 || children[0].Path != nodePath(1) || children[4].Path != nodePath(6) {
		t.Fatalf("children: %+v", children)
	}
	var info string
	if err := call(nodePath(3), "org.freedesktop.DBus.Introspectable.Introspect").Store(&info); err != nil || !strings.Contains(info, "CurrentValue") {
		t.Fatalf("introspection: %v %s", err, info)
	}
	if err := call(nodePath(4), "org.freedesktop.DBus.Properties.Get", prefix+"Value", "Text").Err; err == nil {
		t.Fatal("password value exposed")
	}
	var rect extents
	if err := call(nodePath(1), prefix+"Component.GetExtents", uint32(1)).Store(&rect); err != nil || rect.X != 20 || rect.Y != 30 {
		t.Fatalf("geometry: %+v %v", rect, err)
	}
	if err := call(nodePath(1), prefix+"Component.GetExtents", uint32(0)).Store(&rect); err != nil || rect.X != math.MinInt32 {
		t.Fatalf("screen geometry: %+v %v", rect, err)
	}
	var hit Reference
	if err := call(windowPath, prefix+"Component.GetAccessibleAtPoint", int32(25), int32(35), uint32(1)).Store(&hit); err != nil || hit.Path != nodePath(2) {
		t.Fatalf("paint-order hit: %+v %v", hit, err)
	}
	if err := call(nodePath(1), prefix+"Component.GetAccessibleAtPoint", int32(25), int32(35), uint32(1)).Store(&hit); err != nil || hit.Path != nodePath(1) {
		t.Fatalf("subtree hit: %+v %v", hit, err)
	}
	var accepted bool
	if err := call(nodePath(1), prefix+"Action.DoAction", int32(0)).Store(&accepted); err != nil || !accepted {
		t.Fatalf("press: %v %v", accepted, err)
	}
	if err := call(nodePath(1), prefix+"Component.GrabFocus").Store(&accepted); err != nil || !accepted {
		t.Fatalf("focus: %v %v", accepted, err)
	}
	if err := call(nodePath(3), "org.freedesktop.DBus.Properties.Set", prefix+"Value", "CurrentValue", dbus.MakeVariant(float64(67))).Err; err != nil {
		t.Fatal(err)
	}
	if err := call(nodePath(3), prefix+"Action.DoAction", int32(0)).Store(&accepted); err != nil || !accepted {
		t.Fatalf("increment: %v %v", accepted, err)
	}
	for _, want := range []shirei.AccessAction{{ID: 1, Kind: shirei.AccessPress}, {ID: 1, Kind: shirei.AccessFocus}, {ID: 3, Kind: shirei.AccessSetValue, Value: 67}, {ID: 3, Kind: shirei.AccessIncrement}} {
		if got := b.NextAction(); got != want {
			t.Fatalf("action got %+v want %+v", got, want)
		}
	}
	if b.HasActions() {
		t.Fatal("duplicate actions")
	}
	if err := call(nodePath(6), prefix+"Action.DoAction", int32(0)).Store(&accepted); err != nil || accepted {
		t.Fatalf("disabled: %v %v", accepted, err)
	}
	if err := call(nodePath(3), "org.freedesktop.DBus.Properties.Set", prefix+"Value", "CurrentValue", dbus.MakeVariant(math.NaN())).Err; err == nil {
		t.Fatal("NaN accepted")
	}
	if err := call(nodePath(1), "org.freedesktop.DBus.Properties.Set", prefix+"Accessible", "Name", dbus.MakeVariant("overwrite")).Err; err == nil {
		t.Fatal("read-only name accepted")
	}
	// Label-only changes and keyboard focus must produce cache-coherent events.
	for len(signals) > 0 {
		<-signals
	}
	nodes[0].Label = "Store preferences"
	nodes[0].KeyboardFocused = true
	nodes[1].Checked = true
	nodes[2].Number = 70
	b.Publish(nodes, true, shirei.Vec2{400, 300}, true)
	wanted := map[string]bool{"PropertyChange:accessible-name": false, "StateChanged:focused": false, "StateChanged:checked": false, "PropertyChange:accessible-value": false}
	deadline := time.After(3 * time.Second)
	for remaining := len(wanted); remaining > 0; {
		select {
		case s := <-signals:
			if len(s.Body) == 5 {
				detail, _ := s.Body[0].(string)
				key := strings.TrimPrefix(s.Name, prefix+"Event.Object.") + ":" + detail
				if seen, ok := wanted[key]; ok && !seen {
					wanted[key] = true
					remaining--
				}
			}
		case <-deadline:
			t.Fatalf("missing events: %v", wanted)
		}
	}
	if got := prop(nodePath(1), "Accessible", "Name").Value(); got != "Store preferences" {
		t.Fatalf("stale label: %v", got)
	}
	// Snapshot ownership: mutation after Publish cannot alter exported strings.
	nodes[0].Label = "unpublished"
	if got := prop(nodePath(1), "Accessible", "Name").Value(); got != "Store preferences" {
		t.Fatalf("borrowed snapshot: %v", got)
	}
	b.Publish(nodes[1:], true, shirei.Vec2{400, 300}, false)
	wait(t, func() bool { return call(nodePath(1), prefix+"Accessible.GetRole").Err != nil })
	if call(nodePath(1), prefix+"Action.DoAction", int32(0)).Err == nil {
		t.Fatal("removed target remains callable")
	}
	if b.HasActions() {
		t.Fatal("invalid request queued")
	}
	// Restart the real bus while the window keeps publishing. The bridge must
	// reconnect and expose the latest tree, without reusing closed signal channels.
	client.Close()
	stopBus()
	startBus(t, address)
	client, err = dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	nodes[1].Label = "Sound after reconnect"
	b.Publish(nodes[1:], true, shirei.Vec2{400, 300}, false)
	wait(t, func() bool {
		b.mu.RLock()
		bus = b.bus
		b.mu.RUnlock()
		var v dbus.Variant
		err := call(nodePath(2), "org.freedesktop.DBus.Properties.Get", prefix+"Accessible", "Name").Store(&v)
		return err == nil && v.Value() == "Sound after reconnect"
	})

}

func wait(t *testing.T, ready func() bool) {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	for time.Now().Before(until) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for accessibility state")
}

func startBus(t *testing.T, address string) func() {
	t.Helper()
	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon is required for the AT-SPI transport test")
	}
	cmd := exec.Command(daemon, "--session", "--nofork", "--nopidfile", "--address="+address, "--print-address=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }) }
	t.Cleanup(stop)
	if _, err = bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	return stop
}

func TestBusAvailableAfterWindow(t *testing.T) {
	address := "unix:path=" + filepath.Join(t.TempDir(), "bus")
	t.Setenv("AT_SPI_BUS_ADDRESS", address)
	b := Start("Late bus", func() {})
	defer b.Close()
	b.Publish([]shirei.AccessNode{{ID: 42, AccessAttrs: shirei.AccessAttrs{Role: "button", Label: "Late button"}}}, true, shirei.Vec2{100, 100}, true)
	wait(t, func() bool { b.mu.RLock(); defer b.mu.RUnlock(); return len(b.nodes) == 3 })
	startBus(t, address)
	client, err := dbus.Connect(address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	wait(t, func() bool {
		b.mu.RLock()
		bus := b.bus
		b.mu.RUnlock()
		if bus == "" {
			return false
		}
		var v dbus.Variant
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := client.Object(bus, nodePath(42)).CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", 0, prefix+"Accessible", "Name").Store(&v)
		return err == nil && v.Value() == "Late button"
	})
}
