//go:build !js

// Package atspi publishes Shirei's semantic snapshots on the Linux accessibility
// bus. D-Bus callbacks read owned data and queue actions for the window thread.
package atspi

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"go.hasen.dev/shirei"
)

const (
	prefix                     = "org.a11y.atspi."
	rootPath   dbus.ObjectPath = "/org/a11y/atspi/accessible/root"
	windowPath dbus.ObjectPath = "/org/a11y/atspi/accessible/window"
	cachePath  dbus.ObjectPath = "/org/a11y/atspi/cache"
	registry                   = "org.a11y.atspi.Registry"
)

// Reference is the AT-SPI (bus name, object path) wire representation.
type Reference struct {
	Bus  string
	Path dbus.ObjectPath
}

var nullReference = Reference{"", "/org/a11y/atspi/null"}

type element struct {
	node     shirei.AccessNode
	parent   dbus.ObjectPath
	children []dbus.ObjectPath
	index    int32
}
type snapshot struct {
	nodes   []shirei.AccessNode
	size    shirei.Vec2
	focused bool
}

// Bridge belongs to one backend window. Publication and action draining happen
// on the window thread; the connection worker owns exports and notifications.
type Bridge struct {
	mu         sync.RWMutex
	title, bus string
	nodes      map[dbus.ObjectPath]element
	order      []dbus.ObjectPath
	desktop    Reference
	appID      int32
	focused    bool
	actions    []shirei.AccessAction
	wake       func()
	updates    chan snapshot
	cancel     context.CancelFunc
	done       chan struct{}
	// Publication bookkeeping belongs exclusively to the window thread.
	published     bool
	size          shirei.Vec2
	windowFocused bool
}

func Start(title string, wake func()) *Bridge {
	ctx, cancel := context.WithCancel(context.Background())
	b := &Bridge{title: title, wake: wake, desktop: nullReference, updates: make(chan snapshot, 1), cancel: cancel, done: make(chan struct{})}
	b.replace(snapshot{})
	go b.run(ctx)
	return b
}

func (b *Bridge) Close() { b.cancel(); <-b.done }

// Publish copies the final settled snapshot before the backend can reuse it.
// Intermediate publications may coalesce while the bus is busy.
func (b *Bridge) Publish(nodes []shirei.AccessNode, changed bool, size shirei.Vec2, focused bool) {
	if b.published && !changed && b.size == size && b.windowFocused == focused {
		return
	}
	b.published, b.size, b.windowFocused = true, size, focused
	s := snapshot{slices.Clone(nodes), size, focused}
	select {
	case <-b.updates:
	default:
	}
	b.updates <- s
}

// NextAction drains one request. Core interaction helpers revalidate its target
// against the current widget and the previous settled frame.
func (b *Bridge) NextAction() shirei.AccessAction {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.actions) == 0 {
		return shirei.AccessAction{}
	}
	a := b.actions[0]
	b.actions = b.actions[1:]
	return a
}
func (b *Bridge) HasActions() bool { b.mu.RLock(); defer b.mu.RUnlock(); return len(b.actions) > 0 }

func nodePath(id uint64) dbus.ObjectPath {
	return dbus.ObjectPath("/org/a11y/atspi/accessible/n" + strconv.FormatUint(id, 10))
}
func (b *Bridge) ref(path dbus.ObjectPath) Reference { return Reference{b.bus, path} }
func (b *Bridge) parent(e element) Reference {
	if e.parent == "" {
		return b.desktop
	}
	return b.ref(e.parent)
}

// replace is called under mu after the connection worker starts.
func (b *Bridge) replace(s snapshot) {
	b.focused = s.focused
	b.nodes = make(map[dbus.ObjectPath]element, len(s.nodes)+2)
	b.order = []dbus.ObjectPath{rootPath, windowPath}
	app := shirei.AccessNode{AccessAttrs: shirei.AccessAttrs{Role: "application", Label: b.title}}
	win := shirei.AccessNode{AccessAttrs: shirei.AccessAttrs{Role: "frame", Label: b.title}, Bounds: shirei.Rect{Size: s.size}, Rect: shirei.Rect{Size: s.size}}
	b.nodes[rootPath] = element{node: app, children: []dbus.ObjectPath{windowPath}, index: -1}
	b.nodes[windowPath] = element{node: win, parent: rootPath}
	for _, n := range s.nodes {
		if n.Hidden {
			continue
		}
		// Platform records never retain a live container pointer or a secret value.
		n.Container = nil
		if n.Protected {
			n.Value = ""
		}
		path := nodePath(n.ID)
		b.nodes[path] = element{node: n}
		b.order = append(b.order, path)
	}
	for _, path := range b.order[2:] {
		e := b.nodes[path]
		e.parent = windowPath
		if p := nodePath(e.node.ParentID); e.node.ParentID != 0 {
			if _, ok := b.nodes[p]; ok {
				e.parent = p
			}
		}
		parent := b.nodes[e.parent]
		e.index = int32(len(parent.children))
		parent.children = append(parent.children, path)
		b.nodes[e.parent] = parent
		// Preserve children already attached if snapshot traversal changes order.
		e.children = b.nodes[path].children
		b.nodes[path] = e
	}
}

