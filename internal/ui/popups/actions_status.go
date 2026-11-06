package popups

import (
	"context"
	"image"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// "Set a status" modal (status.png empty, status2.png typing): 42px input
// (radius 8, focus ring 1px #1264a3 + 4px) with a leading emoji button,
// 18px text, placeholder "What's your status?", clear button when filled.
// Empty: preset sections (legend 13/700 rgba(29,28,29,.7), rows emoji 20 +
// **text** — duration 15px). Filled: "Duration" 15/700, a 44px select
// (radius 8, 18px value, chevron) and "Pause notifications until status is
// cleared" with a 16px checkbox.

type actDur int

const (
	actDurNone actDur = iota // Don't clear
	actDur30m
	actDur1h
	actDur4h
	actDurToday
	actDurWeek
	actDurKeep // keep the current status's expiration
)

var actDurLabels = [...]string{"Don’t clear", "30 minutes", "1 hour", "4 hours", "Today", "This week", ""}

type actPreset struct {
	emoji, text string
	dur         actDur
}

var actPresets = []actPreset{
	{"calendar", "In a meeting", actDur1h},
	{"bus", "Commuting", actDur30m},
	{"face_with_thermometer", "Out sick", actDurToday},
	{"palm_tree", "Vacationing", actDurNone},
	{"house_with_garden", "Working remotely", actDurToday},
}

// actStatusExpiry is the status_expiration (unix seconds, 0 = never) for
// duration d chosen at now; keep is the existing expiration for actDurKeep.
func actStatusExpiry(now time.Time, d actDur, keep int64) int64 {
	y, m, day := now.Date()
	loc := now.Location()
	switch d {
	case actDur30m:
		return now.Add(30 * time.Minute).Unix()
	case actDur1h:
		return now.Add(time.Hour).Unix()
	case actDur4h:
		return now.Add(4 * time.Hour).Unix()
	case actDurToday:
		return time.Date(y, m, day, 23, 59, 59, 0, loc).Unix()
	case actDurWeek:
		// end of Sunday (Slack weeks end on Sunday).
		days := (7 - int(now.Weekday())) % 7
		return time.Date(y, m, day+days, 23, 59, 59, 0, loc).Unix()
	case actDurKeep:
		return keep
	}
	return 0
}

// actKeepLabel describes an existing expiration ("Until 5:00 PM").
func actKeepLabel(exp int64, now time.Time) string {
	t := time.Unix(exp, 0).In(now.Location())
	y1, m1, d1 := t.Date()
	y2, m2, d2 := now.Date()
	if y1 == y2 && m1 == m2 && d1 == d2 {
		return "Until " + clock(t)
	}
	return "Until " + t.Format("Jan ") + ordinal(d1) + " at " + clock(t)
}

type actStatus struct {
	h       Host
	modal   *Modal
	ed      widget.Editor
	emoji   string
	dur     actDur
	keepExp int64
	pause   bool
	had     bool
	team    string
	focus   bool // focus the input on the next layout

	emojiBtn, clearBtn, selBtn, pauseBtn, oooBtn widget.Clickable
	presetBtn                                    []widget.Clickable

	emojiRect, selRect image.Rectangle // body coords, last layout
}

// NewSetStatusModal builds the "Set a status" modal, prefilled with the
// viewer's current status. Show it with h.ShowOverlay.
func NewSetStatusModal(h Host) *Modal {
	s := &actStatus{h: h, dur: actDurToday, focus: true, team: "Hack Club"}
	s.ed.SingleLine = true
	s.ed.Submit = true
	s.presetBtn = make([]widget.Clickable, len(actPresets))
	if a := h.Core(); a != nil {
		me := a.Self()
		if me.StatusText != "" || me.StatusEmoji != "" {
			s.had = true
			s.ed.SetText(me.StatusText)
			s.emoji = strings.Trim(me.StatusEmoji, ":")
			if me.StatusExp == 0 {
				s.dur = actDurNone
			} else {
				s.dur, s.keepExp = actDurKeep, me.StatusExp
			}
		}
	}
	if snap, ok := actSnapshot(h); ok && snap.Team.Name != "" {
		s.team = snap.Team.Name
	}
	var btns []*ModalButton
	if s.had {
		btns = append(btns, &ModalButton{Label: "Clear Status", Kind: BtnOutline, Do: func() bool {
			s.clearStatus()
			return true
		}})
	}
	btns = append(btns,
		&ModalButton{Label: "Cancel", Kind: BtnOutline},
		&ModalButton{Label: "Save", Kind: BtnPrimary, Enabled: s.canSave, Do: func() bool {
			s.save()
			return true
		}},
	)
	s.modal = NewModal(h, ModalOpts{Title: "Set a status", Body: s.layout, Buttons: btns})
	return s.modal
}

func (s *actStatus) text() string { return strings.TrimSpace(s.ed.Text()) }

func (s *actStatus) canSave() bool { return s.text() != "" || s.emoji != "" || s.had }

func (s *actStatus) clearStatus() {
	actDo(s.h, "Status cleared", func(ctx context.Context, a *core.App) error { return a.SetStatus(ctx, "", "", 0) })
}

func (s *actStatus) save() {
	h := s.h
	txt, emoji := s.text(), s.emoji
	if txt == "" && emoji == "" {
		s.clearStatus()
		return
	}
	a := h.Core()
	if a == nil {
		h.Toast("Not connected yet")
		return
	}
	now := time.Now()
	exp := actStatusExpiry(now, s.dur, s.keepExp)
	pause := s.pause && exp > now.Unix()
	mins := 0
	if pause {
		mins = actMinutesUntil(now, time.Unix(exp, 0))
	}
	run(h, func(ctx context.Context) (struct{}, error) {
		if err := a.SetStatus(ctx, txt, emoji, exp); err != nil {
			return struct{}{}, err
		}
		if pause {
			return struct{}{}, a.SetDND(ctx, mins)
		}
		return struct{}{}, nil
	}, func(_ struct{}, err error) {
		if err != nil {
			h.Toast("Couldn't do that: " + errText(err))
			return
		}
		if pause {
			actDNDUntil = time.Unix(exp, 0)
		}
		h.Toast("Status updated")
	})
}

// win converts a body-local point to window coordinates.
func (s *actStatus) win(gtx layout.Context, p image.Point) image.Point {
	return s.modal.rect.Min.Add(image.Pt(dp(gtx, 28), dp(gtx, 70))).Add(p)
}

func (s *actStatus) durLabel(d actDur) string {
	if d == actDurKeep {
		return actKeepLabel(s.keepExp, time.Now())
	}
	return actDurLabels[d]
}

func (s *actStatus) events(gtx layout.Context) {
	h := s.h
	for {
		e, ok := s.ed.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok && s.canSave() {
			s.save()
			s.modal.Dismiss()
			return
		}
	}
	if s.emojiBtn.Clicked(gtx) {
		r := s.emojiRect
		h.OpenEmojiPicker(s.win(gtx, image.Pt(r.Min.X, r.Max.Y+dp(gtx, 4))), func(name string) {
			s.emoji = strings.Trim(name, ":")
			s.focus = true
			h.Invalidate()
		})
	}
	if s.clearBtn.Clicked(gtx) {
		s.ed.SetText("")
		s.emoji = ""
		if s.dur == actDurKeep {
			s.dur = actDurToday
		}
		s.focus = true
	}
	for i := range s.presetBtn {
		if s.presetBtn[i].Clicked(gtx) {
			p := actPresets[i]
			s.ed.SetText(p.text)
			n := s.ed.Len()
			s.ed.SetCaret(n, n)
			s.emoji, s.dur = p.emoji, p.dur
			s.focus = true
		}
	}
	if s.oooBtn.Clicked(gtx) {
		h.Toast("Scheduling a status isn't supported yet")
	}
	if s.pauseBtn.Clicked(gtx) {
		s.pause = !s.pause
	}
	if s.selBtn.Clicked(gtx) {
		s.openDurations(gtx)
	}
}

func (s *actStatus) openDurations(gtx layout.Context) {
	h := s.h
	var items []MenuItem
	pick := func(d actDur) func() {
		return func() {
			s.dur = d
			s.focus = true
			h.Invalidate()
		}
	}
	if s.keepExp > 0 {
		items = append(items, MenuItem{Label: s.durLabel(actDurKeep), Checked: s.dur == actDurKeep, Do: pick(actDurKeep)})
	}
	for d := actDurNone; d <= actDurWeek; d++ {
		items = append(items, MenuItem{Label: actDurLabels[d], Checked: s.dur == d, Do: pick(d)})
	}
	items = append(items, MenuItem{Label: "Choose a start and end time…", Do: actToast(h, "Custom status times aren't supported yet")})
	r := s.selRect
	wr := image.Rectangle{Min: s.win(gtx, r.Min), Max: s.win(gtx, r.Max)}
	ppd := gtx.Metric.PxPerDp
	if ppd <= 0 {
		ppd = 1
	}
	wdp := float32(r.Dx()) / ppd
	h.ShowOverlay(NewMenuOpts(h, items, MenuOpts{Anchor: wr, Width: wdp, OnClose: func() { s.focus = true }}))
}

func (s *actStatus) layout(gtx layout.Context) layout.Dimensions {
	if s.modal.done {
		return layout.Dimensions{}
	}
	if s.focus {
		gtx.Execute(key.FocusCmd{Tag: &s.ed})
		s.focus = false
	}
	s.events(gtx)
	th := s.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	at(gtx, image.Pt(0, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, dp(gtx, 42))}
		return s.input(gtx, th, cl)
	})
	y += dp(gtx, 42)
	if s.ed.Len() == 0 && s.emoji == "" {
		y += dp(gtx, 16)
		y += s.legend(gtx, th, cl, y, "Schedule for later")
		y += s.preset(gtx, th, cl, y, &s.oooBtn, "no_entry", "Out of office", "Set a start and end date")
		y += dp(gtx, 16)
		y += s.legend(gtx, th, cl, y, "For "+s.team)
		for i, p := range actPresets {
			y += s.preset(gtx, th, cl, y, &s.presetBtn[i], p.emoji, p.text, actDurLabels[p.dur])
		}
		return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 4))}
	}
	// duration
	y += dp(gtx, 20)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return text(gtx, th, 15, theme.Bold, cl.Fg, "Duration")
	})
	y += dp(gtx, 22) + dp(gtx, 8)
	sh := dp(gtx, 44)
	s.selRect = image.Rect(0, y, w, y+sh)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &s.selBtn, nil, func(gtx layout.Context) layout.Dimensions {
			r := image.Rect(0, 0, w, sh)
			border := theme.WithAlpha(cl.Fg, 0x55)
			if s.selBtn.Hovered() {
				border = theme.WithAlpha(cl.Fg, 0x99)
			}
			rrect(gtx, r, dp(gtx, 8), border)
			rrect(gtx, r.Inset(1), dp(gtx, 8)-1, cl.Bg)
			at(gtx, image.Pt(dp(gtx, 12), 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 52)), sh)}
				return vcenter(gtx, sh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 18, theme.Regular, cl.Fg, s.durLabel(s.dur))
				})
			})
			at(gtx, image.Pt(w-dp(gtx, 12+16), (sh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "chevron-down", 16, cl.Fg)
			})
			return layout.Dimensions{Size: r.Max}
		})
	})
	y += sh
	if s.dur != actDurNone {
		y += dp(gtx, 12)
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &s.pauseBtn, nil, func(gtx layout.Context) layout.Dimensions {
				rh := dp(gtx, 22)
				actCheckbox(gtx, cl, image.Pt(0, (rh-dp(gtx, 16))/2), s.pause)
				lbl := "Pause notifications until status is cleared"
				at(gtx, image.Pt(dp(gtx, 25), 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
						return text(gtx, th, 15, theme.Regular, cl.Fg, lbl)
					})
				})
				return layout.Dimensions{Size: image.Pt(dp(gtx, 25)+textW(gtx, th, 15, theme.Regular, lbl), rh)}
			})
		})
		y += dp(gtx, 22)
	}
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 4))}
}

