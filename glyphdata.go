package shirei

import (
	"slices"

	"github.com/cespare/xxhash/v2"
	g "go.hasen.dev/generic"
)

// GlyphRunData owns immutable line-relative glyph geometry. A Surface retains
// it across frames and shape-cache eviction. Position and uniform color belong
// to each Surface instance, so moving or recoloring a line shares its geometry.
type GlyphRunData struct {
	glyphs       []GlyphRun
	hash         uint64
	dependencies []glyphDependency
}

type glyphDependency struct {
	font  FontId
	glyph GlyphId
	em    float32
}

// NewGlyphRunData copies line-relative glyphs into an immutable render object.
// Its cached content hash and bitmap dependencies remain valid for its lifetime.
func NewGlyphRunData(glyphs []GlyphRun) *GlyphRunData {
	return ownGlyphRunData(slices.Clone(glyphs))
}

// ownGlyphRunData takes exclusive ownership of a newly constructed glyph slice.
func ownGlyphRunData(glyphs []GlyphRun) *GlyphRunData {
	d := &GlyphRunData{glyphs: glyphs, hash: xxhash.Sum64(g.UnsafeSliceBytes(glyphs))}
	seen := make(map[glyphDependency]bool)
	for _, glyph := range glyphs {
		dep := glyphDependency{glyph.FontId, glyph.GlyphId, glyph.Rect.Size[1]}
		if dep.font == 0 || dep.glyph == 0 || seen[dep] {
			continue
		}
		seen[dep] = true
		d.dependencies = append(d.dependencies, dep)
	}
	return d
}

// Len returns the number of glyphs in the immutable run.
func (d *GlyphRunData) Len() int { return len(d.glyphs) }

// GlyphRunAt returns a glyph in screen coordinates with its display color.
// Shared GlyphData uses Rect.Origin for placement and nonzero Color1 as a
// uniform color override. Otherwise GlyphRunFirst indexes the supplied buffer,
// whose glyphs already contain screen positions and colors.
func (s *Surface) GlyphRunAt(i int, runs []GlyphRun) GlyphRun {
	if s.GlyphData == nil {
		return runs[int(s.GlyphRunFirst)+i]
	}
	glyph := s.GlyphData.glyphs[i]
	glyph.Rect.Origin[0] += s.Rect.Origin[0]
	glyph.Rect.Origin[1] += s.Rect.Origin[1]
	if s.Color1 != (Vec4{}) {
		glyph.Color = s.Color1
	}
	return glyph
}

type coloredGlyphKey struct {
	geometry *GlyphRunData
	style    uint64
}
