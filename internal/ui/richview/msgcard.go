package richview

import (
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/some-du6e/yoloslackgoclient/internal/richtext"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/icons"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
)

// Message permalinks (richtext/msgmention.go has Slack's rules):
// message_mention pills (RichTextSlug, .p-rich_text_slug) and message
// unfurl cards (MessageAttachmentSlackMessageCard, card__OWdT5).

// iconURL prefixes an image frag that is a vector icon (icons.Get),
// painted in the frag's color: inline icons in pills and card lines.
const iconURL = "icon:"

func paintIcon(ops *op.Ops, name string, f *frag) {
	gtx := layout.Context{Ops: ops, Metric: unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(f.size, f.size))}
	defer op.Offset(image.Pt(int(f.x+0.5), f.y)).Push(ops).Pop()
	icons.GetSlack(name)(gtx, unit.Dp(f.size), f.col) // Slack icon names ("message")
}

// addIcon adds an inline icon of px at the flow's position.
func (l *lay) addIcon(name string, px int, k faceKey, col color.NRGBA, dc, tg int16) {
	p0, p1 := l.plainAdd("")
	l.items = append(l.items, item{kind: itImage, face: k, w: float32(px + 2*l.dp(1)), url: iconURL + name, size: px,
		col: col, deco: dc, target: tg, p0: p0, p1: p1})
}

// messageMention lays out a message_mention: the .p-rich_text_slug pill
// (content-hgl-1 on surf-hgl-1, 3px radius, 1px 2px padding), the
// "message" icon (4px gap), the label; a private channel's lock icon goes
// before its name. Disabled ("Private message"): content-pry on
// foreground_soft .06, no link.
func (l *lay) messageMention(sp *richtext.Span, fs *flowStyle, k faceKey, pt uint8) {
	p := &l.st.Palette
	bg, fg := p.MentionBg, p.MentionText
	var tg int16 = -1
	if sp.Off {
		bg, fg = rgba(p.Text.R, p.Text.G, p.Text.B, 0.06), p.Text
	} else if sp.URL != "" {
		tg = l.d.addTarget(target{kind: tLink, url: sp.URL})
	}
	pad, padV := l.dpf(2), l.dpf(1)
	l.decos = append(l.decos, deco{kind: 1, bg: bg, padL: pad, padR: pad, padT: padV, padB: padV, radius: l.dp(3), target: tg, face: k, self: sp.Off, part: pt})
	dc := int16(len(l.decos) - 1)
	ip := fs.size
	l.addPad(pad, dc, k)
	l.addIcon("message", ip, k, fg, dc, tg)
	l.addPad(l.dpf(3), dc, k) // icon margin-right 4 (the icon item ends with 1px)
	txt := sp.Text
	if sp.Lock && sp.Pre > 0 && sp.Pre <= len(txt) {
		l.addText(txt[:sp.Pre], fs, k, fg, sp.Style, dc, tg)
		l.addIcon("lock-filled", ip, k, fg, dc, tg)
		txt = txt[sp.Pre:]
	}
	l.addText(txt, fs, k, fg, sp.Style, dc, tg)
	l.addPad(pad, dc, k)
}

// cardTime is AttachmentFooterTimestamp's compactTime: the time today,
// "Yesterday"/"Tomorrow" next to it, else the date.
func cardTime(ts string) string {
	sec := int64(0)
	if i := strings.IndexByte(ts, '.'); i >= 0 {
		ts = ts[:i]
	}
	for _, c := range ts {
		if c < '0' || c > '9' {
			return ""
		}
		sec = sec*10 + int64(c-'0')
	}
	if sec <= 0 {
		return ""
	}
	t := time.Unix(sec, 0).Local()
	now := time.Now()
	day := func(x time.Time) time.Time { y, m, d := x.Date(); return time.Date(y, m, d, 0, 0, 0, 0, time.Local) }
	switch day(t).Sub(day(now)) {
	case 0:
		return t.Format(uipref.Layout("3:04 PM"))
	case -24 * time.Hour:
		return "Yesterday"
	case 24 * time.Hour:
		return "Tomorrow"
	}
	return slackDate(sec)
}