// input draws the 42px status field.
func (s *actStatus) input(gtx layout.Context, th *theme.Theme, cl *Colors) layout.Dimensions {
	w := gtx.Constraints.Max.X
	hh := dp(gtx, 42)
	r := image.Rect(0, 0, w, hh)
	rad := dp(gtx, 8)
	if gtx.Focused(&s.ed) {
		ring := dp(gtx, 4)
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		rrect(gtx, r, rad, cl.Focus)
	} else {
		rrect(gtx, r, rad, cl.InputBorder)
	}
	rrect(gtx, r.Inset(1), rad-1, cl.Bg)

	bs := dp(gtx, 28)
	bp := image.Pt(dp(gtx, 7), (hh-bs)/2)
	s.emojiRect = image.Rectangle{Min: bp, Max: bp.Add(image.Pt(bs, bs))}
	at(gtx, bp, func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, &s.emojiBtn, func(gtx layout.Context, sz image.Point, hovered bool) {
			if hovered {
				rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), cl.BgHover)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			is := dp(gtx, 20)
			at(gtx, image.Pt((bs-is)/2, (bs-is)/2), func(gtx layout.Context) layout.Dimensions {
				if s.emoji != "" {
					return emojiImage(gtx, s.h, s.emoji, 20)
				}
				return icon(gtx, "emoji", 20, cl.Fg2)
			})
			return layout.Dimensions{Size: image.Pt(bs, bs)}
		})
	})
	filled := s.ed.Len() > 0 || s.emoji != ""
	right := w - dp(gtx, 12)
	if filled {
		cp := image.Pt(w-dp(gtx, 7)-bs, (hh-bs)/2)
		at(gtx, cp, func(gtx layout.Context) layout.Dimensions {
			return iconButton(gtx, &s.clearBtn, "x-circle", 28, 20, cl.Fg, cl.BgHover, 4)
		})
		right = cp.X - dp(gtx, 4)
	}
	x := dp(gtx, 42)
	tw := maxInt(1, right-x)
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, hh, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Min: image.Pt(tw, 0), Max: image.Pt(tw, hh)}
			if s.ed.Len() == 0 {
				call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 18, theme.Regular, cl.Placeholder, "What's your status?")
				})
				call.Add(gtx.Ops)
			}
			return th.Editor(gtx, &s.ed, unit.Sp(18), cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
		})
	})
	return layout.Dimensions{Size: r.Max}
}

