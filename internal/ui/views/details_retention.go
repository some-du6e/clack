package views

// Settings' Message history row and "export" section (CDN build
// 35b42de8b870, 2026-11-21; admin-channel-modal oWlM; the rules and calls
// are core/channel_retention.go):
//
//   - Message history: an EditableField at the end of the permissions
//     group ("Message history", aria "Edit message history"), for a DM or
//     group DM the section's only row. Its text (tU) is the retention
//     getDataRetention answers, or the team's: "Delete messages and
//     revisions after {n} days" ({n} years when a whole number of years),
//     else "Keep all messages, except for revisions" (type 0) / "Keep all
//     messages, including revisions" (type 1). Until the answer it reads
//     as type 0, and it isn't read again after a save (tU fetches once
//     per mount).
//   - Edit opens MessageRetentionModal, stacked: "Edit {ChannelName}
//     message history" (ChannelName draws nothing for a DM or group DM),
//     a jumbo spinner until getDataRetention answers, then DataRetention:
//     the intro, the shared-channel note (boxed info alert) for an
//     externally shared channel, the radios "Use the default period for
//     {team}" with getTeamRetentionDescription under it and "Delete
//     messages after a specific time period", and for the second a number
//     input with a Days / Years select (100 wide, 16 left), its error
//     line, and a warning alert (the team's description when the channel
//     override isn't enforced, else getRetentionDurationWarning). Cancel /
//     Save Changes (disabled until the last-changed field differs from
//     the answer, and while the duration is invalid); Save Changes calls
//     setDataRetention with the days, closes on ok, else "Oops, something
//     went wrong with updating message retention settings. Please try
//     again." at the top.
//   - Export: ButtonField "Export message history" (history icon) opens
//     ExportChannelHistoryModal: "Export this channel’s messages", the
//     JSON-file text, Cancel / Export Messages (primary), which sends
//     submitChannelExport and closes at once (its failure isn't shown).
//     The external-limited roadblock (preDispatchHook YQZj) isn't drawn:
//     Clack doesn't read isExternalLimited.

import (
	"context"
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/richtext"
	"github.com/some-du6e/clack/internal/ui/richview"
	"github.com/some-du6e/clack/internal/ui/slackc/css"
	"github.com/some-du6e/clack/internal/ui/theme"
)

const (
	cdSecExport = "export"
	// DataRetention's shared-channel "Learn more" (articleId 0x1ac6c81c0b,
	// hash message-and-file-retention)
	cdRetentionSharedArticle = "https://slack.com/help/articles/115004152843#message-and-file-retention"
)

// cdRetention is MessageRetentionModalContents' state.
type cdRetention struct {
	load               async[core.Retention]
	loaded             bool
	typ, unit          int // retention_type 0 | 1; the select: 0 days, 1 years
	dur                widget.Editor
	durText            string // the editor's text last frame
	initTyp, initDur   int
	initUnit           int
	team               core.Retention
	changed            bool // setHasChanges: the last-changed field differs from the answer
	radios             [2]widget.Clickable
	unitDD             dirDropdown
	err                string
	saving             bool
	op                 async[bool]
	enforced, extShare bool
}

// retentionInf is inf with the conversation as the rules read it.
func (d *ChannelDetails) retentionInf(inf core.ChannelDetails) core.ChannelDetails {
	inf.Conv = d.conv(inf, true)
	return inf
}

func (d *ChannelDetails) historyShown(inf core.ChannelDetails) bool {
	c := d.h.Core()
	return c != nil && c.MessageHistoryShown(d.retentionInf(inf))
}

func (d *ChannelDetails) exportShown(inf core.ChannelDetails) bool {
	c := d.h.Core()
	return c != nil && c.ExportHistoryShown(d.retentionInf(inf))
}

// cdRetentionText is tU's text for a shown retention.
func cdRetentionText(r core.Retention) string {
	ns := nsChanOptions
	switch {
	case r.Duration != 0:
		if r.Duration > 364 && r.Duration%365 == 0 {
			return ns.T("Delete messages and revisions after {numYears, plural, =1 {# year} other {# years}}", "numYears", r.Duration/365)
		}
		return ns.T("Delete messages and revisions after {numDays, plural, =1 {# day} other {# days}}", "numDays", r.Duration)
	case r.Type == 0:
		return ns.T("Keep all messages, except for revisions")
	case r.Type == 1:
		return ns.T("Keep all messages, including revisions")
	}
	return ""
}

