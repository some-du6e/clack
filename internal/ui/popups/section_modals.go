package popups

import (
	"context"
	"image"
	"sort"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"github.com/some-du6e/yoloslackgoclient/internal/core"
	"github.com/some-du6e/yoloslackgoclient/internal/ui/theme"
)

// The sidebar section modals, as Slack's web client draws them (build
// a84662d2d135, 2026-11-08):
//
//   - Rename (boot-render BaseChannelSectionModalInput, "current_status"
//     strings): "Rename this section", a name input with an emoji picker at
//     its left, "Save" (disabled until the name or emoji changed and the
//     name is valid). Errors under the input: "You already have a section
//     with this name.", "Unfortunately, that name isn’t valid. Try
//     another?"; empty and over-80 names only disable Save.
//   - Create (boot-shared +jJb, create_section_modal strings): step 1
//     "Create a section" with "Section name" (emoji picker + input) and
//     "Sort conversations by" (A-Z / Recency / Priority, the user's global
//     sort tagged "Your default"), footer "Step 1 of 2" and "Create and Add
//     Channels". Step 2 "Add conversations": "Filter by name…", All /
//     Channels / Direct Messages, "Selected Only", "Select all", the
//     conversations with checkboxes, footer "Step 2 of 2", "Skip" and "Add
//     Conversations". "Manage conversations" opens step 2 alone for an
//     existing section with "Update Section". Moving conversations that sit
//     in another custom section asks first (ChannelSectionModalSpeedBump):
//     "Some conversations you selected are already organized in other
//     sections. Are you sure you want to move them here?" with "Edit
//     Selections" and "Move Conversations".
//   - Delete (channel_browser_coachmark BaseSectionHeadingConfirmationModal):
//     "Delete this section?", "Anything you added to <section> will move
//     back to their default sections:" Channels / Direct messages, "And
//     don’t worry — you won’t be removed from any channels when you delete
//     this section.", Cancel / Delete (danger).
//   - Start a Huddle (boot-shared Q9fi in start mode): "Start a Huddle",
//     "Find a person or channel to huddle with", a search, the
//     conversations, Cancel / "Start a Huddle".

// SectionConv is a conversation the section and huddle pickers offer.
type SectionConv struct {
	ID, Name string
	Icon     string // hash | lock | dms
	DM       bool
}

func emojiButton(gtx layout.Context, h Host, cl *Colors, btn *widget.Clickable, emoji string, sz int) layout.Dimensions {
	return clickBox(gtx, btn, func(gtx layout.Context, s image.Point, hovered bool) {
		if hovered {
			rrect(gtx, image.Rectangle{Max: s}, dp(gtx, 6), cl.BgHover)
		}
	}, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(sz, sz))
		in := (sz - dp(gtx, 20)) / 2
		at(gtx, image.Pt(in, in), func(gtx layout.Context) layout.Dimensions {
			if emoji != "" {
				return emojiImage(gtx, h, strings.Trim(emoji, ":"), 20)
			}
			return icon(gtx, "emoji", 20, cl.Fg3)
		})
		return layout.Dimensions{Size: image.Pt(sz, sz)}
	})
}

// sectionNameField is the name input with the emoji picker inside its left
// edge and the validation alert under it.
func sectionNameField(gtx layout.Context, h Host, th *theme.Theme, cl *Colors, ed *widget.Editor, btn *widget.Clickable, emoji, alert string, w int) int {
	ih := dp(gtx, 44)
	at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ih))
		d := field(gtx, th, ed, "", "", 44, 18)
		at(gtx, image.Pt(dp(gtx, 4), dp(gtx, 4)), func(gtx layout.Context) layout.Dimensions {
			return emojiButton(gtx, h, cl, btn, emoji, ih-dp(gtx, 8))
		})
		if alert != "" {
			stroke(gtx, image.Rect(0, 0, w, ih), dp(gtx, 8), maxInt(1, dp(gtx, 1)), ccErrColor(th, cl))
		}
		return d
	})
	y := ih
	if alert != "" {
		y += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			ec := ccErrColor(th, cl)
			at(gtx, image.Pt(0, dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions { return icon(gtx, "warning", 14, ec) })
			return at(gtx, image.Pt(dp(gtx, 20), 0), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 20), 1<<20)}
				return para(gtx, th, 13, 18, theme.Regular, ec, alert, 0)
			})
		})
		y += d.Size.Y
	}
	return y
}

