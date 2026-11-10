package blockkit

// Slack's SVG icons for the agent blocks (agent.go) that Clack's icon set
// lacks or draws differently: the 20×20 SvgIcon paths from the web
// client's icon table (modern.vendor, 2026-11-08), drawn with their
// fill-rule (evenodd: subpaths are re-wound by nesting depth so Gio's
// nonzero fill matches). Names Slack's icon_button maps to another set
// fall back to internal/ui/icons.

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"sync"

	"gioui.org/f32"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var agentIconPaths = map[string]string{
	"thumbs-up":               "M12.433 1.294a2.4 2.4 0 0 0-3.346 1.133l-.19.427a10.2 10.2 0 0 1-2.853 3.741L6 6.594H1.75a.75.75 0 0 0-.75.75v8.096a2.653 2.653 0 0 0 2.653 2.654H6a.75.75 0 0 0 .733-.591l1.763.354a12.8 12.8 0 0 0 5.008 0l.056-.01a4.75 4.75 0 0 0 3.717-3.68l.862-4.092c.395-1.88-1.209-3.576-3.108-3.285l-2.43.373.61-1.832.005-.013.003-.008.364-1.217a2.4 2.4 0 0 0-1.15-2.799M6.75 15.976V7.95l.14-.111a11.7 11.7 0 0 0 3.378-4.377l.189-.427a.903.903 0 0 1 1.69.626l-.359 1.194-1 2.998a.75.75 0 0 0 .825.978l3.645-.559a1.25 1.25 0 0 1 1.413 1.494l-.861 4.092a3.25 3.25 0 0 1-2.543 2.517l-.057.01c-1.459.293-2.96.292-4.419 0zm-1.5-7.882v8.5H3.653A1.153 1.153 0 0 1 2.5 15.44V8.095z",
	"thumbs-up-filled":        "M12.997 1.77a2.315 2.315 0 0 0-3.3 1.304 7.5 7.5 0 0 1-1.06 2.052L7.146 7.17A.75.75 0 0 0 7 7.613v9.919a.75.75 0 0 0 .65.743l1.765.237c1.396.188 2.814.142 4.195-.134l.45-.09a4.75 4.75 0 0 0 3.717-3.68l.862-4.092c.395-1.88-1.209-3.576-3.108-3.285l-2.43.373.61-1.832.005-.012.003-.01.386-1.287a2.31 2.31 0 0 0-1.108-2.693M5.75 7.785a.75.75 0 0 0-.75-.75H2.25a.75.75 0 0 0-.75.75v8.096a2.653 2.653 0 0 0 2.653 2.654H5a.75.75 0 0 0 .75-.75z",
	"thumbs-down":             "M12.933 18.312a2.4 2.4 0 0 1-3.346-1.133l-.19-.427a10.2 10.2 0 0 0-2.853-3.742l-.044.002H2.25a.75.75 0 0 1-.75-.75V4.165a2.653 2.653 0 0 1 2.653-2.653H6.5c.36 0 .66.253.733.59l1.763-.353a12.8 12.8 0 0 1 5.008-.001l.056.01a4.75 4.75 0 0 1 3.717 3.68l.862 4.092c.395 1.88-1.209 3.576-3.108 3.285l-2.43-.373.61 1.832.008.022.364 1.217a2.4 2.4 0 0 1-1.15 2.798M7.25 3.63v8.024l.14.112a11.7 11.7 0 0 1 3.378 4.376l.189.427a.903.903 0 0 0 1.69-.626l-.359-1.194-1-2.997a.75.75 0 0 1 .825-.979l3.645.559a1.25 1.25 0 0 0 1.413-1.493l-.861-4.092a3.25 3.25 0 0 0-2.543-2.518l-.057-.01a11.3 11.3 0 0 0-4.419 0zm-1.5 7.882v-8.5H4.153C3.516 3.012 3 3.528 3 4.165v7.347z",
	"thumbs-down-filled":      "M12.997 18.343a2.315 2.315 0 0 1-3.3-1.303 7.5 7.5 0 0 0-1.06-2.052l-1.492-2.045A.75.75 0 0 1 7 12.5V2.58a.75.75 0 0 1 .65-.743L9.415 1.6a12.8 12.8 0 0 1 4.195.135l.45.09a4.75 4.75 0 0 1 3.717 3.68l.862 4.091c.395 1.88-1.209 3.576-3.108 3.285l-2.43-.373.61 1.832.005.012.003.01.386 1.287a2.31 2.31 0 0 1-1.108 2.693M5.75 12.33a.75.75 0 0 1-.75.75H2.25a.75.75 0 0 1-.75-.75V4.232a2.653 2.653 0 0 1 2.653-2.653H5a.75.75 0 0 1 .75.75z",
	"check-circle-filled":     "M10 19a9 9 0 1 0 0-18 9 9 0 0 0 0 18m3.722-11.315a.75.75 0 1 0-1.144-.97l-4.125 4.863-1.141-1.611a.75.75 0 1 0-1.224.867l1.7 2.4a.75.75 0 0 0 1.184.051z",
	"warning-filled":          "M7.712 3.542c1.02-1.723 3.556-1.723 4.576 0l6.1 10.303c1.043 1.76-.28 3.905-2.287 3.905H3.9c-2.008 0-3.331-2.144-2.288-3.905zM10 12.75a1 1 0 1 0 0 2 1 1 0 0 0 0-2m0-6a.75.75 0 0 0-.75.75V11a.75.75 0 0 0 1.5 0V7.5a.75.75 0 0 0-.75-.75",
	"icon-placeholder-filled": "M19 10a9 9 0 1 1-18 0 9 9 0 0 1 18 0",
	"caret-right":             "M7.72 5.72a.75.75 0 0 1 1.06 0l3.75 3.75a.75.75 0 0 1 0 1.06l-3.75 3.75a.75.75 0 1 1-1.06-1.06L10.94 10 7.72 6.78a.75.75 0 0 1 0-1.06",
	"caret-down":              "M13.22 7.47a.75.75 0 1 1 1.06 1.06l-3.75 3.75a.75.75 0 0 1-1.06 0L5.72 8.53a.75.75 0 1 1 1.06-1.06L10 10.69z",
	"caret-up":                "M10 7c.199 0 .39.08.53.22l3.75 3.75a.75.75 0 1 1-1.06 1.06L10 8.81l-3.22 3.22a.75.75 0 1 1-1.06-1.06l3.75-3.75.114-.094A.75.75 0 0 1 10 7",
	"hourglass":               "M13.103 1.5c2 0 3.581 1.698 3.439 3.693a2.55 2.55 0 0 1-.638 1.512l-2.967 3.339a.25.25 0 0 0 .01.343l2.867 2.866c.428.428.688.997.73 1.601A3.403 3.403 0 0 1 13.15 18.5H6.876a3.43 3.43 0 0 1-3.419-3.672c.041-.58.281-1.128.679-1.553l2.713-2.893a.25.25 0 0 0 .008-.331L4.047 6.68a2.57 2.57 0 0 1-.587-1.46A3.473 3.473 0 0 1 6.924 1.5zM6.922 3a1.97 1.97 0 0 0-1.967 2.113c.016.223.102.434.244.606L8.01 9.09a1.75 1.75 0 0 1-.068 2.317L5.23 14.301c-.162.173-.26.398-.277.634A1.93 1.93 0 0 0 6.876 17h6.274a1.903 1.903 0 0 0 1.899-2.038 1.02 1.02 0 0 0-.296-.649l-2.866-2.866a1.75 1.75 0 0 1-.072-2.4l2.968-3.338a1.05 1.05 0 0 0 .263-.622A1.95 1.95 0 0 0 13.102 3zM13.5 15.65c0 .35-.887.35-3.5.35s-3.5 0-3.5-.35c0-.933 2.345-3.15 3.5-3.15 1.19 0 3.5 2.228 3.5 3.15m-1-9.025c0 .45-1.875 2.5-2.5 2.5s-2.5-2.1-2.5-2.5C7.5 6 8.125 6 10 6s2.5 0 2.5.625",
	"ai-sparkle-filled":       "m13.141 10.421-2.405 1.2a3.73 3.73 0 0 0-1.67 1.668l-1.2 2.405a.465.465 0 0 1-.833 0l-1.2-2.405a3.73 3.73 0 0 0-1.67-1.667l-2.405-1.2a.465.465 0 0 1 0-.833l2.405-1.2a3.73 3.73 0 0 0 1.67-1.668l1.2-2.404a.465.465 0 0 1 .833 0l1.2 2.404a3.73 3.73 0 0 0 1.67 1.668l2.405 1.2a.465.465 0 0 1 0 .832m5.247 5.347-1.03-.517a1.6 1.6 0 0 1-.716-.715l-.518-1.03a.2.2 0 0 0-.357 0l-.518 1.03a1.6 1.6 0 0 1-.715.715l-1.031.517a.199.199 0 0 0 0 .358l1.03.517c.312.152.56.407.716.715l.518 1.03a.2.2 0 0 0 .357 0l.518-1.03a1.6 1.6 0 0 1 .715-.715l1.031-.517a.199.199 0 0 0 0-.358m0-11.894-1.03-.517a1.6 1.6 0 0 1-.716-.715l-.518-1.03a.2.2 0 0 0-.357 0l-.518 1.03a1.6 1.6 0 0 1-.715.715l-1.031.517a.2.2 0 0 0 0 .358l1.03.517c.312.152.56.407.716.715l.518 1.03a.2.2 0 0 0 .357 0l.518-1.03a1.6 1.6 0 0 1 .715-.715l1.031-.517a.2.2 0 0 0 0-.358",
	"check-filled":            "M15.795 3.443a1.25 1.25 0 0 1 1.91 1.614l-9.72 11.5A1.25 1.25 0 0 1 6 16.46l-3.78-5.476a1.25 1.25 0 0 1 2.057-1.42l2.852 4.13z",
	"trash":                   "M11.25 2.5a.75.75 0 0 1 .75.75v.25H8v-.25a.75.75 0 0 1 .75-.75zm-4.75 1v-.25A2.25 2.25 0 0 1 8.75 1h2.5a2.25 2.25 0 0 1 2.25 2.25v.25h2.25A2.25 2.25 0 0 1 18 5.75v1.5a.75.75 0 0 1-.75.75h-.75v8.25a2.25 2.25 0 0 1-2.25 2.25h-8.5a2.25 2.25 0 0 1-2.25-2.25V8h-.75A.75.75 0 0 1 2 7.25v-1.5A2.25 2.25 0 0 1 4.25 3.5zM7.25 5h-3a.75.75 0 0 0-.75.75v.75h13v-.75a.75.75 0 0 0-.75-.75zM5 16.25V8h10v8.25a.75.75 0 0 1-.75.75h-8.5a.75.75 0 0 1-.75-.75m2.5-1.5v-4.5a.75.75 0 0 1 1.5 0v4.5a.75.75 0 0 1-1.5 0m3.5-4.5v4.5a.75.75 0 0 0 1.5 0v-4.5a.75.75 0 0 0-1.5 0",
	"copy":                    "M16.25 3a.75.75 0 0 1 .75.75v8.5a.75.75 0 0 1-.75.75h-8.5a.75.75 0 0 1-.75-.75v-8.5A.75.75 0 0 1 7.75 3zm0 11.5a2.25 2.25 0 0 0 2.25-2.25v-8.5a2.25 2.25 0 0 0-2.25-2.25h-8.5A2.25 2.25 0 0 0 5.5 3.75v8.5a2.25 2.25 0 0 0 2.25 2.25zM1.5 7.75v8.5a2.25 2.25 0 0 0 2.25 2.25h8.5a2.25 2.25 0 0 0 2.25-2.25V16H13v.25a.75.75 0 0 1-.75.75h-8.5a.75.75 0 0 1-.75-.75v-8.5A.75.75 0 0 1 3.75 7H4V5.5h-.25A2.25 2.25 0 0 0 1.5 7.75",
	"refresh":                 "M13.287 2.161A8.5 8.5 0 0 0 2.284 6.436a.75.75 0 1 0 1.361.628 7 7 0 0 1 4.484-3.81A7 7 0 0 1 15.743 6h-2.494a.75.75 0 0 0 0 1.5h4.5a.75.75 0 0 0 .75-.75v-4.5a.75.75 0 0 0-1.5 0v2.93a8.5 8.5 0 0 0-3.712-3.019M6.713 17.84a8.5 8.5 0 0 0 11.124-4.549.75.75 0 1 0-1.383-.58 7 7 0 0 1-4.584 4.035A7 7 0 0 1 4.257 14H6.75a.75.75 0 0 0 0-1.5h-4.5a.75.75 0 0 0-.75.75v4.5a.75.75 0 0 0 1.5 0v-2.93a8.5 8.5 0 0 0 3.713 3.019",
	"link":                    "M17.255 2.695a4.25 4.25 0 0 0-6.01 0L9.47 4.47a.75.75 0 1 0 1.06 1.06l1.776-1.775a2.75 2.75 0 0 1 3.888 0l.051.051a2.75 2.75 0 0 1 0 3.889l-3.18 3.18a2.75 2.75 0 0 1-3.979-.095l-.031-.034a.75.75 0 0 0-1.11 1.01l.03.033a4.25 4.25 0 0 0 6.151.146l3.18-3.18a4.25 4.25 0 0 0 0-6.01zM2.745 17.306a4.25 4.25 0 0 0 6.01 0l1.775-1.775a.75.75 0 1 0-1.06-1.06l-1.776 1.775a2.75 2.75 0 0 1-3.888 0l-.051-.051a2.75 2.75 0 0 1 0-3.889l3.18-3.18a2.75 2.75 0 0 1 3.98.095l.03.034a.75.75 0 0 0 1.11-1.01l-.03-.033a4.25 4.25 0 0 0-6.15-.146l-3.18 3.18a4.25 4.25 0 0 0 0 6.01z",
	"arrow-down-right":        "M11.707 16.768a.75.75 0 0 1 .025-1.061l4.146-3.957H8.854c-1.23 0-3.05-.385-4.583-1.554C2.704 9 1.5 7.026 1.5 4A.75.75 0 0 1 3 4c0 2.573.998 4.1 2.182 5.004 1.219.93 2.7 1.246 3.672 1.246h7.023l-4.145-3.957a.75.75 0 0 1 1.036-1.086l5.5 5.25a.75.75 0 0 1 0 1.086l-5.5 5.25a.75.75 0 0 1-1.061-.025",
}

