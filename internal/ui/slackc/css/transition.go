package css

import "strings"

// CSS transitions: the transition shorthand and the three longhands that
// say whether, how long and how late a property animates. Clack's natives
// draw a handful of Slack's transitions themselves (slackc's
// nativeTransitionsCSS lists them); they ask TransitionFor for their own
// property, so a theme, a plugin (Slick's Snappy collapses every
// transition Slack's stylesheets declare to .01ms) or user.css changes
// them the way it would in a browser. Timing functions and
// transition-behavior parse and compute to nothing: natives keep Slack's
// curves.

// TransitionFor is the duration and delay (seconds) of a transition of
// prop (a longhand such as "opacity" or "height"): the last
// transition-property entry naming prop or "all" picks the index, and the
// duration and delay lists repeat to cover it, as in CSS Transitions §2.
// ok is false when prop doesn't transition (transition-property none, or
// a list without it): it changes at once.
func (s *Style) TransitionFor(prop string) (dur, delay float32, ok bool) {
	idx := -1
	if s.TransitionProperty == nil {
		idx = 0 // initial: all
	}
	for i, p := range s.TransitionProperty {
		if p == prop || p == "all" {
			idx = i
		}
	}
	if idx < 0 {
		return 0, 0, false
	}
	return listAt(s.TransitionDuration, idx), listAt(s.TransitionDelay, idx), true
}

func listAt(l []float32, i int) float32 {
	if len(l) == 0 {
		return 0
	}
	return l[i%len(l)]
}

// timeOf parses a <time> (1s, .25s, 80ms). CSS has no unitless times.
func timeOf(c *cv) (float32, bool) {
	if c.t.k != tDimension {
		return 0, false
	}
	switch strings.ToLower(c.t.s) {
	case "s":
		return float32(c.t.num), true
	case "ms":
		return float32(c.t.num / 1000), true
	}
	return 0, false
}

// timeList parses a comma-separated <time> list (transition-duration,
// transition-delay). Durations can't be negative; delays can.
func timeList(cs []cv, duration bool) ([]float32, bool) {
	parts := splitComma(cs)
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		if len(p) != 1 {
			return nil, false
		}
		t, ok := timeOf(&p[0])
		if !ok || duration && t < 0 {
			return nil, false
		}
		out = append(out, t)
	}
	return out, true
}

// transitionProps parses transition-property: none, or a comma-separated
// list of property names (all, opacity, transform, …).
func transitionProps(cs []cv) ([]string, bool) {
	parts := splitComma(cs)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) != 1 || p[0].t.k != tIdent {
			return nil, false
		}
		n := strings.ToLower(p[0].t.s)
		if n == "none" && len(parts) > 1 {
			return nil, false
		}
		if a, ok := propAlias[n]; ok {
			n = a
		}
		out = append(out, n)
	}
	return out, true
}

// timingKeywords are the <easing-function> keywords.
var timingKeywords = map[string]bool{
	"ease": true, "linear": true, "ease-in": true, "ease-out": true, "ease-in-out": true,
	"step-start": true, "step-end": true,
}

// transitionShort expands transition: each comma-separated item is
// [property] [duration] [easing] [delay] [behavior] in any order (the
// first time is the duration, the second the delay), missing parts at
// their initial values; the longhands get one entry per item.
func transitionShort(longs []string) func(string, []cv) []string {
	return func(src string, cs []cv) []string {
		items := splitComma(cs)
		var props, durs, eases, delays, behs []string
		for _, it := range items {
			if len(it) == 0 {
				return nil
			}
			prop, ease, beh := "", "", ""
			var times []string
			var first float32
			for i := range it {
				c := &it[i]
				switch {
				case c.t.k == tDimension:
					t, ok := timeOf(c)
					if !ok || len(times) == 2 {
						return nil
					}
					if len(times) == 0 {
						first = t
					}
					times = append(times, c.text(src))
				case c.t.k == tFunction:
					switch c.name {
					case "cubic-bezier", "steps", "linear":
						if ease != "" {
							return nil
						}
						ease = c.text(src)
					default:
						return nil
					}
				case c.t.k == tIdent:
					n := strings.ToLower(c.t.s)
					switch {
					case timingKeywords[n]:
						if ease != "" {
							return nil
						}
						ease = n
					case n == "normal" || n == "allow-discrete":
						if beh != "" {
							return nil
						}
						beh = n
					default:
						if prop != "" || wideKeyword(n) != "" {
							return nil
						}
						prop = n
					}
				default:
					return nil
				}
			}
			if prop == "none" && len(items) > 1 {
				return nil
			}
			if prop == "" {
				prop = "all"
			}
			if ease == "" {
				ease = "ease"
			}
			if beh == "" {
				beh = "normal"
			}
			dur, delay := "0s", "0s"
			if len(times) > 0 {
				if first < 0 { // durations can't be negative
					return nil
				}
				dur = times[0]
			}
			if len(times) > 1 {
				delay = times[1]
			}
			props, durs, eases, delays, behs = append(props, prop), append(durs, dur), append(eases, ease), append(delays, delay), append(behs, beh)
		}
		j := func(l []string) string { return strings.Join(l, ", ") }
		return []string{j(props), j(durs), j(eases), j(delays), j(behs)}
	}
}
