package slug

import (
	"bytes"
	"image/color"
	"math"
	"testing"

	"github.com/hajimehoshi/ebiten/v2"

	"github.com/numberoverzero/slug-ebiten/internal/blob"
)

func TestDilate(t *testing.T) {
	const scale = 100
	base := float32(aaMargin / scale) // uniform-scale padding

	approxEq := func(got, want float32) bool { return math.Abs(float64(got-want)) < 1e-4 }

	t.Run("uniform scale reduces to aaMargin/scale", func(t *testing.T) {
		var g ebiten.GeoM
		g.Scale(scale, scale)
		px, py, ok := dilate(&g)
		if !ok || !approxEq(px, base) || !approxEq(py, base) {
			t.Fatalf("got (%v, %v, %v), want (%v, %v, true)", px, py, ok, base, base)
		}
	})

	t.Run("pure rotation preserves padding", func(t *testing.T) {
		var g ebiten.GeoM
		g.Scale(scale, scale)
		g.Rotate(0.7)
		px, py, ok := dilate(&g)
		if !ok || !approxEq(px, base) || !approxEq(py, base) {
			t.Fatalf("got (%v, %v, %v), want (%v, %v, true)", px, py, ok, base, base)
		}
	})

	t.Run("non-uniform scale shrinks padding on the magnified axis", func(t *testing.T) {
		var g ebiten.GeoM
		g.Scale(2*scale, scale) // x stretched 2x relative to y
		px, py, ok := dilate(&g)
		// x reaches aaMargin px with half the em padding; y is unchanged.
		if !ok || !approxEq(px, base/2) || !approxEq(py, base) {
			t.Fatalf("got (%v, %v, %v), want (%v, %v, true)", px, py, ok, base/2, base)
		}
	})

	t.Run("singular transform is not drawable", func(t *testing.T) {
		var g ebiten.GeoM
		g.Scale(scale, 0)
		if _, _, ok := dilate(&g); ok {
			t.Fatal("singular transform reported drawable")
		}
	})
}

// TestShaderCompiles ensures frag.kage is valid Kage. Package init calls
// ebiten.NewShader and panics on a compile error, so merely running any test in
// this package exercises it; this names the guarantee explicitly.
func TestShaderCompiles(t *testing.T) {
	if shader == nil {
		t.Fatal("shader is nil")
	}
}

// testFont builds a font with glyph 0 as the missing-glyph (.notdef) and glyph 1
// mapped from 'A'. upem 16, so em advances are 0.375 (.notdef) and 0.5 ('A').
func testFont(t *testing.T) *Font {
	t.Helper()
	d := blob.Encode(16, 12, 4, 2,
		[]blob.GlyphOutline{
			{Advance: 6, Curves: []float32{0, 0, 6, 0, 6, 12}}, // glyph 0: .notdef
			{Advance: 8, Curves: []float32{0, 0, 8, 0, 8, 16}}, // glyph 1: 'A'
		},
		[]blob.Cmap{{Rune: 'A', Glyph: 1}},
	)
	var buf bytes.Buffer
	if err := blob.Marshal(&buf, d); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	f, err := Load(buf.Bytes())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f
}

// TestLoad checks the curve texture and glyph metadata stream through unchanged.
func TestLoad(t *testing.T) {
	f := testFont(t)
	if f.curveTex == nil {
		t.Fatal("curveTex is nil")
	}
	if w := f.curveTex.Bounds().Dx(); w != int(f.texWidth) {
		t.Errorf("texture width = %d, want %d", w, int(f.texWidth))
	}
	if got := f.glyphs[1].Advance; math.Abs(float64(got)-0.5) > 1e-6 {
		t.Errorf("glyph 1 advance = %v, want 0.5", got)
	}
}

func TestContains(t *testing.T) {
	f := testFont(t)
	if !f.Contains('A') {
		t.Error("Contains('A') = false, want true")
	}
	if f.Contains('B') {
		t.Error("Contains('B') = true, want false")
	}
}

