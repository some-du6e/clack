package popups

import (
	"context"
	"gioui.org/io/event"
	"image"
	"image/color"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/ui/theme"
)

// Slack's "Create a channel" modal (boot-shared Gznj CreateChannelModal,
// build a84662d2d135, 2026-11-08) and the "Add people" step after it
// (EtJj onCreateChannelComplete → L3vL channel invite modal, isFromCreate).
//
// Steps (uihW useSteps): "workspace" (Grid accounts in several
// workspaces; Clack signs in to one, so it never shows), "name",
// "visibility". The footer reads "Step N of M" with Back from step 2 and
// Next, then Create on the last step. While the name has an error (even a
// hidden one), is empty or is still being checked, Next is disabled.
//
//   - Name (LwQ5 + 45Rv): label "Name", a 44px input with the channel
//     icon, placeholder "e.g. plan-budget" and an 80-character countdown;
//     the hint "Channels are where conversations happen around a topic. Use
//     a name that is easy to find and understand." Each edit is lowercased,
//     leading "#"s dropped and whitespace turned into "-"
//     (core.NormalizeChannelName), then checked with
//     conversations.validateName 500 ms later. Errors (PvOQ strings)
//     replace the hint, but only once a second has passed since the first
//     edit (the modal's suppressInlineError).
//   - Visibility (yqBD): "Visibility", radios "Public — anyone in
//     <b>{workspace}</b>" and "Private — only specific people" with
//     "Can only be viewed or joined by invitation". The header adds the
//     channel name under the title with a # or lock icon.
//   - Create: conversations.create. An error replaces the content with its
//     message and the footer with one "Okay" button that closes.
//   - Then Slack opens the new channel and the invite modal: "Add people
//     to #name", a people search ("ex. Nathalie, or @nathalie": Clack
//     adds members by name, not email), and "Skip for now" until someone
//     is picked, when the button becomes "Add".

// CreateChannelOpts open the modal.
type CreateChannelOpts struct {
	// Section is the sidebar section it was opened from (a custom section
	// or Starred keeps the new channel; see core.CreateChannel).
	Section string
	// Name prefills the name; Private starts on Private.
	Name    string
	Private bool
}

// Create-channel strings (create_channel_modal / channel_modal /
// channel_invite namespaces).
const (
	ccTitle        = "Create a channel"
	ccNameLabel    = "Name"
	ccPlaceholder  = "e.g. plan-budget"
	ccHint         = "Channels are where conversations happen around a topic. Use a name that is easy to find and understand."
	ccVisibility   = "Visibility"
	ccPublicPre    = "Public — anyone in "
	ccPrivate      = "Private — only specific people"
	ccPrivateSub   = "Can only be viewed or joined by invitation"
	ccInviteTitle  = "Add people to "
	ccInviteSearch = "ex. Nathalie, or @nathalie"
	ccSkip         = "Skip for now"
	ccAdd          = "Add"
)

// ccDebounce is 45Rv's validateName debounce; ccRevealErr is how long the
// modal keeps name errors hidden after the first edit.
var (
	ccDebounce  = 500 * time.Millisecond
	ccRevealErr = time.Second
)

// CreateChannelModal is the open modal.
type CreateChannelModal struct {
	h     Host
	o     CreateChannelOpts
	modal *Modal

	step      int // 0 name, 1 visibility
	ed        widget.Editor
	focused   bool
	name      string
	gen       int    // bumped by every edit: stale checks are dropped
	checking  bool   // a validateName is pending for name
	code      string // its verdict ("" ok)
	edited    bool
	showErr   bool
	private   bool
	pubBtn    widget.Clickable
	privBtn   widget.Clickable
	creating  bool
	createErr string
	workspace string

	next, back, create, okay *ModalButton
	// OnCreated runs after the channel exists (tests; the shell navigates
	// through the Host).
	OnCreated func(core.Conv)
}

