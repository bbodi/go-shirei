//go:build !js

package atspi

import (
	"slices"

	"github.com/godbus/dbus/v5"
)

type event struct {
	path dbus.ObjectPath
	name string
	args []any
}

// changes runs under the snapshot lock. The worker emits the resulting wire
// records after releasing the lock, so readers never wait for signal delivery.
func (b *Bridge) changes(old map[dbus.ObjectPath]element, oldOrder []dbus.ObjectPath, wasFocused bool) []event {
	var events []event
	emit := func(path dbus.ObjectPath, name, detail string, a, c int32, value any) {
		events = append(events, event{path, prefix + name, []any{detail, a, c, dbus.MakeVariant(value), map[string]dbus.Variant{}}})
	}
	// Remove old child positions in reverse order so cached indices stay valid.
	for _, p := range oldOrder {
		prev := old[p]
		next := b.nodes[p]
		if !slices.Equal(prev.children, next.children) {
			for i := len(prev.children) - 1; i >= 0; i-- {
				emit(p, "Event.Object.ChildrenChanged", "remove", int32(i), 0, b.ref(prev.children[i]))
			}
		}
	}
	for _, p := range oldOrder {
		if _, ok := b.nodes[p]; !ok {
			emit(p, "Event.Object.StateChanged", "defunct", 1, 0, int32(0))
			events = append(events, event{cachePath, prefix + "Cache.RemoveAccessible", []any{b.ref(p)}})
		}
	}
	for _, p := range b.order {
		e := b.nodes[p]
		prev, exists := old[p]
		if !exists || !slices.Equal(interfaces(prev.node), interfaces(e.node)) {
			events = append(events, event{cachePath, prefix + "Cache.AddAccessible", []any{b.cacheItem(p)}})
		}
	}
	for _, p := range b.order {
		e := b.nodes[p]
		prev, exists := old[p]
		if !slices.Equal(prev.children, e.children) {
			for i, ch := range e.children {
				emit(p, "Event.Object.ChildrenChanged", "add", int32(i), 0, b.ref(ch))
			}
		}
		if !exists {
			continue
		}
		if e.parent != prev.parent {
			emit(p, "Event.Object.PropertyChange", "accessible-parent", 0, 0, b.parent(e))
		}
		if e.node.Label != prev.node.Label {
			emit(p, "Event.Object.PropertyChange", "accessible-name", 0, 0, e.node.Label)
		}
		if e.node.Description != prev.node.Description {
			emit(p, "Event.Object.PropertyChange", "accessible-description", 0, 0, e.node.Description)
		}
		r, _ := role(e.node)
		pr, _ := role(prev.node)
		if r != pr {
			emit(p, "Event.Object.PropertyChange", "accessible-role", 0, 0, r)
		}
		if e.node.Numeric && (e.node.Number != prev.node.Number || e.node.Value != prev.node.Value) {
			emit(p, "Event.Object.PropertyChange", "accessible-value", 0, 0, float64(e.node.Number))
		}
		if e.node.Bounds != prev.node.Bounds {
			r, _ := (object{b, p}).bounds(0)
			emit(p, "Event.Object.BoundsChanged", "", 0, 0, r)
		}
		before, after := states(prev.node, wasFocused), states(e.node, b.focused)
		for _, s := range []struct {
			bit  uint
			name string
		}{{1, "active"}, {4, "checked"}, {7, "editable"}, {8, "enabled"}, {11, "focusable"}, {12, "focused"}, {24, "sensitive"}, {25, "showing"}, {30, "visible"}, {41, "checkable"}} {
			a, c := before[s.bit/32]&(1<<(s.bit%32)), after[s.bit/32]&(1<<(s.bit%32))
			if a == c {
				continue
			}
			enabled := int32(0)
			if c != 0 {
				enabled = 1
			}
			emit(p, "Event.Object.StateChanged", s.name, enabled, 0, int32(0))
		}
	}
	if wasFocused != b.focused {
		name := "Event.Window.Deactivate"
		if b.focused {
			name = "Event.Window.Activate"
		}
		emit(windowPath, name, "", 0, 0, b.title)
	}
	// A newly inserted focused control also needs a focus event.
	for _, p := range b.order {
		e := b.nodes[p]
		if _, ok := old[p]; !ok && e.node.KeyboardFocused && b.focused {
			emit(p, "Event.Object.StateChanged", "focused", 1, 0, int32(0))
		}
	}
	return events
}