func TestMeasure(t *testing.T) {
	f := testFont(t)
	const size = 100

	// 'A' at size 100: advance 0.5*100, ink box x [0, 50], y [-100, 0] (y-down,
	// baseline at 0, so the glyph top is negative).
	m := f.Measure([]Run{{Text: "A"}}, size)
	if m.Size != size {
		t.Errorf("Size = %v, want %v", m.Size, float64(size))
	}
	if math.Abs(m.Advance-50) > 1e-4 {
		t.Errorf("Advance = %v, want 50", m.Advance)
	}
	if math.Abs(m.MinX-0) > 1e-4 || math.Abs(m.MaxX-50) > 1e-4 ||
		math.Abs(m.MinY-(-100)) > 1e-4 || math.Abs(m.MaxY-0) > 1e-4 {
		t.Errorf("bounds = (%v,%v,%v,%v), want (0,-100,50,0)", m.MinX, m.MinY, m.MaxX, m.MaxY)
	}
	if m.LineCount != 1 {
		t.Errorf("LineCount = %v, want 1", m.LineCount)
	}
	if m.LineMetrics != f.LineMetrics(size) {
		t.Errorf("LineMetrics = %+v, want %+v", m.LineMetrics, f.LineMetrics(size))
	}

	// A missing rune measures as the .notdef fallback: advance 0.375*100.
	if got := f.Measure([]Run{{Text: "B"}}, size).Advance; math.Abs(got-37.5) > 1e-4 {
		t.Errorf("missing-rune Advance = %v, want 37.5 (.notdef)", got)
	}

	// Empty text: zero advance and an empty box, but still one line.
	if m := f.Measure([]Run{{Text: ""}}, size); m.Advance != 0 || m.MaxX != 0 || m.MinY != 0 || m.LineCount != 1 {
		t.Errorf("empty Measure = %+v, want zero advance and box, LineCount 1", m)
	}
}

func TestMeasureMultiline(t *testing.T) {
	f := testFont(t)
	const size = 100
	// Vertical metrics: ascent+descent+lineGap = (12+4+2)/16 = 1.125 em.
	lineHeight := 1.125 * size

	// "A\nA": Advance resets to the second line (0.5*100). The box spans both
	// lines: first 'A' top at y=-100, second 'A' baseline at y=lineHeight.
	m := f.Measure([]Run{{Text: "A\nA"}}, size)
	if math.Abs(m.Advance-50) > 1e-4 {
		t.Errorf("Advance = %v, want 50 (last line)", m.Advance)
	}
	if math.Abs(m.MinY-(-100)) > 1e-4 || math.Abs(m.MaxY-lineHeight) > 1e-4 {
		t.Errorf("vertical span = [%v, %v], want [-100, %v]", m.MinY, m.MaxY, lineHeight)
	}
	if math.Abs(m.MaxX-50) > 1e-4 {
		t.Errorf("MaxX = %v, want 50", m.MaxX)
	}
	if m.LineCount != 2 {
		t.Errorf("LineCount = %v, want 2", m.LineCount)
	}
}

func TestLineMetrics(t *testing.T) {
	f := testFont(t)
	// testFont vertical metrics: ascent 12/16, descent 4/16, lineGap 2/16 em.
	got := f.LineMetrics(100)
	want := LineMetrics{Ascent: 75, Descent: 25, LineGap: 12.5, LineHeight: 112.5}
	if math.Abs(got.Ascent-want.Ascent) > 1e-4 || math.Abs(got.Descent-want.Descent) > 1e-4 ||
		math.Abs(got.LineGap-want.LineGap) > 1e-4 || math.Abs(got.LineHeight-want.LineHeight) > 1e-4 {
		t.Errorf("LineMetrics(100) = %+v, want %+v", got, want)
	}
	if math.Abs(got.LineHeight-(got.Ascent+got.Descent+got.LineGap)) > 1e-4 {
		t.Errorf("LineHeight %v != Ascent+Descent+LineGap", got.LineHeight)
	}
}

