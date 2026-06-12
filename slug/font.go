// Package slug renders text from glyph outlines with per-pixel GPU curve
// coverage, for sharp text at any size on Ebitengine.
package slug

import (
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/numberoverzero/slug-ebiten/internal/blob"
)

//go:embed frag.kage
var shaderSrc []byte

var shader *ebiten.Shader

func init() {
	var err error
	shader, err = ebiten.NewShader(shaderSrc)
	if err != nil {
		panic("shader compile error: " + err.Error())
	}
}

// Font is a compiled font ready to draw. Create one with Load.
type Font struct {
	glyphs   []blob.Glyph
	curveTex *ebiten.Image
	texWidth float32
	bias     [2]float32
	scale    [2]float32
	// Vertical metrics in em units. Their sum is the line height Draw uses for '\n'.
	ascent, descent, lineGap float32
	cmap                     map[rune]uint32
}

func (f *Font) lineHeightEm() float32 { return f.ascent + f.descent + f.lineGap }

// Load reads a compiled font blob into a Font.
// Errors if the blob is malformed or has no glyphs.
func Load(data []byte) (*Font, error) {
	d, err := blob.Unmarshal(data)
	if err != nil {
		return nil, err
	}
	// need at least one glyph for fallback rendering.
	if len(d.Glyphs) == 0 {
		return nil, fmt.Errorf("slug: blob has no glyphs")
	}

	img := ebiten.NewImageWithOptions(image.Rect(0, 0, int(d.TexWidth), int(d.TexHeight)), &ebiten.NewImageOptions{Unmanaged: true})
	img.WritePixels(d.Pixels)

	f := &Font{
		glyphs:   d.Glyphs,
		curveTex: img,
		texWidth: float32(d.TexWidth),
		bias:     d.Bias,
		scale:    d.Scale,
		ascent:   d.Ascent,
		descent:  d.Descent,
		lineGap:  d.LineGap,
		cmap:     make(map[rune]uint32, len(d.Cmap)),
	}
	for _, e := range d.Cmap {
		f.cmap[rune(e.Rune)] = e.Glyph
	}
	return f, nil
}

// DrawOptions controls a Draw call.
//
// GeoM places and sizes the text. Build it with Scale and Translate, plus
// Rotate or Skew. The zero value draws one-pixel-per-em text at the origin.
type DrawOptions struct {
	// GeoM places and sizes the text.
	GeoM ebiten.GeoM
	// Color is the fallback color for runs that don't set their own.
	Color color.Color
	// OpticalWeight makes thin strokes read heavier at small sizes.
	OpticalWeight bool
}

// Run is one colored span of text in a Draw call. Pass several runs to draw
// multi-colored text in one call. They share the call's GeoM, so they differ
// only in color.
type Run struct {
	// Text is the run's text. A '\n' starts a new line.
	Text string
	// Color is the run's color. Nil falls back to DrawOptions.Color.
	Color color.Color
}

// aaMargin expands each glyph's quad beyond its outline bounding box, in
// destination pixels, so the shader's antialiasing falloff is not clipped by
// the quad edge. The coverage shader fades over about one pixel at the outline
// edge, so the quad must reach at least that far. It is a correctness margin
// since ebiten does not support vertex shaders.
const aaMargin = 1.5

// dilate returns the per-axis em-space padding that keeps an aaMargin-pixel
// border in dst around a glyph after the em->dst map geo, so the antialiasing
// falloff is never clipped. For a uniform scale S it reduces to aaMargin/S. ok
// is false for a singular transform.
//
// geo's linear columns are u = (a, c) for the x axis and v = (b, d) for the y
// axis. Expanding the em-x extent by aaMargin*|v|/|det| and the em-y extent by
// aaMargin*|u|/|det| moves each edge out by aaMargin pixels along its dst normal.
func dilate(geo *ebiten.GeoM) (padX, padY float32, ok bool) {
	a, b := geo.Element(0, 0), geo.Element(0, 1)
	c, d := geo.Element(1, 0), geo.Element(1, 1)
	det := a*d - b*c
	if math.Abs(det) < 1e-9 {
		return 0, 0, false
	}
	inv := 1 / math.Abs(det)
	return float32(aaMargin * math.Hypot(b, d) * inv), float32(aaMargin * math.Hypot(a, c) * inv), true
}

