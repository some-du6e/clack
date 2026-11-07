package css

import (
	"image/color"
	"math"
	"strings"
)

// --- lengths and calc() ---

// lexpr is a length expression that can't be folded into a linear
// Px+Pct+Vw+Vh sum (min()/max()/clamp() over mixed units). Length.x
// points to one; Length.Resolve evaluates it.
type lexpr struct {
	op   byte // 'v' leaf, '+' sum, '*' scale kids[0] by f, 'm' min, 'M' max
	v    [4]float32
	f    float32
	kids []*lexpr
}

func (x *lexpr) eval(base, vw, vh float32) float32 {
	switch x.op {
	case 'v':
		return x.v[0] + x.v[1]*base/100 + x.v[2]*vw/100 + x.v[3]*vh/100
	case '+':
		var s float32
		for _, k := range x.kids {
			s += k.eval(base, vw, vh)
		}
		return s
	case '*':
		return x.kids[0].eval(base, vw, vh) * x.f
	case 'm', 'M':
		r := x.kids[0].eval(base, vw, vh)
		for _, k := range x.kids[1:] {
			v := k.eval(base, vw, vh)
			if x.op == 'm' && v < r || x.op == 'M' && v > r {
				r = v
			}
		}
		return r
	}
	return 0
}

// calcVal is a calc() operand: a number, an angle (radians) or a length.
type calcVal struct {
	kind byte // 'n' number, 'l' length, 'a' angle, 'p' bare percentage (number contexts)
	num  float64
	lin  [4]float64 // px, pct, vw, vh
	x    *lexpr
}

func (v calcVal) leaf() *lexpr {
	if v.x != nil {
		return v.x
	}
	return &lexpr{op: 'v', v: [4]float32{float32(v.lin[0]), float32(v.lin[1]), float32(v.lin[2]), float32(v.lin[3])}}
}

// unitLen converts a dimension to a linear length.
func (cx *cctx) unitLen(n float64, unit string) ([4]float64, bool) {
	u := strings.ToLower(unit)
	fs := float64(cx.emBase())
	switch u {
	case "px":
		return [4]float64{n, 0, 0, 0}, true
	case "em":
		return [4]float64{n * fs, 0, 0, 0}, true
	case "rem":
		return [4]float64{n * float64(cx.rootFont()), 0, 0, 0}, true
	case "ex", "ch":
		return [4]float64{n * fs * 0.5, 0, 0, 0}, true
	case "cap":
		return [4]float64{n * fs * 0.7, 0, 0, 0}, true
	case "ic":
		return [4]float64{n * fs, 0, 0, 0}, true
	case "lh":
		return [4]float64{n * fs * 1.2, 0, 0, 0}, true
	case "rlh":
		return [4]float64{n * float64(cx.rootFont()) * 1.2, 0, 0, 0}, true
	case "rex", "rch":
		return [4]float64{n * float64(cx.rootFont()) * 0.5, 0, 0, 0}, true
	case "vw", "svw", "lvw", "dvw", "vi", "svi", "lvi", "dvi", "cqw", "cqi":
		return [4]float64{0, 0, n, 0}, true
	case "vh", "svh", "lvh", "dvh", "vb", "svb", "lvb", "dvb", "cqh", "cqb":
		return [4]float64{0, 0, 0, n}, true
	case "vmin", "svmin", "lvmin", "dvmin", "cqmin":
		if cx.env.Height > 0 && cx.env.Height < cx.env.Width {
			return [4]float64{0, 0, 0, n}, true
		}
		return [4]float64{0, 0, n, 0}, true
	case "vmax", "svmax", "lvmax", "dvmax", "cqmax":
		if cx.env.Height > cx.env.Width {
			return [4]float64{0, 0, 0, n}, true
		}
		return [4]float64{0, 0, n, 0}, true
	}
	if px, ok := absLen(n, u, fs); ok {
		return [4]float64{px, 0, 0, 0}, true
	}
	return [4]float64{}, false
}

func angleUnit(n float64, unit string) (float64, bool) {
	switch strings.ToLower(unit) {
	case "deg":
		return n * math.Pi / 180, true
	case "rad":
		return n, true
	case "grad":
		return n * math.Pi / 200, true
	case "turn":
		return n * 2 * math.Pi, true
	}
	return 0, false
}

