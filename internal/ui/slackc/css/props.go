package css

import (
	"image/color"
	"strings"
)

// Property names: aliases (vendor prefixes, logical properties mapped to
// physical ones for horizontal LTR text), shorthands expanded into
// longhands at parse time, and parse-time validation so an invalid
// declaration drops out of the cascade the way it does in browsers.

var propAlias = map[string]string{
	"-webkit-box-shadow": "box-shadow", "-moz-box-shadow": "box-shadow",
	"-webkit-transform": "transform", "-moz-transform": "transform", "-ms-transform": "transform", "-o-transform": "transform",
	"-webkit-transform-origin": "transform-origin", "-ms-transform-origin": "transform-origin",
	"-webkit-filter": "filter", "-webkit-backdrop-filter": "backdrop-filter",
	"-webkit-border-radius": "border-radius", "-moz-border-radius": "border-radius",
	"-webkit-border-top-left-radius": "border-top-left-radius", "-webkit-border-top-right-radius": "border-top-right-radius",
	"-webkit-border-bottom-left-radius": "border-bottom-left-radius", "-webkit-border-bottom-right-radius": "border-bottom-right-radius",
	"-webkit-flex": "flex", "-ms-flex": "flex", "-webkit-flex-direction": "flex-direction", "-ms-flex-direction": "flex-direction",
	"-webkit-flex-wrap": "flex-wrap", "-ms-flex-wrap": "flex-wrap", "-webkit-flex-flow": "flex-flow", "-ms-flex-flow": "flex-flow",
	"-webkit-flex-grow": "flex-grow", "-webkit-flex-shrink": "flex-shrink", "-webkit-flex-basis": "flex-basis",
	"-ms-flex-positive": "flex-grow", "-ms-flex-negative": "flex-shrink", "-ms-flex-preferred-size": "flex-basis",
	"-webkit-align-items": "align-items", "-webkit-align-self": "align-self", "-webkit-align-content": "align-content",
	"-webkit-justify-content": "justify-content", "-webkit-order": "order", "-ms-flex-order": "order",
	"-ms-flex-align": "align-items", "-ms-flex-pack": "justify-content",
	"-webkit-box-sizing": "box-sizing", "-moz-box-sizing": "box-sizing",
	"-webkit-user-select": "user-select", "-moz-user-select": "user-select", "-ms-user-select": "user-select",
	"-webkit-opacity": "opacity", "-moz-opacity": "opacity",
	"-webkit-text-decoration": "text-decoration", "-webkit-text-decoration-line": "text-decoration-line",
	"-webkit-text-decoration-color": "text-decoration-color", "-webkit-text-decoration-style": "text-decoration-style",
	"-webkit-appearance": "appearance", "-moz-appearance": "appearance",
	"-webkit-transition": "transition", "-moz-transition": "transition", "-o-transition": "transition",
	"-webkit-animation": "animation", "-webkit-animation-name": "animation-name", "-webkit-animation-duration": "animation-duration",
	"-o-text-overflow": "text-overflow", "-ms-text-overflow": "text-overflow",
	"word-wrap": "overflow-wrap", "-webkit-word-break": "word-break",
	"grid-row-gap": "row-gap", "grid-column-gap": "column-gap", "-webkit-column-gap": "column-gap",
	"-webkit-background-clip": "background-clip", "-webkit-background-size": "background-size", "-webkit-background-origin": "background-origin",
	"-webkit-mix-blend-mode": "mix-blend-mode", "-webkit-object-fit": "object-fit",
	"-webkit-aspect-ratio": "aspect-ratio", "-webkit-cursor": "cursor", "-webkit-hyphens": "hyphens",
	"-webkit-perspective": "perspective", "-webkit-backface-visibility": "backface-visibility",
	"-webkit-transform-style": "transform-style", "-webkit-clip-path": "clip-path",
	"-webkit-white-space": "white-space", "-webkit-font-feature-settings": "font-feature-settings",
	"-webkit-pointer-events": "pointer-events",

	"margin-inline-start": "margin-left", "margin-inline-end": "margin-right",
	"margin-block-start": "margin-top", "margin-block-end": "margin-bottom",
	"padding-inline-start": "padding-left", "padding-inline-end": "padding-right",
	"padding-block-start": "padding-top", "padding-block-end": "padding-bottom",
	"inset-inline-start": "left", "inset-inline-end": "right", "inset-block-start": "top", "inset-block-end": "bottom",
	"-webkit-margin-start": "margin-left", "-webkit-margin-end": "margin-right",
	"-webkit-padding-start": "padding-left", "-webkit-padding-end": "padding-right",
	"border-inline-start": "border-left", "border-inline-end": "border-right",
	"border-block-start": "border-top", "border-block-end": "border-bottom",
	"border-inline-start-width": "border-left-width", "border-inline-end-width": "border-right-width",
	"border-block-start-width": "border-top-width", "border-block-end-width": "border-bottom-width",
	"border-inline-start-style": "border-left-style", "border-inline-end-style": "border-right-style",
	"border-block-start-style": "border-top-style", "border-block-end-style": "border-bottom-style",
	"border-inline-start-color": "border-left-color", "border-inline-end-color": "border-right-color",
	"border-block-start-color": "border-top-color", "border-block-end-color": "border-bottom-color",
	"border-start-start-radius": "border-top-left-radius", "border-start-end-radius": "border-top-right-radius",
	"border-end-start-radius": "border-bottom-left-radius", "border-end-end-radius": "border-bottom-right-radius",
	"inline-size": "width", "block-size": "height", "min-inline-size": "min-width", "min-block-size": "min-height",
	"max-inline-size": "max-width", "max-block-size": "max-height",
	"overflow-inline": "overflow-x", "overflow-block": "overflow-y",
	"contain-intrinsic-inline-size": "contain-intrinsic-width", "contain-intrinsic-block-size": "contain-intrinsic-height",
}

