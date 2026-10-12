package shirei

import (
	"bytes"
	"encoding/json"
	"fmt"

	g "go.hasen.dev/generic"
)

var DEBUG_ENV = g.EnvTruthy("DEBUG")

// PROFILE_ENV enables the in-app CPU profiler panel (widgets.ProfileButton).
// Independent of DEBUG so a profile is not mixed with DebugPanel labels.
var PROFILE_ENV = g.EnvTruthy("SHIREI_PPROF")

// FPS_ENV enables the in-app frame-time panel (widgets.FPSCounter).
var FPS_ENV = g.EnvTruthy("FPS_COUNTER")

type _DebugPanel struct {
	messages    []string
	position    Vec2
	frameNumber int64
}

var _panel = _DebugPanel{position: Vec2{10, 10}}

// DebugPanel draws the floating, draggable debug overlay holding the messages
// collected via DebugMessage and DebugVar this frame (when show is true), then
// clears them for the next frame. Call it once per frame, typically at the end
// of your UI.
func DebugPanel() {
	if DEBUG_ENV && len(_panel.messages) > 0 {
		ContainerWithKey(&_panel, Attrs(FloatVec(_panel.position), Background(0, 0, 0, 0.8), Corners(4), Pad(4), Gap(4), NoAnimate), func() {
			PressAction()
			if IsActive() {
				_panel.position = Vec2Add(_panel.position, GetFrameInput().Motion)
			}
			var sz = GetResolvedSize()
			var br = Vec2Add(_panel.position, sz)
			if br[0] > GetHost().WindowSize[0] {
				_panel.position[0] = GetHost().WindowSize[0] - sz[0]
			}
			if br[1] > GetHost().WindowSize[1] {
				_panel.position[1] = GetHost().WindowSize[1] - sz[1]
			}
			for _, msg := range _panel.messages {
				Label(msg, TextColor(0, 0, 100, 1), FontSize(10), Fonts(Monospace...))
			}
		})
	}
	_panel.messages = nil
	_panel.frameNumber = ActiveUI().FrameNumber
}

// DebugMessage adds a line of text to the debug overlay for this frame. It is a
// no-op when DebugPanel isn't being called, so messages don't accumulate.
func DebugMessage(msg string) {
	if !DEBUG_ENV {
		return
	}
	if ActiveUI().FrameNumber > _panel.frameNumber+1 {
		// DebugPanel is not called; don't do anything (don't leak memory)
		return
	}
	_panel.messages = append(_panel.messages, msg)
}

// DebugVar adds a "name: value" line to the debug overlay, formatting value as
// compact JSON. Formatting is skipped when the overlay isn't accepting messages.
func DebugVar(name string, value any) {
	if !DEBUG_ENV || ActiveUI().FrameNumber > _panel.frameNumber+1 {
		return
	}
	DebugMessage(fmt.Sprintf("%s: %v", name, compactJson(value)))
}

func compactJson(value any) string {
	var buf0, _ = json.MarshalIndent(value, "", "")
	var buf bytes.Buffer
	json.Compact(&buf, buf0)
	return buf.String()
}