// blockKitIconNames is Slack's deriveIconName (8/nj): the icon_button
// names Block Kit accepts and the SvgIcon each draws.
var blockKitIconNames = map[string]string{
	"archive": "archive", "book": "book", "bookmark": "bookmark", "bot": "bot", "bug": "bug", "calendar": "calendar",
	"call": "call", "caret-left": "caret-left", "caret-right": "caret-right", "check": "check", "clipboard": "copy",
	"code": "code", "comment": "comment", "compass": "explore", "copy": "copy", "cube": "blocks", "download": "download",
	"edit": "edit", "email": "email", "eye-closed": "eye-closed", "eye-open": "eye-open", "file": "file", "flag": "flag",
	"folder": "folder", "gear": "settings", "globe": "globe", "heart": "heart", "help": "help", "image": "image",
	"info": "info", "key": "key", "lightbulb": "emoji-objects", "link": "link", "map": "map", "mobile": "mobile",
	"new-window": "new-window", "pin": "pin", "plus": "plus", "refine": "refine", "refresh": "refresh", "rocket": "rocket",
	"save": "save", "screen": "screen", "share": "share", "sparkle": "sparkles", "star": "star", "star-filled": "star-filled",
	"tag": "tag", "thumbs-down": "thumbs-down", "thumbs-up": "thumbs-up", "trash": "trash", "upload": "file-upload",
	"user": "user", "warning": "warning",
}

