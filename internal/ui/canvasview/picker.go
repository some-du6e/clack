package canvasview

// The editor's mention picker: typing "@name" lists people (core's
// directory search), "#name" lists channels; Up/Down move, Enter or Tab
// (or a click) inserts the mention as a pill, Escape closes.
//
// Emoji, as Slack's canvas editor has them (quip-collab-editor, canvas
// build 2026-11-12; modules CCtt, 7gVQ and YG+U from quip-collab-webapp):
//
//   - the ":" typeahead (slack_emoji_matchAutocompleteRange): the text
//     before the caret ends with ":" (or the full-width "：") and a query
//     of at least two characters with none of ~@#$%^&*()=[]{}\|!?`;:",.<>/
//     or whitespace (CCtt _$); the character before the colon is the
//     line's start, whitespace or one of {[(*_/"“'‘’< (trimAllowedMatchPrefix).
//     The suggestions are the shell's emoji autocomplete (EmojiSearch);
//     picking one inserts the emoji control.
//   - a typed ":name:" (the control text matcher, matchTypedEmojiCode with
//     endOnly): typing the closing colon turns ":name:" before the caret
//     into the emoji control when the name is an emoji (the control
//     reverts to its text when the emoji doesn't load: here, nothing
//     converts), the colon preceded by the line's start, whitespace or one
//     of those characters, and not inside a URL.

