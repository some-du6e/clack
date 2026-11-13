package popups

// Naming a group DM: Slack's RenameMpdmDialog (boot-shared ouJu, build
// 4d0c78085232), opened by the channel header's EditMpdmNameButton
// (entrypoint "header") and the details' MpdmConversationNameField
// ("channel_details", stacked). A DeprecatedBaseDialog (the topic dialog's
// p-channel_topic_dialog frame) titled "Edit conversation name" with
// "Save":
//
//   - the input starts with the custom name when the group DM has one
//     (tpcx Ry), else empty, placeholder "Add a name", one line, at most
//     80 graphemes (PvOQ getMaxLength); Enter does nothing (onTextyEnter);
//   - under it (BasicContainer), the error first, then the hints: "Name
//     your conversation with **<members>**. This will be visible to all
//     participants." (the members alphabetically, getDisplayNameForChannel
//     with forceMpimNames) and, when another group DM already carries the
//     typed name (LHz7), "You already have a conversation with this name.
//     Consider using a unique name to tell them apart." in content.hgl.3;
//   - Save is disabled until the trimmed text differs from the start, or
//     while it is too long; the same text closes. Otherwise
//     renameChannelApi (core.RenameMpdm: conversations.rename {channel,
//     name, validate: true}); an empty name removes the custom name. A
//     refusal keeps the dialog open with the error and the input outlined
//     red: invalid_name / invalid_name_specials "Conversation names can’t
//     contain the punctuation you included.", restricted_action /
//     not_authorized "You don’t have permission to rename.", anything else
//     "For some weird reason, that didn’t work. Please try again to
//     continue." Typing clears it.
//
// Slack's input is a Texty field that also completes :emoji: (not for
// Slack Connect group DMs); Clack's input is plain text.