// agentIcon draws the named Slack icon in a size×size (dp) box.
func agentIcon(gtx C, name string, size float32, col color.NRGBA) D {
	if d := agentIconPath(name); d != nil {
		return d.draw(gtx, gtx.Dp(unit.Dp(size)), col)
	}
	return icons.GetSlack(name)(gtx, unit.Dp(size), col)
}

// agentIconPx is agentIcon with the box in pixels.
func agentIconPx(gtx C, name string, px int, col color.NRGBA) {
	if d := agentIconPath(name); d != nil {
		d.draw(gtx, px, col)
		return
	}
	icons.GetSlack(name)(gtx, unit.Dp(float32(px)/gtx.Metric.PxPerDp), col)
}

type svgIcon struct{ subs []subpath }

var (
	svgMu    sync.Mutex
	svgCache = map[string]*svgIcon{}
)

func agentIconPath(name string) *svgIcon {
	src, ok := agentIconPaths[name]
	if !ok {
		return nil
	}
	svgMu.Lock()
	defer svgMu.Unlock()
	if ic := svgCache[name]; ic != nil {
		return ic
	}
	ic := &svgIcon{subs: evenOdd(parseSVG(src))}
	svgCache[name] = ic
	return ic
}

func (ic *svgIcon) draw(gtx C, px int, col color.NRGBA) D {
	sc := float32(px) / 20
	var p theme.PathPen
	p.Begin(gtx.Ops)
	for _, s := range ic.subs {
		p.MoveTo(s.start.Mul(sc))
		for _, g := range s.segs {
			if g.cubic {
				p.CubeTo(g.c1.Mul(sc), g.c2.Mul(sc), g.to.Mul(sc))
			} else {
				p.LineTo(g.to.Mul(sc))
			}
		}
		p.Close()
	}
	paint.FillShape(gtx.Ops, col, clip.Outline{Path: p.End()}.Op())
	return D{Size: image.Pt(px, px)}
}