func isMathFn(name string) bool {
	switch name {
	case "calc", "-webkit-calc", "-moz-calc", "min", "max", "clamp", "abs", "sign", "round":
		return true
	}
	return false
}

// calcFn evaluates a math function cv.
func (cx *cctx) calcFn(c *cv) (calcVal, bool) {
	args := splitComma(c.args)
	switch c.name {
	case "calc", "-webkit-calc", "-moz-calc":
		if len(args) != 1 {
			return calcVal{}, false
		}
		return cx.calcSum(args[0])
	case "min", "max":
		vals := make([]calcVal, 0, len(args))
		for _, a := range args {
			v, ok := cx.calcSum(a)
			if !ok {
				return calcVal{}, false
			}
			vals = append(vals, v)
		}
		return calcMinMax(vals, c.name == "max")
	case "clamp":
		if len(args) != 3 {
			return calcVal{}, false
		}
		var vs [3]calcVal
		for i, a := range args {
			if len(a) == 1 && a[0].t.ident("none") && i != 1 {
				vs[i] = calcVal{kind: 'x'}
				continue
			}
			v, ok := cx.calcSum(a)
			if !ok {
				return calcVal{}, false
			}
			vs[i] = v
		}
		r := vs[1]
		var ok bool
		if vs[2].kind != 'x' {
			if r, ok = calcMinMax([]calcVal{r, vs[2]}, false); !ok {
				return calcVal{}, false
			}
		}
		if vs[0].kind != 'x' {
			if r, ok = calcMinMax([]calcVal{vs[0], r}, true); !ok {
				return calcVal{}, false
			}
		}
		return r, true
	case "abs", "sign":
		if len(args) != 1 {
			return calcVal{}, false
		}
		v, ok := cx.calcSum(args[0])
		if !ok || v.x != nil {
			return calcVal{}, false
		}
		f := func(x float64) float64 {
			if c.name == "abs" {
				return math.Abs(x)
			}
			if x > 0 {
				return 1
			} else if x < 0 {
				return -1
			}
			return 0
		}
		if v.kind == 'l' {
			if v.lin[1] != 0 || v.lin[2] != 0 || v.lin[3] != 0 {
				return calcVal{}, false
			}
			if c.name == "sign" {
				return calcVal{kind: 'n', num: f(v.lin[0])}, true
			}
			v.lin[0] = f(v.lin[0])
			return v, true
		}
		if c.name == "sign" {
			return calcVal{kind: 'n', num: f(v.num)}, true
		}
		v.num = f(v.num)
		return v, true
	case "round":
		// round([strategy,] A [, B])
		strat := "nearest"
		if len(args) > 0 && len(args[0]) == 1 && args[0][0].t.k == tIdent {
			strat = strings.ToLower(args[0][0].t.s)
			args = args[1:]
		}
		if len(args) < 1 || len(args) > 2 {
			return calcVal{}, false
		}
		a, ok := cx.calcSum(args[0])
		if !ok || a.x != nil {
			return calcVal{}, false
		}
		step := 1.0
		if len(args) == 2 {
			b, ok := cx.calcSum(args[1])
			if !ok || b.x != nil {
				return calcVal{}, false
			}
			if b.kind == 'l' {
				step = b.lin[0]
			} else {
				step = b.num
			}
		}
		if step == 0 {
			return calcVal{}, false
		}
		rnd := func(x float64) float64 {
			q := x / step
			switch strat {
			case "up":
				q = math.Ceil(q)
			case "down":
				q = math.Floor(q)
			case "to-zero":
				q = math.Trunc(q)
			default:
				q = math.Floor(q + 0.5)
			}
			return q * step
		}
		if a.kind == 'l' {
			if a.lin[1] != 0 || a.lin[2] != 0 || a.lin[3] != 0 {
				return calcVal{}, false
			}
			a.lin[0] = rnd(a.lin[0])
		} else {
			a.num = rnd(a.num)
		}
		return a, true
	}
	return calcVal{}, false
}