// shorthand expanders return one value per longhand, or nil when invalid.
type shorthand struct {
	longs  []string
	expand func(src string, cs []cv) []string
}

var shorthands map[string]*shorthand

func sides(p, suffix string) []string {
	return []string{p + "-top" + suffix, p + "-right" + suffix, p + "-bottom" + suffix, p + "-left" + suffix}
}

func init() {
	shorthands = map[string]*shorthand{}
	add := func(name string, longs []string, fn func(longs []string) func(string, []cv) []string) {
		shorthands[name] = &shorthand{longs: longs, expand: fn(longs)}
	}
	add("margin", sides("margin", ""), box4)
	add("padding", sides("padding", ""), box4)
	add("inset", []string{"top", "right", "bottom", "left"}, box4)
	add("border-width", sides("border", "-width"), box4)
	add("border-style", sides("border", "-style"), box4)
	add("border-color", sides("border", "-color"), box4)
	add("margin-inline", []string{"margin-left", "margin-right"}, pair)
	add("margin-block", []string{"margin-top", "margin-bottom"}, pair)
	add("padding-inline", []string{"padding-left", "padding-right"}, pair)
	add("padding-block", []string{"padding-top", "padding-bottom"}, pair)
	add("inset-inline", []string{"left", "right"}, pair)
	add("inset-block", []string{"top", "bottom"}, pair)
	add("border-inline-width", []string{"border-left-width", "border-right-width"}, pair)
	add("border-block-width", []string{"border-top-width", "border-bottom-width"}, pair)
	add("border-inline-style", []string{"border-left-style", "border-right-style"}, pair)
	add("border-block-style", []string{"border-top-style", "border-bottom-style"}, pair)
	add("border-inline-color", []string{"border-left-color", "border-right-color"}, pair)
	add("border-block-color", []string{"border-top-color", "border-bottom-color"}, pair)
	add("overflow", []string{"overflow-x", "overflow-y"}, pair)
	add("gap", []string{"row-gap", "column-gap"}, pair)
	add("grid-gap", []string{"row-gap", "column-gap"}, pair)
	add("place-content", []string{"align-content", "justify-content"}, pair)
	add("place-items", []string{"align-items", "justify-items"}, pair)
	add("place-self", []string{"align-self", "justify-self"}, pair)
	for _, s := range []string{"top", "right", "bottom", "left"} {
		add("border-"+s, []string{"border-" + s + "-width", "border-" + s + "-style", "border-" + s + "-color"}, borderSide)
	}
	add("outline", []string{"outline-width", "outline-style", "outline-color"}, borderSide)
	add("border", append(append(sides("border", "-width"), sides("border", "-style")...), sides("border", "-color")...), borderAll)
	add("border-inline", []string{"border-left-width", "border-right-width", "border-left-style", "border-right-style", "border-left-color", "border-right-color"}, borderAll)
	add("border-block", []string{"border-top-width", "border-bottom-width", "border-top-style", "border-bottom-style", "border-top-color", "border-bottom-color"}, borderAll)
	add("border-radius", []string{"border-top-left-radius", "border-top-right-radius", "border-bottom-right-radius", "border-bottom-left-radius"}, radius)
	add("flex", []string{"flex-grow", "flex-shrink", "flex-basis"}, flexShort)
	add("flex-flow", []string{"flex-direction", "flex-wrap"}, anyOrder)
	add("text-decoration", []string{"text-decoration-line", "text-decoration-style", "text-decoration-color", "text-decoration-thickness"}, textDeco)
	add("background", []string{"background-color", "background-image", "background-position", "background-size",
		"background-repeat", "background-attachment", "background-origin", "background-clip"}, background)
	add("font", []string{"font-style", "font-variant", "font-weight", "font-stretch", "font-size", "line-height", "font-family"}, font)
	add("grid-template", []string{"grid-template-rows", "grid-template-columns", "grid-template-areas"}, gridTemplate)
	add("grid", []string{"grid-template-rows", "grid-template-columns", "grid-template-areas"}, gridTemplate)
	add("list-style", []string{"list-style-type", "list-style-position", "list-style-image"}, acceptAll)
	add("-webkit-text-stroke", []string{"-webkit-text-stroke-width", "-webkit-text-stroke-color"}, acceptAll)
	add("columns", []string{"column-width", "column-count"}, acceptAll)
}