// ---- SVG path data ----

type pathSeg struct {
	cubic      bool
	c1, c2, to f32.Point
}

type subpath struct {
	start f32.Point
	segs  []pathSeg
}

// parseSVG reads SVG path data (M L H V C S Q T A Z, absolute and
// relative) into closed subpaths of lines and cubics.
func parseSVG(d string) []subpath {
	var out []subpath
	var cur *subpath
	var pos, startPt, lastCtrl f32.Point
	var lastCmd byte
	i := 0
	skip := func() {
		for i < len(d) && (d[i] == ' ' || d[i] == ',' || d[i] == '\n' || d[i] == '\t') {
			i++
		}
	}
	num := func() float32 {
		skip()
		j := i
		if j < len(d) && (d[j] == '-' || d[j] == '+') {
			j++
		}
		dot := false
		for j < len(d) {
			c := d[j]
			if c >= '0' && c <= '9' {
				j++
			} else if c == '.' && !dot {
				dot = true
				j++
			} else if (c == 'e' || c == 'E') && j+1 < len(d) {
				j++
				if d[j] == '-' || d[j] == '+' {
					j++
				}
			} else {
				break
			}
		}
		v, _ := strconv.ParseFloat(d[i:j], 32)
		i = j
		return float32(v)
	}
	flag := func() bool {
		skip()
		if i < len(d) && (d[i] == '0' || d[i] == '1') {
			i++
			return d[i-1] == '1'
		}
		return num() != 0
	}
	begin := func(p f32.Point) {
		out = append(out, subpath{start: p})
		cur = &out[len(out)-1]
		startPt = p
	}
	line := func(p f32.Point) {
		if cur == nil {
			begin(pos)
		}
		cur.segs = append(cur.segs, pathSeg{to: p})
		pos = p
	}
	cube := func(c1, c2, p f32.Point) {
		if cur == nil {
			begin(pos)
		}
		cur.segs = append(cur.segs, pathSeg{cubic: true, c1: c1, c2: c2, to: p})
		lastCtrl = c2
		pos = p
	}
	for {
		skip()
		if i >= len(d) {
			break
		}
		cmd := d[i]
		if (cmd >= 'a' && cmd <= 'z') || (cmd >= 'A' && cmd <= 'Z') {
			i++
		} else if lastCmd != 0 {
			cmd = lastCmd // implicit repeat
			if cmd == 'M' {
				cmd = 'L'
			} else if cmd == 'm' {
				cmd = 'l'
			}
		} else {
			break
		}
		rel := cmd >= 'a'
		off := f32.Point{}
		if rel {
			off = pos
		}
		pt := func() f32.Point { x := num(); y := num(); return f32.Pt(x, y).Add(off) }
		prevCubic := lastCmd == 'C' || lastCmd == 'c' || lastCmd == 'S' || lastCmd == 's'
		switch cmd {
		case 'M', 'm':
			pos = pt()
			begin(pos)
		case 'L', 'l':
			line(pt())
		case 'H', 'h':
			x := num()
			if rel {
				x += pos.X
			}
			line(f32.Pt(x, pos.Y))
		case 'V', 'v':
			y := num()
			if rel {
				y += pos.Y
			}
			line(f32.Pt(pos.X, y))
		case 'C', 'c':
			c1 := pt()
			c2 := pt()
			cube(c1, c2, pt())
		case 'S', 's':
			c1 := pos
			if prevCubic {
				c1 = pos.Mul(2).Sub(lastCtrl)
			}
			c2 := pt()
			cube(c1, c2, pt())
		case 'Q', 'q':
			q := pt()
			p := pt()
			cube(pos.Add(q.Sub(pos).Mul(2.0/3)), p.Add(q.Sub(p).Mul(2.0/3)), p)
		case 'A', 'a':
			rx, ry := num(), num()
			rot := num()
			large, sweep := flag(), flag()
			p := pt()
			for _, c := range arcCubics(pos, p, rx, ry, rot, large, sweep) {
				cube(c[0], c[1], c[2])
			}
			pos = p
		case 'Z', 'z':
			pos = startPt
			cur = nil
		default:
			return out
		}
		lastCmd = cmd
		if cmd == 'Z' || cmd == 'z' {
			lastCmd = 0
		}
	}
	return out
}