func TestMeasureSplitRuns(t *testing.T) {
	f := testFont(t)
	const size = 100

	// Splitting text across runs measures the same as the concatenated string:
	// runs share one pen, '\n' breaks across runs, and per-run color is
	// irrelevant to layout.
	red := &color.RGBA{R: 0xff, A: 0xff}
	cases := []struct {
		runs []Run
		text string
	}{
		{[]Run{{Text: "A"}, {Text: "A"}}, "AA"},
		{[]Run{{Text: "A", Color: red}, {Text: "A"}}, "AA"},
		{[]Run{{Text: "A\n"}, {Text: "A"}}, "A\nA"},
		{[]Run{{Text: "B"}}, "B"}, // missing rune -> .notdef fallback in both
	}
	for _, c := range cases {
		if got, want := f.Measure(c.runs, size), f.Measure([]Run{{Text: c.text}}, size); got != want {
			t.Errorf("Measure(%v) = %+v, want Measure(%q) = %+v", c.runs, got, c.text, want)
		}
	}
}

// TestAppendRuns covers the batched vertex builder on the CPU (the GPU draw is
// verified by the helloworld example): one quad per inked glyph, the curve slice
// carried in Custom2/Custom3, per-run color in the vertex color, and rc kept in
// the glyph's own space so a single batch can place many glyphs.
func TestAppendRuns(t *testing.T) {
	f := testFont(t)
	var geom ebiten.GeoM
	geom.Scale(100, 100)
	black := premul(nil)

	v, idx := f.appendRuns(nil, nil, []Run{{Text: "A"}}, &geom, black)
	if len(v) != 4 || len(idx) != 6 {
		t.Fatalf(`appendRuns("A") = %d verts, %d indices; want 4, 6`, len(v), len(idx))
	}
	g := f.glyph('A')
	for i, vert := range v {
		if vert.Custom2 != float32(g.CurveStart) || vert.Custom3 != float32(g.CurveCount) {
			t.Errorf("vert %d curve slice = (%v, %v), want (%d, %d)", i, vert.Custom2, vert.Custom3, g.CurveStart, g.CurveCount)
		}
	}

	// '\n' advances the line but draws nothing; a singular transform draws nothing.
	if v, idx := f.appendRuns(nil, nil, []Run{{Text: "\n"}}, &geom, black); len(v) != 0 || len(idx) != 0 {
		t.Errorf("newline produced %d verts, %d indices; want 0, 0", len(v), len(idx))
	}
	var singular ebiten.GeoM
	singular.Scale(1, 0)
	if v, _ := f.appendRuns(nil, nil, []Run{{Text: "A"}}, &singular, black); len(v) != 0 {
		t.Errorf("singular transform produced %d verts; want 0", len(v))
	}

	// Per-run color lands in the (premultiplied) vertex color; rc (Custom0) stays
	// in the glyph's own space across the batch while the dst x advances.
	v, _ = f.appendRuns(nil, nil, []Run{{Text: "AA", Color: &color.RGBA{R: 0xff, A: 0xff}}}, &geom, black)
	if len(v) != 8 {
		t.Fatalf(`appendRuns("AA") = %d verts; want 8`, len(v))
	}
	if v[0].ColorR != 1 || v[0].ColorG != 0 || v[0].ColorB != 0 || v[0].ColorA != 1 {
		t.Errorf("vert color = (%v,%v,%v,%v), want premultiplied red (1,0,0,1)", v[0].ColorR, v[0].ColorG, v[0].ColorB, v[0].ColorA)
	}
	if v[4].Custom0 != v[0].Custom0 {
		t.Errorf("second glyph Custom0 = %v, want %v (rc is glyph-local, not pen-offset)", v[4].Custom0, v[0].Custom0)
	}
	if v[4].DstX <= v[0].DstX {
		t.Errorf("second glyph DstX = %v, want > first %v (pen advanced)", v[4].DstX, v[0].DstX)
	}
}