// NewCreateChannelModal builds the modal; show it with h.ShowOverlay.
func NewCreateChannelModal(h Host, o CreateChannelOpts) *CreateChannelModal {
	c := &CreateChannelModal{h: h, o: o, private: o.Private}
	c.ed.SingleLine = true
	c.ed.Submit = true
	c.workspace = "this workspace"
	if a := h.Core(); a != nil {
		select {
		case <-a.Ready():
			if n := strings.TrimSpace(a.Snapshot().Team.Name); n != "" {
				c.workspace = n
			}
		default:
		}
	}
	c.next = &ModalButton{Label: "Next", Kind: BtnPrimary, Enabled: c.canNext, Do: func() bool { c.Next(); return false }}
	c.back = &ModalButton{Label: "Back", Kind: BtnOutline, Do: func() bool { c.Back(); return false }}
	c.create = &ModalButton{Label: "Create", Kind: BtnPrimary, Enabled: func() bool { return !c.creating }, Do: func() bool { c.Create(); return false }}
	c.okay = &ModalButton{Label: "Okay", Kind: BtnPrimary}
	c.modal = NewModal(h, ModalOpts{Title: ccTitle, Body: c.layout, FocusStops: func() []event.Tag {
		if c.step == 0 {
			return []event.Tag{&c.ed}
		}
		if c.private { // a radio group is one tab stop: the checked radio
			return []event.Tag{&c.privBtn}
		}
		return []event.Tag{&c.pubBtn}
	}})
	if o.Name != "" {
		c.SetName(o.Name)
	}
	c.sync()
	return c
}

// Layout and Dismissed make it an Overlay.
func (c *CreateChannelModal) Layout(gtx layout.Context) layout.Dimensions { return c.modal.Layout(gtx) }
func (c *CreateChannelModal) Dismissed() bool                             { return c.modal.Dismissed() }

// Dismiss closes it.
func (c *CreateChannelModal) Dismiss() { c.modal.Dismiss() }

// Step is "name" or "visibility".
func (c *CreateChannelModal) Step() string {
	if c.step == 1 {
		return "visibility"
	}
	return "name"
}

// Name is the (normalized) name typed so far.
func (c *CreateChannelModal) Name() string { return c.name }

// NameError is the message under the name input ("" for none or while
// still hidden).
func (c *CreateChannelModal) NameError() string {
	if !c.showErr {
		return ""
	}
	return core.ChannelNameErrorText(c.code, c.workspaceName())
}

// CreateError is the error that replaced the content after Create.
func (c *CreateChannelModal) CreateError() string { return c.createErr }

func (c *CreateChannelModal) workspaceName() string {
	if c.workspace == "this workspace" {
		return ""
	}
	return c.workspace
}

// SetName replaces the name as if typed (normalized and checked).
func (c *CreateChannelModal) SetName(s string) {
	c.ed.SetText(s)
	c.onEdit()
}

// SetPrivate picks the visibility.
func (c *CreateChannelModal) SetPrivate(p bool) { c.private = p; c.sync() }

// CanNext reports whether Next would leave the name step: a name whose
// validateName check came back clean (tests wait on it; NameError alone
// reads "" while the check is still pending or its error still hidden).
func (c *CreateChannelModal) CanNext() bool { return c.canNext() }

func (c *CreateChannelModal) canNext() bool {
	return c.step == 0 && !c.checking && c.code == "" && c.name != ""
}

// Next leaves the name step.
func (c *CreateChannelModal) Next() {
	if !c.canNext() {
		return
	}
	c.step = 1
	c.sync()
}

// Back returns to the name step.
func (c *CreateChannelModal) Back() {
	if c.step > 0 {
		c.step--
	}
	c.sync()
}

// Create makes the channel (conversations.create); on success the shell
// opens it and the "Add people" step follows.
func (c *CreateChannelModal) Create() {
	if c.creating || c.step != 1 {
		return
	}
	c.creating = true
	o := core.CreateChannelOpts{Name: c.name, Private: c.private, Section: c.o.Section}
	run(c.h, func(ctx context.Context) (core.Conv, error) {
		a := c.h.Core()
		if a == nil {
			return core.Conv{}, context.Canceled
		}
		return a.CreateChannel(ctx, o)
	}, func(v core.Conv, err error) {
		c.creating = false
		if err != nil {
			c.createErr = core.ChannelNameErrorText(core.ChannelNameCode(err), c.workspaceName())
			c.sync()
			return
		}
		c.modal.Dismiss()
		c.h.Navigate("c/" + v.ID)
		if c.OnCreated != nil {
			c.OnCreated(v)
		}
		c.h.ShowOverlay(NewAddPeopleModal(c.h, v))
	})
	c.h.Invalidate()
}

