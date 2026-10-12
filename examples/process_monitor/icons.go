package main

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"strconv"
	"sync"
	"time"

	. "go.hasen.dev/shirei"
)

// One extract at a time, and not more often than this. NSWorkspace /
// SHGetFileInfo is expensive; a CPU/power sort changes the visible set
// every sample, and firing one AppKit call per new row would put this
// process over the 10% idle budget.
const iconFetchGap = 350 * time.Millisecond

// iconHold is how long a path must keep being requested (stay on screen)
// before we extract its icon. Rows that flicker through a CPU sort do not
// pay for AppKit.
const iconHold = 3 * time.Second

type iconKey struct {
	path string
	px   int
}

// iconStore decodes executable icons off the frame path and bakes them to
// the device-pixel size they will be drawn at. Get is safe from the UI; a
// miss enqueues a fetch and returns nil until a later frame.
type iconStore struct {
	mu        sync.Mutex
	raw       map[string]*image.RGBA
	sized     map[iconKey]*image.RGBA
	wanted    map[string]map[int]struct{}
	pid       map[string]int
	pending   map[string]bool
	firstSeen map[string]time.Time
	queue     []string
	wake      chan struct{}
}

var icons = &iconStore{
	raw:       make(map[string]*image.RGBA),
	sized:     make(map[iconKey]*image.RGBA),
	wanted:    make(map[string]map[int]struct{}),
	pid:       make(map[string]int),
	pending:   make(map[string]bool),
	firstSeen: make(map[string]time.Time),
	wake:      make(chan struct{}, 1),
}

func init() {
	go icons.loop()
}

func (s *iconStore) reset() {
	s.mu.Lock()
	s.raw = make(map[string]*image.RGBA)
	s.sized = make(map[iconKey]*image.RGBA)
	s.wanted = make(map[string]map[int]struct{})
	s.pid = make(map[string]int)
	s.pending = make(map[string]bool)
	s.firstSeen = make(map[string]time.Time)
	s.queue = nil
	s.mu.Unlock()
}

func (s *iconStore) idle() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue) == 0 && len(s.pending) == 0
}

func (s *iconStore) noteWanted(path string, px int) {
	m := s.wanted[path]
	if m == nil {
		m = make(map[int]struct{})
		s.wanted[path] = m
	}
	m[px] = struct{}{}
}

func (s *iconStore) Get(path string, pid, px int) *image.RGBA {
	if path == "" || px < 1 {
		return nil
	}
	key := iconKey{path, px}

	s.mu.Lock()
	if pid > 0 {
		s.pid[path] = pid
	}
	if img, ok := s.sized[key]; ok {
		s.mu.Unlock()
		if img == nil || img.Bounds().Dx() == 0 {
			return nil
		}
		return img
	}
	if raw, ok := s.raw[path]; ok {
		s.mu.Unlock()
		if raw == nil || raw.Bounds().Dx() == 0 {
			s.mu.Lock()
			s.sized[key] = raw
			s.mu.Unlock()
			return nil
		}
		baked := scaleIcon(raw, px)
		s.mu.Lock()
		if cur, exists := s.sized[key]; exists {
			baked = cur
		} else {
			s.sized[key] = baked
		}
		s.mu.Unlock()
		if baked == nil || baked.Bounds().Dx() == 0 {
			return nil
		}
		return baked
	}
	s.noteWanted(path, px)
	seen, ok := s.firstSeen[path]
	if !ok {
		s.firstSeen[path] = time.Now()
		s.mu.Unlock()
		return nil
	}
	if time.Since(seen) < iconHold || s.pending[path] {
		s.mu.Unlock()
		return nil
	}
	s.pending[path] = true
	s.queue = append(s.queue, path)
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

func (s *iconStore) loop() {
	var last time.Time
	for range s.wake {
		for {
			s.mu.Lock()
			if len(s.queue) == 0 {
				s.mu.Unlock()
				break
			}
			path := s.queue[0]
			s.queue = s.queue[1:]
			s.mu.Unlock()

			if d := iconFetchGap - time.Since(last); d > 0 {
				time.Sleep(d)
			}
			s.fetch(path)
			last = time.Now()
		}
	}
}

func (s *iconStore) fetch(path string) {
	s.mu.Lock()
	pid := s.pid[path]
	s.mu.Unlock()

	var img *image.RGBA
	if raw := IconPNG(pid, path); len(raw) > 0 {
		if decoded, err := png.Decode(bytes.NewReader(raw)); err == nil {
			img = asRGBA(decoded)
		}
	}
	if img == nil {
		img = image.NewRGBA(image.Rect(0, 0, 0, 0))
	}

	s.mu.Lock()
	s.raw[path] = img
	var wants []int
	for px := range s.wanted[path] {
		wants = append(wants, px)
	}
	delete(s.pending, path)
	s.mu.Unlock()

	if img.Bounds().Dx() == 0 {
		s.mu.Lock()
		for _, px := range wants {
			s.sized[iconKey{path, px}] = img
		}
		s.mu.Unlock()
		RequestNextFrame()
		return
	}
	for _, px := range wants {
		baked := scaleIcon(img, px)
		s.mu.Lock()
		s.sized[iconKey{path, px}] = baked
		s.mu.Unlock()
	}
	RequestNextFrame()
}

// scaleIcon bakes src to a square of `side` device pixels. This is the only
// resample; paint then 1:1-blits.
func scaleIcon(src *image.RGBA, side int) *image.RGBA {
	if src == nil || side < 1 {
		return src
	}
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw == side && sh == side {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		sy := b.Min.Y + y*sh/side
		for x := 0; x < side; x++ {
			sx := b.Min.X + x*sw/side
			copy(dst.Pix[dst.PixOffset(x, y):dst.PixOffset(x, y)+4],
				src.Pix[src.PixOffset(sx, sy):src.PixOffset(sx, sy)+4])
		}
	}
	return dst
}

func asRGBA(src image.Image) *image.RGBA {
	if src == nil {
		return nil
	}
	if r, ok := src.(*image.RGBA); ok {
		return r
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

func iconDevicePx(size f32) int {
	scale := GetHost().WindowScale
	if scale < 0.5 {
		scale = 1
	}
	px := int(size*scale + 0.5)
	if px < 1 {
		px = 1
	}
	return px
}

// processIconView draws the executable icon in a fixed logical box. The
// bitmap is baked at size×WindowScale so paint is a 1:1 blit. Row (16) and
// detail (20) use separate baked sizes and UseImage keys.
func processIconView(pid int, path string, size f32) {
	px := iconDevicePx(size)
	img := icons.Get(path, pid, px)
	if img == nil {
		Element(Attrs(FixSize(size, size)))
		return
	}
	ImageViewAt(UseImage("proc-icon:"+path+":"+strconv.Itoa(px), img), Vec2{size, size})
}