// retentionRowText is the Message history row's text now.
func (d *ChannelDetails) retentionRowText(inf core.ChannelDetails) string {
	c, id := d.h.Core(), d.id
	if !d.x.retention.started() {
		d.x.retention.load(d.h, false, func(ctx context.Context) (core.Retention, error) { return c.ChannelRetention(ctx, id) })
	}
	own, done, err := d.x.retention.get()
	if !done || err != nil {
		return cdRetentionText(core.Retention{}) // tU's initial state (0, 0)
	}
	return cdRetentionText(c.ShownRetention(d.retentionInf(inf), own))
}

// retentionRow is the EditableField.
func (d *ChannelDetails) retentionRow(inf core.ChannelDetails) W {
	return func(gtx C) D {
		if d.x.retRow.Clicked(gtx) {
			d.openRetention(inf)
		}
		text := d.retentionRowText(inf)
		return d.cdFieldOf(gtx, &d.x.retRow, cdFieldSpec{
			titleText: nsChannelDetails.T("Message history"),
			text:      func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, text) },
		})
	}
}

// exportRows is the export section's ButtonField.
func (d *ChannelDetails) exportRows() []W {
	return []W{func(gtx C) D {
		if d.x.exportRow.Clicked(gtx) {
			d.openDialog(cdDlgExport)
		}
		return d.cdFieldOf(gtx, &d.x.exportRow, cdFieldSpec{titleText: nsChannelDetails.T("Export message history"), titleIcon: "history", button: true})
	}}
}

// openRetention opens MessageRetentionModal and asks getDataRetention.
func (d *ChannelDetails) openRetention(inf core.ChannelDetails) {
	c, id := d.h.Core(), d.id
	d.openDialog(cdDlgRetention)
	ri := d.retentionInf(inf)
	rt := &cdRetention{team: c.TeamRetention(ri.Conv.Kind), enforced: c.RetentionOverrideEnforced(ri), extShare: inf.IsExtShared}
	rt.dur.SingleLine, rt.dur.Filter = true, "0123456789-"
	rt.unitDD.labels = []string{nsChanOptions.T("Days"), nsChanOptions.T("Years")}
	rt.load.load(d.h, false, func(ctx context.Context) (core.Retention, error) { return c.ChannelRetention(ctx, id) })
	d.x.dlg.rt = rt
}

// durValue is the input's number; ok false for an empty or unparsable
// field (Slack's NaN).
func (rt *cdRetention) durValue() (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(rt.dur.Text()))
	return n, err == nil
}

// cdRetentionDays is convertRetentionDurationToDays.
func cdRetentionDays(n, unit int) int {
	if unit == 1 {
		return 365 * n
	}
	return n
}

// cdRetentionValid is isRetentionValid (no redaction buffer).
func cdRetentionValid(typ, n int, ok bool, unit int) bool {
	if typ == 0 {
		return true
	}
	if typ != 1 || !ok {
		return false
	}
	s := cdRetentionDays(n, unit)
	return s > 0 && s < 36500
}

// cdRetentionDurationError is getCustomRetentionDurationError.
func cdRetentionDurationError(n, unit int) string {
	ns := nsChanOptions
	if cdRetentionDays(n, unit) <= 0 {
		return ns.T("Messages must be kept for at least 1 day.")
	}
	return ns.T("Slack can only keep messages for up to 99 years, but thanks for your vote of confidence.")
}

// cdTeamRetentionDescription is getTeamRetentionDescription(duration, type).
func cdTeamRetentionDescription(dur, typ int) string {
	ns := nsChanOptions
	years := func() float64 { return float64(int(float64(dur)/365*10+0.5)) / 10 } // lodash round(e/365, 1)
	switch typ {
	case 0:
		return ns.T("By default, messages are kept forever, but revisions aren’t tracked.")
	case 2:
		if dur > 364 {
			return ns.T("By default, messages older than {durationDays} days ({durationYears, plural, =1 {# year} other {# years}}) will be deleted.", "durationDays", dur, "durationYears", years())
		}
		return ns.T("By default, messages older than {teamRetentionDuration, plural, =1 {# day} other {# days}} will be deleted.", "teamRetentionDuration", dur)
	case 3:
		if dur > 364 {
			return ns.T("By default, messages older than {durationDays} days ({durationYears, plural, =1 {# year} other {# years}}) will be deleted (full history still accessible via compliance export).", "durationDays", dur, "durationYears", years())
		}
		return ns.T("By default, messages older than {teamRetentionDuration, plural, =1 {# day} other {# days}} will be deleted (full history still accessible via compliance export).", "teamRetentionDuration", dur)
	}
	return ns.T("By default, messages are kept forever.")
}