// sync points the modal's header and footer at the current step.
func (c *CreateChannelModal) sync() {
	o := &c.modal.o
	o.Subtitle, o.SubtitleIcon = "", ""
	switch {
	case c.createErr != "":
		o.Buttons, o.FooterNote = []*ModalButton{c.okay}, ""
	case c.step == 0:
		o.Buttons, o.FooterNote = []*ModalButton{c.next}, "Step 1 of 2"
	default:
		o.Buttons, o.FooterNote = []*ModalButton{c.back, c.create}, "Step 2 of 2"
		o.Subtitle, o.SubtitleIcon = c.name, "hash"
		if c.private {
			o.SubtitleIcon = "lock"
		}
	}
}

// onEdit normalizes the input like Slack's and schedules the check.
func (c *CreateChannelModal) onEdit() {
	raw := c.ed.Text()
	norm := core.NormalizeChannelName(raw)
	if norm != raw {
		caret, _ := c.ed.Selection()
		prefix := string([]rune(raw)[:min(caret, utf8.RuneCountInString(raw))])
		n := utf8.RuneCountInString(core.NormalizeChannelName(prefix))
		c.ed.SetText(norm)
		c.ed.SetCaret(n, n)
	}
	if norm == c.name && c.edited {
		return
	}
	c.name = norm
	c.gen++
	gen := c.gen
	c.checking = true
	if !c.edited {
		c.edited = true
		time.AfterFunc(ccRevealErr, func() { post(c.h, func() { c.showErr = true }) })
	}
	time.AfterFunc(ccDebounce, func() {
		post(c.h, func() {
			if gen != c.gen || c.modal.Dismissed() {
				return
			}
			name := c.name
			run(c.h, func(ctx context.Context) (string, error) {
				a := c.h.Core()
				if a == nil {
					return "", context.Canceled
				}
				return a.ValidateChannelName(ctx, name)
			}, func(_ string, err error) {
				if gen != c.gen {
					return
				}
				c.checking = false
				c.code = core.ChannelNameCode(err)
			})
		})
	})
}

func (c *CreateChannelModal) events(gtx layout.Context) {
	if !c.focused && c.step == 0 && c.createErr == "" {
		gtx.Execute(key.FocusCmd{Tag: &c.ed})
		c.focused = true
	}
	for {
		e, ok := c.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			c.onEdit()
		case widget.SubmitEvent:
			c.Next()
		}
	}
	if c.pubBtn.Clicked(gtx) {
		c.private = false
		c.sync()
	}
	if c.privBtn.Clicked(gtx) {
		c.private = true
		c.sync()
	}
}

func (c *CreateChannelModal) layout(gtx layout.Context) layout.Dimensions {
	c.events(gtx)
	th := c.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	if c.createErr != "" {
		d := para(loose(gtx, image.Pt(w, 1<<20)), th, 15, 22, theme.Regular, cl.Fg, c.createErr, 0)
		return layout.Dimensions{Size: image.Pt(w, d.Size.Y+dp(gtx, 4))}
	}
	if c.step == 1 {
		return c.layoutVisibility(gtx, th, cl, w)
	}
	return c.layoutName(gtx, th, cl, w)
}

func ccErrColor(th *theme.Theme, cl *Colors) color.NRGBA {
	if th.Dark {
		return cl.Danger
	}
	return rgb(0xe01e5a) // --dt_color-content-imp
}