// modalPoint turns a point in m's body into window coordinates (the emoji
// picker's anchor), like the status modal's.
func modalPoint(gtx layout.Context, m *Modal, p image.Point) image.Point {
	return dtModalOrigin(gtx, m).Add(image.Pt(p.X-dp(gtx, 28), p.Y))
}

// ---- Rename ----

// RenameSectionModal is "Rename this section".
type RenameSectionModal struct {
	h                Host
	id, name0, emoji string
	emoji0           string
	modal            *Modal
	ed               widget.Editor
	emojiBtn         widget.Clickable
	focused          bool
	pending          bool
	serverErr        string
	save             *ModalButton
	// OnRenamed runs after Slack took the new name (the shell's "Renamed
	// to …" toast with Undo).
	OnRenamed func(oldName, oldEmoji, newName string)
}

// NewRenameSectionModal opens it for section id, prefilled.
func NewRenameSectionModal(h Host, id, name, emoji string) *RenameSectionModal {
	m := &RenameSectionModal{h: h, id: id, name0: name, emoji: emoji, emoji0: emoji}
	m.ed.SingleLine, m.ed.Submit = true, true
	m.ed.SetText(name)
	m.ed.SetCaret(len([]rune(name)), len([]rune(name)))
	m.save = &ModalButton{Label: "Save", Kind: BtnPrimary, Enabled: m.CanSave, Do: func() bool { m.Save(); return false }}
	m.modal = NewModal(h, ModalOpts{Title: "Rename this section", Body: m.layout, Buttons: []*ModalButton{m.save}})
	return m
}

func (m *RenameSectionModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *RenameSectionModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *RenameSectionModal) Dismiss()                                    { m.modal.Dismiss() }

// SetName types a name; SetEmoji picks an emoji (":tada:" or "tada").
func (m *RenameSectionModal) SetName(s string) { m.ed.SetText(s); m.serverErr = "" }
func (m *RenameSectionModal) SetEmoji(e string) {
	if e != "" && !strings.HasPrefix(e, ":") {
		e = ":" + e + ":"
	}
	m.emoji = e
}

func (m *RenameSectionModal) code() string {
	if m.serverErr != "" {
		return m.serverErr
	}
	a := m.h.Core()
	if a == nil {
		return ""
	}
	return a.SectionNameError(m.ed.Text(), m.id)
}

// Error is the alert under the input ("" for none).
func (m *RenameSectionModal) Error() string { return core.SectionNameErrorText(m.code()) }

// CanSave mirrors Save's enabled state.
func (m *RenameSectionModal) CanSave() bool {
	changed := m.ed.Text() != m.name0 || m.emoji != m.emoji0
	return changed && m.code() == "" && !m.pending
}

// Save sends users.channelSections.set.
func (m *RenameSectionModal) Save() {
	if !m.CanSave() {
		return
	}
	m.pending = true
	name, emoji := m.ed.Text(), m.emoji
	run(m.h, func(ctx context.Context) (struct{}, error) {
		a := m.h.Core()
		if a == nil {
			return struct{}{}, context.Canceled
		}
		return struct{}{}, a.RenameSection(ctx, m.id, name, emoji)
	}, func(_ struct{}, err error) {
		m.pending = false
		if err != nil {
			switch {
			case strings.Contains(err.Error(), "already_exists"):
				m.serverErr = core.SectionNameTaken
			case strings.Contains(err.Error(), "name_invalid"):
				m.serverErr = core.SectionNameNotValid
			case strings.Contains(err.Error(), "emoji_invalid"):
				m.serverErr = "emoji_invalid"
			default:
				m.serverErr = "unknown"
			}
			return
		}
		m.modal.Dismiss()
		if m.OnRenamed != nil {
			m.OnRenamed(m.name0, m.emoji0, name)
		}
	})
}