// arcCubics converts an SVG elliptical arc to cubic Béziers.
func arcCubics(p0, p1 f32.Point, rx, ry, rotDeg float32, large, sweep bool) [][3]f32.Point {
	if p0 == p1 {
		return nil
	}
	if rx == 0 || ry == 0 {
		return [][3]f32.Point{{p0, p1, p1}}
	}
	rxf, ryf := math.Abs(float64(rx)), math.Abs(float64(ry))
	phi := float64(rotDeg) * math.Pi / 180
	cs, sn := math.Cos(phi), math.Sin(phi)
	dx, dy := float64(p0.X-p1.X)/2, float64(p0.Y-p1.Y)/2
	x1 := cs*dx + sn*dy
	y1 := -sn*dx + cs*dy
	lam := x1*x1/(rxf*rxf) + y1*y1/(ryf*ryf)
	if lam > 1 {
		s := math.Sqrt(lam)
		rxf, ryf = rxf*s, ryf*s
	}
	num := rxf*rxf*ryf*ryf - rxf*rxf*y1*y1 - ryf*ryf*x1*x1
	den := rxf*rxf*y1*y1 + ryf*ryf*x1*x1
	co := 0.0
	if den != 0 && num > 0 {
		co = math.Sqrt(num / den)
	}
	if large == sweep {
		co = -co
	}
	cxp := co * rxf * y1 / ryf
	cyp := -co * ryf * x1 / rxf
	cx := cs*cxp - sn*cyp + float64(p0.X+p1.X)/2
	cy := sn*cxp + cs*cyp + float64(p0.Y+p1.Y)/2
	ang := func(ux, uy, vx, vy float64) float64 {
		a := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
		return a
	}
	t1 := ang(1, 0, (x1-cxp)/rxf, (y1-cyp)/ryf)
	dt := ang((x1-cxp)/rxf, (y1-cyp)/ryf, (-x1-cxp)/rxf, (-y1-cyp)/ryf)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dt) / (math.Pi / 2)))
	if n < 1 {
		n = 1
	}
	step := dt / float64(n)
	k := 4.0 / 3 * math.Tan(step/4)
	pt := func(t float64) (float64, float64, float64, float64) {
		ct, st := math.Cos(t), math.Sin(t)
		x := cx + rxf*ct*cs - ryf*st*sn
		y := cy + rxf*ct*sn + ryf*st*cs
		ddx := -rxf*st*cs - ryf*ct*sn
		ddy := -rxf*st*sn + ryf*ct*cs
		return x, y, ddx, ddy
	}
	var out [][3]f32.Point
	t := t1
	for j := 0; j < n; j++ {
		ax, ay, adx, ady := pt(t)
		bx, by, bdx, bdy := pt(t + step)
		c1 := f32.Pt(float32(ax+k*adx), float32(ay+k*ady))
		c2 := f32.Pt(float32(bx-k*bdx), float32(by-k*bdy))
		end := f32.Pt(float32(bx), float32(by))
		if j == n-1 {
			end = p1
		}
		out = append(out, [3]f32.Point{c1, c2, end})
		t += step
	}
	return out
}

