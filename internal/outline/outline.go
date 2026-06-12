// Package outline extracts glyph outlines from a font as a flat list of
// quadratic Bezier control points (6 floats per curve), in design units with Y
// up to match the shader's winding.
package outline

import (
	"errors"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// cubicTolerance is the max deviation (as a fraction of an em) when
// approximating a cubic with quadratics. ~0.0002 em is well under a pixel at any
// practical size.
const cubicTolerance = 0.0002

// Extract returns the glyph's outline at the given ppem. Pass ppem equal to the
// font's units-per-em to get coordinates in design units. A glyph with no
// contours (e.g. space) returns a nil slice.
func Extract(f *sfnt.Font, b *sfnt.Buffer, idx sfnt.GlyphIndex, ppem fixed.Int26_6) ([]float32, error) {
	segs, err := f.LoadGlyph(b, idx, ppem, nil)
	if err != nil {
		if errors.Is(err, sfnt.ErrColoredGlyph) {
			return nil, nil // not representable as an outline; skip
		}
		return nil, err
	}

	var out []float32
	var cur, start [2]float32
	open := false

	pt := func(p fixed.Point26_6) [2]float32 {
		return [2]float32{float32(p.X) / 64, -float32(p.Y) / 64} // negate for y-up
	}
	addQuad := func(p0, p1, p2 [2]float32) {
		if p0 == p1 && p1 == p2 {
			return // skip zero-length
		}
		out = append(out, p0[0], p0[1], p1[0], p1[1], p2[0], p2[1])
	}

	// Cubic segments subdivide to within tol of the curve.
	tol := float32(ppem) / 64 * cubicTolerance

	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			if open && cur != start {
				addQuad(cur, mid(cur, start), start)
			}
			cur = pt(s.Args[0])
			start = cur
			open = true
		case sfnt.SegmentOpLineTo:
			e := pt(s.Args[0])
			addQuad(cur, mid(cur, e), e)
			cur = e
		case sfnt.SegmentOpQuadTo:
			c := pt(s.Args[0])
			e := pt(s.Args[1])
			addQuad(cur, c, e)
			cur = e
		case sfnt.SegmentOpCubeTo:
			e := pt(s.Args[2])
			cubicToQuads(cur, pt(s.Args[0]), pt(s.Args[1]), e, tol, addQuad)
			cur = e
		}
	}
	if open && cur != start {
		addQuad(cur, mid(cur, start), start)
	}
	return out, nil
}