// validLonghand checks a longhand value at parse time.
func validLonghand(prop, val string) bool {
	val = strings.TrimSpace(val)
	if hasVar(val) || wideKeyword(val) != "" {
		return true
	}
	st := Initial(color.NRGBA{A: 255})
	cx := cctx{st: &st, validate: true, env: Env{Width: 1000, Height: 1000}}
	return cx.apply(prop, val)
}

// knownLonghand reports a longhand Clack knows (computed or accepted).
func knownLonghand(p string) bool {
	if acceptedProps[p] || inheritedProps[p] {
		return true
	}
	_, ok := computedProps[p]
	return ok
}

var computedProps = map[string]struct{}{}

func init() {
	for _, p := range strings.Fields(`display position visibility z-index top right bottom left width height
min-width min-height max-width max-height box-sizing aspect-ratio margin-top margin-right margin-bottom margin-left
padding-top padding-right padding-bottom padding-left border-top-width border-right-width border-bottom-width border-left-width
border-top-style border-right-style border-bottom-style border-left-style border-top-color border-right-color border-bottom-color
border-left-color border-top-left-radius border-top-right-radius border-bottom-right-radius border-bottom-left-radius
outline-width outline-style outline-color outline-offset flex-direction flex-wrap justify-content align-items align-self
align-content justify-items justify-self row-gap column-gap flex-grow flex-shrink flex-basis order -webkit-box-orient
-webkit-box-direction -webkit-box-pack -webkit-box-align -webkit-box-flex grid-template-columns grid-template-areas
overflow-x overflow-y opacity transform translate rotate scale transform-origin filter backdrop-filter box-shadow
text-shadow background-color background-image background-size background-position background-repeat mix-blend-mode
color font-family font-size font-weight font-style line-height letter-spacing word-spacing text-align
text-decoration-line text-decoration-color text-transform white-space text-wrap text-wrap-mode text-overflow
-webkit-line-clamp line-clamp word-break overflow-wrap vertical-align cursor pointer-events user-select content
fill stroke stroke-width object-fit`) {
		computedProps[p] = struct{}{}
	}
}