import (
	"context"
	"image"
	"regexp"
	"strings"
	"time"
	"unicode"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/canvas"
	"github.com/some-du6e/clack/internal/ui/composer"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const pickMax = 6

type pickItem struct {
	kind           composer.AtomKind
	id, label, sub string
	avatar         string
}

type picker struct {
	on         bool
	trig       rune
	start, end int // the trigger's rune range in the editor doc
	query      string
	asked      string
	gen        int
	items      []pickItem
	sel        int
	clicks     [pickMax]widget.Clickable
	accept     int // index to accept at the next frame (-1 none); keys run inside the editor
	acceptSet  bool
	prevN      int // the doc's rune count and caret last frame (a typed ":")
	prevC      int
	prevSet    bool
}

// EmojiSearch is optional on the Host: the shell's emoji autocomplete
// for a ":" query (best first).
type EmojiSearch interface {
	CanvasEmoji(query string, limit int) []EmojiHit
}

// EmojiHit is one emoji suggestion: the name to insert and its image.
type EmojiHit struct{ Name, URL string }

// emojiPrefix is CCtt's uz: what may stand right before an emoji's colon
// besides whitespace.
var emojiPrefix = map[rune]bool{'{': true, '[': true, '(': true, '*': true, '_': true, '/': true, '"': true, '\u201c': true,
	'\'': true, '\u2018': true, '\u2019': true, '<': true}

// emojiQueryStop are the characters a ":" query can't hold (CCtt _$).
const emojiQueryStop = "~@#$%^&*()=[]{}\\|!?`;:\uff1a\",.<>/"

// emojiPrefixOK: r may stand before an emoji's colon.
func emojiPrefixOK(r rune) bool { return unicode.IsSpace(r) || emojiPrefix[r] }

// emojiTrigger finds the ":" query ending at c: the colon's index and the
// query, ok false when the text before the caret isn't one.
func emojiTrigger(rs []rune, c int) (colon int, q string, ok bool) {
	j := c
	for j > 0 && !unicode.IsSpace(rs[j-1]) && !strings.ContainsRune(emojiQueryStop, rs[j-1]) && rs[j-1] != composer.ObjRune {
		j--
	}
	if j == 0 || (rs[j-1] != ':' && rs[j-1] != '\uff1a') || c-j < 2 {
		return 0, "", false
	}
	colon = j - 1
	if colon > 0 && rs[colon-1] != '\n' && !emojiPrefixOK(rs[colon-1]) {
		return 0, "", false
	}
	return colon, string(rs[j:c]), true
}

// emojiCodeRE is YG+U's :name: (with its skin tones), at the text's end.
var emojiCodeRE = regexp.MustCompile(`:([a-zA-Z0-9\p{L}\p{N}\-_+'\x{FF3F}\x{FF0B}\x{30FC}\x{3005}]+(?:::skin-tone-[2-6](?:-[2-6])*)?):$`)

// urlRE is YWPg's URL test.
var urlRE = regexp.MustCompile(`(?i)^((ftp|http|https)://|\bw{3}\.)[a-z0-9\-.]+[a-z]+(:[a-z0-9]*)?/?([@a-z0-9\-._?,'/\\+&%:!$#=~()*])*$`)

// typedEmoji is the ":name:" a just-typed colon at c closes: its start
// and name.
func typedEmoji(rs []rune, c int) (start int, name string, ok bool) {
	ls := c
	for ls > 0 && rs[ls-1] != '\n' {
		ls--
	}
	line := string(rs[ls:c])
	m := emojiCodeRE.FindStringSubmatchIndex(line)
	if m == nil {
		return 0, "", false
	}
	start = ls + len([]rune(line[:m[0]]))
	if start > ls && !emojiPrefixOK(rs[start-1]) {
		return 0, "", false
	}
	word := line[:m[1]]
	if i := strings.LastIndexAny(word[:m[0]], " \t"); i >= 0 {
		word = word[i+1:]
	}
	if urlRE.MatchString(strings.TrimSpace(word)) {
		return 0, "", false
	}
	return start, line[m[2]:m[3]], true
}

// convertTyped turns a just-typed ":name:" into the emoji control.
func (p *picker) convertTyped(v *View, s *session) {
	d := s.ed.Doc()
	c := cursor(s.ed)
	n := len(d.Runes)
	typed := p.prevSet && n == p.prevN+1 && c == p.prevC+1
	p.prevN, p.prevC, p.prevSet = n, c, true
	if !typed || c < 2 || d.Runes[c-1] != ':' {
		return
	}
	start, name, ok := typedEmoji(d.Runes, c)
	if !ok || s.kind == canvas.Code {
		return
	}
	for i := start; i < c && i < len(d.Attrs); i++ { // code and links keep their text
		if at := d.Attrs[i]; at.Style&composer.Code != 0 || at.Link != "" || at.Atom != nil {
			return
		}
	}
	a := v.h.Core()
	if a == nil {
		return
	}
	u, t, known := a.Emoji(name)
	if !known {
		return
	}
	s.ed.InsertAtom(start, c, &composer.Atom{Kind: composer.AtomEmoji, ID: name, Text: ":" + name + ":", URL: u, Uni: t})
	// the control replaces the match alone (emojiControlMatch): no space
	// after it, as InsertAtom adds for a picked suggestion
	if after := cursor(s.ed); after > 0 && s.ed.Doc().Runes[after-1] == ' ' {
		s.ed.Replace(after-1, after, "")
	}
	d = s.ed.Doc()
	p.prevN, p.prevC = len(d.Runes), cursor(s.ed)
	p.on, p.items = false, nil
	v.h.Invalidate()
}

func (p *picker) active() bool { return p.on && len(p.items) > 0 }

// key handles a key the editor offered (Intercept); true consumes it.
func (p *picker) key(name key.Name) bool {
	switch name {
	case key.NameUpArrow:
		p.sel = (p.sel + len(p.items) - 1) % len(p.items)
	case key.NameDownArrow:
		p.sel = (p.sel + 1) % len(p.items)
	case key.NameReturn, key.NameEnter, key.NameTab:
		p.accept, p.acceptSet = p.sel, true
	case key.NameEscape:
		p.on, p.items = false, nil
	default:
		return false
	}
	return true
}

// update finds the "@query" / "#query" before the caret and refreshes the
// suggestions (off the UI goroutine).
func (p *picker) update(v *View, s *session) {
	if p.acceptSet {
		p.acceptSet = false
		if p.accept >= 0 && p.accept < len(p.items) {
			p.insert(v, s, p.items[p.accept])
		}
		return
	}
	p.convertTyped(v, s)
	d := s.ed.Doc()
	c := cursor(s.ed)
	if colon, q, ok := emojiTrigger(d.Runes, c); ok {
		p.emojiQuery(v, colon, c, q)
		return
	}
	i := c
	for i > 0 {
		r := d.Runes[i-1]
		if r == ' ' || r == '\n' || r == '\t' || r == composer.ObjRune || c-i > 40 {
			break
		}
		i--
	}
	if i >= c || (d.Runes[i] != '@' && d.Runes[i] != '#') {
		p.on, p.items = false, nil
		return
	}
	trig, q := d.Runes[i], string(d.Runes[i+1:c])
	if p.on && p.trig == trig && p.start == i && p.query == q {
		p.end = c
		return
	}
	p.on, p.trig, p.start, p.end, p.query = true, trig, i, c, q
	p.asked = q
	p.gen++
	gen := p.gen
	a := v.h.Core()
	if a == nil || (trig == '@' && q == "") {
		p.items = nil
		return
	}
	e := v.edit
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var items []pickItem
		ql := strings.ToLower(q)
		if trig == '@' {
			for _, b := range []string{"here", "channel", "everyone"} {
				if strings.HasPrefix(b, ql) {
					items = append(items, pickItem{kind: composer.AtomBroadcast, id: b, label: "@" + b, sub: "Notify everyone"})
				}
			}
			us, _ := a.SearchUsers(ctx, q, pickMax)
			for _, u := range us {
				items = append(items, pickItem{kind: composer.AtomUser, id: u.ID, label: u.Name, sub: u.RealName, avatar: u.Avatar})
			}
		} else {
			snap := a.Snapshot()
			for _, c := range snap.Convs {
				if c.Kind != "channel" && c.Kind != "private" {
					continue
				}
				if strings.HasPrefix(strings.ToLower(c.Name), ql) {
					items = append(items, pickItem{kind: composer.AtomChannel, id: c.ID, label: c.Name})
				}
				if len(items) >= pickMax {
					break
				}
			}
		}
		if len(items) > pickMax {
			items = items[:pickMax]
		}
		v.post(func() {
			if e.pick.gen == gen {
				e.pick.items, e.pick.sel = items, 0
			}
		})
	}()
}

