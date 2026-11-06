package icons

import "testing"

func TestAllIconsParse(t *testing.T) {
	for _, n := range Names() {
		d := registry[n]
		for i, p := range d.parts {
			segs := parsePath(p.d)
			if len(segs) == 0 || segs[0].op != 'M' {
				t.Errorf("%s part %d: bad path %q -> %v", n, i, p.d, segs)
			}
			for _, s := range segs {
				for _, pt := range s.p {
					if pt.X < -1 || pt.X > 21 || pt.Y < -1 || pt.Y > 21 || pt.X != pt.X {
						t.Errorf("%s part %d: point out of viewbox %v", n, i, pt)
					}
				}
			}
		}
	}
}

func TestParseRelativeAndArc(t *testing.T) {
	segs := parsePath("M2 2 h4 v4 l-4 0 z M10 10 a2 2 0 1 1 4 0")
	if len(segs) < 6 {
		t.Fatalf("segs = %v", segs)
	}
	last := segs[len(segs)-1]
	if last.op != 'C' || last.p[2].X != 14 || last.p[2].Y != 10 {
		t.Fatalf("arc end = %v", last)
	}
}