// cdRetentionWarning is getRetentionDurationWarning ("" for none).
func cdRetentionWarning(typ, n int, ok bool, unit int, team core.Retention, admin bool) string {
	if typ == 0 || !cdRetentionValid(typ, n, ok, unit) {
		return ""
	}
	ns := nsChanOptions
	l := cdRetentionDays(n, unit)
	switch {
	case l < 30:
		return ns.T("Historical messages help people find the information they need to get work done. Please check with your team before choosing to delete messages in this channel after {retentionDuration, plural, =1 {# day} other {# days}}.", "retentionDuration", n)
	case team.Type == 2 && l > team.Duration:
		if admin {
			return ns.T("Heads up, It looks like you’re choosing to keep messages in this channel longer than the rest of this workspace.")
		}
		return ns.T("It looks like you’re choosing to keep messages in this channel longer than the rest of this workspace. You may want to check with an Admin to make sure that’s OK.")
	case (team.Type == 1 || team.Type == 0) && l > 5475:
		return ns.T("If you want to keep your messages forever, you can choose the default period for your organization.")
	}
	return ""
}

// updateRetention handles the modal's input before it draws.
func (d *ChannelDetails) updateRetention(gtx C) {
	dl := &d.x.dlg
	rt := dl.rt
	if rt == nil {
		return
	}
	c, id := d.h.Core(), d.id
	if !rt.loaded {
		if r, done, err := rt.load.get(); done && !rt.load.busy() {
			if err == nil {
				rt.typ, rt.initTyp, rt.initDur = r.Type, r.Type, r.Duration
			}
			rt.dur.SetText(strconv.Itoa(rt.initDur))
			rt.durText = rt.dur.Text()
			rt.loaded = true // BaseMessageRetention shows the form once the fetch settles
		}
		return
	}
	for i := range rt.radios {
		if rt.radios[i].Clicked(gtx) && rt.typ != i {
			rt.typ = i
			rt.changed = rt.initTyp != i
		}
	}
	if rt.unitDD.update(gtx) {
		rt.unit = rt.unitDD.sel
		rt.changed = rt.initUnit != rt.unit
	}
	if t := rt.dur.Text(); t != rt.durText {
		rt.durText = t
		n, ok := rt.durValue()
		rt.changed = !ok || n != rt.initDur // parseInt: NaN differs from anything
	}
	n, ok := rt.durValue()
	if dl.ok.Clicked(gtx) && !rt.saving && rt.changed && cdRetentionValid(rt.typ, n, ok, rt.unit) {
		typ, days := rt.typ, cdRetentionDays(n, rt.unit)
		rt.saving = true
		rt.op.load(d.h, false, func(ctx context.Context) (bool, error) { return true, c.SetChannelRetention(ctx, id, typ, days) })
	}
	if rt.saving {
		if _, done, err := rt.op.get(); done && !rt.op.busy() {
			rt.saving = false
			if err != nil {
				rt.err = nsChannelDetails.T("Oops, something went wrong with updating message retention settings. Please try again.")
				return
			}
			d.closeDialog()
		}
	}
}

// retentionDialog draws MessageRetentionModal.
func (d *ChannelDetails) retentionDialog(gtx C, full image.Point, name string, cancel W) {
	dl := &d.x.dlg
	rt := dl.rt
	if rt == nil {
		return
	}
	title := nsChannelDetails.T("Edit {channelName} message history", "channelName", name)
	var body []W
	if rt.err != "" {
		body = append(body, d.cdLegacyAlert("error", rt.err), cdSpace(16))
	}
	n, ok := rt.durValue()
	valid := cdRetentionValid(rt.typ, n, ok, rt.unit)
	if !rt.loaded {
		body = append(body, func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.Inset{Top: 40, Bottom: 40}.Layout(gtx, func(gtx C) D {
				return layout.Center.Layout(gtx, func(gtx C) D { return actSpinner(gtx, gtx.Dp(32), d.p.Muted) }) // jumbo
			})
		})
	} else {
		body = append(body, d.retentionForm(rt, n, ok, valid)...)
	}
	save := d.cdDlgButton(&dl.ok, btnPrimary, nsChannelDetails.T("Save Changes"), !rt.changed || !valid)
	d.skModal(gtx, full, title, true, body, []W{cancel, save})
}