// legend draws a preset section heading at y and returns its height.
func (s *actStatus) legend(gtx layout.Context, th *theme.Theme, cl *Colors, y int, label string) int {
	lh := dp(gtx, 30)
	at(gtx, image.Pt(dp(gtx, 8), y), func(gtx layout.Context) layout.Dimensions {
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 13, theme.Bold, theme.WithAlpha(cl.Fg, 0xb3), label)
		})
	})
	return lh
}

// preset draws one "emoji **text** — duration" row at y; returns its height.
func (s *actStatus) preset(gtx layout.Context, th *theme.Theme, cl *Colors, y int, btn *widget.Clickable, emoji, label, dur string) int {
	w := gtx.Constraints.Max.X
	rh := dp(gtx, 28)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, rh))
		return clickBox(gtx, btn, func(gtx layout.Context, sz image.Point, hovered bool) {
			if hovered {
				rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), cl.BgHover)
			}
		}, func(gtx layout.Context) layout.Dimensions {
			at(gtx, image.Pt(dp(gtx, 8), (rh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
				return emojiImage(gtx, s.h, emoji, 20)
			})
			x := dp(gtx, 40)
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Bold, cl.Fg, label)
				})
			})
			x += textW(gtx, th, 15, theme.Bold, label)
			at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x), rh)}
				return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
					return text(gtx, th, 15, theme.Regular, cl.Muted, " — "+dur)
				})
			})
			return layout.Dimensions{Size: image.Pt(w, rh)}
		})
	})
	return rh
}

// actCheckbox draws a 16px Slack checkbox at p.
func actCheckbox(gtx layout.Context, cl *Colors, p image.Point, on bool) {
	sz := dp(gtx, 16)
	r := image.Rectangle{Min: p, Max: p.Add(image.Pt(sz, sz))}
	rad := dp(gtx, 4)
	if on {
		rrect(gtx, r, rad, cl.Highlight)
		is := dp(gtx, 12)
		at(gtx, p.Add(image.Pt((sz-is)/2, (sz-is)/2)), func(gtx layout.Context) layout.Dimensions {
			return icon(gtx, "check", 12, cl.HighText)
		})
		return
	}
	rrect(gtx, r, rad, theme.WithAlpha(cl.Fg, 0x80))
	rrect(gtx, r.Inset(1), rad-1, cl.Bg)
}