func (c *CreateChannelModal) layoutName(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	y := 0
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, ccNameLabel)
		})
	})
	y += lh + dp(gtx, 8) // .p-channel_name_input__input margin-top 8
	ih := dp(gtx, 44)    // c-input_text--large
	errText := c.NameError()
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ih))
		d := field(gtx, th, &c.ed, ccPlaceholder, "hash", 44, 18)
		if errText != "" {
			stroke(gtx, image.Rect(0, 0, w, ih), dp(gtx, 8), maxInt(1, dp(gtx, 1)), ccErrColor(th, cl))
		}
		// .c-input_character_count--large: the characters left, 18px,
		// content-sec (content-imp when negative), on the input's bg
		left := core.MaxChannelNameLength - utf8.RuneCountInString(c.name)
		num := strconv.Itoa(left)
		col := cl.Fg3
		if left < 0 {
			col = ccErrColor(th, cl)
		}
		nw := textW(gtx, th, 18, theme.Regular, num)
		bx := w - dp(gtx, 12) - nw
		rrect(gtx, image.Rect(bx-dp(gtx, 6), dp(gtx, 2), w-dp(gtx, 2), ih-dp(gtx, 2)), dp(gtx, 6), cl.Bg)
		at(gtx, image.Pt(bx, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(nw+dp(gtx, 2), ih)}
			return vcenter(gtx, ih, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 18, theme.Regular, col, num)
			})
		})
		return d
	})
	y += ih
	if errText != "" {
		// c-inline_alert: 13px content-imp with the warning icon, margin 4 0
		y += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			ec := ccErrColor(th, cl)
			at(gtx, image.Pt(0, dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "warning", 14, ec)
			})
			return at(gtx, image.Pt(dp(gtx, 20), 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 20), 1<<20)}
				return para(gtx, th, 13, 18, theme.Regular, ec, errText, 0)
			})
		})
		y += d.Size.Y + dp(gtx, 4)
	} else {
		// .c-input hint: 13px, #1d1c1db3, padding-top 4
		y += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, ccHint, 0)
		})
		y += d.Size.Y
	}
	y += dp(gtx, 20) // margin-bottom 20
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// radio draws Slack's 18px radio (c-input_radio).
func radio(gtx layout.Context, cl *Colors, on bool) layout.Dimensions {
	sz := dp(gtx, 18)
	c := image.Pt(sz/2, sz/2)
	if on {
		theme.Circle(gtx, c, sz, cl.Highlight)
		theme.Circle(gtx, c, dp(gtx, 8), rgb(0xffffff))
	} else {
		theme.Circle(gtx, c, sz, theme.WithAlpha(cl.Fg, 0x80))
		theme.Circle(gtx, c, sz-2*maxInt(1, dp(gtx, 1)), cl.Bg)
	}
	return layout.Dimensions{Size: image.Pt(sz, sz)}
}

func (c *CreateChannelModal) layoutVisibility(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	y := 0
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
		return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Bold, cl.Fg, ccVisibility)
		})
	})
	y += lh + dp(gtx, 8)
	row := func(btn *widget.Clickable, on bool, label func(gtx layout.Context) layout.Dimensions, sub string) {
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return clickBox(gtx, btn, nil, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(0, dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions { return radio(gtx, cl, on) })
				tx := dp(gtx, 28)
				ld := at(gtx, image.Pt(tx, 0), label)
				hgt := ld.Size.Y
				if sub != "" {
					sd := at(gtx, image.Pt(tx, hgt), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Max: image.Pt(w-tx, 1<<20)}
						return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, sub, 0)
					})
					hgt += sd.Size.Y
				}
				return layout.Dimensions{Size: image.Pt(w, maxInt(hgt, dp(gtx, 22)))}
			})
		})
		y += d.Size.Y + dp(gtx, 12)
	}
	row(&c.pubBtn, !c.private, func(gtx layout.Context) layout.Dimensions {
		pw := textW(gtx, th, 15, theme.Regular, ccPublicPre)
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
		vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, cl.Fg, ccPublicPre)
		})
		at(gtx, image.Pt(pw, 0), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Bold, cl.Fg, c.workspace)
			})
		})
		return layout.Dimensions{Size: image.Pt(w, lh)}
	}, "")
	row(&c.privBtn, c.private, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
		vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 15, theme.Regular, cl.Fg, ccPrivate)
		})
		return layout.Dimensions{Size: image.Pt(w, lh)}
	}, ccPrivateSub)
	y += dp(gtx, 8)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// ---- Add people (the channel invite modal, isFromCreate) ----

// AddPeopleModal is "Add people to #channel" after a create.
type AddPeopleModal struct {
	h       Host
	ch      core.Conv
	modal   *Modal
	ed      widget.Editor
	focused bool
	query   string
	gen     int
	results []core.User
	rowBtn  []widget.Clickable
	picked  []core.User
	chipBtn []widget.Clickable
	adding  bool
	skip    *ModalButton
	add     *ModalButton
}

// NewAddPeopleModal builds the invite step for a channel just created.
func NewAddPeopleModal(h Host, ch core.Conv) *AddPeopleModal {
	p := &AddPeopleModal{h: h, ch: ch}
	p.ed.SingleLine = true
	p.ed.Submit = true
	ic := "hash"
	if ch.Kind == "private" {
		ic = "lock"
	}
	p.skip = &ModalButton{Label: ccSkip, Kind: BtnOutline}
	p.add = &ModalButton{Label: ccAdd, Kind: BtnPrimary, Enabled: func() bool { return !p.adding }, Do: func() bool { p.Add(); return false }}
	p.modal = NewModal(h, ModalOpts{Title: ccInviteTitle, TitleIcon: ic, TitleAfter: ch.Name, Body: p.layout,
		Buttons: []*ModalButton{p.skip}, FocusStops: func() []event.Tag { return []event.Tag{&p.ed} }})
	return p
}

