package outline

import (
	"math"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

func evalCubic(p0, p1, p2, p3 [2]float32, t float32) [2]float32 {
	u := 1 - t
	a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
	return [2]float32{a*p0[0] + b*p1[0] + c*p2[0] + d*p3[0], a*p0[1] + b*p1[1] + c*p2[1] + d*p3[1]}
}

func evalQuad(p0, p1, p2 [2]float32, t float32) [2]float32 {
	u := 1 - t
	a, b, c := u*u, 2*u*t, t*t
	return [2]float32{a*p0[0] + b*p1[0] + c*p2[0], a*p0[1] + b*p1[1] + c*p2[1]}
}

func TestCubicToQuads(t *testing.T) {
	collect := func(p0, p1, p2, p3 [2]float32, tol float32) [][3][2]float32 {
		var qs [][3][2]float32
		cubicToQuads(p0, p1, p2, p3, tol, func(a, b, c [2]float32) {
			qs = append(qs, [3][2]float32{a, b, c})
		})
		return qs
	}

	t.Run("endpoints, continuity, and deviation", func(t *testing.T) {
		p0, p1, p2, p3 := [2]float32{0, 0}, [2]float32{0, 100}, [2]float32{100, 100}, [2]float32{100, 0}
		const tol = 0.5
		qs := collect(p0, p1, p2, p3, tol)
		if len(qs) < 2 {
			t.Fatalf("a curvy cubic produced %d quads, want > 1", len(qs))
		}
		if qs[0][0] != p0 || qs[len(qs)-1][2] != p3 {
			t.Errorf("endpoints not preserved: start %v end %v", qs[0][0], qs[len(qs)-1][2])
		}
		for i := 1; i < len(qs); i++ {
			if qs[i][0] != qs[i-1][2] {
				t.Errorf("gap between quad %d and %d: %v vs %v", i-1, i, qs[i-1][2], qs[i][0])
			}
		}
		// Build a dense point cloud of the emitted quads and confirm the cubic
		// never strays further than the tolerance (with slack for sampling).
		var cloud [][2]float32
		for _, q := range qs {
			for j := 0; j <= 64; j++ {
				cloud = append(cloud, evalQuad(q[0], q[1], q[2], float32(j)/64))
			}
		}
		var worst float64
		for i := 0; i <= 400; i++ {
			pc := evalCubic(p0, p1, p2, p3, float32(i)/400)
			best := math.MaxFloat64
			for _, c := range cloud {
				dx, dy := float64(pc[0]-c[0]), float64(pc[1]-c[1])
				if d := math.Hypot(dx, dy); d < best {
					best = d
				}
			}
			worst = math.Max(worst, best)
		}
		if worst > tol*1.5 {
			t.Errorf("max deviation %v exceeds tol %v (with slack)", worst, tol)
		}
	})

	t.Run("a near-quadratic cubic needs one quad", func(t *testing.T) {
		// Elevate quadratic (0,0)-(50,100)-(100,0) to a cubic; it stays exact.
		q := [2]float32{50, 100}
		p0, p3 := [2]float32{0, 0}, [2]float32{100, 0}
		p1 := [2]float32{p0[0] + 2.0/3*(q[0]-p0[0]), p0[1] + 2.0/3*(q[1]-p0[1])}
		p2 := [2]float32{p3[0] + 2.0/3*(q[0]-p3[0]), p3[1] + 2.0/3*(q[1]-p3[1])}
		if got := collect(p0, p1, p2, p3, 0.5); len(got) != 1 {
			t.Errorf("near-quadratic produced %d quads, want 1", len(got))
		}
	})

	t.Run("a finer tolerance never emits fewer quads", func(t *testing.T) {
		p0, p1, p2, p3 := [2]float32{0, 0}, [2]float32{0, 100}, [2]float32{100, 100}, [2]float32{100, 0}
		if coarse, fine := len(collect(p0, p1, p2, p3, 2)), len(collect(p0, p1, p2, p3, 0.1)); fine < coarse {
			t.Errorf("finer tol emitted %d quads, fewer than coarse %d", fine, coarse)
		}
	})
}

func TestExtractGoRegular(t *testing.T) {
	f, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	var b sfnt.Buffer
	ppem := fixed.I(int(f.UnitsPerEm()))

	idx, err := f.GlyphIndex(&b, 'o')
	if err != nil {
		t.Fatal(err)
	}
	curves, err := Extract(f, &b, idx, ppem)
	if err != nil {
		t.Fatal(err)
	}
	if len(curves) == 0 || len(curves)%6 != 0 {
		t.Fatalf("got %d floats, want a nonzero multiple of 6", len(curves))
	}

	// Space has no contours.
	sp, err := f.GlyphIndex(&b, ' ')
	if err != nil {
		t.Fatal(err)
	}
	if curves, err := Extract(f, &b, sp, ppem); err != nil || len(curves) != 0 {
		t.Fatalf("space: got %d floats (err %v), want 0", len(curves), err)
	}
}