// expandDecl appends the longhands of one declaration to out. Invalid
// declarations (unknown property, bad value) append nothing.
func expandDecl(name, val string, imp bool, out Decls) Decls {
	val = strings.TrimSpace(val)
	if strings.HasPrefix(name, "--") {
		return append(out, Decl{Prop: name, Value: val, Important: imp})
	}
	if a, ok := propAlias[name]; ok {
		name = a
	}
	if name == "all" {
		k := wideKeyword(val)
		if k == "" {
			if !hasVar(val) {
				return out
			}
		}
		for _, p := range allLonghands {
			if k != "" {
				out = append(out, Decl{Prop: p, Value: k, Important: imp})
			} else {
				out = append(out, Decl{Prop: p, Value: val, Important: imp, Shorthand: "all"})
			}
		}
		return out
	}
	if sh, ok := shorthands[name]; ok {
		if k := wideKeyword(val); k != "" {
			for _, l := range sh.longs {
				out = append(out, Decl{Prop: l, Value: k, Important: imp})
			}
			return out
		}
		if hasVar(val) {
			for _, l := range sh.longs {
				out = append(out, Decl{Prop: l, Value: val, Important: imp, Shorthand: name})
			}
			return out
		}
		vals := sh.expand(val, parseCVs(val))
		if vals == nil {
			return out
		}
		for i, l := range sh.longs {
			out = append(out, Decl{Prop: l, Value: vals[i], Important: imp})
		}
		return out
	}
	if !knownLonghand(name) {
		return out
	}
	if !validLonghand(name, val) {
		return out
	}
	return append(out, Decl{Prop: name, Value: val, Important: imp})
}

// expandPending expands a pending-substitution shorthand value and
// returns the longhand prop's part.
func expandPending(short, prop, val string) (string, bool) {
	if short == "all" {
		if k := wideKeyword(strings.TrimSpace(val)); k != "" {
			return k, true
		}
		return "", false
	}
	sh, ok := shorthands[short]
	if !ok {
		return "", false
	}
	if k := wideKeyword(strings.TrimSpace(val)); k != "" {
		return k, true
	}
	vals := sh.expand(val, parseCVs(val))
	if vals == nil {
		return "", false
	}
	for i, l := range sh.longs {
		if l == prop {
			return vals[i], true
		}
	}
	return "", false
}

var allLonghands []string

func init() {
	for p := range computedProps {
		switch p {
		case "direction", "unicode-bidi":
			continue
		}
		allLonghands = append(allLonghands, p)
	}
	// deterministic order
	for i := 1; i < len(allLonghands); i++ {
		for j := i; j > 0 && allLonghands[j] < allLonghands[j-1]; j-- {
			allLonghands[j], allLonghands[j-1] = allLonghands[j-1], allLonghands[j]
		}
	}
}

// texts returns each cv's source text.
func texts(src string, cs []cv) []string {
	out := make([]string, len(cs))
	for i := range cs {
		out[i] = cs[i].text(src)
	}
	return out
}

func box4(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		if len(cs) < 1 || len(cs) > 4 {
			return nil
		}
		t := texts(src, cs)
		var v [4]string
		switch len(t) {
		case 1:
			v = [4]string{t[0], t[0], t[0], t[0]}
		case 2:
			v = [4]string{t[0], t[1], t[0], t[1]}
		case 3:
			v = [4]string{t[0], t[1], t[2], t[1]}
		case 4:
			v = [4]string{t[0], t[1], t[2], t[3]}
		}
		for i := range v {
			if !validLonghand(longs[i], v[i]) {
				return nil
			}
		}
		return v[:]
	}
}