func (m *RenameSectionModal) layout(gtx layout.Context) layout.Dimensions {
	if !m.focused {
		gtx.Execute(key.FocusCmd{Tag: &m.ed})
		m.focused = true
	}
	for {
		e, ok := m.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			m.serverErr = ""
		case widget.SubmitEvent:
			m.Save()
		}
	}
	if m.emojiBtn.Clicked(gtx) {
		m.h.OpenEmojiPicker(modalPoint(gtx, m.modal, image.Pt(dp(gtx, 4), dp(gtx, 48))), func(name string) { post(m.h, func() { m.SetEmoji(name) }) })
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := sectionNameField(gtx, m.h, th, cl, &m.ed, &m.emojiBtn, m.emoji, m.Error(), w)
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 20))}
}

// ---- Create / Manage conversations ----

// CreateSectionOpts open the create modal (or, with Edit, "Manage
// conversations").
type CreateSectionOpts struct {
	Next       string // the section whose menu opened it: the new one goes before it
	Edit       string // an existing section: step 2 only, "Update Section"
	EditName   string
	Candidates []SectionConv
	Selected   []string // Edit: the section's conversations
	// DefaultSort is the user's global sort (channel_sort): default |
	// recent | priority, tagged "Your default".
	DefaultSort string
	// Done runs after the section exists and its conversations moved
	// (id, name; added is how many conversations went in).
	Done func(id, name string, added int)
}

// CreateSectionModal is Slack's two-step "Create a section".
type CreateSectionModal struct {
	h     Host
	o     CreateSectionOpts
	modal *Modal

	step     int // 0 name, 1 conversations
	ed       widget.Editor
	emoji    string
	emojiBtn widget.Clickable
	focused  bool
	sortVal  string
	sortBtn  [3]widget.Clickable
	pending  bool
	err      string
	id, name string

	filter   widget.Editor
	tab      int // 0 all, 1 channels, 2 DMs
	tabBtn   [3]widget.Clickable
	selOnly  bool
	selBtn   widget.Clickable
	allBtn   widget.Clickable
	picked   map[string]bool
	initial  map[string]bool
	rowBtn   map[string]*widget.Clickable
	list     layout.List
	bump     bool // the speed bump is showing
	create   *ModalButton
	skip     *ModalButton
	add      *ModalButton
	update   *ModalButton
	cancel   *ModalButton
	editSel  *ModalButton
	moveConv *ModalButton
}

var csSortKeys = [3]string{"default", "recent", "priority"}

// NewCreateSectionModal builds it.
func NewCreateSectionModal(h Host, o CreateSectionOpts) *CreateSectionModal {
	m := &CreateSectionModal{h: h, o: o, picked: map[string]bool{}, initial: map[string]bool{}, rowBtn: map[string]*widget.Clickable{}}
	m.ed.SingleLine, m.ed.Submit = true, true
	m.filter.SingleLine = true
	m.list.Axis = layout.Vertical
	m.sortVal = o.DefaultSort
	if m.sortVal == "" {
		m.sortVal = "default"
	}
	for _, id := range o.Selected {
		m.picked[id], m.initial[id] = true, true
	}
	m.create = &ModalButton{Label: "Create and Add Channels", Kind: BtnPrimary, Enabled: m.CanCreate, Do: func() bool { m.Create(); return false }}
	m.skip = &ModalButton{Label: "Skip", Kind: BtnOutline, Do: func() bool { m.finish(0); return true }}
	m.add = &ModalButton{Label: "Add Conversations", Kind: BtnPrimary, Enabled: func() bool { return !m.pending }, Do: func() bool { m.Submit(); return false }}
	m.update = &ModalButton{Label: "Update Section", Kind: BtnPrimary, Enabled: func() bool { return !m.pending }, Do: func() bool { m.Submit(); return false }}
	m.cancel = &ModalButton{Label: "Cancel", Kind: BtnOutline}
	m.editSel = &ModalButton{Label: "Edit Selections", Kind: BtnOutline, Do: func() bool { m.bump = false; m.sync(); return false }}
	m.moveConv = &ModalButton{Label: "Move Conversations", Kind: BtnPrimary, Enabled: func() bool { return !m.pending }, Do: func() bool { m.move(); return false }}
	m.modal = NewModal(h, ModalOpts{Title: "Create a section", Body: m.layout})
	if o.Edit != "" {
		m.step, m.id, m.name = 1, o.Edit, o.EditName
	}
	m.sync()
	return m
}

