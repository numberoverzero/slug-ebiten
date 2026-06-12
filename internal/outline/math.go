package outline

// This file is pure Bézier math: mapping a cubic to quadratics, with no font,
// glyph, or unit concepts. Points are [x, y] in whatever space the caller uses.

const (
	// cubicMaxDepth caps the recursion in case of a degenerate control polygon.
	cubicMaxDepth = 10
	// cubicErrCoeffSq is (sqrt(3)/36)^2, the squared coefficient in the closed-
	// form bound on a single quadratic's deviation from the cubic it replaces.
	cubicErrCoeffSq = 3.0 / (36.0 * 36.0)
)

func mid(a, c [2]float32) [2]float32 {
	return [2]float32{(a[0] + c[0]) / 2, (a[1] + c[1]) / 2}
}

// cubicToQuads approximates the cubic Bézier from p0 to p3 (control points p1,
// p2) with quadratic segments that stay within tol of the curve, emitting each
// as (start, control, end) via emit.
func cubicToQuads(p0, p1, p2, p3 [2]float32, tol float32, emit func(a, b, c [2]float32)) {
	cubicSeg(p0, p1, p2, p3, tol, 0, emit)
}

func cubicSeg(p0, p1, p2, p3 [2]float32, tol float32, depth int, emit func(a, b, c [2]float32)) {
	// The single quadratic matching the endpoints and end tangents deviates from
	// the cubic by at most (sqrt(3)/36)*|p3-3p2+3p1-p0|. Subdivide until that is
	// within tol, or the depth guard trips on a degenerate control polygon.
	dx := p3[0] - 3*p2[0] + 3*p1[0] - p0[0]
	dy := p3[1] - 3*p2[1] + 3*p1[1] - p0[1]
	if depth >= cubicMaxDepth || cubicErrCoeffSq*(dx*dx+dy*dy) <= tol*tol {
		q := [2]float32{
			(3*p1[0] - p0[0] + 3*p2[0] - p3[0]) / 4,
			(3*p1[1] - p0[1] + 3*p2[1] - p3[1]) / 4,
		}
		emit(p0, q, p3)
		return
	}
	// de Casteljau split at t = 0.5 into two cubics.
	p01, p12, p23 := mid(p0, p1), mid(p1, p2), mid(p2, p3)
	p012, p123 := mid(p01, p12), mid(p12, p23)
	m := mid(p012, p123)
	cubicSeg(p0, p01, p012, m, tol, depth+1, emit)
	cubicSeg(m, p123, p23, p3, tol, depth+1, emit)
}