func pair(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		var t []string
		// place-*: "safe center" style prefixes keep their word pairs
		if strings.HasPrefix(longs[0], "align") {
			t = alignPairs(src, cs)
		} else {
			t = texts(src, cs)
		}
		if len(t) < 1 || len(t) > 2 {
			return nil
		}
		if len(t) == 1 {
			t = append(t, t[0])
			if longs[1] == "justify-items" && t[0] == "baseline" {
				t[1] = "baseline"
			}
		}
		for i := range t {
			if !validLonghand(longs[i], t[i]) {
				return nil
			}
		}
		return t
	}
}

// alignPairs groups "safe center" / "first baseline" as one value.
func alignPairs(src string, cs []cv) []string {
	var out []string
	for i := 0; i < len(cs); i++ {
		switch strings.ToLower(cs[i].t.s) {
		case "safe", "unsafe", "first", "last":
			if cs[i].t.k == tIdent && i+1 < len(cs) {
				out = append(out, src[cs[i].t.start:cs[i+1].end])
				i++
				continue
			}
		}
		out = append(out, cs[i].text(src))
	}
	return out
}

func anyOrder(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		vals := make([]string, len(longs))
		initial := map[string]string{"flex-direction": "row", "flex-wrap": "nowrap"}
		for i, l := range longs {
			vals[i] = initial[l]
		}
		used := make([]bool, len(longs))
		for i := range cs {
			t := cs[i].text(src)
			ok := false
			for j, l := range longs {
				if !used[j] && validLonghand(l, t) {
					vals[j], used[j], ok = t, true, true
					break
				}
			}
			if !ok {
				return nil
			}
		}
		return vals
	}
}

// borderSide: width || style || color.
func borderSide(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		if len(cs) < 1 || len(cs) > 3 {
			return nil
		}
		w, s, c := "medium", "none", "currentcolor"
		var hw, hs, hc bool
		for i := range cs {
			t := cs[i].text(src)
			switch {
			case !hs && validLonghand(longs[1], t):
				s, hs = t, true
			case !hw && validLonghand(longs[0], t):
				w, hw = t, true
			case !hc && validLonghand(longs[2], t):
				c, hc = t, true
			default:
				return nil
			}
		}
		return []string{w, s, c}
	}
}

// borderAll expands border / border-inline / border-block: longs are the
// width sides, then the style sides, then the color sides.
func borderAll(longs []string) func(string, []cv) []string {
	n := len(longs) / 3
	side := borderSide([]string{longs[0], longs[n], longs[2*n]})
	return func(src string, cs []cv) []string {
		v := side(src, cs)
		if v == nil {
			return nil
		}
		out := make([]string, len(longs))
		for i := range out {
			out[i] = v[i/n]
		}
		return out
	}
}

func radius(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		parts := splitDelim(cs, '/')
		if len(parts) > 2 {
			return nil
		}
		h := box4(longs)(src, parts[0])
		if h == nil {
			return nil
		}
		if len(parts) == 2 {
			// the vertical radii must be valid too; Clack draws the horizontal
			if box4(longs)(src, parts[1]) == nil {
				return nil
			}
		}
		return h
	}
}

func flexShort(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		switch kw(cs) {
		case "none":
			return []string{"0", "0", "auto"}
		case "auto":
			return []string{"1", "1", "auto"}
		case "initial":
			return []string{"0", "1", "auto"}
		}
		if len(cs) < 1 || len(cs) > 3 {
			return nil
		}
		var nums []string
		basis, basisAt := "", -1
		for i := range cs {
			c := &cs[i]
			t := c.text(src)
			if c.t.k == tNumber || c.t.k == tFunction && isMathFn(c.name) && validLonghand("flex-grow", t) && !validLonghand("flex-basis", t) {
				nums = append(nums, t)
				continue
			}
			if basis != "" || !validLonghand("flex-basis", t) {
				return nil
			}
			basis, basisAt = t, i
		}
		if len(nums) == 3 && basis == "" {
			basis, nums = nums[2], nums[:2]
		}
		if len(nums) > 2 || len(nums) == 2 && basisAt == 1 {
			return nil
		}
		grow, shrink := "1", "1"
		if len(nums) > 0 {
			grow = nums[0]
			if basis == "" {
				basis = "0%"
			}
		}
		if len(nums) > 1 {
			shrink = nums[1]
		}
		if !validLonghand("flex-grow", grow) || !validLonghand("flex-shrink", shrink) || !validLonghand("flex-basis", basis) {
			return nil
		}
		return []string{grow, shrink, basis}
	}
}