// appendRuns appends each run's glyph quads to verts/indices and returns the
// grown slices. Layout matches Draw: left to right from the origin, '\n' breaks
// a line, '\r' is ignored, missing runes use glyph 0. Each run's premultiplied
// color goes into its vertices, so one batch can mix colors. A singular geo
// appends nothing.
//
// uint16 indices cap a batch at 65536 vertices (16384 glyphs); longer text needs
// splitting.
func (f *Font) appendRuns(verts []ebiten.Vertex, indices []uint16, runs []Run, geo *ebiten.GeoM, fallback [4]float32) ([]ebiten.Vertex, []uint16) {
	padX, padY, ok := dilate(geo)
	if !ok {
		return verts, indices
	}
	lineHeight := f.lineHeightEm()
	penX, penY := float32(0), float32(0)

	// corner emits one quad vertex. cx, cy are the glyph-space curve coords the
	// shader reads as rc (no pen offset); the dst position adds the pen and maps
	// through geo. col/cs/cc are set per glyph below.
	var col [4]float32
	var cs, cc float32
	corner := func(cx, cy float32) ebiten.Vertex {
		x, y := geo.Apply(float64(penX+cx), float64(penY-cy))
		return ebiten.Vertex{
			DstX:    float32(x),
			DstY:    float32(y),
			ColorR:  col[0],
			ColorG:  col[1],
			ColorB:  col[2],
			ColorA:  col[3],
			Custom0: cx,
			Custom1: cy,
			Custom2: cs,
			Custom3: cc,
		}
	}

	for ri := range runs {
		col = fallback
		if runs[ri].Color != nil {
			col = premul(runs[ri].Color)
		}
		for _, r := range runs[ri].Text {
			switch r {
			case '\n':
				penX, penY = 0, penY+lineHeight
				continue
			case '\r':
				continue
			}
			g := f.glyph(r)
			if g.CurveCount > 0 {
				cs, cc = float32(g.CurveStart), float32(g.CurveCount)
				minX, minY := g.MinX-padX, g.MinY-padY
				maxX, maxY := g.MaxX+padX, g.MaxY+padY
				base := uint16(len(verts))
				verts = append(verts, corner(minX, minY), corner(maxX, minY), corner(maxX, maxY), corner(minX, maxY))
				indices = append(indices, base, base+1, base+2, base, base+2, base+3)
			}
			penX += g.Advance
		}
	}
	return verts, indices
}

// Draw renders runs of text from the pen origin, left to right. Pass several
// runs for multi-colored text and they all draw in one call. A '\n' starts a
// new line and '\r' is ignored. Runes the font lacks render as its missing-glyph
// symbol. Draw does nothing if opts is nil.
//
// opts.GeoM sizes and places the text. To draw size-px text with its baseline
// left corner at (x, y):
//
//	var g ebiten.GeoM
//	g.Scale(size, size) // size is pixels per em
//	g.Translate(x, y)
//	f.Draw(dst, []slug.Run{{Text: "hi"}}, &slug.DrawOptions{GeoM: g, Color: color.Black})
//
// Add g.Rotate or g.Skew before Translate to rotate or skew about the pen origin.
func (f *Font) Draw(dst *ebiten.Image, runs []Run, opts *DrawOptions) {
	if opts == nil {
		return
	}
	geo := opts.GeoM
	verts, indices := f.appendRuns(nil, nil, runs, &geo, premul(opts.Color))
	if len(indices) == 0 {
		return
	}

	weight := 0
	if opts.OpticalWeight {
		weight = 1
	}
	sopts := &ebiten.DrawTrianglesShaderOptions{}
	sopts.Images[0] = f.curveTex
	sopts.Uniforms = map[string]any{
		"TexWidth":      f.texWidth,
		"CurveBias":     f.bias[:],
		"CurveScale":    f.scale[:],
		"OpticalWeight": weight,
	}
	dst.DrawTrianglesShader(verts, indices, shader, sopts)
}

// Contains reports whether the font has a glyph for r.
func (f *Font) Contains(r rune) bool {
	_, ok := f.cmap[r]
	return ok
}