func calcMinMax(vals []calcVal, isMax bool) (calcVal, bool) {
	if len(vals) == 0 {
		return calcVal{}, false
	}
	kind := vals[0].kind
	for _, v := range vals {
		if v.kind != kind {
			// a bare number 0 can join lengths
			return calcVal{}, false
		}
	}
	if kind != 'l' {
		r := vals[0].num
		for _, v := range vals[1:] {
			if isMax && v.num > r || !isMax && v.num < r {
				r = v.num
			}
		}
		return calcVal{kind: kind, num: r}, true
	}
	// fold when every operand varies in the same single component
	comp := -1
	foldable := true
	for _, v := range vals {
		if v.x != nil {
			foldable = false
			break
		}
		for i := 0; i < 4; i++ {
			if v.lin[i] != 0 {
				if comp >= 0 && comp != i {
					foldable = false
				}
				comp = i
			}
		}
	}
	if foldable {
		if comp < 0 {
			comp = 0
		}
		r := vals[0]
		for _, v := range vals[1:] {
			if isMax && v.lin[comp] > r.lin[comp] || !isMax && v.lin[comp] < r.lin[comp] {
				r = v
			}
		}
		return r, true
	}
	op := byte('m')
	if isMax {
		op = 'M'
	}
	x := &lexpr{op: op}
	for _, v := range vals {
		x.kids = append(x.kids, v.leaf())
	}
	return calcVal{kind: 'l', x: x}, true
}

func (cx *cctx) calcSum(cs []cv) (calcVal, bool) {
	if len(cs) == 0 {
		return calcVal{}, false
	}
	// split into products at + / - delims
	var acc calcVal
	have := false
	sign := 1.0
	start := 0
	flush := func(end int) bool {
		if end <= start {
			return false
		}
		v, ok := cx.calcProduct(cs[start:end])
		if !ok {
			return false
		}
		if sign < 0 {
			v = calcScale(v, -1)
		}
		if !have {
			acc, have = v, true
			return true
		}
		r, ok := calcAdd(acc, v)
		acc = r
		return ok
	}
	for i := 0; i < len(cs); i++ {
		if cs[i].t.k == tDelim && (cs[i].t.s == "+" || cs[i].t.s == "-") {
			if !flush(i) {
				return calcVal{}, false
			}
			sign = 1
			if cs[i].t.s == "-" {
				sign = -1
			}
			start = i + 1
		} else if i > start && !(cs[i].t.is('*') || cs[i].t.is('/')) && !(cs[i-1].t.is('*') || cs[i-1].t.is('/')) {
			// "1px -2px": a signed operand without an operator
			return calcVal{}, false
		}
	}
	if !flush(len(cs)) {
		return calcVal{}, false
	}
	return acc, true
}

func calcScale(v calcVal, f float64) calcVal {
	if v.x != nil {
		v.x = &lexpr{op: '*', f: float32(f), kids: []*lexpr{v.x}}
		return v
	}
	v.num *= f
	for i := range v.lin {
		v.lin[i] *= f
	}
	return v
}

func calcAdd(a, b calcVal) (calcVal, bool) {
	if a.kind == 'p' && b.kind == 'l' {
		a = calcVal{kind: 'l', lin: [4]float64{0, a.num, 0, 0}}
	}
	if b.kind == 'p' && a.kind == 'l' {
		b = calcVal{kind: 'l', lin: [4]float64{0, b.num, 0, 0}}
	}
	if a.kind != b.kind {
		return calcVal{}, false
	}
	if a.kind != 'l' {
		a.num += b.num
		return a, true
	}
	if a.x == nil && b.x == nil {
		for i := range a.lin {
			a.lin[i] += b.lin[i]
		}
		return a, true
	}
	return calcVal{kind: 'l', x: &lexpr{op: '+', kids: []*lexpr{a.leaf(), b.leaf()}}}, true
}

func (cx *cctx) calcProduct(cs []cv) (calcVal, bool) {
	v, ok := cx.calcUnit(&cs[0])
	if !ok {
		return calcVal{}, false
	}
	for i := 1; i < len(cs); i += 2 {
		if i+1 >= len(cs) {
			return calcVal{}, false
		}
		r, ok := cx.calcUnit(&cs[i+1])
		if !ok {
			return calcVal{}, false
		}
		switch {
		case cs[i].t.is('*'):
			if r.kind == 'n' {
				v = calcScale(v, r.num)
			} else if v.kind == 'n' {
				v = calcScale(r, v.num)
			} else {
				return calcVal{}, false
			}
		case cs[i].t.is('/'):
			if r.kind != 'n' || r.num == 0 {
				return calcVal{}, false
			}
			v = calcScale(v, 1/r.num)
		default:
			return calcVal{}, false
		}
	}
	return v, true
}