func (m *CreateSectionModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *CreateSectionModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *CreateSectionModal) Dismiss()                                    { m.modal.Dismiss() }

// Step is "name" or "conversations"; Bumped reports the speed bump.
func (m *CreateSectionModal) Step() string {
	if m.step == 1 {
		return "conversations"
	}
	return "name"
}
func (m *CreateSectionModal) Bumped() bool { return m.bump }

// Title is the modal's current title.
func (m *CreateSectionModal) Title() string { return m.modal.o.Title }

// SetName, SetEmoji and SetSort fill step 1 as if typed and clicked.
func (m *CreateSectionModal) SetName(s string) { m.ed.SetText(s); m.err = "" }
func (m *CreateSectionModal) SetEmoji(e string) {
	if e != "" && !strings.HasPrefix(e, ":") {
		e = ":" + e + ":"
	}
	m.emoji = e
}
func (m *CreateSectionModal) SetSort(s string) { m.sortVal = s }

// Toggle picks or unpicks a conversation on step 2.
func (m *CreateSectionModal) Toggle(id string) { m.picked[id] = !m.picked[id] }

func (m *CreateSectionModal) code() string {
	if m.err != "" {
		return m.err
	}
	if a := m.h.Core(); a != nil {
		return a.SectionNameError(m.ed.Text(), "")
	}
	return ""
}

// Error is step 1's alert.
func (m *CreateSectionModal) Error() string { return core.SectionNameErrorText(m.code()) }

// CanCreate mirrors "Create and Add Channels".
func (m *CreateSectionModal) CanCreate() bool { return m.step == 0 && !m.pending && m.code() == "" }

// Create makes the section (users.channelSections.create, then the sort
// pref when it isn't the user's default) and moves to step 2.
func (m *CreateSectionModal) Create() {
	if !m.CanCreate() {
		return
	}
	m.pending = true
	name, emoji, next, srt, def := m.ed.Text(), m.emoji, m.o.Next, m.sortVal, m.o.DefaultSort
	run(m.h, func(ctx context.Context) (string, error) {
		a := m.h.Core()
		if a == nil {
			return "", context.Canceled
		}
		id, err := a.CreateSection(ctx, name, emoji, next)
		if err == nil && srt != def && !(def == "" && srt == "default") {
			_ = a.SetSectionPref(ctx, id, "sort", srt)
		}
		return id, err
	}, func(id string, err error) {
		m.pending = false
		if err != nil {
			if strings.Contains(err.Error(), "already_exists") {
				m.err = core.SectionNameTaken
			} else if strings.Contains(err.Error(), "invalid_name") || strings.Contains(err.Error(), "name_invalid") {
				m.err = core.SectionNameNotValid
			} else {
				m.err = "unknown"
			}
			return
		}
		m.id, m.name, m.step = id, name, 1
		m.sync()
	})
}

func (m *CreateSectionModal) diff() (ins, rem []string) {
	for _, c := range m.o.Candidates {
		switch {
		case m.picked[c.ID] && !m.initial[c.ID]:
			ins = append(ins, c.ID)
		case !m.picked[c.ID] && m.initial[c.ID]:
			rem = append(rem, c.ID)
		}
	}
	return
}