// glyph returns the glyph for r, falling back to the missing-glyph (index 0)
// when r is not in the font.
func (f *Font) glyph(r rune) blob.Glyph {
	if gi, ok := f.cmap[r]; ok {
		return f.glyphs[gi]
	}
	return f.glyphs[0]
}

// Metrics is the result of Measure, in destination pixels at the measured size.
// It uses the same frame Draw lays out in, before opts.GeoM: x runs right along
// the baseline and y runs down, with the baseline at y=0. So MinY is usually
// negative (above the baseline) and MaxY positive. Apply your GeoM to place the
// box where Draw would.
type Metrics struct {
	// Size is the size passed to Measure.
	Size float64
	// Advance is where the next glyph would start, the pen travel of the last
	// line. For multi-line text the bounding box is the better width.
	Advance float64
	// MinX, MinY, MaxX, MaxY is the ink bounding box, unioned over all lines.
	// Empty or whitespace-only text gives a zero box.
	MinX, MinY, MaxX, MaxY float64
	// LineCount is the number of lines the text lays out to: 0 when there are no
	// runs, otherwise one more than the total '\n' count (blank lines included).
	LineCount int
	// LineMetrics is the font's vertical metrics at the measured size. It does
	// not depend on the text; it is included so callers can derive the line-box
	// height (LineCount * LineHeight), baselines, or ascent/descent without a
	// second call.
	LineMetrics LineMetrics
}

// Measure reports the combined layout of runs at the given em size without
// drawing them. See Metrics for the frame of reference.
func (f *Font) Measure(runs []Run, size float64) Metrics {
	m := Metrics{Size: size, LineMetrics: f.LineMetrics(size)}
	// Any runs at all start on line 1; each '\n' opens another, so blank lines
	// still add height. No runs means no lines and zero height.
	if len(runs) > 0 {
		m.LineCount = 1
	}
	scale := float32(size)
	lineHeight := f.lineHeightEm() * scale
	penX, penY := float32(0), float32(0)
	inked := false
	for ri := range runs {
		for _, r := range runs[ri].Text {
			switch r {
			case '\n':
				penX, penY = 0, penY+lineHeight
				m.LineCount++
				continue
			case '\r':
				continue
			}
			g := f.glyph(r)
			if g.CurveCount > 0 {
				// em is y-up but local space is y-down, so glyph top MaxY maps to min y.
				minX, maxX := float64(penX+g.MinX*scale), float64(penX+g.MaxX*scale)
				minY, maxY := float64(penY-g.MaxY*scale), float64(penY-g.MinY*scale)
				if !inked {
					m.MinX, m.MinY, m.MaxX, m.MaxY = minX, minY, maxX, maxY
					inked = true
				} else {
					m.MinX, m.MinY = min(m.MinX, minX), min(m.MinY, minY)
					m.MaxX, m.MaxY = max(m.MaxX, maxX), max(m.MaxY, maxY)
				}
			}
			penX += g.Advance * scale
		}
	}
	m.Advance = float64(penX)
	return m
}

// LineMetrics holds a font's vertical metrics in pixels at a given size. Use it
// to size or vertically center a text box, or to set line spacing.
// LineHeight is the distance Draw steps down per '\n'.
type LineMetrics struct {
	Ascent     float64 // baseline to top, positive
	Descent    float64 // baseline to bottom, positive
	LineGap    float64 // extra leading between lines
	LineHeight float64 // Ascent + Descent + LineGap
}

// LineMetrics returns the font's vertical metrics at the given size. Unlike
// Measure they do not depend on the text.
func (f *Font) LineMetrics(size float64) LineMetrics {
	return LineMetrics{
		Ascent:     float64(f.ascent) * size,
		Descent:    float64(f.descent) * size,
		LineGap:    float64(f.lineGap) * size,
		LineHeight: float64(f.lineHeightEm()) * size,
	}
}

func premul(c color.Color) [4]float32 {
	if c == nil {
		return [4]float32{0, 0, 0, 1}
	}
	r, g, b, a := c.RGBA() // already alpha-premultiplied, 0..0xffff
	const m = 0xffff
	return [4]float32{float32(r) / m, float32(g) / m, float32(b) / m, float32(a) / m}
}
