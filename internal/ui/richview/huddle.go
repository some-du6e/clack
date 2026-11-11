package richview

import (
	"image"

	"gioui.org/font"

	"github.com/some-du6e/clack/internal/richtext"
)

// HuddleJoiner is a Handler that joins a huddle message's huddle: Slack's
// HuddleMsgJoinButton, startHuddle with entryPoint huddle_message_button.
type HuddleJoiner interface {
	JoinHuddle(channel string)
}

// huddleMsg lays out a huddle_thread message's HuddleMsgText
// (gantry-v2-async-client-boot-shared EKdW, build 4d0c78085232; the
// client-boot-styles rules) at (x, y) and returns its height:
//
//   - the sentence (richtext's getHuddleMsgText) in
//     .c-message__body--automated: rgba(sk_foreground_max_solid), the
//     palette's Text2;
//   - while the huddle is live and you're not in it, the Join Huddle
//     button on its own line: .p-huddle_msg_join_button__container
//     (padding 8px 0) around a medium outline c-button (36px, 15px bold,
//     padding 0 12px) with a 1px plt-jade-20 border and 8px corners,
//     holding the avatar stack (up to three x_small 20px faces at 25%
//     corners, each overlapping the one before; margin-right 8px) and
//     "Join Huddle".
func (l *lay) huddleMsg(b *richtext.Block, x, y, w int) int {
	a := b.Att
	h := a.Huddle
	p := &l.st.Palette
	y0 := y
	if len(a.Text) > 0 && len(a.Text[0].Spans) > 0 {
		fs := l.bodyStyle()
		fs.col = p.Text2
		l.addSpans(a.Text[0].Spans, &fs)
		th, _ := l.flow(x, y, w, &fs)
		y += th
	}
	if h == nil || !h.Joinable || h.Channel == "" {
		return y - y0
	}
	l.plainAdd("\n")
	y += l.dp(8)
	bh := l.dp(36)
	if l.opts.Compact { // the dense density's small button
		bh = l.dp(28)
	}
	pad := l.dp(12)
	face, step := l.dp(20), l.dpf(18.5)
	facesW := 0
	if n := len(h.Faces); n > 0 {
		facesW = face + int(float32(n-1)*step+0.5) + l.dp(8)
	}
	size := l.sp(15)
	k := faceKey{f: font.Font{Typeface: l.st.Face, Weight: font.Bold}, size: fixedPx(size)}
	label := "Join Huddle"
	tw := int(l.measure(label, k) + 0.999)
	bw := min(pad+facesW+tw+pad, w)
	r := image.Rect(x, y, x+bw, y+bh)
	tg := l.d.addTarget(target{kind: tHuddleJoin, id: h.Channel})
	l.d.boxes = append(l.d.boxes, box{kind: bBorder, r: r, radius: l.dp(8), fill: p.Panel, stroke: p.HuddleJoinBorder,
		bw: max(1, l.dp(1)), target: tg})
	fx := x + pad
	for i, u := range h.Faces {
		fr := image.Rect(0, 0, face, face).Add(image.Pt(fx+int(float32(i)*step+0.5), y+(bh-face)/2))
		rad := face / 4
		l.d.imgs = append(l.d.imgs, imgPrim{r: fr, url: u, radii: [4]int{rad, rad, rad, rad}, target: tg, px: face})
	}
	fs := flowStyle{face: k.f, size: size, lineH: bh, col: p.Text, emoji: l.dp(16), maxLines: 1, noSelect: true}
	l.plainFlow(label, x+pad+facesW, y-l.dp(1), tw+l.dp(2), &fs, tg)
	l.d.hits = append(l.d.hits, hit{r: r, t: tg})
	return y + bh + l.dp(8) - y0
}