// Submit moves the picked conversations in (asking first when some sit in
// another custom section).
func (m *CreateSectionModal) Submit() {
	if m.pending || m.step != 1 {
		return
	}
	ins, _ := m.diff()
	if a := m.h.Core(); a != nil && !m.bump {
		for _, id := range ins {
			if a.OtherSectionOf(id, m.id) != "" {
				m.bump = true
				m.sync()
				return
			}
		}
	}
	m.move()
}

func (m *CreateSectionModal) move() {
	ins, rem := m.diff()
	if len(ins)+len(rem) == 0 {
		m.modal.Dismiss()
		m.finish(0)
		return
	}
	m.pending = true
	id := m.id
	run(m.h, func(ctx context.Context) (struct{}, error) {
		a := m.h.Core()
		if a == nil {
			return struct{}{}, context.Canceled
		}
		return struct{}{}, a.MoveToSection(ctx, id, ins, rem)
	}, func(_ struct{}, err error) {
		m.pending = false
		if err != nil {
			m.h.Toast("Something went wrong. Reloading Slack might clear things up.")
			return
		}
		m.modal.Dismiss()
		m.finish(len(ins))
	})
}

func (m *CreateSectionModal) finish(added int) {
	if m.o.Done != nil && m.id != "" {
		m.o.Done(m.id, m.name, added)
	}
}

func (m *CreateSectionModal) sync() {
	o := &m.modal.o
	switch {
	case m.step == 0:
		o.Title, o.FooterNote, o.Buttons = "Create a section", "Step 1 of 2", []*ModalButton{m.create}
	case m.bump:
		o.Title, o.FooterNote, o.Buttons = m.modal.o.Title, "", []*ModalButton{m.editSel, m.moveConv}
	case m.o.Edit != "":
		o.Title, o.FooterNote, o.Buttons = "Manage conversations", "", []*ModalButton{m.cancel, m.update}
	default:
		o.Title, o.FooterNote, o.Buttons = "Add conversations", "Step 2 of 2", []*ModalButton{m.skip, m.add}
	}
}

// Visible is step 2's rows after the tab, filter and Selected Only.
func (m *CreateSectionModal) Visible() []SectionConv {
	q := strings.ToLower(strings.TrimSpace(m.filter.Text()))
	var out []SectionConv
	for _, c := range m.o.Candidates {
		if m.tab == 1 && c.DM || m.tab == 2 && !c.DM {
			continue
		}
		if m.selOnly && !m.picked[c.ID] {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(c.Name), q) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (m *CreateSectionModal) layout(gtx layout.Context) layout.Dimensions {
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	if m.bump {
		d := para(loose(gtx, image.Pt(w, 1<<20)), th, 15, 22, theme.Regular, cl.Fg,
			"Some conversations you selected are already organized in other sections. Are you sure you want to move them here?", 0)
		return layout.Dimensions{Size: image.Pt(w, d.Size.Y+dp(gtx, 8))}
	}
	if m.step == 1 {
		return m.layoutConvs(gtx, th, cl, w)
	}
	if !m.focused {
		gtx.Execute(key.FocusCmd{Tag: &m.ed})
		m.focused = true
	}
	for {
		e, ok := m.ed.Update(gtx)
		if !ok {
			break
		}
		switch e.(type) {
		case widget.ChangeEvent:
			m.err = ""
		case widget.SubmitEvent:
			m.Create()
		}
	}
	if m.emojiBtn.Clicked(gtx) {
		m.h.OpenEmojiPicker(modalPoint(gtx, m.modal, image.Pt(dp(gtx, 4), dp(gtx, 30+48))), func(name string) { post(m.h, func() { m.SetEmoji(name) }) })
	}
	for i := range m.sortBtn {
		if m.sortBtn[i].Clicked(gtx) {
			m.sortVal = csSortKeys[i]
		}
	}
	lh := dp(gtx, 22)
	y := 0
	label := func(s string) {
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Bold, cl.Fg, s) })
		})
		y += lh + dp(gtx, 8)
	}
	label("Section name")
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(w, sectionNameField(gtx, m.h, th, cl, &m.ed, &m.emojiBtn, m.emoji, m.Error(), w))}
	})
	y += d.Size.Y + dp(gtx, 20)
	label("Sort conversations by")
	def := m.o.DefaultSort
	if def == "" {
		def = "default"
	}
	for i, k := range csSortKeys {
		lbl := map[string]string{"default": "A-Z", "recent": "Recency", "priority": "Priority"}[k]
		on := m.sortVal == k
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &m.sortBtn[i], nil, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(0, dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions { return radio(gtx, cl, on) })
				x := dp(gtx, 28)
				tw := at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w-x, lh)}
					return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, cl.Fg, lbl) })
				})
				if k == def { // Tag style="gray"
					tx := x + tw.Size.X + dp(gtx, 8)
					tag := "Your default"
					tgw := textW(gtx, th, 12, theme.Bold, tag) + dp(gtx, 8)
					rrect(gtx, image.Rect(tx, dp(gtx, 2), tx+tgw, lh-dp(gtx, 2)), dp(gtx, 4), cl.BgHover)
					at(gtx, image.Pt(tx+dp(gtx, 4), 0), func(gtx layout.Context) layout.Dimensions {
						return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 12, theme.Bold, cl.Fg2, tag) })
					})
				}
				return layout.Dimensions{Size: image.Pt(w, lh)}
			})
		})
		y += d.Size.Y + dp(gtx, 8)
	}
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 12))}
}

