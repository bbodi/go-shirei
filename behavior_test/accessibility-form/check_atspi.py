#!/usr/bin/env python3
"""Run the real form through libatspi from a separate process.

Run inside a Linux Wayland desktop session:
    python3 check_atspi.py /path/to/accessibility-form

Requires the distribution's AT-SPI introspection data and Python GObject
bindings. Orca is useful for the separate speech smoke test, not required here.
"""

import argparse
import os
from pathlib import Path
import platform
import subprocess
import sys
import time

try:
    import gi
    gi.require_version("Atspi", "2.0")
    from gi.repository import Atspi, GLib
except (ImportError, ValueError) as error:
    sys.exit(f"AT-SPI Python bindings unavailable: {error}\n"
             "Install AT-SPI and Python GObject introspection packages for your distribution.")


def children(node):
    return [node.get_child_at_index(i) for i in range(node.get_child_count())]


def walk(node):
    yield node
    for child in children(node):
        if child is not None:
            yield from walk(child)


def require(condition, message):
    if not condition:
        raise AssertionError(message)


def run(binary):
    require(os.environ.get("WAYLAND_DISPLAY"), "Run this check inside the Wayland desktop session.")
    Atspi.init()
    context = GLib.MainContext.default()
    events = []

    def on_event(event, *_):
        events.append((event.source, event.type, event.detail1))

    listener = Atspi.EventListener.new(on_event, None)
    require(listener.register("object:state-changed"), "Cannot subscribe to state changes")
    require(listener.register("object:property-change"), "Cannot subscribe to property changes")
    proc = subprocess.Popen([str(binary), "--manual"])

    def await_result(description, condition, timeout=10):
        deadline = time.monotonic() + timeout
        last_error = None
        while time.monotonic() < deadline:
            require(proc.poll() is None, f"Form exits early with status {proc.returncode}")
            # Let libatspi update its own caches and dispatch notifications.
            while context.pending():
                context.iteration(False)
            try:
                value = condition()
                if value:
                    return value
            except GLib.Error as error:
                last_error = error
            time.sleep(0.02)
        raise AssertionError(f"Timeout: {description}" + (f" ({last_error})" if last_error else ""))

    def find_app():
        desktop = Atspi.get_desktop(0)
        for app in children(desktop):
            if app is not None and app.get_process_id() == proc.pid:
                return app
        return None

    def has_event(node, event_type, detail=None):
        return any(source == node and kind == event_type and (detail is None or value == detail)
                   for source, kind, value in events)

    try:
        app = await_result("application registration on the accessibility bus", find_app, 20)
        require(app.get_toolkit_name() == "shirei", "Wrong toolkit identity")
        window = children(app)[0]
        require(window.get_role() == Atspi.Role.FRAME, "Missing frame below application")
        nodes = list(walk(window))
        controls = {n.get_accessible_id(): n for n in nodes if n.get_accessible_id()}
        save, sound, volume = (controls[name] for name in ("save", "sound", "volume"))
        require(save.get_name() == "Save preferences", "Save spoken label")
        require(sound.get_name() == "Enable sound", "Checkbox spoken label")
        require(volume.get_name() == "Volume", "Slider spoken label")
        button_role = getattr(Atspi.Role, "BUTTON", None) or getattr(Atspi.Role, "PUSH_BUTTON")
        require(save.get_role() == button_role, "Save role")
        require(sound.get_role() == Atspi.Role.CHECK_BOX, "Checkbox role")
        require(volume.get_role() == Atspi.Role.SLIDER, "Slider role")
        require(nodes.index(save) < nodes.index(sound) < nodes.index(volume), "Form reading order")
        require(any(n.get_name() == "Close window" for n in nodes), "Accessible titlebar Close button")
        rect = save.get_component_iface().get_extents(Atspi.CoordType.WINDOW)
        require(rect.width > 0 and rect.height > 0 and rect.y >= 34, "Bounds include the titlebar once")
        value = volume.get_value_iface()
        require(value.get_minimum_value() == 0 and value.get_maximum_value() == 100,
                "Slider range")
        require(value.get_minimum_increment() == 10, "Slider increment")
        print("PASS: registry discovery, hierarchy, labels, roles, bounds and ranges", flush=True)

        print("Focus the form window if it is not already active.", flush=True)
        await_result("form window has keyboard focus",
                     lambda: window.get_state_set().contains(Atspi.StateType.ACTIVE), 60)
        # Move away and back so the focus notification is observable even if Save
        # already has the framework's initial keyboard focus.
        require(sound.get_component_iface().grab_focus(), "Checkbox focus request rejected")
        await_result("checkbox keyboard focus", lambda: sound.get_state_set().contains(Atspi.StateType.FOCUSED))
        events.clear()
        require(save.get_component_iface().grab_focus(), "Save focus request rejected")
        await_result("Save keyboard focus", lambda: save.get_state_set().contains(Atspi.StateType.FOCUSED))
        await_result("focus notification", lambda: has_event(save, "object:state-changed:focused", 1))
        print("PASS: focus action, focused state and native event", flush=True)

        require(save.get_action_iface().do_action(0), "Save action rejected")
        await_result("Save visible result", lambda: any(n.get_name() == "Saved 1 times. Volume 40." for n in walk(window)))
        events.clear()
        require(sound.get_action_iface().do_action(0), "Checkbox action rejected")
        await_result("checkbox checked state", lambda: sound.get_state_set().contains(Atspi.StateType.CHECKED))
        await_result("checkbox notification", lambda: has_event(sound, "object:state-changed:checked", 1))
        events.clear()
        require(value.set_current_value(67), "Slider write rejected")
        await_result("slider snaps to 70", lambda: value.get_current_value() == 70)
        await_result("slider notification", lambda: has_event(volume, "object:property-change:accessible-value"))
        await_result("visible saved count and slider value", lambda: any(n.get_name() == "Saved 1 times. Volume 70." for n in walk(window)))
        require(next(n for n in walk(window) if n.get_accessible_id() == "save") == save,
                "Save accessible identity changes across updates")
        print("PASS: native press/toggle/value actions, events and visible results", flush=True)
        print("PASS: all AT-SPI form checks (speech is a separate Orca check)", flush=True)
    finally:
        listener.deregister("object:state-changed")
        listener.deregister("object:property-change")
        if proc.poll() is None:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
        Atspi.exit()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path, nargs="?", help="Linux accessibility-form executable (defaults to repo bin/ for this architecture)")
    args = parser.parse_args()
    if args.binary is None:
        arch = {"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64", "amd64": "amd64"}.get(platform.machine())
        if arch is None:
            sys.exit("Specify an accessibility-form binary for this architecture.")
        args.binary = Path(__file__).resolve().parents[3] / "bin" / f"accessibility-form-linux-{arch}"
    try:
        run(args.binary.resolve())
    except (AssertionError, GLib.Error, OSError, KeyError, IndexError) as error:
        sys.exit(f"FAIL: {error}")
