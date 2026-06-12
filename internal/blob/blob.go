// Package blob defines the on-disk font container format: Encode (the compile
// side) packs glyph outlines into it, and Unmarshal (the runtime side) reads it
// back.
//
// Layout (little-endian):
//
//	magic   [4]byte "SLUG"
//	header  {version uint16; texW, texH, numGlyphs, numPixels, numCmap uint32; bias, scale [2]float32; ascent, descent, lineGap float32}
//	glyphs  [numGlyphs]Glyph
//	pixels  [numPixels]byte            // RGBA8 curve texture, texW*texH*4 bytes
//	cmap    [numCmap]Cmap              // sorted by Rune
package blob

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Magic identifies a font blob.
var Magic = [4]byte{'S', 'L', 'U', 'G'}

// Version is the blob format version.
const Version uint16 = 0

// texWidth is the curve texture width in texels. 4096 is safe across ebiten backends.
const texWidth = 4096

// Glyph holds a glyph's measurements in em units.
type Glyph struct {
	Advance                float32
	CurveStart, CurveCount uint32 // indices into the curves, counted in curves
	MinX, MinY, MaxX, MaxY float32
}

// Cmap maps a rune to a glyph index.
type Cmap struct {
	Rune  uint32
	Glyph uint32
}

// GlyphOutline is a per-glyph input to Encode: an advance and a flat list of
// quadratic control points (6 floats per curve) in design units.
type GlyphOutline struct {
	Advance float32
	Curves  []float32
}

// Data is the decoded contents of a blob.
type Data struct {
	Bias      [2]float32 // em coordinate that fixed-point 0 decodes to, per axis
	Scale     [2]float32 // em units per fixed-point step, per axis
	Ascent    float32    // baseline to top, em units, positive up
	Descent   float32    // baseline to bottom, em units, positive down
	LineGap   float32    // extra leading between lines, em units
	TexWidth  uint32
	TexHeight uint32
	Pixels    []byte // RGBA8: one texel per control point, x in R,G and y in B,A
	Glyphs    []Glyph
	Cmap      []Cmap
}

// Errors returned when a blob cannot be read.
var (
	ErrBadMagic = errors.New("blob: bad magic")
	ErrVersion  = errors.New("blob: unsupported version")
)

type header struct {
	Version   uint16
	TexWidth  uint32
	TexHeight uint32
	NumGlyphs uint32
	NumPixels uint32
	NumCmap   uint32
	Bias      [2]float32
	Scale     [2]float32
	Ascent    float32
	Descent   float32
	LineGap   float32
}

// Encode compiles design-unit glyph outlines into a Data struct.
func Encode(unitsPerEm uint16, ascent, descent, lineGap float32, glyphs []GlyphOutline, cmap []Cmap) *Data {
	inv := float32(1) / float32(unitsPerEm)

	var curves []float32
	out := make([]Glyph, len(glyphs))
	for i, g := range glyphs {
		out[i] = Glyph{
			Advance:    g.Advance * inv,
			CurveStart: uint32(len(curves) / 6),
			CurveCount: uint32(len(g.Curves) / 6),
		}
		for _, c := range g.Curves {
			curves = append(curves, c*inv)
		}
	}

	bias, scale := extents(curves)
	for i := range out {
		out[i].MinX, out[i].MinY, out[i].MaxX, out[i].MaxY = bounds(curves, out[i].CurveStart, out[i].CurveCount)
	}
	pixels, w, h := pack(curves, bias, scale)

	return &Data{
		Bias:      bias,
		Scale:     scale,
		Ascent:    ascent * inv,
		Descent:   descent * inv,
		LineGap:   lineGap * inv,
		TexWidth:  w,
		TexHeight: h,
		Pixels:    pixels,
		Glyphs:    out,
		Cmap:      cmap,
	}
}