import (
	"context"
	"errors"
	"image"
	"image/color"
	"strings"
	"unicode"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/slack"
	"github.com/some-du6e/clack/internal/ui/icons"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// RenameMpdmDialog is "Edit conversation name" for one group DM.
type RenameMpdmDialog struct {
	h       Host
	ch      string
	members string // the hint's member list
	start   string // F: the custom name, or ""
	modal   *Modal
	ed      widget.Editor
	last    string // the text the error belongs to (typing clears it)
	errID   string // conversations.rename's error id
	pending bool
	focused bool
}

// NewRenameMpdmDialog opens the dialog for group DM ch; members is its
// member list as the hint names it.
func NewRenameMpdmDialog(h Host, ch, members string) *RenameMpdmDialog {
	d := &RenameMpdmDialog{h: h, ch: ch, members: members}
	if a := h.Core(); a != nil && a.MpdmHasCustomName(ch) {
		if c, ok := a.Conv(ch); ok {
			d.start = c.Name
		}
	}
	d.ed.SingleLine = true
	d.ed.SetText(d.start)
	d.ed.SetCaret(len([]rune(d.start)), len([]rune(d.start)))
	d.last = d.start
	// the channel_details I18n's rename modal
	save := &ModalButton{Label: nsChDetails.T("Save"), Kind: BtnPrimary, Enabled: d.canSave, Do: func() bool { return d.Save() }}
	d.modal = NewModal(h, ModalOpts{Title: nsChDetails.T("Edit conversation name"), Body: d.layout, PadX: 24, HeadH: 68,
		Buttons:    []*ModalButton{{Label: nsChDetails.T("Cancel"), Kind: BtnOutline}, save},
		FocusStops: func() []event.Tag { return []event.Tag{&d.ed} }}).OldDialog()
	return d
}

func (d *RenameMpdmDialog) Layout(gtx layout.Context) layout.Dimensions { return d.modal.Layout(gtx) }
func (d *RenameMpdmDialog) Dismissed() bool                             { return d.modal.Dismissed() }
func (d *RenameMpdmDialog) Dismiss()                                    { d.modal.Dismiss() }

// SetText types into the input; Text reads it.
func (d *RenameMpdmDialog) SetText(s string) { d.ed.SetText(s) }
func (d *RenameMpdmDialog) Text() string     { return d.ed.Text() }

// Error is the error line's text ("" when there is none).
func (d *RenameMpdmDialog) Error() string { return renameMpdmErrorText(d.errID) }

// Hints are the hint lines' texts, the duplicate warning last.
func (d *RenameMpdmDialog) Hints() []string {
	out := []string{nsChDetails.T(renameMpdmHint, "channelName", d.members)}
	if d.duplicate() {
		out = append(out, nsChDetails.T(renameMpdmDuplicate))
	}
	return out
}

const (
	renameMpdmDuplicate = "You already have a conversation with this name. Consider using a unique name to tell them apart."
	renameMpdmHint      = "Name your conversation with {channelName}. This will be visible to all participants."
)

func (d *RenameMpdmDialog) duplicate() bool {
	a := d.h.Core()
	return a != nil && a.HasMpdmWithDuplicateName(d.ed.Text(), d.ch)
}

// canSave is the go button's `!z || K` turned around: the trimmed text
// changed and isn't too long.
func (d *RenameMpdmDialog) canSave() bool {
	v := strings.TrimSpace(d.ed.Text())
	return !d.pending && v != strings.TrimSpace(d.start) && GraphemeLen(v) <= core.MpdmNameMaxLen
}

// Save is the go button: the same name closes; otherwise the rename,
// closing once Slack took it. It reports whether the dialog closes now.
func (d *RenameMpdmDialog) Save() bool {
	name := strings.TrimSpace(d.ed.Text())
	if name == d.start {
		return true
	}
	a := d.h.Core()
	if a == nil || d.pending || !d.canSave() {
		return false
	}
	d.pending, d.errID = true, ""
	ch := d.ch
	run(d.h, func(ctx context.Context) (struct{}, error) { return struct{}{}, a.RenameMpdm(ctx, ch, name) },
		func(_ struct{}, err error) {
			d.pending = false
			if err != nil {
				d.errID = slackErrID(err)
				d.last = d.ed.Text()
				d.h.Invalidate()
				return
			}
			d.modal.Dismiss()
		})
	return false
}

// slackErrID is the error id Slack answered (Slack's r.data.error),
// "unknown_error" for a failure without one.
func slackErrID(err error) string {
	var se *slack.Error
	if errors.As(err, &se) && se.Code != "" {
		return se.Code
	}
	return "unknown_error"
}

// renameMpdmErrorText is RenameMpdmDialog's W for an error id.
func renameMpdmErrorText(id string) string {
	switch id {
	case "":
		return ""
	case "invalid_name", "invalid_name_specials":
		return nsChDetails.T("Conversation names can’t contain the punctuation you included.")
	case "restricted_action", "not_authorized":
		return nsChDetails.T("You don’t have permission to rename.")
	}
	return nsChDetails.T("For some weird reason, that didn’t work. Please try again to continue.")
}

// GraphemeLen counts user-perceived characters the way Slack's
// getStringLength (dZPS: Intl.Segmenter, grapheme granularity) does for
// names: combining marks, variation selectors, emoji modifiers and
// ZWJ-joined emoji stay with the character before them, and two regional
// indicators make one flag. (Conjunct and Hangul jamo clusters count per
// code point.)
func GraphemeLen(s string) int {
	n := 0
	join := false // the previous rune was a ZWJ
	ri := 0       // regional indicators in the current run
	for _, r := range s {
		switch {
		case r == '‍':
			join = true
			continue
		case join:
			join = false
			continue
		case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r), unicode.Is(unicode.Mc, r),
			r >= 0xfe00 && r <= 0xfe0f, r >= 0x1f3fb && r <= 0x1f3ff, r >= 0xe0020 && r <= 0xe007f:
			continue
		case r >= 0x1f1e6 && r <= 0x1f1ff:
			ri++
			if ri%2 == 0 {
				continue
			}
		default:
			ri = 0
		}
		n++
	}
	return n
}

