//go:build !js

package atspi

import (
	"math"
	"os"
	"strconv"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"go.hasen.dev/shirei"
)

type object struct {
	b    *Bridge
	path dbus.ObjectPath
}
type accessible struct{ object }
type component struct{ object }
type action struct{ object }
type application struct{ object }
type properties struct{ object }
type cache struct{ b *Bridge }

// CacheItem mirrors the AT-SPI cache wire signature, not a second semantic tree.
type CacheItem struct {
	Object, Application, Parent Reference
	Index, Children             int32
	Interfaces                  []string
	Name                        string
	Role                        uint32
	Description                 string
	State                       []uint32
}
type relation struct {
	Kind    uint32
	Targets []Reference
}
type actionInfo struct{ Name, Description, KeyBinding string }
type extents struct{ X, Y, Width, Height int32 }

func interfaces(n shirei.AccessNode) []string {
	result := []string{prefix + "Accessible"}
	if n.Role == "application" {
		return append(result, prefix+"Application")
	}
	result = append(result, prefix+"Component")
	if n.Actions&(shirei.AccessPress|shirei.AccessIncrement|shirei.AccessDecrement) != 0 {
		result = append(result, prefix+"Action")
	}
	if n.Numeric && !n.Protected {
		result = append(result, prefix+"Value")
	}
	return result
}
func role(n shirei.AccessNode) (uint32, string) {
	if n.Protected {
		return 40, "password text"
	}
	switch n.Role {
	case "application":
		return 75, "application"
	case "frame":
		return 23, "frame"
	case "statictext":
		return 29, "label"
	case "text":
		if n.Editable {
			return 79, "entry"
		}
		return 29, "label"
	case "button":
		return 43, "push button"
	case "checkbox", "switch":
		return 7, "check box"
	case "radio":
		return 44, "radio button"
	case "slider":
		return 51, "slider"
	case "progressbar":
		return 42, "progress bar"
	case "menuitem":
		return 35, "menu item"
	case "menu":
		return 33, "menu"
	default:
		return 39, "panel"
	}
}
func states(n shirei.AccessNode, focused bool) []uint32 {
	result := []uint32{0, 0}
	set := func(bit uint, enabled bool) {
		if enabled {
			result[bit/32] |= 1 << (bit % 32)
		}
	}
	set(8, !n.Disabled)
	set(24, !n.Disabled)
	set(30, true)
	set(25, n.Role == "application" || (n.Rect.Size[0] > 0 && n.Rect.Size[1] > 0))
	set(1, n.Role == "frame" && focused)
	set(11, n.Focusable)
	set(12, n.KeyboardFocused && focused)
	set(4, n.Checked)
	set(41, n.Role == "checkbox" || n.Role == "switch" || n.Role == "radio")
	set(14, n.Role == "slider")
	set(7, n.Editable && !n.Disabled)
	set(17, n.Editable && n.Multiline)
	set(26, n.Editable && !n.Multiline)
	return result
}
func (b *Bridge) cacheItem(p dbus.ObjectPath) CacheItem {
	e := b.nodes[p]
	r, _ := role(e.node)
	return CacheItem{b.ref(p), b.ref(rootPath), b.parent(e), e.index, int32(len(e.children)), interfaces(e.node), e.node.Label, r, e.node.Description, states(e.node, b.focused)}
}
func (c cache) GetItems() ([]CacheItem, *dbus.Error) {
	c.b.mu.RLock()
	defer c.b.mu.RUnlock()
	items := make([]CacheItem, 0, len(c.b.order))
	for _, p := range c.b.order {
		items = append(items, c.b.cacheItem(p))
	}
	return items, nil
}