// emojiQuery refreshes the ":" suggestions for q (the colon at colon,
// the caret at c).
func (p *picker) emojiQuery(v *View, colon, c int, q string) {
	if p.on && p.trig == ':' && p.start == colon && p.query == q {
		p.end = c
		return
	}
	p.on, p.trig, p.start, p.end, p.query, p.asked = true, ':', colon, c, q, q
	p.gen++
	es, ok := v.h.(EmojiSearch)
	if !ok {
		p.items = nil
		return
	}
	var items []pickItem
	for _, h := range es.CanvasEmoji(q, pickMax) {
		items = append(items, pickItem{kind: composer.AtomEmoji, id: h.Name, label: ":" + h.Name + ":", avatar: h.URL})
	}
	p.items, p.sel = items, 0
}

func (p *picker) insert(v *View, s *session, it pickItem) {
	if it.kind == composer.AtomEmoji {
		at := &composer.Atom{Kind: composer.AtomEmoji, ID: it.id, Text: ":" + it.id + ":", URL: it.avatar}
		if a := v.h.Core(); a != nil {
			if _, t, ok := a.Emoji(it.id); ok {
				at.Uni = t
			}
		}
		s.ed.InsertAtom(p.start, p.end, at)
		p.on, p.items, p.asked = false, nil, ""
		p.prevN, p.prevC = len(s.ed.Doc().Runes), cursor(s.ed)
		s.focus = true
		v.h.Invalidate()
		return
	}
	at := &composer.Atom{Kind: it.kind, ID: it.id, Text: "@" + it.label}
	switch it.kind {
	case composer.AtomChannel:
		at.Text = "#" + it.label
	case composer.AtomBroadcast:
		at.Text = it.label
	}
	s.ed.InsertAtom(p.start, p.end, at)
	p.on, p.items, p.asked = false, nil, ""
	s.focus = true
	v.h.Invalidate()
}