// layout is the input (15/22, padding 8 12, radius 8; red when invalid)
// and the subtexts 8px apart under it (c-basic_container__subtext): the
// error (InlineAlert: warning glyph, 13px content.imp), then the hints.
func (d *RenameMpdmDialog) layout(gtx layout.Context) layout.Dimensions {
	th := d.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	if !d.focused {
		d.focused = true // focusOnMount
		gtx.Execute(key.FocusCmd{Tag: &d.ed})
	}
	for {
		ev, ok := d.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok && d.ed.Text() != d.last {
			d.errID = "" // onTextChange: N(void 0)
		}
	}
	m := gtx
	m.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 24), 1<<20)}
	call, td := record(m, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		gtx.Constraints.Min.Y = gtx.Sp(22)
		if d.ed.Len() == 0 {
			text(gtx, th, 15, theme.Regular, cl.Muted, nsChDetails.T("Add a name"))
		}
		return th.Editor(gtx, &d.ed, 15, cl.Fg, theme.WithAlpha(cl.Link, 0x40))
	})
	ih := td.Size.Y + dp(gtx, 16)
	r := image.Rect(0, 0, w, ih)
	rad := dp(gtx, 8)
	invalid := d.errID != ""
	focus, ringCol := cl.Focus, cl.FocusRing
	if invalid { // c-basic_container--invalid: slack red, raspberry ring
		focus, ringCol = rgb(0xe01e5a), theme.WithAlpha(rgb(0xe01e5a), 0x4d)
	}
	switch {
	case gtx.Focused(&d.ed):
		ring := dp(gtx, 5)
		rrect(gtx, r.Inset(-ring), rad+ring, ringCol)
		one := maxInt(1, dp(gtx, 1))
		rrect(gtx, r.Inset(-one), rad+one, focus)
		rrect(gtx, r, rad, cl.ModalBg)
	case invalid:
		rrect(gtx, r, rad, focus)
		rrect(gtx, r.Inset(border1(gtx)), rad-border1(gtx), cl.ModalBg)
	default:
		rrect(gtx, r, rad, cl.InputBorder)
		rrect(gtx, r.Inset(border1(gtx)), rad-border1(gtx), cl.ModalBg)
	}
	at(gtx, image.Pt(dp(gtx, 12), dp(gtx, 8)), func(gtx layout.Context) layout.Dimensions {
		call.Add(gtx.Ops)
		return td
	})
	y := ih
	if e := d.Error(); e != "" {
		y += dp(gtx, 8)
		ed := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return renameAlert(gtx, th, cl, e, w)
		})
		y += ed.Size.Y
	}
	y += dp(gtx, 8)
	hd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return cnRuns(gtx, th, cl, rtRuns(nsChDetails.RT(renameMpdmHint, "channelName", d.members), ""), 13, 18, cl.Fg3, w)
	})
	y += hd.Size.Y
	if d.duplicate() {
		y += dp(gtx, 8)
		dd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, renameWarnColor(th), nsChDetails.T(renameMpdmDuplicate), 0)
		})
		y += dd.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// renameWarnColor is --dt_color-content-hgl-3 (duplicateWarning__F8nEW).
func renameWarnColor(th *theme.Theme) color.NRGBA {
	if th != nil && th.Dark {
		return rgb(0xdea700)
	}
	return rgb(0x6b5000)
}

// renameAlert is InlineAlert: the warning glyph and the text in
// content.imp, 13px.
func renameAlert(gtx layout.Context, th *theme.Theme, cl *Colors, s string, w int) layout.Dimensions {
	is := dp(gtx, 13)
	icons.Get("warning")(gtx, 13, cl.Danger)
	off := is + textW(gtx, th, 13, theme.Regular, " ")
	d := at(gtx, image.Pt(off, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(w-off, 1), 1<<20)}
		return para(gtx, th, 13, 18, theme.Regular, cl.Danger, s, 0)
	})
	return layout.Dimensions{Size: image.Pt(w, maxInt(d.Size.Y, is))}
}
