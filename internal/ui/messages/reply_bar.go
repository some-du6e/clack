package messages

import (
	"image"
	"strconv"

	"gioui.org/layout"
	"gioui.org/unit"

	"github.com/some-du6e/clack/internal/ui/theme"
)

// The channel's reply bar (Slack's ReplyBar, client-boot-shared v8ac,
// build 407aff01646b): the repliers' faces, "N replies", the last reply
// ("View thread" on hover) and a caret.

// ThreadComposerFocuser is optional: the reply bar opens the thread with
// its reply composer focused (Slack's openThread sets focusInput for the
// thread, and the pane's input focuses on mount).
type ThreadComposerFocuser interface {
	FocusThreadComposer(channel, ts string)
}

// replyBarFaces is renderFaces: the repliers (reply_users without
// "B00"), all of them up to five; past five the first four and a fifth
// carrying "+N" for reply_users_count (else the list's length) minus 5.
func replyBarFaces(users []string, count int) (faces []string, overlay int) {
	for _, u := range users {
		if u != "B00" {
			faces = append(faces, u)
		}
	}
	if len(faces) <= 5 {
		return faces, 0
	}
	n := count
	if n == 0 {
		n = len(faces)
	}
	return faces[:5], n - 5
}

// replyOverlay draws the fifth face's c-avatar__additional_overlay: the
// avatar under rgba(0,0,0,.5) with "+N" ("9+" past nine) in white 15px
// bold, centred.
func replyOverlay(gtx layout.Context, th *theme.Theme, n, sz int) {
	txt := "+" + strconv.Itoa(n)
	if n > 9 {
		txt = "9+"
	}
	r := image.Rectangle{Max: image.Pt(sz, sz)}
	rrect(gtx.Ops, r, avatarRadius(sz), rgba(0x000000, 0.5))
	ts := textStyle{size: 15, weight: theme.Bold, col: rgb(0xffffff)}
	d := measure(gtx, th, ts, txt)
	at(gtx, (sz-d.X)/2, (sz-d.Y)/2, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max = image.Pt(1<<20, gtx.Sp(unit.Sp(30)))
		return label(gtx, th, ts, txt)
	})
}