// ---- even-odd under a nonzero fill ----

// evenOdd rewinds subpaths so a nonzero fill draws what evenodd would:
// a subpath nested in an odd number of others winds against the outer.
func evenOdd(subs []subpath) []subpath {
	polys := make([][]f32.Point, len(subs))
	for i, s := range subs {
		polys[i] = flatten(s)
	}
	out := make([]subpath, len(subs))
	for i, s := range subs {
		depth := 0
		if len(polys[i]) > 0 {
			probe := polys[i][0]
			for j := range subs {
				if j != i && area(polys[j]) != 0 && inside(polys[j], probe) && math.Abs(area(polys[j])) > math.Abs(area(polys[i])) {
					depth++
				}
			}
		}
		want := depth%2 == 0
		if (area(polys[i]) >= 0) != want {
			s = reverse(s)
		}
		out[i] = s
	}
	return out
}

func flatten(s subpath) []f32.Point {
	pts := []f32.Point{s.start}
	p := s.start
	for _, g := range s.segs {
		if g.cubic {
			for k := 1; k <= 8; k++ {
				t := float32(k) / 8
				u := 1 - t
				q := p.Mul(u * u * u).Add(g.c1.Mul(3 * u * u * t)).Add(g.c2.Mul(3 * u * t * t)).Add(g.to.Mul(t * t * t))
				pts = append(pts, q)
			}
		} else {
			pts = append(pts, g.to)
		}
		p = g.to
	}
	return pts
}

func area(ps []f32.Point) float64 {
	a := 0.0
	for i := range ps {
		j := (i + 1) % len(ps)
		a += float64(ps[i].X*ps[j].Y - ps[j].X*ps[i].Y)
	}
	return a / 2
}

func inside(poly []f32.Point, p f32.Point) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.Y > p.Y) != (b.Y > p.Y) && p.X < (b.X-a.X)*(p.Y-a.Y)/(b.Y-a.Y)+a.X {
			in = !in
		}
	}
	return in
}

func reverse(s subpath) subpath {
	pts := make([]f32.Point, 0, len(s.segs)+1)
	pts = append(pts, s.start)
	for _, g := range s.segs {
		pts = append(pts, g.to)
	}
	n := len(s.segs)
	r := subpath{start: pts[n]}
	for i := n - 1; i >= 0; i-- {
		g := s.segs[i]
		if g.cubic {
			r.segs = append(r.segs, pathSeg{cubic: true, c1: g.c2, c2: g.c1, to: pts[i]})
		} else {
			r.segs = append(r.segs, pathSeg{to: pts[i]})
		}
	}
	return r
}
