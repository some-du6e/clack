package chart

import "math"

// Cmd is one path command: 'M' (X, Y), 'L' (X, Y), 'C' (control points
// then the end: P[0..5]), or 'Z'.
type Cmd struct {
	Op byte
	P  [6]float64
}

// The line and area paths are ECharts' ECPolyline and ECPolygon
// (chart/line/poly.ts): drawSegment's smoothed beziers (smooth 0.5,
// smoothConstraint, smoothMonotone none), a run per stretch of values
// between nulls (connectNulls false).

func illegal(x, y float64) bool { return math.IsNaN(x) || math.IsNaN(y) }

// drawSegment draws pts[start…] in direction dir and returns how many
// points it walked.
func drawSegment(cmds []Cmd, pts []float64, start, segLen, allLen, dir int, smooth float64) ([]Cmd, int) {
	var prevX, prevY, cpx0, cpy0, cpx1, cpy1 float64
	at := func(i int) (float64, float64) {
		if i < 0 || 2*i+1 >= len(pts) {
			return math.NaN(), math.NaN() // reading past the array: undefined
		}
		return pts[2*i], pts[2*i+1]
	}
	idx := start
	k := 0
	for ; k < segLen; k++ {
		if idx >= allLen || idx < 0 {
			break
		}
		x, y := at(idx)
		if illegal(x, y) {
			break
		}
		if idx == start {
			op := byte('M')
			if dir < 0 {
				op = 'L'
			}
			cmds = append(cmds, Cmd{Op: op, P: [6]float64{x, y}})
			cpx0, cpy0 = x, y
		} else {
			dx, dy := x-prevX, y-prevY
			if dx*dx+dy*dy < 0.5 {
				idx += dir
				continue
			}
			if smooth > 0 {
				nextIdx := idx + dir
				nextX, nextY := at(nextIdx)
				for nextX == x && nextY == y && k < segLen {
					k++
					nextIdx += dir
					idx += dir
					nextX, nextY = at(nextIdx)
					x, y = at(idx)
					dx, dy = x-prevX, y-prevY
				}
				tmpK := k + 1
				var nextCpx0, nextCpy0 float64
				if tmpK >= segLen || illegal(nextX, nextY) {
					cpx1, cpy1 = x, y
				} else {
					vx, vy := nextX-prevX, nextY-prevY
					dx0, dx1, dy0, dy1 := x-prevX, nextX-x, y-prevY, nextY-y
					lenPrev := math.Sqrt(dx0*dx0 + dy0*dy0)
					lenNext := math.Sqrt(dx1*dx1 + dy1*dy1)
					ratio := lenNext / (lenNext + lenPrev)
					cpx1 = x - vx*smooth*(1-ratio)
					cpy1 = y - vy*smooth*(1-ratio)
					nextCpx0 = x + vx*smooth*ratio
					nextCpy0 = y + vy*smooth*ratio
					nextCpx0 = math.Min(nextCpx0, math.Max(nextX, x))
					nextCpy0 = math.Min(nextCpy0, math.Max(nextY, y))
					nextCpx0 = math.Max(nextCpx0, math.Min(nextX, x))
					nextCpy0 = math.Max(nextCpy0, math.Min(nextY, y))
					vx, vy = nextCpx0-x, nextCpy0-y
					cpx1 = x - vx*lenPrev/lenNext
					cpy1 = y - vy*lenPrev/lenNext
					cpx1 = math.Min(cpx1, math.Max(prevX, x))
					cpy1 = math.Min(cpy1, math.Max(prevY, y))
					cpx1 = math.Max(cpx1, math.Min(prevX, x))
					cpy1 = math.Max(cpy1, math.Min(prevY, y))
					vx, vy = x-cpx1, y-cpy1
					nextCpx0 = x + vx*lenNext/lenPrev
					nextCpy0 = y + vy*lenNext/lenPrev
				}
				cmds = append(cmds, Cmd{Op: 'C', P: [6]float64{cpx0, cpy0, cpx1, cpy1, x, y}})
				cpx0, cpy0 = nextCpx0, nextCpy0
			} else {
				cmds = append(cmds, Cmd{Op: 'L', P: [6]float64{x, y}})
			}
		}
		prevX, prevY = x, y
		idx += dir
	}
	return cmds, k
}

// polylinePath is ECPolyline.buildPath: pts as x, y pairs (NaN y: null).
func polylinePath(pts []float64, smooth float64) []Cmd {
	var cmds []Cmd
	n := len(pts) / 2
	for i := 0; i < n; {
		var k int
		cmds, k = drawSegment(cmds, pts, i, n, n, 1, smooth)
		i += k + 1
	}
	return cmds
}

// polygonPath is ECPolygon.buildPath: each run along pts, then back along
// base (the area's origin) with baseSmooth, closed.
func polygonPath(pts, base []float64, smooth, baseSmooth float64) []Cmd {
	var cmds []Cmd
	n := len(pts) / 2
	for i := 0; i < n; {
		var k int
		cmds, k = drawSegment(cmds, pts, i, n, n, 1, smooth)
		if k > 0 {
			cmds, _ = drawSegment(cmds, base, i+k-1, k, n, -1, baseSmooth)
			cmds = append(cmds, Cmd{Op: 'Z'})
		}
		i += k + 1
	}
	return cmds
}