func (cx *cctx) calcUnit(c *cv) (calcVal, bool) {
	switch c.t.k {
	case tNumber:
		return calcVal{kind: 'n', num: c.t.num}, true
	case tPercentage:
		if cx.calcPctNum {
			return calcVal{kind: 'p', num: c.t.num}, true
		}
		return calcVal{kind: 'l', lin: [4]float64{0, c.t.num, 0, 0}}, true
	case tDimension:
		if l, ok := cx.unitLen(c.t.num, c.t.s); ok {
			return calcVal{kind: 'l', lin: l}, true
		}
		if a, ok := angleUnit(c.t.num, c.t.s); ok {
			return calcVal{kind: 'a', num: a}, true
		}
		switch strings.ToLower(c.t.s) {
		case "s":
			return calcVal{kind: 't', num: c.t.num}, true
		case "ms":
			return calcVal{kind: 't', num: c.t.num / 1000}, true
		}
		return calcVal{}, false
	case tLParen:
		return cx.calcSum(c.args)
	case tFunction:
		if isMathFn(c.name) {
			return cx.calcFn(c)
		}
	case tIdent:
		switch strings.ToLower(c.t.s) {
		case "pi":
			return calcVal{kind: 'n', num: math.Pi}, true
		case "e":
			return calcVal{kind: 'n', num: math.E}, true
		case "infinity":
			return calcVal{kind: 'n', num: math.MaxFloat32}, true
		case "-infinity":
			return calcVal{kind: 'n', num: -math.MaxFloat32}, true
		}
	}
	return calcVal{}, false
}

func (cx *cctx) toLength(v calcVal) (Length, bool) {
	if v.kind == 'n' && v.num == 0 {
		return Zero, true
	}
	if v.kind != 'l' {
		return Length{}, false
	}
	if v.x != nil {
		l := Length{x: v.x}
		l.Px = v.x.eval(0, cx.env.Width, cx.env.Height)
		return l, true
	}
	return Length{Px: float32(v.lin[0]), Pct: float32(v.lin[1]), Vw: float32(v.lin[2]), Vh: float32(v.lin[3])}, true
}

// length parses a <length-percentage> (pct=false: <length>).
func (cx *cctx) length(c *cv, pct bool) (Length, bool) {
	switch c.t.k {
	case tNumber:
		if c.t.num == 0 {
			return Zero, true
		}
		if cx.quirkUnitless {
			return Px(float32(c.t.num)), true
		}
	case tDimension:
		l, ok := cx.unitLen(c.t.num, c.t.s)
		if !ok {
			return Length{}, false
		}
		return Length{Px: float32(l[0]), Vw: float32(l[2]), Vh: float32(l[3])}, true
	case tPercentage:
		if pct {
			return Pct(float32(c.t.num)), true
		}
	case tFunction:
		if isMathFn(c.name) {
			v, ok := cx.calcFn(c)
			if !ok {
				return Length{}, false
			}
			l, ok := cx.toLength(v)
			if !ok || !pct && (l.Pct != 0 || l.x != nil && lexprHasPct(l.x)) {
				return Length{}, false
			}
			return l, true
		}
	}
	return Length{}, false
}

func lexprHasPct(x *lexpr) bool {
	if x.op == 'v' {
		return x.v[1] != 0
	}
	for _, k := range x.kids {
		if lexprHasPct(k) {
			return true
		}
	}
	return false
}

// px parses a <length> to px (viewport units resolved against env).
func (cx *cctx) px(c *cv) (float32, bool) {
	l, ok := cx.length(c, false)
	if !ok {
		return 0, false
	}
	return l.Resolve(0, cx.env.Width, cx.env.Height, 0), true
}

// number parses a <number> (also calc()).
func (cx *cctx) number(c *cv) (float64, bool) {
	switch c.t.k {
	case tNumber:
		return c.t.num, true
	case tFunction:
		if isMathFn(c.name) {
			v, ok := cx.calcFn(c)
			if ok && v.kind == 'n' {
				return v.num, true
			}
		}
	}
	return 0, false
}