func (o accessible) GetChildren() ([]Reference, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	e, ok := o.b.nodes[o.path]
	if !ok {
		return nil, unknown(o.path)
	}
	refs := make([]Reference, 0, len(e.children))
	for _, p := range e.children {
		refs = append(refs, o.b.ref(p))
	}
	return refs, nil
}
func (o accessible) GetChildAtIndex(index int32) (Reference, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	e, ok := o.b.nodes[o.path]
	if !ok {
		return nullReference, unknown(o.path)
	}
	if index < 0 || int(index) >= len(e.children) {
		return nullReference, invalid("Child index is out of range")
	}
	return o.b.ref(e.children[index]), nil
}
func (o accessible) GetIndexInParent() (int32, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	e, ok := o.b.nodes[o.path]
	if !ok {
		return -1, unknown(o.path)
	}
	return e.index, nil
}
func (o accessible) GetRole() (uint32, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	e, ok := o.b.nodes[o.path]
	if !ok {
		return 0, unknown(o.path)
	}
	r, _ := role(e.node)
	return r, nil
}
func (o accessible) GetRoleName() (string, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	e, ok := o.b.nodes[o.path]
	if !ok {
		return "", unknown(o.path)
	}
	_, r := role(e.node)
	return r, nil
}
func (o accessible) GetLocalizedRoleName() (string, *dbus.Error) { return o.GetRoleName() }
func (o accessible) GetState() ([]uint32, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	e, ok := o.b.nodes[o.path]
	if !ok {
		return []uint32{1 << 6, 0}, nil
	}
	return states(e.node, o.b.focused), nil
}
func (o accessible) GetRelationSet() ([]relation, *dbus.Error) { return []relation{}, nil }
func (o accessible) GetAttributes() (map[string]string, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	e, ok := o.b.nodes[o.path]
	if !ok {
		return nil, unknown(o.path)
	}
	attrs := map[string]string{"toolkit": "shirei"}
	if e.node.Name != "" {
		attrs["id"] = e.node.Name
	}
	return attrs, nil
}
func (o accessible) GetApplication() (Reference, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	return o.b.ref(rootPath), nil
}
func (o accessible) GetInterfaces() ([]string, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	e, ok := o.b.nodes[o.path]
	if !ok {
		return nil, unknown(o.path)
	}
	return interfaces(e.node), nil
}
func locale() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	return "C"
}
func (o application) GetLocale(uint32) (string, *dbus.Error)          { return locale(), nil }
func (o application) GetApplicationBusAddress() (string, *dbus.Error) { return "", nil }

// propertyValues runs with the snapshot read lock held. D-Bus variants preserve
// the exact integer widths required by AT-SPI.
func (o object) propertyValues(iface string) (map[string]dbus.Variant, *dbus.Error) {
	values := map[string]any{}
	if o.path == cachePath {
		if iface != prefix+"Cache" {
			return nil, invalid("Unknown interface")
		}
		values["version"] = uint32(1)
	} else {
		e, ok := o.b.nodes[o.path]
		if !ok {
			return nil, unknown(o.path)
		}
		supported := false
		for _, i := range interfaces(e.node) {
			if i == iface {
				supported = true
			}
		}
		if !supported {
			return nil, invalid("Unknown interface")
		}
		switch iface {
		case prefix + "Accessible":
			id := e.node.Name
			if id == "" && e.node.ID != 0 {
				id = strconv.FormatUint(e.node.ID, 10)
			}
			values = map[string]any{"version": uint32(1), "Name": e.node.Label, "Description": e.node.Description, "Parent": o.b.parent(e), "ChildCount": int32(len(e.children)), "Locale": locale(), "AccessibleId": id, "HelpText": e.node.Description}
		case prefix + "Application":
			values = map[string]any{"ToolkitName": "shirei", "Version": "", "ToolkitVersion": "", "AtspiVersion": "2.1", "InterfaceVersion": uint32(1), "Id": o.b.appID}
		case prefix + "Component":
			values["version"] = uint32(1)
		case prefix + "Action":
			values = map[string]any{"version": uint32(1), "NActions": int32(len(actionKinds(e.node)))}
		case prefix + "Value":
			values = map[string]any{"version": uint32(1), "MinimumValue": float64(e.node.Min), "MaximumValue": float64(e.node.Max), "MinimumIncrement": float64(e.node.Step), "CurrentValue": float64(e.node.Number), "Text": e.node.Value}
		}
	}
	result := make(map[string]dbus.Variant, len(values))
	for k, v := range values {
		result[k] = dbus.MakeVariant(v)
	}
	return result, nil
}
func (o properties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	return o.propertyValues(iface)
}
func (o properties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	values, err := o.GetAll(iface)
	if err != nil {
		return dbus.Variant{}, err
	}
	v, ok := values[name]
	if !ok {
		return dbus.Variant{}, invalid("Unknown property")
	}
	return v, nil
}
func (o properties) Set(iface, name string, value dbus.Variant) *dbus.Error {
	o.b.mu.Lock()
	defer o.b.mu.Unlock()
	if o.path == rootPath && iface == prefix+"Application" && name == "Id" {
		id, ok := value.Value().(int32)
		if !ok {
			return invalid("Application Id must be int32")
		}
		o.b.appID = id
		return nil
	}
	if iface == prefix+"Value" && name == "CurrentValue" {
		v, ok := value.Value().(float64)
		if !ok || math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > math.MaxFloat32 {
			return invalid("Value must be a finite number")
		}
		if !o.enqueue(shirei.AccessSetValue, float32(v)) {
			return failure("Value is not writable")
		}
		return nil
	}
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []any{"Property is read-only"})
}

