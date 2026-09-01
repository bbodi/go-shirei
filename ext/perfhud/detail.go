package perfhud

import (
	"strings"

	wdg "go.hasen.dev/shirei/widgets"

	. "go.hasen.dev/shirei"
)

// The detail view: click a row in one of the panel's tables and the text
// behind it opens here — for the top-allocator table, the full call stack of
// every site folded into that row, with file and line, the bytes and objects
// in the scan window, and what is still live.
//
// Shirei runs one window per process, so a library cannot open a second OS
// window. This is a floating panel inside the app's own window instead. An
// app that does have a way to open a real window (spawning itself with a
// flag, the way borg does its card window) sets Config.OpenDetail and takes
// over.

var detail struct {
	open  bool
	title string
	lines []string
	// key identifies which row is open, so clicking the same row again
	// closes it and clicking a different one swaps the contents.
	key string
}

// openDetail shows text under title, or hands it to Config.OpenDetail when
// the app wants to present it itself.
func openDetail(key, title, text string) {
	if text == "" {
		return
	}
	if detail.open && detail.key == key {
		detail.open = false
		return
	}
	if Config.OpenDetail != nil && Config.OpenDetail(title, text) {
		return
	}
	detail.open = true
	detail.key = key
	detail.title = title
	detail.lines = strings.Split(strings.TrimRight(text, "\n"), "\n")
}

// CloseDetail closes the detail view.
func CloseDetail() { detail.open = false }

// detailChromeH is the title bar plus the body's padding — everything in the
// panel that is not a line of text.
const detailChromeH = 46

func clampf(v, lo, hi float32) float32 {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// drawDetail paints the detail view. It is drawn after the panel, so it sits
// above it.
func drawDetail() {
	if !detail.open {
		return
	}
	win := GetHost().WindowSize
	if win[0] <= 0 || win[1] <= 0 {
		return
	}
	th := &Config.Theme

	// Size to the content, capped by the window: a short stack should not
	// open a panel with an acre of empty space under it.
	longest := 0
	for _, line := range detail.lines {
		if n := len([]rune(line)); n > longest {
			longest = n
		}
	}
	w := clampf(float32(longest+2)*monoAdvance()+24, 360, win[0]*0.92)
	lineH := monoLineHeight() + 1 // + the Gap(1) between rows
	h := clampf(detailChromeH+float32(len(detail.lines))*lineH, 160, win[1]*0.86)
	x, y := (win[0]-w)/2, (win[1]-h)/2

	Container(Attrs(Float(x, y), InFront, FixWidth(w), FixHeight(h), Gap(0),
		BackgroundVec(th.Panel), BorderWidth(1), BorderColorVec(th.Border),
		Corners(6), BoxShadow(18)), func() {

		// title bar
		Container(Attrs(Row, CrossMid, FixWidth(w), Pad2(6, 10), Gap(8)), func() {
			Label(detail.title, small(th.Text))
			Element(Attrs(Grow(1)))
			if wdg.CtrlButton(wdg.NoIcon, "copy", true) {
				RequestTextCopy(strings.Join(detail.lines, "\n"))
			}
			if wdg.CtrlButton(wdg.NoIcon, "close", true) {
				detail.open = false
			}
		})

		// body: one label per line, so a stack keeps its indentation and the
		// long file paths stay readable rather than reflowing
		Container(Attrs(Expand, Clip, Pad2(6, 10), Gap(1), BackgroundVec(th.Plot)), func() {
			ScrollOnInput()
			wdg.ScrollBars()
			for i, line := range detail.lines {
				ContainerWithKey(i, Attrs(Row, CrossMid), func() {
					Label(line, small(detailLineColor(line)))
				})
			}
		})
	})
}

// detailLineColor grades the report so a stack can be skimmed: source
// positions recede, the "--- stack n ---" separators and the numbers stand
// out, function names read as the content.
func detailLineColor(line string) Vec4 {
	th := &Config.Theme
	trimmed := strings.TrimLeft(line, " ")
	switch {
	case strings.HasPrefix(trimmed, "---"):
		return th.RowValue
	case strings.HasPrefix(line, "      "): // a file:line under its function
		return th.Faint
	case strings.HasPrefix(line, "  "): // a function name in a stack
		return th.RowName
	default:
		return th.Dim
	}
}
