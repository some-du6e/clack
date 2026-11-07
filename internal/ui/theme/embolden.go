package theme

import (
	"container/list"
	"math"
	"sync"

	"gioui.org/f32"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/text"
	gtfont "github.com/go-text/typesetting/font"
	gtot "github.com/go-text/typesetting/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Emboldened outlines.
//
// Dark text used to get its extra ink from a hairline clip.Stroke of every
// glyph line on top of the fill. Gio tessellates strokes on the CPU into
// several quads per outline segment and keeps them in GPU buffers (process
// memory on software GL): with a channel open that was ~220 MB of vertex
// cache plus as much again in buffers, and the stroke pass was about half
// of every text-heavy frame.
//
// EmboldenedShape instead pushes each contour outward by w/2 (FreeType's
// FT_Outline_EmboldenXY: miter offsets along the corner bisector, clamped
// on short edges) and fills it once. A stem gets the same +w as a w-wide
// stroke, the path has exactly as many segments as the plain outline, and
// nothing is tessellated.

// shaperFaces maps shapers built by NewShaperFor to their Lato faces in
// Gio's face-index order (the collection order; system fallback faces get
// indices after them and are not emboldened here).
var shaperFaces sync.Map // *text.Shaper -> *emboldener

type emboldener struct {
	faces []*gtfont.Face

	mu    sync.Mutex
	runs  map[runKey]*list.Element
	lru   list.List // front = most recent; values *runEntry
	pts   []f32.Point
	ops   []gtot.SegmentOp
	shift []f32.Point
}

type runKey struct {
	hash uint64
	emb  float32
}

type runEntry struct {
	key    runKey
	glyphs []runGlyph
	path   clip.PathSpec
}

type runGlyph struct {
	id text.GlyphID
	x  fixed.Int26_6 // relative to the first glyph
	oy fixed.Int26_6
}

// maxRuns bounds the emboldened-line cache (Gio's own path cache holds 1000).
const maxRuns = 1500

func registerShaperFaces(sh *text.Shaper, faces []*gtfont.Face) {
	shaperFaces.Store(sh, &emboldener{faces: faces, runs: map[runKey]*list.Element{}})
}

// EmboldenedShape is sh.Shape(gs) with every contour grown by w/2 pixels
// on each side: the ink of the outline plus a w-wide stroke, as one fill.
// It reports false (and returns nothing) when the run uses a face it does
// not know (system fallback fonts) or sh was not built by NewShaperFor;
// callers then fall back to Shape plus clip.Stroke.
func EmboldenedShape(sh *text.Shaper, gs []text.Glyph, w float32) (clip.PathSpec, bool) {
	v, ok := shaperFaces.Load(sh)
	if !ok || len(gs) == 0 || w <= 0 {
		return clip.PathSpec{}, false
	}
	return v.(*emboldener).shape(gs, w)
}

func (e *emboldener) shape(gs []text.Glyph, w float32) (clip.PathSpec, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	k := runKey{hashRun(gs), w}
	if el, ok := e.runs[k]; ok {
		if re := el.Value.(*runEntry); re.matches(gs) {
			e.lru.MoveToFront(el)
			return re.path, true
		}
	}
	nf := len(e.faces)
	for i := range gs {
		if _, fi, _ := splitGlyphID(gs[i].ID); fi >= nf {
			return clip.PathSpec{}, false
		}
	}
	path := e.build(gs, w)
	re := &runEntry{key: k, path: path, glyphs: make([]runGlyph, len(gs))}
	for i, g := range gs {
		re.glyphs[i] = runGlyph{id: g.ID, x: g.X - gs[0].X, oy: g.Offset.Y}
	}
	if el, ok := e.runs[k]; ok { // hash collision: replace
		e.lru.Remove(el)
	}
	e.runs[k] = e.lru.PushFront(re)
	for e.lru.Len() > maxRuns {
		old := e.lru.Back()
		e.lru.Remove(old)
		delete(e.runs, old.Value.(*runEntry).key)
	}
	return path, true
}

func (re *runEntry) matches(gs []text.Glyph) bool {
	if len(re.glyphs) != len(gs) {
		return false
	}
	x0 := gs[0].X
	for i, g := range gs {
		if r := re.glyphs[i]; r.id != g.ID || r.x != g.X-x0 || r.oy != g.Offset.Y {
			return false
		}
	}
	return true
}

func hashRun(gs []text.Glyph) uint64 {
	h := uint64(14695981039346656037)
	x0 := gs[0].X
	for _, g := range gs {
		h ^= uint64(g.X - x0)
		h *= 1099511628211
		h ^= uint64(g.ID)
		h *= 1099511628211
		h ^= uint64(uint32(g.Offset.Y))
		h *= 1099511628211
	}
	return h
}

// Gio's GlyphID layout (gioui.org/text: 16 bits face, 16 bits ppem, 32 bits gid).
const (
	gidBits  = 32
	sizeBits = 16
)

func splitGlyphID(g text.GlyphID) (fixed.Int26_6, int, gtfont.GID) {
	faceIdx := int(uint64(g) >> (gidBits + sizeBits))
	ppem := fixed.Int26_6((uint64(g) >> gidBits) & (1<<sizeBits - 1))
	gid := gtfont.GID(uint64(g) & (1<<gidBits - 1))
	return ppem, faceIdx, gid
}

// build records the emboldened outline of a line, positioned like
// text.Shaper.Shape (origin at the first glyph's pen position).
func (e *emboldener) build(gs []text.Glyph, w float32) clip.PathSpec {
	var b clip.Path
	b.Begin(new(op.Ops))
	x0 := gs[0].X
	for _, g := range gs {
		ppem, fi, gid := splitGlyphID(g.ID)
		face := e.faces[fi]
		var outline gtfont.GlyphOutline
		switch d := face.GlyphData(gid).(type) {
		case gtfont.GlyphOutline:
			outline = d
		case gtfont.GlyphSVG:
			outline = d.Outline
		default:
			continue
		}
		scale := float32(ppem) / 64 / float32(face.Upem())
		org := f32.Pt(float32(g.X-x0-g.Offset.X)/64, -float32(g.Offset.Y)/64)
		e.pts, e.ops = e.pts[:0], e.ops[:0]
		for _, s := range outline.Segments {
			n := 1
			switch s.Op {
			case gtot.SegmentOpQuadTo:
				n = 2
			case gtot.SegmentOpCubeTo:
				n = 3
			}
			e.ops = append(e.ops, s.Op)
			for i := 0; i < n; i++ {
				e.pts = append(e.pts, f32.Pt(org.X+s.Args[i].X*scale, org.Y-s.Args[i].Y*scale))
			}
		}
		e.shift = emboldenPoints(e.pts, e.ops, w/2, e.shift)
		p := 0
		for _, sop := range e.ops {
			switch sop {
			case gtot.SegmentOpMoveTo:
				b.MoveTo(e.pts[p].Add(e.shift[p]))
				p++
			case gtot.SegmentOpLineTo:
				b.LineTo(e.pts[p].Add(e.shift[p]))
				p++
			case gtot.SegmentOpQuadTo:
				b.QuadTo(e.pts[p].Add(e.shift[p]), e.pts[p+1].Add(e.shift[p+1]))
				p += 2
			case gtot.SegmentOpCubeTo:
				b.CubeTo(e.pts[p].Add(e.shift[p]), e.pts[p+1].Add(e.shift[p+1]), e.pts[p+2].Add(e.shift[p+2]))
				p += 3
			}
		}
	}
	return b.End()
}

// emboldenPoints returns, for every point of a glyph outline (on- and
// off-curve, in segment order; each MoveTo starts a contour), the offset
// that moves it s pixels outward. Outward is decided from the glyph's
// overall winding, as FreeType does, so holes shrink while outer contours
// grow. shift is reused as the result's storage.
func emboldenPoints(pts []f32.Point, ops []gtot.SegmentOp, s float32, shift []f32.Point) []f32.Point {
	if cap(shift) < len(pts) {
		shift = make([]f32.Point, len(pts))
	}
	shift = shift[:len(pts)]
	for i := range shift {
		shift[i] = f32.Point{}
	}
	// contour boundaries
	type span struct{ a, b int } // [a, b)
	var spans [64]span
	cs := spans[:0]
	p := 0
	for _, op := range ops {
		if op == gtot.SegmentOpMoveTo {
			if len(cs) > 0 {
				cs[len(cs)-1].b = p
			}
			cs = append(cs, span{a: p})
		}
		switch op {
		case gtot.SegmentOpQuadTo:
			p += 2
		case gtot.SegmentOpCubeTo:
			p += 3
		default:
			p++
		}
	}
	if len(cs) == 0 {
		return shift
	}
	cs[len(cs)-1].b = p
	// winding of the whole glyph (shoelace)
	var area float32
	for _, c := range cs {
		n := c.b - c.a
		for i := 0; i < n; i++ {
			p0, p1 := pts[c.a+i], pts[c.a+(i+1)%n]
			area += p0.X*p1.Y - p1.X*p0.Y
		}
	}
	sign := float32(1)
	if area < 0 {
		sign = -1
	}
	for _, c := range cs {
		n := c.b - c.a
		closed := n > 1 && pts[c.a] == pts[c.b-1]
		m := n
		if closed {
			m = n - 1 // the closing point repeats the first
		}
		if m < 3 {
			continue
		}
		at := func(i int) f32.Point { return pts[c.a+((i%m)+m)%m] }
		for i := 0; i < m; i++ {
			cur := at(i)
			// nearest distinct neighbours
			var in, out f32.Point
			var lin, lout float32
			for j := 1; j < m; j++ {
				d := cur.Sub(at(i - j))
				if l := hypot(d); l > 1e-6 {
					in, lin = d.Mul(1/l), l
					break
				}
			}
			for j := 1; j < m; j++ {
				d := at(i + j).Sub(cur)
				if l := hypot(d); l > 1e-6 {
					out, lout = d.Mul(1/l), l
					break
				}
			}
			if lin == 0 || lout == 0 {
				continue
			}
			d := 1 + in.X*out.X + in.Y*out.Y // 1 + cos(turn)
			if d < 0.0625 {
				continue // turn over ~160°: leave the spike alone
			}
			// outward normals of the two edges, summed (the bisector)
			nx := sign * (in.Y + out.Y)
			ny := -sign * (in.X + out.X)
			q := float32(math.Abs(float64(in.X*out.Y - in.Y*out.X)))
			l := min(lin, lout)
			k := s / d
			if s*q > l*d {
				k = l / q
			}
			shift[c.a+i] = f32.Pt(nx*k, ny*k)
		}
		if closed {
			shift[c.b-1] = shift[c.a]
		}
	}
	return shift
}

func hypot(p f32.Point) float32 { return float32(math.Sqrt(float64(p.X*p.X + p.Y*p.Y))) }