// retentionForm is DataRetention.
func (d *ChannelDetails) retentionForm(rt *cdRetention, n int, ok, valid bool) []W {
	ns := nsChanOptions
	team := d.teamName()
	out := []W{
		func(gtx C) D {
			return d.para(gtx, 15, 22, wReg, d.p.Text, 0, ns.T("You can choose to delete messages in this channel after a set amount of time or use the default time period for the workspace"))
		},
		cdSpace(16), // the <p>'s margin
	}
	if rt.extShare {
		out = append(out, func(gtx C) D {
			return d.cdLegacyAlertW(gtx, "info", d.retentionSharedNote())
		}, cdSpace(16)) // margin_bottom_100
	}
	radio := func(i int, text, sub string) W {
		return func(gtx C) D {
			return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D { // c-label--inline
				return rt.radios[i].Layout(gtx, func(gtx C) D {
					pointerCursor(gtx)
					return row(gtx, layout.Start,
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Top: 3, Right: 12}.Layout(gtx, func(gtx C) D { return cdRadio(&d.base, gtx, rt.typ == i) })
						}),
						layout.Flexed(1, func(gtx C) D {
							return col(gtx,
								layout.Rigid(func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, text) }),
								layout.Rigid(func(gtx C) D {
									if sub == "" {
										return D{}
									}
									return d.para(gtx, 15, 22, wReg, d.p.Muted, 0, sub) // c-label__subtext
								}),
							)
						}),
					)
				})
			})
		}
	}
	out = append(out,
		radio(0, ns.T("Use the default period for {teamName}", "teamName", team), cdTeamRetentionDescription(rt.team.Duration, rt.team.Type)),
		radio(1, ns.T("Delete messages after a specific time period"), ""),
	)
	if rt.typ == 1 {
		out = append(out, cdSpace(8), func(gtx C) D { return d.retentionInput(gtx, rt, n, ok, valid) })
	}
	if rt.typ == 1 && !rt.enforced {
		out = append(out, func(gtx C) D {
			return d.cdLegacyAlert("warning", cdTeamRetentionDescription(rt.team.Duration, rt.team.Type))(gtx)
		})
	} else if w := cdRetentionWarning(rt.typ, n, ok, rt.unit, rt.team, d.h.Core().IsWorkspaceAdmin()); w != "" {
		out = append(out, d.cdLegacyAlert("warning", w))
	}
	return out
}

// retentionInput is CustomRetentionDurationInput: the number field (a
// c-input_text: 36 tall, radius 8, 1px border, 4px 12px padding, 15px,
// 20px under it) and the Days / Years select, then the error line.
func (d *ChannelDetails) retentionInput(gtx C, rt *cdRetention, n int, ok, valid bool) D {
	invalid := ok && !valid // the field is red unless it's empty (NaN)
	return col(gtx,
		layout.Rigid(func(gtx C) D {
			sel := gtx.Dp(100) + gtx.Dp(16)
			return row(gtx, layout.Start,
				layout.Flexed(1, func(gtx C) D {
					w, h := gtx.Constraints.Max.X, gtx.Dp(36)
					r := image.Rect(0, 0, w, h)
					rad := gtx.Dp(8)
					line := d.p.Faint
					if invalid {
						line = d.p.Red
					}
					if gtx.Focused(&rt.dur) {
						ring := gtx.Dp(5)
						rrect(gtx, r.Inset(-ring), rad+ring, flatten(theme.WithAlpha(d.p.Link, 0x4d), d.p.Surface))
						one := max(1, gtx.Dp(1))
						rrect(gtx, r.Inset(-one), rad+one, d.p.Link)
					}
					rrect(gtx, r, rad, d.p.Surface)
					border(gtx, r, rad, max(1, gtx.Dp(1)), line)
					g := gtx
					g.Constraints = layout.Exact(image.Pt(w, h))
					layout.Inset{Left: 12, Right: 12}.Layout(g, func(gtx C) D {
						return layout.W.Layout(gtx, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							return d.th.Editor(gtx, &rt.dur, 15, d.p.Text, d.p.Mention)
						})
					})
					return D{Size: image.Pt(w, h+gtx.Dp(20))}
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: 16}.Layout(gtx, func(gtx C) D {
						rt.unitDD.sel = rt.unit
						dims := rt.unitDD.layoutSelect(&d.base, gtx, 100)
						dims.Size.X = sel - gtx.Dp(16)
						return dims
					})
				}),
			)
		}),
		layout.Rigid(func(gtx C) D {
			if !invalid {
				return D{}
			}
			// margin_top_50 margin_bottom_50 sk_raspberry_red: info-circle
			// (inherit size) 4px before the text
			return layout.Inset{Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return row(gtx, layout.Start,
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: 3, Right: 4}.Layout(gtx, func(gtx C) D { return d.icon(gtx, "info", 15, d.p.Red) })
					}),
					layout.Flexed(1, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Red, 0, cdRetentionDurationError(n, rt.unit)) }),
				)
			})
		}),
	)
}