// discover uses the desktop-neutral accessibility bus service. Missing services
// are retried without making window creation or frame production wait on D-Bus.
func discover(ctx context.Context) (*dbus.Conn, error) {
	address := os.Getenv("AT_SPI_BUS_ADDRESS")
	if address == "" {
		session, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
		if err != nil {
			return nil, err
		}
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = session.Object("org.a11y.Bus", "/org/a11y/bus").CallWithContext(callCtx, "org.a11y.Bus.GetAddress", 0).Store(&address)
		cancel()
		session.Close()
		if err != nil {
			return nil, err
		}
	}
	return dbus.Connect(address, dbus.WithContext(ctx))
}

func (b *Bridge) embed(ctx context.Context, conn *dbus.Conn) bool {
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	b.mu.RLock()
	root := b.ref(rootPath)
	b.mu.RUnlock()
	var desktop Reference
	registryRoot := conn.Object(registry, rootPath)
	var children []Reference
	if err := registryRoot.CallWithContext(callCtx, prefix+"Accessible.GetChildren", 0).Store(&children); err != nil {
		return false
	}
	if slices.Contains(children, root) {
		// An earlier registration may succeed even if its reply times out.
		// The registry does not deduplicate Embed requests.
		desktop.Path = rootPath
		if err := conn.BusObject().CallWithContext(callCtx, "org.freedesktop.DBus.GetNameOwner", 0, registry).Store(&desktop.Bus); err != nil {
			return false
		}
	} else if err := registryRoot.CallWithContext(callCtx, prefix+"Socket.Embed", 0, root).Store(&desktop); err != nil {
		return false
	}
	b.mu.Lock()
	b.desktop = desktop
	rootItem := b.cacheItem(rootPath)
	var focus dbus.ObjectPath
	for _, p := range b.order {
		if b.nodes[p].node.KeyboardFocused && b.focused {
			focus = p
			break
		}
	}
	focused := b.focused
	b.mu.Unlock()
	_ = conn.Emit(cachePath, prefix+"Cache.AddAccessible", rootItem)
	if focused {
		_ = conn.Emit(windowPath, prefix+"Event.Window.Activate", "", int32(0), int32(0), dbus.MakeVariant(b.title), map[string]dbus.Variant{})
	}
	if focus != "" {
		_ = conn.Emit(focus, prefix+"Event.Object.StateChanged", "focused", int32(1), int32(0), dbus.MakeVariant(int32(0)), map[string]dbus.Variant{})
	}
	return true
}

func (b *Bridge) run(ctx context.Context) {
	defer close(b.done)
	retry := time.NewTicker(3 * time.Second)
	defer retry.Stop()
	var conn *dbus.Conn
	var disconnected <-chan struct{}
	var signals chan *dbus.Signal
	registered := false
	connect := func() {
		if conn != nil {
			if !registered {
				registered = b.embed(ctx, conn)
			}
			return
		}
		c, err := discover(ctx)
		if err != nil {
			return
		}
		conn = c
		disconnected = c.Context().Done()
		b.mu.Lock()
		b.bus = c.Names()[0]
		b.desktop = nullReference
		b.appID = 0
		for _, p := range b.order {
			b.export(c, p)
		}
		b.exportCache(c)
		b.mu.Unlock()
		signals = make(chan *dbus.Signal, 16)
		c.Signal(signals)
		matchCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		err = c.AddMatchSignalContext(matchCtx, dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchInterface("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, registry))
		cancel()
		if err != nil {
			c.Close()
			conn = nil
			disconnected = nil
			signals = nil
			return
		}
		registered = b.embed(ctx, c)
	}
	connect()
	for {
		select {
		case <-ctx.Done():
			if conn != nil {
				conn.Close()
			}
			return
		case <-retry.C:
			connect()
		case <-disconnected:
			conn.Close()
			conn = nil
			disconnected = nil
			signals = nil
			registered = false
		case sig, open := <-signals:
			if !open {
				signals = nil
				continue
			}
			if sig != nil && len(sig.Body) == 3 && sig.Body[0] == registry {
				b.mu.RLock()
				owner := b.desktop.Bus
				b.mu.RUnlock()
				if registered && sig.Body[2] == owner {
					continue
				}
				registered = false
				if sig.Body[2] != "" && conn != nil {
					registered = b.embed(ctx, conn)
				}
			}
		case s := <-b.updates:
			b.mu.Lock()
			old, order, wasFocused := b.nodes, b.order, b.focused
			b.replace(s)
			if conn != nil {
				for _, p := range b.order {
					prev, exists := old[p]
					if !exists || !slices.Equal(interfaces(prev.node), interfaces(b.nodes[p].node)) {
						b.export(conn, p)
					}
				}
				for _, p := range order {
					if _, exists := b.nodes[p]; !exists {
						b.unexport(conn, p, old[p].node)
					}
				}
			}
			events := b.changes(old, order, wasFocused)
			b.mu.Unlock()
			if conn != nil {
				for _, ev := range events {
					_ = conn.Emit(ev.path, ev.name, ev.args...)
				}
			}
		}
	}
}

func failure(message string) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.Failed", []any{message})
}
func invalid(message string) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{message})
}
func unknown(path dbus.ObjectPath) *dbus.Error {
	return failure(fmt.Sprintf("Accessible object %s is no longer available", path))
}