// numPct parses <number> | <percentage> as a number (50% = 0.5).
func (cx *cctx) numPct(c *cv) (float64, bool) {
	if c.t.k == tPercentage {
		return c.t.num / 100, true
	}
	if c.t.k == tFunction && isMathFn(c.name) {
		saved := cx.calcPctNum
		cx.calcPctNum = true
		v, ok := cx.calcFn(c)
		cx.calcPctNum = saved
		if !ok {
			return 0, false
		}
		switch v.kind {
		case 'n':
			return v.num, true
		case 'p':
			return v.num / 100, true
		}
		return 0, false
	}
	return cx.number(c)
}

// angle parses an <angle> (unitless 0 allowed) to radians.
func (cx *cctx) angle(c *cv, unitless bool) (float64, bool) {
	switch c.t.k {
	case tNumber:
		if c.t.num == 0 || unitless {
			return c.t.num * math.Pi / 180, true
		}
	case tDimension:
		return angleUnit(c.t.num, c.t.s)
	case tFunction:
		if isMathFn(c.name) {
			v, ok := cx.calcFn(c)
			if ok && v.kind == 'a' {
				return v.num, true
			}
			if ok && v.kind == 'n' && (v.num == 0 || unitless) {
				return v.num * math.Pi / 180, true
			}
		}
	}
	return 0, false
}

func (cx *cctx) integer(c *cv) (int, bool) {
	v, ok := cx.number(c)
	if !ok {
		return 0, false
	}
	if c.t.k == tNumber && !c.t.isInt {
		return 0, false
	}
	return int(math.Round(v)), true
}

// --- colors ---