// cdTok is Slack's --dt_color-<name> in this mode, a stylesheet theme's
// own value when it sets one.
func (d *ChannelDetails) cdTok(name string) color.NRGBA {
	c, _ := css.ParseColor(css.SlackVars(d.th.Dark)["--dt_color-"+name])
	if d.th.Tokens != nil {
		theme.SetColor(d.th.Tokens, &c, "var(--dt_color-"+name+")")
	}
	return c
}

// cdLegacyAlert is the legacy Alert (MlDl, c-alert--boxed) at level
// info | warning | error: 12px 16px padding, radius 8, the level's
// surface, a 20px icon 12px before the 15/22 message.
func (d *ChannelDetails) cdLegacyAlert(level, text string) W {
	return func(gtx C) D {
		return d.cdLegacyAlertW(gtx, level, func(gtx C) D { return d.para(gtx, 15, 22, wReg, d.p.Text, 0, text) })
	}
}

func (d *ChannelDetails) cdLegacyAlertW(gtx C, level string, msg W) D {
	bg, ic, icon := "surf-hgl-1", "content-hgl-1", "info"
	switch level {
	case "warning":
		bg, ic, icon = "surf-hgl-3", "content-hgl-3", "warning"
	case "error":
		bg, ic, icon = "surf-imp", "content-imp", "warning"
	}
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Top: 12, Bottom: 12, Left: 16, Right: 16}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return row(gtx, layout.Start,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 2, Right: 12}.Layout(gtx, func(gtx C) D { return d.icon(gtx, icon, 20, d.cdTok(ic)) })
			}),
			layout.Flexed(1, msg),
		)
	})
	call := m.Stop()
	rrect(gtx, image.Rectangle{Max: dims.Size}, gtx.Dp(8), flatten(d.cdTok(bg), d.p.Surface))
	call.Add(gtx.Ops)
	return dims
}

// retentionSharedNote is the shared-channel alert's message: rt's
// "<strong>Note:</strong> …", a space, and the HelpLink.
func (d *ChannelDetails) retentionSharedNote() W {
	ns := nsChanOptions
	return func(gtx C) D {
		var spans []richtext.Span
		var plain strings.Builder
		add := func(sp richtext.Span) {
			if sp.Text != "" {
				spans = append(spans, sp)
				plain.WriteString(sp.Text)
			}
		}
		for _, p := range ns.RT("<strong>Note:</strong> Since this channel is shared with other organizations, your retention settings will only apply to messages from members of your organization.") {
			sp := richtext.Span{Kind: richtext.Text, Text: p.Text}
			if strings.EqualFold(p.Tag, "strong") {
				sp.Style = richtext.Bold
			}
			add(sp)
		}
		add(richtext.Span{Kind: richtext.Text, Text: " "})
		for _, p := range ns.RT("<a>Learn more about retention in shared channels</a>") {
			sp := richtext.Span{Kind: richtext.Text, Text: p.Text}
			if strings.EqualFold(p.Tag, "a") {
				sp.Kind, sp.URL = richtext.Link, cdRetentionSharedArticle
			}
			add(sp)
		}
		doc := richtext.Doc{Blocks: []richtext.Block{{Kind: richtext.Paragraph, Spans: spans}}}
		return d.richDoc(gtx, "dlg/ret-shared/"+d.id, plain.String(), doc, richview.Options{}, 0)
	}
}

// exportBody is ExportChannelHistoryModalContents' text.
func (d *ChannelDetails) exportBody() []W {
	return []W{d.cdPara("export", nsChannelDetails.T("You can export all the messages from this channel as a JSON file. Slackbot will notify you when your file is ready. "))}
}

// exportNow is Export Messages: exportChannelHistory, then the modal
// closes without waiting.
func (d *ChannelDetails) exportNow() {
	c, id := d.h.Core(), d.id
	d.x.exportOp.load(d.h, true, func(ctx context.Context) (bool, error) { return true, c.ExportChannel(ctx, id) })
	d.closeDialog()
}
