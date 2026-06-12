// Package compile turns a TrueType or OpenType font into a slug blob, ready for
// slug.Load.
package compile

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/numberoverzero/slug-ebiten/internal/blob"
	"github.com/numberoverzero/slug-ebiten/internal/outline"
)

// GlyphRange is an inclusive range of runes to keep when subsetting a font.
type GlyphRange struct {
	Lo, Hi rune
}

// Compile turns a TrueType or OpenType font into a slug blob to be consumed by slug.Load.
//
// With no ranges, every glyph in the font is included. With one or more ranges,
// only the runes they cover are kept.  Errors if the font doesn't include a requested rune.
func Compile(fontFile []byte, ranges []GlyphRange) ([]byte, error) {
	f, err := sfnt.Parse(fontFile)
	if err != nil {
		return nil, err
	}
	if f.NumGlyphs() == 0 {
		return nil, fmt.Errorf("font has no glyphs")
	}

	upem := int(f.UnitsPerEm())
	ppem := fixed.I(upem) // design units come back unscaled

	var b sfnt.Buffer
	m, err := f.Metrics(&b, ppem, font.HintingNone)
	if err != nil {
		return nil, fmt.Errorf("metrics: %w", err)
	}
	ascent := float32(m.Ascent) / 64
	descent := float32(m.Descent) / 64
	lineGap := float32(m.Height-m.Ascent-m.Descent) / 64

	var outlines []blob.GlyphOutline
	var cmap []blob.Cmap
	if len(ranges) == 0 {
		outlines, cmap, err = all(f, &b, ppem)
	} else {
		outlines, cmap, err = subset(f, &b, ppem, ranges)
	}
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := blob.Marshal(&buf, blob.Encode(uint16(upem), ascent, descent, lineGap, outlines, cmap)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// all extracts every glyph and maps every rune in the Basic Multilingual Plane.
func all(f *sfnt.Font, b *sfnt.Buffer, ppem fixed.Int26_6) ([]blob.GlyphOutline, []blob.Cmap, error) {
	n := f.NumGlyphs()
	outlines := make([]blob.GlyphOutline, n)
	for i := range n {
		o, err := glyphOutline(f, b, sfnt.GlyphIndex(i), ppem)
		if err != nil {
			return nil, nil, err
		}
		outlines[i] = o
	}

	var cmap []blob.Cmap
	for r := rune(0); r <= 0xFFFF; r++ {
		gi, err := f.GlyphIndex(b, r)
		if err != nil {
			return nil, nil, fmt.Errorf("cmap %U: %w", r, err)
		}
		if gi != 0 {
			cmap = append(cmap, blob.Cmap{Rune: uint32(r), Glyph: uint32(gi)})
		}
	}
	return outlines, cmap, nil
}

// subset extracts glyph 0 (the missing-glyph symbol) plus the glyphs for the
// runes the ranges cover. Errors if the font doesn't include a requested rune.
func subset(f *sfnt.Font, b *sfnt.Buffer, ppem fixed.Int26_6, ranges []GlyphRange) ([]blob.GlyphOutline, []blob.Cmap, error) {
	notdef, err := glyphOutline(f, b, 0, ppem)
	if err != nil {
		return nil, nil, err
	}
	outlines := []blob.GlyphOutline{notdef}
	newIndex := map[sfnt.GlyphIndex]uint32{0: 0}

	var cmap []blob.Cmap
	var missing []rune
	for _, r := range rangeRunes(ranges) {
		gi, err := f.GlyphIndex(b, r)
		if err != nil {
			return nil, nil, fmt.Errorf("cmap %U: %w", r, err)
		}
		if gi == 0 {
			missing = append(missing, r)
			continue
		}
		ni, ok := newIndex[gi]
		if !ok {
			o, err := glyphOutline(f, b, gi, ppem)
			if err != nil {
				return nil, nil, err
			}
			ni = uint32(len(outlines))
			newIndex[gi] = ni
			outlines = append(outlines, o)
		}
		cmap = append(cmap, blob.Cmap{Rune: uint32(r), Glyph: ni})
	}
	if len(missing) > 0 {
		return nil, nil, fmt.Errorf("font is missing %d requested rune(s): %s", len(missing), formatRunes(missing))
	}
	return outlines, cmap, nil
}

func rangeRunes(ranges []GlyphRange) []rune {
	seen := map[rune]bool{}
	var runes []rune
	for _, r := range ranges {
		for c := r.Lo; c <= r.Hi; c++ {
			if !seen[c] {
				seen[c] = true
				runes = append(runes, c)
			}
		}
	}
	slices.Sort(runes)
	return runes
}

func glyphOutline(f *sfnt.Font, b *sfnt.Buffer, gi sfnt.GlyphIndex, ppem fixed.Int26_6) (blob.GlyphOutline, error) {
	curves, err := outline.Extract(f, b, gi, ppem)
	if err != nil {
		return blob.GlyphOutline{}, fmt.Errorf("glyph %d: %w", gi, err)
	}
	adv, err := f.GlyphAdvance(b, gi, ppem, font.HintingNone)
	if err != nil {
		return blob.GlyphOutline{}, fmt.Errorf("glyph %d advance: %w", gi, err)
	}
	return blob.GlyphOutline{Advance: float32(adv) / 64, Curves: curves}, nil
}

func formatRunes(rs []rune) string {
	const limit = 20
	var sb strings.Builder
	for i, r := range rs {
		if i == limit {
			fmt.Fprintf(&sb, " (+%d more)", len(rs)-limit)
			break
		}
		if i > 0 {
			sb.WriteByte(' ')
		}
		fmt.Fprintf(&sb, "U+%04X", r)
	}
	return sb.String()
}