func clamp01(v float64) float64 {
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func to8(v float64) uint8 { return uint8(math.Round(clamp01(v) * 255)) }

func rgbaF(r, g, b, a float64) color.NRGBA {
	return color.NRGBA{R: to8(r), G: to8(g), B: to8(b), A: to8(a)}
}

func hexColor(s string) (color.NRGBA, bool) {
	h := func(c byte) (uint8, bool) {
		switch {
		case c >= '0' && c <= '9':
			return c - '0', true
		case c >= 'a' && c <= 'f':
			return c - 'a' + 10, true
		case c >= 'A' && c <= 'F':
			return c - 'A' + 10, true
		}
		return 0, false
	}
	var d [8]uint8
	for i := 0; i < len(s); i++ {
		if i >= 8 {
			return color.NRGBA{}, false
		}
		v, ok := h(s[i])
		if !ok {
			return color.NRGBA{}, false
		}
		d[i] = v
	}
	switch len(s) {
	case 3:
		return color.NRGBA{d[0] * 17, d[1] * 17, d[2] * 17, 255}, true
	case 4:
		return color.NRGBA{d[0] * 17, d[1] * 17, d[2] * 17, d[3] * 17}, true
	case 6:
		return color.NRGBA{d[0]<<4 | d[1], d[2]<<4 | d[3], d[4]<<4 | d[5], 255}, true
	case 8:
		return color.NRGBA{d[0]<<4 | d[1], d[2]<<4 | d[3], d[4]<<4 | d[5], d[6]<<4 | d[7]}, true
	}
	return color.NRGBA{}, false
}

// colorArgs splits color function arguments into channels and alpha:
// legacy comma syntax or modern space syntax with "/ alpha".
func colorArgs(args []cv) (ch []cv, alpha *cv, ok bool) {
	parts := splitComma(args)
	if len(parts) > 1 {
		for _, p := range parts {
			if len(p) != 1 {
				return nil, nil, false
			}
			ch = append(ch, p[0])
		}
		if len(ch) == 4 {
			a := ch[3]
			return ch[:3], &a, true
		}
		return ch, nil, len(ch) == 3
	}
	sl := splitDelim(args, '/')
	switch len(sl) {
	case 1:
		return sl[0], nil, true
	case 2:
		if len(sl[1]) != 1 {
			return nil, nil, false
		}
		a := sl[1][0]
		return sl[0], &a, true
	}
	return nil, nil, false
}

func (cx *cctx) alphaOf(a *cv) (float64, bool) {
	if a == nil {
		return 1, true
	}
	if a.t.ident("none") {
		return 0, true
	}
	v, ok := cx.numPct(a)
	return clamp01(v), ok
}

// chanVal parses a channel: number scaled by 1/numMax, percentage /100,
// none → 0.
func (cx *cctx) chanVal(c *cv, numMax, pctMax float64) (float64, bool) {
	if c.t.ident("none") {
		return 0, true
	}
	if c.t.k == tPercentage {
		return c.t.num / 100 * pctMax, true
	}
	v, ok := cx.number(c)
	if !ok {
		if c.t.k == tFunction && isMathFn(c.name) {
			if p, ok := cx.numPct(c); ok {
				return p * pctMax, true
			}
		}
		return 0, false
	}
	return v / numMax, true
}

func (cx *cctx) hue(c *cv) (float64, bool) {
	if c.t.ident("none") {
		return 0, true
	}
	a, ok := cx.angle(c, true)
	if !ok {
		return 0, false
	}
	deg := math.Mod(a*180/math.Pi, 360)
	if deg < 0 {
		deg += 360
	}
	return deg, true
}

func hslToRGB(h, s, l float64) (r, g, b float64) {
	s, l = clamp01(s), clamp01(l)
	f := func(n float64) float64 {
		k := math.Mod(n+h/30, 12)
		a := s * math.Min(l, 1-l)
		return l - a*math.Max(-1, math.Min(math.Min(k-3, 9-k), 1))
	}
	return f(0), f(8), f(4)
}

func gammaEnc(c float64) float64 {
	if math.Abs(c) <= 0.0031308 {
		return 12.92 * c
	}
	s := 1.0
	if c < 0 {
		s, c = -1, -c
	}
	return s * (1.055*math.Pow(c, 1/2.4) - 0.055)
}

func gammaDec(c float64) float64 {
	if math.Abs(c) <= 0.04045 {
		return c / 12.92
	}
	s := 1.0
	if c < 0 {
		s, c = -1, -c
	}
	return s * math.Pow((c+0.055)/1.055, 2.4)
}

func oklabToSRGB(L, a, b float64) (float64, float64, float64) {
	l := L + 0.3963377774*a + 0.2158037573*b
	m := L - 0.1055613458*a - 0.0638541728*b
	s := L - 0.0894841775*a - 1.2914855480*b
	l, m, s = l*l*l, m*m*m, s*s*s
	r := 4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g := -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	bb := -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
	return gammaEnc(r), gammaEnc(g), gammaEnc(bb)
}

func srgbToOklab(r, g, b float64) (float64, float64, float64) {
	r, g, b = gammaDec(r), gammaDec(g), gammaDec(b)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return 0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s
}

func xyz65ToSRGB(x, y, z float64) (float64, float64, float64) {
	r := 3.2409699419045226*x - 1.537383177570094*y - 0.4986107602930034*z
	g := -0.9692436362808796*x + 1.8759675015077202*y + 0.04155505740717559*z
	b := 0.05563007969699366*x - 0.20397695888897652*y + 1.0569715142428786*z
	return gammaEnc(r), gammaEnc(g), gammaEnc(b)
}

func xyz50To65(x, y, z float64) (float64, float64, float64) {
	return 0.9554734527042182*x - 0.023098536874261423*y + 0.0632593086610217*z,
		-0.028369706963208136*x + 1.0099954580058226*y + 0.021041398966943008*z,
		0.012314001688319899*x - 0.020507696433477912*y + 1.3303659366080753*z
}

func labToSRGB(L, a, b float64) (float64, float64, float64) {
	const eps, kappa = 216.0 / 24389, 24389.0 / 27
	fy := (L + 16) / 116
	fx := fy + a/500
	fz := fy - b/200
	cube := func(f float64) float64 {
		if f*f*f > eps {
			return f * f * f
		}
		return (116*f - 16) / kappa
	}
	x := cube(fx)
	var y float64
	if L > kappa*eps {
		y = fy * fy * fy
	} else {
		y = L / kappa
	}
	z := cube(fz)
	x, y, z = x*0.96422, y, z*0.82521
	return xyz65ToSRGB(xyz50To65(x, y, z))
}

func p3ToSRGB(r, g, b float64) (float64, float64, float64) {
	r, g, b = gammaDec(r), gammaDec(g), gammaDec(b)
	x := 0.4865709486482162*r + 0.26566769316909306*g + 0.1982172852343625*b
	y := 0.2289745640697488*r + 0.6917385218365064*g + 0.079286914093745*b
	z := 0.0*r + 0.04511338185890264*g + 1.043944368900976*b
	return xyz65ToSRGB(x, y, z)
}

// color parses a <color>. cur is currentColor.
func (cx *cctx) color(c *cv) (color.NRGBA, bool) {
	switch c.t.k {
	case tHash:
		return hexColor(c.t.s)
	case tIdent:
		n := strings.ToLower(c.t.s)
		switch n {
		case "transparent":
			return color.NRGBA{}, true
		case "currentcolor":
			return cx.cur(), true
		}
		if v, ok := namedColors[n]; ok {
			return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, true
		}
		if v, ok := systemColors[n]; ok {
			return color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}, true
		}
		return color.NRGBA{}, false
	case tFunction:
	default:
		return color.NRGBA{}, false
	}
	switch c.name {
	case "rgb", "rgba":
		ch, al, ok := colorArgs(c.args)
		if !ok || len(ch) != 3 {
			return color.NRGBA{}, false
		}
		var v [3]float64
		for i := range ch {
			if v[i], ok = cx.chanVal(&ch[i], 255, 1); !ok {
				return color.NRGBA{}, false
			}
		}
		a, ok := cx.alphaOf(al)
		if !ok {
			return color.NRGBA{}, false
		}
		return rgbaF(v[0], v[1], v[2], a), true
	case "hsl", "hsla", "hwb":
		ch, al, ok := colorArgs(c.args)
		if !ok || len(ch) != 3 {
			return color.NRGBA{}, false
		}
		h, ok := cx.hue(&ch[0])
		if !ok {
			return color.NRGBA{}, false
		}
		s, ok1 := cx.chanVal(&ch[1], 100, 1)
		l, ok2 := cx.chanVal(&ch[2], 100, 1)
		a, ok3 := cx.alphaOf(al)
		if !ok1 || !ok2 || !ok3 {
			return color.NRGBA{}, false
		}
		if c.name == "hwb" {
			w, bl := s, l
			if w+bl >= 1 {
				g := w / (w + bl)
				return rgbaF(g, g, g, a), true
			}
			r, g, b := hslToRGB(h, 1, 0.5)
			f := func(x float64) float64 { return x*(1-w-bl) + w }
			return rgbaF(f(r), f(g), f(b), a), true
		}
		r, g, b := hslToRGB(h, s, l)
		return rgbaF(r, g, b, a), true
	case "lab", "lch", "oklab", "oklch":
		ch, al, ok := colorArgs(c.args)
		if !ok || len(ch) != 3 {
			return color.NRGBA{}, false
		}
		ok_ := c.name[0] == 'o'
		lmax, cmax := 100.0, 125.0
		if c.name == "lch" {
			cmax = 150
		}
		if ok_ {
			lmax, cmax = 1, 0.4
		}
		L, ok1 := cx.chanVal(&ch[0], 1, lmax)
		A, ok2 := cx.chanVal(&ch[1], 1, cmax)
		var B float64
		ok3 := true
		polar := strings.HasSuffix(c.name, "lch")
		if polar {
			h, ok := cx.hue(&ch[2])
			ok3 = ok
			hr := h * math.Pi / 180
			A, B = A*math.Cos(hr), A*math.Sin(hr)
		} else {
			B, ok3 = cx.chanVal(&ch[2], 1, cmax)
		}
		a, ok4 := cx.alphaOf(al)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return color.NRGBA{}, false
		}
		var r, g, b float64
		if ok_ {
			r, g, b = oklabToSRGB(L, A, B)
		} else {
			r, g, b = labToSRGB(L, A, B)
		}
		return rgbaF(r, g, b, a), true
	case "color":
		ch, al, ok := colorArgs(c.args)
		if !ok || len(ch) != 4 || ch[0].t.k != tIdent {
			return color.NRGBA{}, false
		}
		var v [3]float64
		for i := 0; i < 3; i++ {
			if v[i], ok = cx.chanVal(&ch[i+1], 1, 1); !ok {
				return color.NRGBA{}, false
			}
		}
		a, ok := cx.alphaOf(al)
		if !ok {
			return color.NRGBA{}, false
		}
		r, g, b := v[0], v[1], v[2]
		switch strings.ToLower(ch[0].t.s) {
		case "srgb", "a98-rgb", "rec2020", "prophoto-rgb":
		case "srgb-linear":
			r, g, b = gammaEnc(r), gammaEnc(g), gammaEnc(b)
		case "display-p3":
			r, g, b = p3ToSRGB(r, g, b)
		case "xyz", "xyz-d65":
			r, g, b = xyz65ToSRGB(r, g, b)
		case "xyz-d50":
			r, g, b = xyz65ToSRGB(xyz50To65(r, g, b))
		default:
			return color.NRGBA{}, false
		}
		return rgbaF(r, g, b, a), true
	case "color-mix":
		return cx.colorMix(c)
	case "light-dark":
		parts := splitComma(c.args)
		if len(parts) != 2 || len(parts[0]) != 1 || len(parts[1]) != 1 {
			return color.NRGBA{}, false
		}
		l, ok1 := cx.color(&parts[0][0])
		d, ok2 := cx.color(&parts[1][0])
		if !ok1 || !ok2 {
			return color.NRGBA{}, false
		}
		if cx.env.Dark {
			return d, true
		}
		return l, true
	}
	return color.NRGBA{}, false
}