// Layout and Dismissed make it an Overlay.
func (p *AddPeopleModal) Layout(gtx layout.Context) layout.Dimensions { return p.modal.Layout(gtx) }
func (p *AddPeopleModal) Dismissed() bool                             { return p.modal.Dismissed() }

// Dismiss closes it ("Skip for now").
func (p *AddPeopleModal) Dismiss() { p.modal.Dismiss() }

// Channel is the channel people are added to.
func (p *AddPeopleModal) Channel() core.Conv { return p.ch }

// Buttons are the footer's labels.
func (p *AddPeopleModal) Buttons() []string {
	var out []string
	for _, b := range p.modal.o.Buttons {
		out = append(out, b.Label)
	}
	return out
}

// Pick adds u to the people to add.
func (p *AddPeopleModal) Pick(u core.User) {
	for _, q := range p.picked {
		if q.ID == u.ID {
			return
		}
	}
	p.picked = append(p.picked, u)
	p.ed.SetText("")
	p.query, p.results = "", nil
	p.sync()
}

// Picked are the people chosen so far.
func (p *AddPeopleModal) Picked() []core.User { return p.picked }

func (p *AddPeopleModal) unpick(i int) {
	p.picked = append(p.picked[:i:i], p.picked[i+1:]...)
	p.sync()
}

func (p *AddPeopleModal) sync() {
	if len(p.picked) > 0 {
		p.modal.o.Buttons = []*ModalButton{p.add}
	} else {
		p.modal.o.Buttons = []*ModalButton{p.skip}
	}
}

// Add invites the picked people (conversations.invite) and closes.
func (p *AddPeopleModal) Add() {
	if p.adding || len(p.picked) == 0 {
		return
	}
	p.adding = true
	ids := make([]string, len(p.picked))
	for i, u := range p.picked {
		ids[i] = u.ID
	}
	run(p.h, func(ctx context.Context) (struct{}, error) {
		a := p.h.Core()
		if a == nil {
			return struct{}{}, context.Canceled
		}
		return struct{}{}, a.Invite(ctx, p.ch.ID, ids...)
	}, func(_ struct{}, err error) {
		p.adding = false
		if err != nil {
			p.h.Toast("Couldn’t add people: " + errText(err))
			return
		}
		p.modal.Dismiss()
	})
}

func (p *AddPeopleModal) search(q string) {
	p.query = q
	p.gen++
	gen := p.gen
	if strings.TrimSpace(q) == "" {
		p.results = nil
		return
	}
	run(p.h, func(ctx context.Context) ([]core.User, error) {
		a := p.h.Core()
		if a == nil {
			return nil, context.Canceled
		}
		return a.SearchUsers(ctx, q, 20)
	}, func(us []core.User, err error) {
		if gen != p.gen || err != nil {
			return
		}
		self := ""
		if a := p.h.Core(); a != nil {
			self = a.C.Creds.UserID
		}
		out := us[:0:0]
		for _, u := range us {
			if u.ID == self || u.IsBot {
				continue
			}
			out = append(out, u)
			if len(out) == 6 {
				break
			}
		}
		p.results = out
	})
}

func (p *AddPeopleModal) events(gtx layout.Context) {
	if !p.focused {
		gtx.Execute(key.FocusCmd{Tag: &p.ed})
		p.focused = true
	}
	for {
		e, ok := p.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			if t := p.ed.Text(); t != p.query {
				p.search(t)
			}
		case widget.SubmitEvent:
			if len(p.results) > 0 {
				p.Pick(p.results[0])
			} else if len(p.picked) > 0 {
				p.Add()
			}
		}
	}
	for i := range p.rowBtn {
		if i < len(p.results) && p.rowBtn[i].Clicked(gtx) {
			p.Pick(p.results[i])
			break
		}
	}
	for i := range p.chipBtn {
		if i < len(p.picked) && p.chipBtn[i].Clicked(gtx) {
			p.unpick(i)
			break
		}
	}
}