func (m *CreateSectionModal) layoutConvs(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	for i := range m.tabBtn {
		if m.tabBtn[i].Clicked(gtx) {
			m.tab = i
		}
	}
	if m.selBtn.Clicked(gtx) {
		m.selOnly = !m.selOnly
	}
	vis := m.Visible()
	if m.allBtn.Clicked(gtx) {
		all := true
		for _, c := range vis {
			all = all && m.picked[c.ID]
		}
		for _, c := range vis {
			m.picked[c.ID] = !all
		}
	}
	for id, b := range m.rowBtn {
		if b.Clicked(gtx) {
			m.Toggle(id)
		}
	}
	for {
		if _, ok := m.filter.Update(gtx); !ok {
			break
		}
	}
	y := 0
	at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, dp(gtx, 36)))
		return field(gtx, th, &m.filter, "Filter by name…", "search", 36, 15)
	})
	y += dp(gtx, 36) + dp(gtx, 12)
	// tabs, then Selected Only / Select all at the right
	x := 0
	th22 := dp(gtx, 28)
	for i, s := range []string{"All", "Channels", "Direct Messages"} {
		on := m.tab == i
		tw := textW(gtx, th, 13, theme.Bold, s) + dp(gtx, 20)
		at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, &m.tabBtn[i], nil, func(gtx layout.Context) layout.Dimensions {
				bg := cl.Bg
				if on {
					bg = cl.BgHover
				}
				rrect(gtx, image.Rect(0, 0, tw, th22), th22/2, bg)
				stroke(gtx, image.Rect(0, 0, tw, th22), th22/2, maxInt(1, dp(gtx, 1)), cl.BtnBorder)
				at(gtx, image.Pt(dp(gtx, 10), 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, th22, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 13, theme.Bold, cl.Fg, s) })
				})
				return layout.Dimensions{Size: image.Pt(tw, th22)}
			})
		})
		x += tw + dp(gtx, 8)
	}
	right := w
	for _, it := range []struct {
		btn *widget.Clickable
		s   string
		on  bool
	}{{&m.allBtn, "Select all", false}, {&m.selBtn, "Selected Only", m.selOnly}} {
		tw := textW(gtx, th, 13, theme.Regular, it.s) + dp(gtx, 26)
		right -= tw
		on := it.on
		s := it.s
		at(gtx, image.Pt(right, y), func(gtx layout.Context) layout.Dimensions {
			return clickBox(gtx, it.btn, nil, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(0, (th22-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions { return Checkbox(gtx, th, on, false, false) })
				at(gtx, image.Pt(dp(gtx, 24), 0), func(gtx layout.Context) layout.Dimensions {
					return vcenter(gtx, th22, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 13, theme.Regular, cl.Fg, s) })
				})
				return layout.Dimensions{Size: image.Pt(tw, th22)}
			})
		})
		right -= dp(gtx, 12)
	}
	y += th22 + dp(gtx, 12)
	rh := dp(gtx, 36)
	listH := rh * 8
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, listH))
		if len(vis) == 0 {
			return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, "If you can’t find a conversation, try searching for it by name.", 0)
		}
		return m.list.Layout(gtx, len(vis), func(gtx layout.Context, i int) layout.Dimensions {
			c := vis[i]
			b := m.rowBtn[c.ID]
			if b == nil {
				b = new(widget.Clickable)
				m.rowBtn[c.ID] = b
			}
			on := m.picked[c.ID]
			return clickBox(gtx, b, func(gtx layout.Context, s image.Point, hovered bool) {
				if hovered {
					rrect(gtx, image.Rectangle{Max: s}, dp(gtx, 6), cl.BgHover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				at(gtx, image.Pt(dp(gtx, 8), (rh-dp(gtx, 18))/2), func(gtx layout.Context) layout.Dimensions { return Checkbox(gtx, th, on, false, false) })
				at(gtx, image.Pt(dp(gtx, 36), (rh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, c.Icon, 16, cl.Fg2) })
				at(gtx, image.Pt(dp(gtx, 60), 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 64), rh)}
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, cl.Fg, c.Name) })
				})
				return layout.Dimensions{Size: image.Pt(w, rh)}
			})
		})
	})
	y += listH
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 12))}
}