// extents returns the bias and per-step scale mapping the curves' em range onto
// 16-bit fixed point.
func extents(curves []float32) (bias, scale [2]float32) {
	var minX, minY, maxX, maxY float32
	for i := 0; i+1 < len(curves); i += 2 {
		x, y := curves[i], curves[i+1]
		if i == 0 {
			minX, maxX, minY, maxY = x, x, y, y
			continue
		}
		minX, maxX = min(minX, x), max(maxX, x)
		minY, maxY = min(minY, y), max(maxY, y)
	}
	spanX, spanY := maxX-minX, maxY-minY
	if spanX <= 0 {
		spanX = 1
	}
	if spanY <= 0 {
		spanY = 1
	}
	return [2]float32{minX, minY}, [2]float32{spanX / 0xffff, spanY / 0xffff}
}

// pack writes the control points into an RGBA8 buffer, one texel per point: x in
// the R,G bytes and y in the B,A bytes as 16-bit fixed point.
func pack(curves []float32, bias, scale [2]float32) (pixels []byte, width, height uint32) {
	numTexels := len(curves) / 2 // two floats per point, one texel per point
	h := max((numTexels+texWidth-1)/texWidth, 1)
	buf := make([]byte, texWidth*h*4)
	for i, off := 0, 0; i+1 < len(curves); i, off = i+2, off+4 {
		ux := encode(curves[i], bias[0], scale[0])
		uy := encode(curves[i+1], bias[1], scale[1])
		buf[off], buf[off+1] = byte(ux>>8), byte(ux)
		buf[off+2], buf[off+3] = byte(uy>>8), byte(uy)
	}
	return buf, texWidth, uint32(h)
}

func encode(v, bias, scale float32) uint16 {
	u := (v - bias) / scale
	if u < 0 {
		u = 0
	}
	if u > 0xffff {
		u = 0xffff
	}
	return uint16(u + 0.5)
}

func bounds(curves []float32, start, count uint32) (minX, minY, maxX, maxY float32) {
	if count == 0 {
		return
	}
	base := start * 6
	minX, maxX = curves[base], curves[base]
	minY, maxY = curves[base+1], curves[base+1]
	for i := uint32(0); i < count*6; i += 2 {
		x, y := curves[base+i], curves[base+i+1]
		minX, maxX = min(minX, x), max(maxX, x)
		minY, maxY = min(minY, y), max(maxY, y)
	}
	return
}

// Marshal encodes d to w.
func Marshal(w io.Writer, d *Data) error {
	if _, err := w.Write(Magic[:]); err != nil {
		return err
	}
	h := header{
		Version:   Version,
		TexWidth:  d.TexWidth,
		TexHeight: d.TexHeight,
		NumGlyphs: uint32(len(d.Glyphs)),
		NumPixels: uint32(len(d.Pixels)),
		NumCmap:   uint32(len(d.Cmap)),
		Bias:      d.Bias,
		Scale:     d.Scale,
		Ascent:    d.Ascent,
		Descent:   d.Descent,
		LineGap:   d.LineGap,
	}
	for _, v := range []any{h, d.Glyphs, d.Pixels, d.Cmap} {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}
	return nil
}

// Unmarshal decodes a blob.
func Unmarshal(b []byte) (*Data, error) {
	r := bytes.NewReader(b)

	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, err
	}
	if magic != Magic {
		return nil, ErrBadMagic
	}

	var h header
	if err := binary.Read(r, binary.LittleEndian, &h); err != nil {
		return nil, err
	}
	if h.Version != Version {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrVersion, h.Version, Version)
	}

	d := &Data{
		Bias:      h.Bias,
		Scale:     h.Scale,
		Ascent:    h.Ascent,
		Descent:   h.Descent,
		LineGap:   h.LineGap,
		TexWidth:  h.TexWidth,
		TexHeight: h.TexHeight,
		Pixels:    make([]byte, h.NumPixels),
		Glyphs:    make([]Glyph, h.NumGlyphs),
		Cmap:      make([]Cmap, h.NumCmap),
	}
	for _, v := range []any{d.Glyphs, d.Pixels, d.Cmap} {
		if err := binary.Read(r, binary.LittleEndian, v); err != nil {
			return nil, err
		}
	}
	return d, nil
}