func (cx *cctx) colorMix(c *cv) (color.NRGBA, bool) {
	parts := splitComma(c.args)
	if len(parts) != 3 || len(parts[0]) < 2 || !parts[0][0].t.ident("in") || parts[0][1].t.k != tIdent {
		return color.NRGBA{}, false
	}
	space := strings.ToLower(parts[0][1].t.s)
	var cols [2]color.NRGBA
	var ps [2]float64
	var have [2]bool
	for i := 0; i < 2; i++ {
		p := parts[i+1]
		if len(p) == 0 || len(p) > 2 {
			return color.NRGBA{}, false
		}
		gotColor := false
		for j := range p {
			if p[j].t.k == tPercentage {
				ps[i], have[i] = p[j].t.num/100, true
				continue
			}
			if p[j].t.k == tFunction && isMathFn(p[j].name) {
				if v, ok := cx.numPct(&p[j]); ok {
					ps[i], have[i] = v, true
					continue
				}
			}
			col, ok := cx.color(&p[j])
			if !ok || gotColor {
				return color.NRGBA{}, false
			}
			cols[i], gotColor = col, true
		}
		if !gotColor {
			return color.NRGBA{}, false
		}
	}
	switch {
	case !have[0] && !have[1]:
		ps[0], ps[1] = 0.5, 0.5
	case !have[0]:
		ps[0] = 1 - ps[1]
	case !have[1]:
		ps[1] = 1 - ps[0]
	}
	if ps[0] < 0 || ps[1] < 0 {
		return color.NRGBA{}, false
	}
	sum := ps[0] + ps[1]
	if sum == 0 {
		return color.NRGBA{}, false
	}
	mult := 1.0
	if sum < 1 {
		mult = sum
	}
	p0, p1 := ps[0]/sum, ps[1]/sum
	a0, a1 := float64(cols[0].A)/255, float64(cols[1].A)/255
	a := a0*p0 + a1*p1
	if a == 0 {
		return color.NRGBA{}, true
	}
	ch := func(col color.NRGBA) [3]float64 {
		r, g, b := float64(col.R)/255, float64(col.G)/255, float64(col.B)/255
		switch space {
		case "srgb-linear":
			return [3]float64{gammaDec(r), gammaDec(g), gammaDec(b)}
		case "oklab", "oklch", "lab", "lch":
			L, A, B := srgbToOklab(r, g, b)
			return [3]float64{L, A, B}
		}
		return [3]float64{r, g, b}
	}
	c0, c1 := ch(cols[0]), ch(cols[1])
	var m [3]float64
	for i := range m {
		m[i] = (c0[i]*a0*p0 + c1[i]*a1*p1) / a
	}
	r, g, b := m[0], m[1], m[2]
	switch space {
	case "srgb-linear":
		r, g, b = gammaEnc(r), gammaEnc(g), gammaEnc(b)
	case "oklab", "oklch", "lab", "lch":
		r, g, b = oklabToSRGB(r, g, b)
	}
	return rgbaF(r, g, b, a*mult), true
}