// ---- Delete ----

// NewDeleteSectionModal is "Delete this section?"; onDelete runs on Delete.
func NewDeleteSectionModal(h Host, name string, onDelete func()) *Modal {
	body := func(gtx layout.Context) layout.Dimensions {
		th := h.Theme()
		cl := colorsOf(th)
		w := gtx.Constraints.Max.X
		y := 0
		line := func(s string, wt float32, x int) {
			d := at(gtx, image.Pt(x, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w-x, 1<<20)}
				return para(gtx, th, 15, 22, theme.Regular, cl.Fg, s, 0)
			})
			y += d.Size.Y + dp(gtx, wt)
		}
		line("Anything you added to "+name+" will move back to their default sections:", 6, 0)
		line("•  Channels", 2, dp(gtx, 12))
		line("•  Direct messages", 12, dp(gtx, 12))
		line("And don’t worry — you won’t be removed from any channels when you delete this section.", 8, 0)
		return layout.Dimensions{Size: image.Pt(w, y)}
	}
	return NewModal(h, ModalOpts{Title: "Delete this section?", Body: body, Buttons: []*ModalButton{
		{Label: "Cancel", Kind: BtnOutline},
		{Label: "Delete", Kind: BtnDanger, Do: func() bool { onDelete(); return true }},
	}})
}

// ---- Start a Huddle ----

// HuddleStartModal is the Create menu's "Start a Huddle" picker.
type HuddleStartModal struct {
	h      Host
	modal  *Modal
	ed     widget.Editor
	cands  []SectionConv
	pick   string
	rowBtn map[string]*widget.Clickable
	list   layout.List
	focus  bool
	start  func(ch string)
}