// Geometry is in surface logical coordinates. Wayland does not supply an
// absolute desktop origin; screen-coordinate requests report unknown position.
func (o object) bounds(coord uint32) (extents, *dbus.Error) {
	e, ok := o.b.nodes[o.path]
	if !ok {
		return extents{}, unknown(o.path)
	}
	r := e.node.Bounds
	switch coord {
	case 0:
		return extents{math.MinInt32, math.MinInt32, int32(r.Size[0]), int32(r.Size[1])}, nil
	case 1:
	case 2:
		if p, ok := o.b.nodes[e.parent]; ok {
			r.Origin = shirei.Vec2Sub(r.Origin, p.node.Bounds.Origin)
		}
	default:
		return extents{}, invalid("Unknown coordinate type")
	}
	return extents{int32(r.Origin[0]), int32(r.Origin[1]), int32(r.Size[0]), int32(r.Size[1])}, nil
}
func (o component) GetExtents(coord uint32) (extents, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	return o.bounds(coord)
}
func (o component) GetPosition(coord uint32) (int32, int32, *dbus.Error) {
	r, e := o.GetExtents(coord)
	return r.X, r.Y, e
}
func (o component) GetSize() (int32, int32, *dbus.Error) {
	r, e := o.GetExtents(1)
	return r.Width, r.Height, e
}
func (o component) GetLayer() (uint32, *dbus.Error) {
	if o.path == windowPath {
		return 7, nil
	}
	return 3, nil
}
func (o component) GetMDIZOrder() (int16, *dbus.Error) { return 0, nil }
func (o component) GetAlpha() (float64, *dbus.Error)   { return 1, nil }
func (o component) GrabFocus() (bool, *dbus.Error) {
	o.b.mu.Lock()
	defer o.b.mu.Unlock()
	return o.enqueue(shirei.AccessFocus, 0), nil
}
func (o component) SetExtents(int32, int32, int32, int32, uint32) (bool, *dbus.Error) {
	return false, nil
}
func (o component) SetPosition(int32, int32, uint32) (bool, *dbus.Error)   { return false, nil }
func (o component) SetSize(int32, int32) (bool, *dbus.Error)               { return false, nil }
func (o component) ScrollTo(uint32) (bool, *dbus.Error)                    { return false, nil }
func (o component) ScrollToPoint(uint32, int32, int32) (bool, *dbus.Error) { return false, nil }
func contains(r shirei.Rect, x, y float32) bool {
	return x >= r.Origin[0] && y >= r.Origin[1] && x < r.Origin[0]+r.Size[0] && y < r.Origin[1]+r.Size[1]
}
func (o object) point(x, y int32, coord uint32) (float32, float32, *dbus.Error) {
	if coord == 0 {
		return 0, 0, failure("Screen coordinates are unavailable on Wayland")
	}
	if coord != 1 && coord != 2 {
		return 0, 0, invalid("Unknown coordinate type")
	}
	e, ok := o.b.nodes[o.path]
	if !ok {
		return 0, 0, unknown(o.path)
	}
	px, py := float32(x), float32(y)
	if coord == 2 {
		p := o.b.nodes[e.parent]
		px += p.node.Bounds.Origin[0]
		py += p.node.Bounds.Origin[1]
	}
	return px, py, nil
}
func (o component) Contains(x, y int32, coord uint32) (bool, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	px, py, err := o.point(x, y, coord)
	if err != nil {
		return false, err
	}
	return contains(o.b.nodes[o.path].node.Rect, px, py), nil
}
func (o component) GetAccessibleAtPoint(x, y int32, coord uint32) (Reference, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	px, py, err := o.point(x, y, coord)
	if err != nil {
		return nullReference, err
	}
	target := nullReference
	order := -1
	for _, p := range o.b.order {
		e := o.b.nodes[p]
		if p == rootPath || !contains(e.node.Rect, px, py) {
			continue
		}
		descendant := p == o.path
		for parent := e.parent; !descendant && parent != ""; parent = o.b.nodes[parent].parent {
			descendant = parent == o.path
		}
		if descendant && e.node.PaintOrder >= order {
			target = o.b.ref(p)
			order = e.node.PaintOrder
		}
	}
	return target, nil
}