func textDeco(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		var lines []string
		style, col, thick := "solid", "currentcolor", "auto"
		var hs, hc, ht bool
		for i := range cs {
			t := cs[i].text(src)
			k := strings.ToLower(t)
			switch {
			case k == "none" && len(lines) == 0:
				lines = append(lines, "none")
			case k == "underline" || k == "overline" || k == "line-through" || k == "blink":
				lines = append(lines, k)
			case !hs && (k == "solid" || k == "double" || k == "dotted" || k == "dashed" || k == "wavy"):
				style, hs = k, true
			case !hc && validLonghand("text-decoration-color", t):
				col, hc = t, true
			case !ht && (k == "auto" || k == "from-font" || cs[i].t.k == tDimension || cs[i].t.k == tPercentage):
				thick, ht = t, true
			default:
				return nil
			}
		}
		line := "none"
		if len(lines) > 0 {
			line = strings.Join(lines, " ")
		}
		if !validLonghand("text-decoration-line", line) {
			return nil
		}
		return []string{line, style, col, thick}
	}
}

func acceptAll(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		out := make([]string, len(longs))
		for i := range out {
			out[i] = src
		}
		return out
	}
}

func gridTemplate(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		if kw(cs) == "none" {
			return []string{"none", "none", "none"}
		}
		parts := splitDelim(cs, '/')
		rows, cols, areas := "none", "none", "none"
		var strs []string
		var rowParts []cv
		for _, c := range parts[0] {
			if c.t.k == tString {
				strs = append(strs, c.text(src))
			} else {
				rowParts = append(rowParts, c)
			}
		}
		if len(strs) > 0 {
			areas = strings.Join(strs, " ")
		}
		if len(rowParts) > 0 {
			rows = spanText(src, rowParts)
		}
		if len(parts) == 2 {
			var cp []cv
			for _, c := range parts[1] {
				switch strings.ToLower(c.t.s) {
				case "auto-flow", "dense":
					if c.t.k == tIdent {
						continue
					}
				}
				cp = append(cp, c)
			}
			if len(cp) > 0 {
				cols = spanText(src, cp)
				if !validLonghand("grid-template-columns", cols) {
					return nil
				}
			}
		} else if len(parts) > 2 {
			return nil
		}
		return []string{rows, cols, areas}
	}
}

// background: [ <bg-layer> , ]* <final-bg-layer>
func background(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		layers := splitComma(cs)
		var imgs, poss, sizes, reps, atts, origs, clips []string
		bg := "transparent"
		for li, layer := range layers {
			img, pos, size, rep, att, orig, clip := "none", "0% 0%", "auto", "repeat", "scroll", "padding-box", "border-box"
			var hImg, hPos, hRep, hAtt, hBox int
			for i := 0; i < len(layer); i++ {
				c := &layer[i]
				t := c.text(src)
				k := strings.ToLower(t)
				switch {
				case hImg == 0 && validLonghand("background-image", t) && !(c.t.k == tIdent && k != "none"):
					img, hImg = t, 1
				case hRep == 0 && isRepeatKw(k):
					rep = t
					if i+1 < len(layer) && isRepeatKw(strings.ToLower(layer[i+1].text(src))) && !strings.HasPrefix(k, "repeat-") {
						rep = src[c.t.start:layer[i+1].end]
						i++
					}
					hRep = 1
				case hAtt == 0 && (k == "scroll" || k == "fixed" || k == "local"):
					att, hAtt = k, 1
				case hBox < 2 && (k == "border-box" || k == "padding-box" || k == "content-box" || k == "text"):
					if hBox == 0 {
						orig, clip = k, k
					} else {
						clip = k
					}
					hBox++
				case hPos == 0 && isPosCV(c):
					j := i
					for j < len(layer) && isPosCV(&layer[j]) && j-i < 4 {
						j++
					}
					pos = spanText(src, layer[i:j])
					hPos = 1
					i = j - 1
					if j < len(layer) && layer[j].t.is('/') {
						k2 := j + 1
						for k2 < len(layer) && k2-(j+1) < 2 && (isSizeCV(&layer[k2])) {
							k2++
						}
						if k2 == j+1 {
							return nil
						}
						size = spanText(src, layer[j+1:k2])
						i = k2 - 1
					}
				case li == len(layers)-1 && bg == "transparent" && validLonghand("background-color", t):
					bg = t
				default:
					return nil
				}
			}
			if hBox == 1 && orig == "text" {
				return nil
			}
			imgs, poss, sizes, reps = append(imgs, img), append(poss, pos), append(sizes, size), append(reps, rep)
			atts, origs, clips = append(atts, att), append(origs, orig), append(clips, clip)
		}
		j := func(s []string) string { return strings.Join(s, ", ") }
		out := []string{bg, j(imgs), j(poss), j(sizes), j(reps), j(atts), j(origs), j(clips)}
		if !validLonghand("background-position", out[2]) || !validLonghand("background-size", out[3]) {
			return nil
		}
		return out
	}
}