// msgCard is MessageAttachmentSlackMessageCard: a 1px otl-ter bordered,
// 12px-radius card (max 716px, c-message_attachment--full_card) with 12px
// padding and 4px gaps: the header row (36px avatar, 8px radius, then
// author 15/900 and the compact time 12px ter on one baseline, and the
// context line 13px ter), the message, its files (8px above). The whole
// card opens the message.
func (l *lay) msgCard(b *richtext.Block, x, y, w int) int {
	a := b.Att
	p := &l.st.Palette
	w = min(w, l.dp(716))
	bw := max(1, l.dp(1))
	pad := l.dp(12)
	gap := l.dp(4)
	bi := len(l.d.boxes)
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, fill: p.Panel, stroke: p.Border, bw: bw, radius: l.dp(12), target: -1})
	var cardTg int16 = -1
	hi := -1
	if a.FromURL != "" {
		cardTg = l.d.addTarget(target{kind: tLink, url: a.FromURL})
		hi = len(l.d.hits)
		l.d.hits = append(l.d.hits, hit{t: cardTg}) // rect set below; inner hits come later (on top)
	}
	cx, cw := x+bw+pad, w-2*(bw+pad)
	cy := y + bw + pad

	// header row
	tx := cx
	headH := 0
	if a.AuthorIcon != "" {
		s := l.dp(36)
		var tg int16 = -1
		if a.AuthorID != "" {
			tg = l.d.addTarget(target{kind: tUser, id: a.AuthorID})
			l.d.hits = append(l.d.hits, hit{r: image.Rect(cx, cy, cx+s, cy+s), t: tg})
		}
		l.d.imgs = append(l.d.imgs, imgPrim{r: image.Rect(cx, cy, cx+s, cy+s), url: a.AuthorIcon,
			radii: [4]int{l.dp(8), l.dp(8), l.dp(8), l.dp(8)}, target: tg, px: s})
		tx += s + l.dp(4) + gap
		headH = s
	}
	tw := cx + cw - tx
	// byline: author (15px, 900) and time (12px ter) on one baseline,
	// margin-top -4
	by := cy - l.dp(4)
	fs := l.bodyStyle()
	fs.maxLines, fs.ellipsis = 1, true
	bh := 0
	{
		ct := l.enter(attAuthorClass)
		nk := l.faceFor(font.Font{Typeface: l.st.Face, Weight: font.Black}, l.sp(15), 0)
		var tg int16 = -1
		if a.AuthorID != "" {
			tg = l.d.addTarget(target{kind: tUser, id: a.AuthorID, underline: true})
		} else if a.AuthorLink != "" {
			tg = l.d.addTarget(target{kind: tLink, url: a.AuthorLink, underline: true})
		}
		l.addText(a.AuthorName, &fs, nk, p.Text, 0, -1, tg)
		if t := cardTime(a.MsgTS); t != "" {
			l.addPad(l.dpf(4), -1, nk)
			tk := l.faceFor(font.Font{Typeface: l.st.Face}, l.sp(12), 0)
			var ttg int16 = -1
			if a.FromURL != "" {
				ttg = l.d.addTarget(target{kind: tLink, url: a.FromURL, underline: true})
			}
			l.addText(t, &fs, tk, p.CardFooter, 0, -1, ttg)
		}
		h, _ := l.flow(tx, by, tw, &fs)
		l.leave(ct)
		l.plainAdd("\n")
		bh = h - l.dp(4)
	}
	// context: 13px ter, margin-bottom 4; the channel is InlineChannelEntity
	ctxH := 0
	if len(a.Context) > 0 {
		ct := l.enter(attFooterClass)
		cs := l.small(p.CardFooter)
		cs.maxLines, cs.ellipsis = 1, true
		var tg int16 = -1
		if a.ContextLink != "" {
			tg = l.d.addTarget(target{kind: tLink, url: a.ContextLink, underline: true})
		}
		k := l.faceFor(cs.face, cs.size, 0)
		for _, sp := range a.Context {
			switch sp.Kind {
			case richtext.Channel:
				icon := "hash"
				if a.ContextLock {
					icon = "lock"
				}
				l.addIcon(icon, l.dp(13), k, p.CardFooter, -1, tg)
				l.addPad(l.dpf(2), -1, k)
				l.addText(strings.TrimPrefix(sp.Text, "#"), &cs, k, p.CardFooter, 0, -1, tg)
			default:
				l.addText(sp.Text, &cs, k, p.CardFooter, 0, -1, tg)
			}
		}
		h, _ := l.flow(tx, cy+bh, tw, &cs)
		l.leave(ct)
		l.plainAdd("\n")
		ctxH = h + l.dp(4)
	}
	headH = max(headH, bh+ctxH)
	cy += headH

	// the message
	if len(a.Text) > 0 {
		cy += gap
		text := a.Text
		if short, cut := truncateBlocks(text, shareMaxChars, shareMaxNewlines); cut {
			if l.expanded[a] {
				text = withExpander(append([]richtext.Block(nil), text...), false)
			} else {
				text = withExpander(short, true)
			}
		}
		l.expandAt = a
		ct := l.enter(attTextClass)
		cy += l.blocks(text, cx, cy, cw, false)
		l.leave(ct)
		l.expandAt = nil
		l.plainAdd("\n")
	}
	// media: the message's files
	if len(b.Children) > 0 {
		cy += gap + l.dp(8)
		cy += l.blocks(b.Children, cx, cy, cw, false)
	}
	bot := cy + pad + bw
	r := image.Rect(x, y, x+w, bot)
	l.d.boxes[bi].r = r
	if hi >= 0 {
		l.d.hits[hi].r = r
	}
	return bot - y
}