func actionKinds(n shirei.AccessNode) []shirei.AccessActionKind {
	result := []shirei.AccessActionKind{}
	for _, k := range []shirei.AccessActionKind{shirei.AccessPress, shirei.AccessIncrement, shirei.AccessDecrement} {
		if n.Actions&k != 0 {
			result = append(result, k)
		}
	}
	return result
}
func actionName(k shirei.AccessActionKind) string {
	switch k {
	case shirei.AccessPress:
		return "click"
	case shirei.AccessIncrement:
		return "increment"
	case shirei.AccessDecrement:
		return "decrement"
	}
	return ""
}
func (o action) GetActions() ([]actionInfo, *dbus.Error) {
	o.b.mu.RLock()
	defer o.b.mu.RUnlock()
	e, ok := o.b.nodes[o.path]
	if !ok {
		return nil, unknown(o.path)
	}
	result := []actionInfo{}
	for _, k := range actionKinds(e.node) {
		result = append(result, actionInfo{actionName(k), "", ""})
	}
	return result, nil
}
func (o action) GetName(index int32) (string, *dbus.Error) {
	a, e := o.GetActions()
	if e != nil {
		return "", e
	}
	if index < 0 || int(index) >= len(a) {
		return "", invalid("Action index is out of range")
	}
	return a[index].Name, nil
}
func (o action) GetLocalizedName(index int32) (string, *dbus.Error) { return o.GetName(index) }
func (o action) GetDescription(index int32) (string, *dbus.Error) {
	_, e := o.GetName(index)
	return "", e
}
func (o action) GetKeyBinding(index int32) (string, *dbus.Error) {
	_, e := o.GetName(index)
	return "", e
}
func (o action) DoAction(index int32) (bool, *dbus.Error) {
	o.b.mu.Lock()
	defer o.b.mu.Unlock()
	e, ok := o.b.nodes[o.path]
	if !ok {
		return false, unknown(o.path)
	}
	kinds := actionKinds(e.node)
	if index < 0 || int(index) >= len(kinds) {
		return false, invalid("Action index is out of range")
	}
	return o.enqueue(kinds[index], 0), nil
}
func (o object) enqueue(kind shirei.AccessActionKind, value float32) bool {
	e, ok := o.b.nodes[o.path]
	if !ok || e.node.Disabled || e.node.Hidden || e.node.ID == 0 || e.node.Actions&kind == 0 || len(o.b.actions) >= 256 {
		return false
	}
	if kind == shirei.AccessFocus && !o.b.focused {
		return false
	}
	o.b.actions = append(o.b.actions, shirei.AccessAction{ID: e.node.ID, Kind: kind, Value: value})
	o.b.wake()
	return true
}

func (b *Bridge) export(conn *dbus.Conn, path dbus.ObjectPath) {
	o := object{b, path}
	n := b.nodes[path].node
	// Remove obsolete pattern exports when a stable node changes capabilities.
	for _, i := range []string{"Application", "Component", "Action", "Value"} {
		_ = conn.Export(nil, path, prefix+i)
	}
	specs := []introspect.Interface{}
	for _, iface := range interfaces(n) {
		var impl any
		switch iface {
		case prefix + "Accessible":
			impl = accessible{o}
		case prefix + "Application":
			impl = application{o}
		case prefix + "Component":
			impl = component{o}
		case prefix + "Action":
			impl = action{o}
		}
		if impl != nil {
			_ = conn.Export(impl, path, iface)
		}
		spec := introspect.Interface{Name: iface}
		if impl != nil {
			spec.Methods = introspect.Methods(impl)
		}
		props, _ := o.propertyValues(iface)
		for name, value := range props {
			access := "read"
			if (iface == prefix+"Application" && name == "Id") || (iface == prefix+"Value" && name == "CurrentValue") {
				access = "readwrite"
			}
			spec.Properties = append(spec.Properties, introspect.Property{Name: name, Type: value.Signature().String(), Access: access})
		}
		specs = append(specs, spec)
	}
	_ = conn.Export(properties{o}, path, "org.freedesktop.DBus.Properties")
	specs = append(specs, introspect.Interface{Name: "org.freedesktop.DBus.Properties", Methods: introspect.Methods(properties{o})})
	_ = conn.Export(introspect.NewIntrospectable(&introspect.Node{Name: string(path), Interfaces: specs}), path, "org.freedesktop.DBus.Introspectable")
}
func (b *Bridge) unexport(c *dbus.Conn, p dbus.ObjectPath, n shirei.AccessNode) {
	for _, i := range append(interfaces(n), "org.freedesktop.DBus.Properties", "org.freedesktop.DBus.Introspectable") {
		_ = c.Export(nil, p, i)
	}
}
func (b *Bridge) exportCache(c *dbus.Conn) {
	_ = c.Export(cache{b}, cachePath, prefix+"Cache")
	_ = c.Export(properties{object{b, cachePath}}, cachePath, "org.freedesktop.DBus.Properties")
	spec := introspect.Interface{Name: prefix + "Cache", Methods: introspect.Methods(cache{b}), Properties: []introspect.Property{{Name: "version", Type: "u", Access: "read"}}, Signals: []introspect.Signal{
		{Name: "AddAccessible", Args: []introspect.Arg{{Type: "((so)(so)(so)iiassusau)"}}}, {Name: "RemoveAccessible", Args: []introspect.Arg{{Type: "(so)"}}},
	}}
	_ = c.Export(introspect.NewIntrospectable(&introspect.Node{Name: string(cachePath), Interfaces: []introspect.Interface{spec}}), cachePath, "org.freedesktop.DBus.Introspectable")
}