func isRepeatKw(k string) bool {
	switch k {
	case "repeat", "repeat-x", "repeat-y", "no-repeat", "space", "round":
		return true
	}
	return false
}

func isPosCV(c *cv) bool {
	switch c.t.k {
	case tIdent:
		switch strings.ToLower(c.t.s) {
		case "left", "right", "top", "bottom", "center":
			return true
		}
		return false
	case tPercentage, tDimension:
		return true
	case tNumber:
		return c.t.num == 0
	case tFunction:
		return isMathFn(c.name)
	}
	return false
}

func isSizeCV(c *cv) bool {
	switch c.t.k {
	case tIdent:
		switch strings.ToLower(c.t.s) {
		case "auto", "cover", "contain":
			return true
		}
		return false
	case tPercentage, tDimension:
		return true
	case tNumber:
		return c.t.num == 0
	case tFunction:
		return isMathFn(c.name)
	}
	return false
}

// font: [style || variant || weight || stretch]? size [/ line-height]? family
func font(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		switch kw(cs) {
		case "caption", "icon", "menu", "message-box", "small-caption", "status-bar":
			return []string{"normal", "normal", "normal", "normal", "13px", "normal", "system-ui"}
		}
		style, variant, weight, stretch := "normal", "normal", "normal", "normal"
		i := 0
		for ; i < len(cs) && i < 4; i++ {
			c := &cs[i]
			t := c.text(src)
			k := strings.ToLower(t)
			if c.t.k == tIdent {
				switch k {
				case "normal":
					continue
				case "italic", "oblique":
					style = k
					continue
				case "small-caps":
					variant = k
					continue
				case "bold", "bolder", "lighter":
					weight = k
					continue
				case "ultra-condensed", "extra-condensed", "condensed", "semi-condensed", "semi-expanded",
					"expanded", "extra-expanded", "ultra-expanded":
					stretch = k
					continue
				}
				break
			}
			if c.t.k == tNumber && c.t.num >= 1 && c.t.num <= 1000 {
				weight = t
				continue
			}
			break
		}
		if i >= len(cs) {
			return nil
		}
		size := cs[i].text(src)
		if !validLonghand("font-size", size) {
			return nil
		}
		i++
		lh := "normal"
		if i < len(cs) && cs[i].t.is('/') {
			if i+1 >= len(cs) {
				return nil
			}
			lh = cs[i+1].text(src)
			if !validLonghand("line-height", lh) {
				return nil
			}
			i += 2
		}
		if i >= len(cs) {
			return nil
		}
		fam := spanText(src, cs[i:])
		if !validLonghand("font-family", fam) {
			return nil
		}
		return []string{style, variant, weight, stretch, size, lh, fam}
	}
}