func (p *AddPeopleModal) layout(gtx layout.Context) layout.Dimensions {
	p.events(gtx)
	th := p.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	// picked people: chips (name + ×), wrapping
	if len(p.picked) > 0 {
		for len(p.chipBtn) < len(p.picked) {
			p.chipBtn = append(p.chipBtn, widget.Clickable{})
		}
		x, rowH := 0, dp(gtx, 28)
		for i, u := range p.picked {
			name := userName(u)
			cw := dp(gtx, 4) + dp(gtx, 20) + dp(gtx, 6) + textW(gtx, th, 15, theme.Bold, name) + dp(gtx, 6) + dp(gtx, 16) + dp(gtx, 6)
			if x > 0 && x+cw > w {
				x, y = 0, y+rowH+dp(gtx, 6)
			}
			at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				return clickBox(gtx, &p.chipBtn[i], func(gtx layout.Context, sz image.Point, hovered bool) {
					bg := theme.WithAlpha(cl.Link, 0x1a)
					if hovered {
						bg = theme.WithAlpha(cl.Link, 0x2a)
					}
					rrect(gtx, image.Rectangle{Max: sz}, dp(gtx, 4), bg)
				}, func(gtx layout.Context) layout.Dimensions {
					at(gtx, image.Pt(dp(gtx, 4), dp(gtx, 4)), func(gtx layout.Context) layout.Dimensions {
						return avatar(gtx, p.h, u.Avatar, 20)
					})
					tx := dp(gtx, 30)
					at(gtx, image.Pt(tx, 0), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Max: image.Pt(cw, rowH)}
						return vcenter(gtx, rowH, func(gtx layout.Context) layout.Dimensions {
							return text(gtx, th, 15, theme.Bold, cl.Link, name)
						})
					})
					at(gtx, image.Pt(cw-dp(gtx, 22), (rowH-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
						return icon(gtx, "close", 16, cl.Link)
					})
					return layout.Dimensions{Size: image.Pt(cw, rowH)}
				})
			})
			x += cw + dp(gtx, 6)
		}
		y += rowH + dp(gtx, 12)
	}
	ih := dp(gtx, 44)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ih))
		return field(gtx, th, &p.ed, ccInviteSearch, "", 44, 18)
	})
	y += ih
	if len(p.results) > 0 {
		for len(p.rowBtn) < len(p.results) {
			p.rowBtn = append(p.rowBtn, widget.Clickable{})
		}
		y += dp(gtx, 4)
		rh := dp(gtx, 36)
		box := image.Rect(0, y, w, y+rh*len(p.results)+dp(gtx, 12))
		card(gtx, cl, box, dp(gtx, 8))
		ry := y + dp(gtx, 6)
		for i, u := range p.results {
			at(gtx, image.Pt(0, ry), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(w, rh))
				return clickBox(gtx, &p.rowBtn[i], func(gtx layout.Context, sz image.Point, hovered bool) {
					if hovered || i == 0 {
						fill(gtx, image.Rectangle{Max: sz}, cl.Highlight)
					}
				}, func(gtx layout.Context) layout.Dimensions {
					fg, fg2 := cl.Fg, cl.Fg3
					if p.rowBtn[i].Hovered() || i == 0 {
						fg, fg2 = cl.HighText, cl.HighText
					}
					at(gtx, image.Pt(dp(gtx, 16), (rh-dp(gtx, 20))/2), func(gtx layout.Context) layout.Dimensions {
						return avatar(gtx, p.h, u.Avatar, 20)
					})
					name := userName(u)
					nx := dp(gtx, 44)
					nw := textW(gtx, th, 15, theme.Bold, name)
					at(gtx, image.Pt(nx, 0), func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints = layout.Constraints{Max: image.Pt(w-nx, rh)}
						return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
							return text(gtx, th, 15, theme.Bold, fg, name)
						})
					})
					if u.RealName != "" && u.RealName != name {
						rx := nx + nw + dp(gtx, 8)
						at(gtx, image.Pt(rx, 0), func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-rx-dp(gtx, 16)), rh)}
							return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions {
								return text(gtx, th, 15, theme.Regular, fg2, u.RealName)
							})
						})
					}
					return layout.Dimensions{Size: image.Pt(w, rh)}
				})
			})
			ry += rh
		}
		y = box.Max.Y
	}
	y += dp(gtx, 20)
	return layout.Dimensions{Size: image.Pt(w, y)}
}
