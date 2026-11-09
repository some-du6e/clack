package messages

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/emoji"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/uipref"
)

// One-click reactions: Slack's BaseQuickReactionActions at the start of
// the hover toolbar (MessageActions hasQuickReactionsBlock), shown while
// user_customized_quick_reactions_display_feature is 1 (Messages & media >
// "Show one-click reactions on messages"; shouldDisplayUsersQuickReactions,
// boot-shared 5j0l). The three emoji (setUsersFrequentOrCustomized-
// QuickReactions, vD/I) are the first three of getFrequentlyUsedEmoji with
// user_customized_quick_reactions_use_frequently_used_emoji, else the ones
// they picked (user_customized_quick_reactions_emoji_1…3), else Slack's
// defaults ✅ 👀 🙌; the preferred skin tone applies (QMob). Not on Slackbot's
// DM. Each button toggles the viewer's reaction, its tooltip is the
// emoji's name ("Completed", "Taking a look…", "Nicely done" for the
// defaults).

// quickDefaults are the QUICK_REACTIONS space's defaults.
var quickDefaults = [3]string{"white_check_mark", "eyes", "raised_hands"}

// quickBar is the toolbar's quick reactions for the hovered row.
type quickBar struct {
	names      []string // skin-toned
	customized bool
	btn        [3]widget.Clickable
	hov        int // hovered, -1 none
}

// prepare picks the row's quick reactions (none when they're off).
func (q *quickBar) prepare(h Host, ch string, r *row) {
	q.names = q.names[:0]
	app := h.Core()
	if app == nil || !uipref.Get().QuickReactions || r == nil {
		return
	}
	if c, ok := app.S.Conv(ch); ok && c.IsIM && c.User == "USLACKBOT" { // isSlackbotIm
		return
	}
	p := uipref.Get()
	var picks []string
	switch {
	case app.PrefBool("user_customized_quick_reactions_use_frequently_used_emoji", false):
		f := app.FrequentlyUsedEmoji() // getFrequentlyUsedEmoji, as the picker's
		picks = append(picks, f[:min(3, len(f))]...)
	default:
		q.customized = app.PrefBool("user_customized_quick_reactions_has_customized", false)
		for i := range quickDefaults {
			n := strings.Trim(app.PrefString("user_customized_quick_reactions_emoji_"+strconv.Itoa(i+1)), ":")
			if n == "" {
				n = quickDefaults[i]
			}
			picks = append(picks, n)
		}
	}
	tone := p.SkinTone
	for _, n := range picks {
		if tone != "" && tone != "1" && !strings.Contains(n, "::skin-tone-") {
			base, _, ok1 := emoji.Standard(n)
			toned, _, ok2 := emoji.Standard(n + "::skin-tone-" + tone)
			if ok1 && ok2 && toned != base { // the emoji has skin tones
				n += "::skin-tone-" + tone
			}
		}
		q.names = append(q.names, n)
	}
}

// tip is getQuickReactionsLabel.
func (q *quickBar) tip(i int) string {
	n := q.names[i]
	base, _, _ := strings.Cut(n, "::")
	if !q.customized {
		switch base {
		case "white_check_mark":
			return "Completed"
		case "eyes":
			return "Taking a look…"
		case "raised_hands":
			return "Nicely done"
		}
	}
	return ":" + base + ":"
}

// quickButton is one c-message_actions__emoji_button.
func (s *listState) quickButton(gtx layout.Context, i int) layout.Dimensions {
	q := &s.quick
	r := s.tbRow
	btn := &q.btn[i]
	name := q.names[i]
	if btn.Clicked(gtx) && s.rc.onReact != nil && r != nil {
		on := false
		for _, re := range r.m.Reactions {
			if re.Name == name && re.Self {
				on = true
			}
		}
		s.rc.onReact(r, name, !on)
	}
	if btn.Hovered() {
		q.hov = i
	}
	c := s.rc.c
	bs := gtx.Constraints.Max.X
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rr := image.Rectangle{Max: image.Pt(bs, bs)}
		es := dp(gtx, 18)
		if btn.Hovered() {
			rrect(gtx.Ops, rr, dp(gtx, 8), c.ToolbarHoverBg)
			es = dp(gtx, 22)
		}
		url := ""
		if app := s.rc.h.Core(); app != nil {
			url, _, _ = app.Emoji(name)
		}
		o := (bs - es) / 2
		if !drawEmoji(gtx, s.rc.h, url, image.Rect(o, o, o+es, o+es)) {
			at(gtx, dp(gtx, 2), (bs-dp(gtx, 18))/2, func(gtx layout.Context) layout.Dimensions {
				return label(gtx, s.rc.th, textStyle{size: 11, col: c.Text, lineH: 18}, ":"+name+":")
			})
		}
		return layout.Dimensions{Size: rr.Max}
	})
}