// NewHuddleStartModal offers cands; start runs with the picked one.
func NewHuddleStartModal(h Host, cands []SectionConv, start func(ch string)) *HuddleStartModal {
	m := &HuddleStartModal{h: h, cands: cands, start: start, rowBtn: map[string]*widget.Clickable{}}
	m.ed.SingleLine = true
	m.list.Axis = layout.Vertical
	sort.SliceStable(m.cands, func(i, j int) bool { return m.cands[i].DM && !m.cands[j].DM })
	m.modal = NewModal(h, ModalOpts{Title: "Start a Huddle", Body: m.layout, Buttons: []*ModalButton{
		{Label: "Cancel", Kind: BtnOutline},
		{Label: "Start a Huddle", Kind: BtnPrimary, Enabled: func() bool { return m.pick != "" }, Do: func() bool { m.Start(); return true }},
	}})
	return m
}

func (m *HuddleStartModal) Layout(gtx layout.Context) layout.Dimensions { return m.modal.Layout(gtx) }
func (m *HuddleStartModal) Dismissed() bool                             { return m.modal.Dismissed() }
func (m *HuddleStartModal) Dismiss()                                    { m.modal.Dismiss() }

// Pick selects a conversation; Start starts the huddle there.
func (m *HuddleStartModal) Pick(id string) { m.pick = id }
func (m *HuddleStartModal) Start() {
	if m.pick != "" && m.start != nil {
		m.start(m.pick)
	}
	m.modal.Dismiss()
}

// Matches is the list under the search.
func (m *HuddleStartModal) Matches() []SectionConv {
	q := strings.ToLower(strings.TrimSpace(m.ed.Text()))
	var out []SectionConv
	for _, c := range m.cands {
		if q == "" || strings.Contains(strings.ToLower(c.Name), q) {
			out = append(out, c)
		}
	}
	return out
}

func (m *HuddleStartModal) layout(gtx layout.Context) layout.Dimensions {
	if !m.focus {
		gtx.Execute(key.FocusCmd{Tag: &m.ed})
		m.focus = true
	}
	for {
		if _, ok := m.ed.Update(gtx); !ok {
			break
		}
	}
	for id, b := range m.rowBtn {
		if b.Clicked(gtx) {
			m.pick = id
		}
	}
	th := m.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	d := at(gtx, image.Point{}, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
		return para(gtx, th, 15, 22, theme.Regular, cl.Fg2, "Find a person or channel to huddle with", 0)
	})
	y += d.Size.Y + dp(gtx, 12)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, dp(gtx, 36)))
		return field(gtx, th, &m.ed, "Search by name", "search", 36, 15)
	})
	y += dp(gtx, 36) + dp(gtx, 8)
	ms := m.Matches()
	rh := dp(gtx, 36)
	listH := rh * 7
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, listH))
		return m.list.Layout(gtx, len(ms), func(gtx layout.Context, i int) layout.Dimensions {
			c := ms[i]
			b := m.rowBtn[c.ID]
			if b == nil {
				b = new(widget.Clickable)
				m.rowBtn[c.ID] = b
			}
			sel := m.pick == c.ID
			return clickBox(gtx, b, func(gtx layout.Context, s image.Point, hovered bool) {
				switch {
				case sel:
					rrect(gtx, image.Rectangle{Max: s}, dp(gtx, 6), cl.Highlight)
				case hovered:
					rrect(gtx, image.Rectangle{Max: s}, dp(gtx, 6), cl.BgHover)
				}
			}, func(gtx layout.Context) layout.Dimensions {
				fg := cl.Fg
				if sel {
					fg = rgb(0xffffff)
				}
				at(gtx, image.Pt(dp(gtx, 10), (rh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions { return icon(gtx, c.Icon, 16, fg) })
				at(gtx, image.Pt(dp(gtx, 34), 0), func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints = layout.Constraints{Max: image.Pt(w-dp(gtx, 40), rh)}
					return vcenter(gtx, rh, func(gtx layout.Context) layout.Dimensions { return text(gtx, th, 15, theme.Regular, fg, c.Name) })
				})
				return layout.Dimensions{Size: image.Pt(w, rh)}
			})
		})
	})
	y += listH
	return layout.Dimensions{Size: image.Pt(w, y+dp(gtx, 8))}
}