// layout draws the suggestions at pos (the block's coordinates), on top of
// everything (op.Defer).
func (p *picker) layout(gtx layout.Context, v *View, s *session, pos image.Point, w int) {
	th := v.h.Theme()
	c := &th.C
	rowH := gtx.Dp(34)
	pw := min(gtx.Dp(300), max(gtx.Dp(200), w))
	ph := rowH*len(p.items) + gtx.Dp(8)
	m := op.Record(gtx.Ops)
	func() {
		defer op.Offset(pos).Push(gtx.Ops).Pop()
		r := image.Rect(0, 0, pw, ph)
		theme.Shadow(gtx, r.Add(image.Pt(0, gtx.Dp(2))), gtx.Dp(8), gtx.Dp(10), theme.WithAlpha(c.PanelShadow, 0x40))
		theme.RRect(gtx, r, gtx.Dp(8), c.BgRaised)
		theme.Border(gtx, r, gtx.Dp(8), gtx.Dp(1), c.Border)
		for i, it := range p.items {
			i, it := i, it
			y := gtx.Dp(4) + i*rowH
			clk := &p.clicks[i]
			hit := clk.Clicked(gtx) // before Layout, which drains a Clickable's events
			at(gtx, image.Pt(gtx.Dp(4), y), image.Pt(pw-gtx.Dp(8), rowH), func(gtx layout.Context) layout.Dimensions {
				return clk.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					sz := gtx.Constraints.Max
					if i == p.sel || clk.Hovered() {
						theme.RRect(gtx, image.Rectangle{Max: sz}, gtx.Dp(6), theme.WithAlpha(c.Accent, 0x22))
					}
					ax := gtx.Dp(8)
					as := gtx.Dp(20)
					switch {
					case it.avatar != "":
						rad := gtx.Dp(4)
						if it.kind == composer.AtomEmoji {
							rad = 0
						}
						if img, ok := v.h.Images().Get(it.avatar, as); ok {
							at(gtx, image.Pt(ax, (sz.Y-as)/2), image.Pt(as, as), func(gtx layout.Context) layout.Dimensions {
								return theme.Image(gtx, img, image.Pt(as, as), rad)
							})
						} else {
							theme.RRect(gtx, image.Rect(ax, (sz.Y-as)/2, ax+as, (sz.Y+as)/2), gtx.Dp(4), c.Skeleton)
						}
					default:
						icon := "hash"
						if it.kind == composer.AtomBroadcast {
							icon = "at"
						}
						at(gtx, image.Pt(ax+gtx.Dp(2), (sz.Y-gtx.Dp(16))/2), image.Pt(gtx.Dp(16), gtx.Dp(16)), func(gtx layout.Context) layout.Dimensions {
							return icons.Get(icon)(gtx, 16, c.FgMuted)
						})
					}
					tx := ax + as + gtx.Dp(8)
					lw := th.Measure(gtx, 14, theme.Bold, it.label)
					at(gtx, image.Pt(tx, 0), image.Pt(min(lw+2, sz.X-tx), sz.Y), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
							return th.Label(gtx, 14, theme.Bold, c.Fg, it.label)
						})
					})
					if it.sub != "" && tx+lw+gtx.Dp(8) < sz.X {
						sx := tx + lw + gtx.Dp(8)
						at(gtx, image.Pt(sx, 0), image.Pt(sz.X-sx, sz.Y), func(gtx layout.Context) layout.Dimensions {
							return vcenter(gtx, sz.Y, func(gtx layout.Context) layout.Dimensions {
								return th.Text(gtx, theme.LabelOpts{Font: theme.Font(theme.Regular), Size: 13, Color: c.FgMuted, MaxLines: 1}, it.sub)
							})
						})
					}
					return layout.Dimensions{Size: sz}
				})
			})
			if hit {
				p.insert(v, s, it)
			}
		}
	}()
	op.Defer(gtx.Ops, m.Stop())
}
