package blob

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	want := Encode(16, 12, 4, 2,
		[]GlyphOutline{
			{Advance: 8, Curves: []float32{0, 0, 8, 0, 8, 16}},
			{Advance: 4, Curves: nil}, // space-like
		},
		[]Cmap{{Rune: 'A', Glyph: 0}, {Rune: ' ', Glyph: 1}},
	)

	var buf bytes.Buffer
	if err := Marshal(&buf, want); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	got, err := Unmarshal(buf.Bytes())
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestEncode(t *testing.T) {
	// One quadratic in design units, upem 16: em coords (0,0) (0.5,0) (0.5,1).
	d := Encode(16, 12, 4, 2, []GlyphOutline{{Advance: 8, Curves: []float32{0, 0, 8, 0, 8, 16}}}, nil)

	// Vertical metrics convert to em like everything else (design / upem).
	if d.Ascent != 12.0/16 || d.Descent != 4.0/16 || d.LineGap != 2.0/16 {
		t.Errorf("vmetrics = (%v, %v, %v), want (0.75, 0.25, 0.125)", d.Ascent, d.Descent, d.LineGap)
	}
	if d.Bias != [2]float32{0, 0} {
		t.Errorf("bias = %v, want {0 0}", d.Bias)
	}
	if want := [2]float32{0.5 / 0xffff, 1.0 / 0xffff}; d.Scale != want {
		t.Errorf("scale = %v, want %v", d.Scale, want)
	}
	if d.TexWidth != texWidth || d.TexHeight != 1 {
		t.Errorf("tex = %dx%d, want %dx1", d.TexWidth, d.TexHeight, texWidth)
	}
	if got := len(d.Pixels); got != texWidth*4 {
		t.Errorf("pixels = %d bytes, want %d", got, texWidth*4)
	}

	g := d.Glyphs[0]
	if math.Abs(float64(g.Advance)-0.5) > 1e-6 {
		t.Errorf("advance = %v, want 0.5", g.Advance)
	}
	if g.CurveStart != 0 || g.CurveCount != 1 {
		t.Errorf("curve range = (%d,%d), want (0,1)", g.CurveStart, g.CurveCount)
	}
	if g.MinX != 0 || g.MinY != 0 || g.MaxX != 0.5 || g.MaxY != 1 {
		t.Errorf("bounds = (%v,%v,%v,%v), want (0,0,0.5,1)", g.MinX, g.MinY, g.MaxX, g.MaxY)
	}
}

// TestFixedPointRoundTrip mirrors encode and the shader's decode and checks that
// em coordinates survive the 16-bit packing below font design-unit resolution.
func TestFixedPointRoundTrip(t *testing.T) {
	bias := float32(-0.3)
	span := float32(1.6) // [-0.3, 1.3], with overshoot
	scale := span / 0xffff

	decode := func(u uint16) float32 {
		hi := math.Floor(float64(u>>8) + 0.5)
		lo := math.Floor(float64(u&0xff) + 0.5)
		return bias + float32(hi*256+lo)*scale
	}

	tol := scale // one fixed-point step
	if worstUnit := float32(1.0 / 2048.0); tol >= worstUnit {
		t.Fatalf("packing resolution %v not finer than a 2048-upem unit %v", tol, worstUnit)
	}
	for i := 0; i <= 1000; i++ {
		v := bias + span*float32(i)/1000
		if got := decode(encode(v, bias, scale)); math.Abs(float64(got-v)) > float64(tol) {
			t.Fatalf("v=%v decoded=%v err=%v > tol=%v", v, got, math.Abs(float64(got-v)), tol)
		}
	}
}

func TestUnmarshalBadMagic(t *testing.T) {
	if _, err := Unmarshal([]byte("XXXXrest")); !errors.Is(err, ErrBadMagic) {
		t.Fatalf("got %v, want ErrBadMagic", err)
	}
}

func TestUnmarshalBadVersion(t *testing.T) {
	var buf bytes.Buffer
	if err := Marshal(&buf, &Data{TexWidth: 1, TexHeight: 1, Pixels: make([]byte, 4)}); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	b := buf.Bytes()
	b[4]++ // bump the version field (first byte after magic)
	if _, err := Unmarshal(b); !errors.Is(err, ErrVersion) {
		t.Fatalf("got %v, want ErrVersion", err)
	}
}
