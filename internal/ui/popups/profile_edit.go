package popups

// Editing your profile: Slack's EditProfileModal (boot-shared eNqz, CDN
// build d3de3a487b1c, 2026-11-12), opened by the openEditProfileModal
// thunk (HjJO, through the circular YUXH), and the section editor
// EditProfileSectionForm (boot-shared vIit). Writes go through
// core/profile_write.go.
//
// The modal swaps panes inside one dialog (ModalWithPanes, defaultPane
// formPane):
//
//   - formPane (EditProfileFormPane, the wide modal: max-width 700): the
//     title "Edit your profile" with a spinner while the header section
//     loads; two columns (.p-edit_profile__columns: the primary flex 1
//     with a 32px right margin, the secondary 192 wide) holding the first
//     three fields and "Profile photo" (the 192px photo, "Upload Photo",
//     and "Remove Photo" when the photo is your own upload); then the rest
//     of the fields full width. The fields are BEl1's list: Full name,
//     Display name (with the workspace's username_policy rules under it),
//     Title (locked until the header section says it's editable),
//     Pronouns when the team has display_pronouns, Name pronunciation
//     when the header section has the element, and Time zone (a select
//     led by "Please select an option"). Name recording is left out:
//     Clack can't record audio here. Typing validates the field (NYcX);
//     Save Changes is disabled while one has an error. Cancel closes;
//     closing any other way with unsaved changes shows the speed bump
//     (Discard changes? Keep Editing / Close & Discard).
//   - uploadPane (EditProfileUploadPane): "Upload Photo" opens the file
//     chooser; the photo uploads with a spinner ("Uploading your
//     photo…"), or a check fails: "Try a larger image", "Try a smaller
//     image", "Try another photo", "Eek" with Slack's emoji and text, and
//     "Try a Different Image". Cancel closes.
//   - cropPane (EditProfileCropPane): "Crop your photo", the image fitted
//     into 460px with a square selection (the largest that fits, centred:
//     defaultCropScale 1, at least 72px) that moves and resizes from its
//     corners, the message preview under it, Cancel (back to the form)
//     and Save (users.setPhoto, back to the form; an error alert on
//     failure).
//   - removePane (EditProfileRemovePane): a back arrow, "Remove profile
//     photo", the photo and "Are you sure…", Cancel and the danger "Yes,
//     Remove Photo" (users.deletePhoto).
//
// The section editor (EditProfileSectionModal): "Edit {sectionLabel}"
// (rimeto_atlas_user), the elements between two 1px lines: a text element
// as an input (a select when it has options), a date with the date
// picker, a link as its label over "Display Text" and "URL", a long text
// as a taller input. People, tags and locations stay as they are (Clack
// has no editor for them) and are sent back unchanged. Save Changes
// writes users.profile.setSections and refetches the profile.
//
// Strings are Slack's I18n namespaces: edit_profile, ui_validation,
// speed_bump_modal, rimeto_atlas_user and date_picker.

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	_ "image/gif"  // decoding the picked photo
	_ "image/jpeg" // decoding the picked photo
	_ "image/png"  // decoding the picked photo
	"math"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	xdraw "golang.org/x/image/draw"

	"github.com/some-du6e/clack/internal/core"
	"github.com/some-du6e/clack/internal/i18n"
	"github.com/some-du6e/clack/internal/ui/slackc"
	"github.com/some-du6e/clack/internal/ui/theme"
)

var (
	nsEditProfile  = i18n.NS("edit_profile")
	nsUIValidation = i18n.NS("ui_validation")
	nsSpeedBump    = i18n.NS("speed_bump_modal")
	nsAtlasUser    = i18n.NS("rimeto_atlas_user")
	nsDatePickerNS = i18n.NS("date_picker")
)

// PickFiles opens the platform file chooser (set by the ui root, like
// blockkit's); accept is an <input accept> list.
var PickFiles func(accept string, multiple bool) ([]core.UploadFile, error)

// EditProfileOpts are openEditProfileModal's arguments.
type EditProfileOpts struct {
	// Focus is focusedFieldName: the field focused on open ("" is the
	// first field).
	Focus string
	// Refetch runs after a save that changed the sections (the profile
	// pane reloads them).
	Refetch func()
}

// EditProfileModal is Slack's Edit profile dialog.
type EditProfileModal struct {
	h    Host
	o    EditProfileOpts
	pane string
	m    *Modal
	done bool
	form *epForm
	upl  *epUpload
	crop *epCrop
	rm   *epRemove
}

// OpenEditProfile opens the Edit profile modal (openEditProfileModal).
func OpenEditProfile(h Host, o EditProfileOpts) *EditProfileModal {
	e := NewEditProfileModal(h, o)
	h.ShowOverlay(e)
	return e
}

// NewEditProfileModal builds the modal on its form pane.
func NewEditProfileModal(h Host, o EditProfileOpts) *EditProfileModal {
	e := &EditProfileModal{h: h, o: o}
	e.form = newEPForm(e)
	e.switchPane("formPane")
	return e
}

func (e *EditProfileModal) Layout(gtx layout.Context) layout.Dimensions {
	if e.done || e.m == nil {
		return layout.Dimensions{}
	}
	m := e.m
	d := m.Layout(gtx)
	if m == e.m && m.Dismissed() {
		e.done = true
	}
	return d
}

// Dismissed reports the modal closed.
func (e *EditProfileModal) Dismissed() bool { return e.done || (e.m != nil && e.m.Dismissed()) }

// Dismiss closes it the way Escape does (the speed bump first when the
// form has unsaved changes).
func (e *EditProfileModal) Dismiss() {
	if e.m != nil {
		e.m.Dismiss()
	}
}

// close is closeModal({force: true}).
func (e *EditProfileModal) close() {
	if e.m != nil {
		e.m.Close()
	}
	e.done = true
}

// Pane is the pane on screen: formPane, uploadPane, cropPane or
// removePane.
func (e *EditProfileModal) Pane() string { return e.pane }

// Form is the form pane.
func (e *EditProfileModal) Form() *epForm { return e.form }

func (e *EditProfileModal) switchPane(p string) {
	e.pane = p
	switch p {
	case "formPane":
		e.m = e.form.m
		e.form.focused = false
	case "uploadPane":
		e.m = e.upl.m
	case "cropPane":
		e.m = e.crop.m
	case "removePane":
		e.rm = newEPRemove(e)
		e.m = e.rm.m
	}
	e.h.Invalidate()
}

// pickPhoto is EditProfileUploadButton: the file chooser, then the upload
// pane with the file.
func (e *EditProfileModal) pickPhoto() {
	if PickFiles == nil {
		e.h.Toast("No file chooser on this platform")
		return
	}
	h := e.h
	go func() {
		fs, err := PickFiles("image/*", false)
		if err != nil || len(fs) == 0 {
			return
		}
		post(h, func() { e.UploadPhoto(fs[0]) })
	}()
}

// UploadPhoto starts the upload pane on f (what the file chooser gave).
func (e *EditProfileModal) UploadPhoto(f core.UploadFile) {
	e.upl = newEPUpload(e, f)
	e.switchPane("uploadPane")
}

// ---- the form pane ----

type epOption struct{ label, value string }

// epField is one of BEl1's fields as BaseEditProfileField draws it.
type epField struct {
	name, label, hint, placeholder string
	kind                           string // text, options_list
	disabled                       bool
	disabledTip                    string
	rules                          string
	options                        []epOption
	ed                             widget.Editor
	sel                            widget.Clickable
	value                          string // options_list's
	err                            core.ProfileFieldError
	tip                            TipState
	hov                            widget.Clickable // the locked input's hover (its tooltip)
	last                           string
}

type epForm struct {
	e        *EditProfileModal
	h        Host
	m        *Modal
	user     string
	defaults map[string]string
	changes  map[string]string
	fields   []*epField
	byName   map[string]*epField

	secsLoading bool
	header      core.ProfileHeader
	pronWas     string
	pronSet     bool // the pronunciation editor took the header's text

	saving   bool
	saveBtn  *ModalButton
	upload   widget.Clickable
	remove   widget.Clickable
	focused  bool
	bump     bool
	bumpKeep *ModalButton
	bumpGo   *ModalButton
	veil     int
	bumpUp   bool // the speed bump took the focus
}

func newEPForm(e *EditProfileModal) *epForm {
	h := e.h
	f := &epForm{e: e, h: h, changes: map[string]string{}, byName: map[string]*epField{}, secsLoading: true}
	a := h.Core()
	if a != nil {
		f.user = a.Self().ID
		f.defaults = a.ProfileFormDefaults(f.user)
	} else {
		f.defaults = map[string]string{}
	}
	f.buildFields()
	f.saveBtn = &ModalButton{Label: nsEditProfile.T("Save Changes"), Kind: BtnPrimary, Enabled: f.canSave, Do: func() bool { f.Save(); return false }}
	cancel := &ModalButton{Label: nsEditProfile.T("Cancel"), Kind: BtnOutline} // handleCancel: closeModal({force: true})
	f.bumpKeep = &ModalButton{Label: nsSpeedBump.T("Keep Editing"), Kind: BtnOutline}
	f.bumpGo = &ModalButton{Label: nsSpeedBump.T("Close & Discard"), Kind: BtnDanger}
	f.m = NewModal(h, ModalOpts{
		Title: nsEditProfile.T("Edit your profile"), Width: 700, Body: f.layout, Border: true,
		Buttons: []*ModalButton{cancel, f.saveBtn},
		FocusStops: func() []event.Tag {
			if f.bump {
				return []event.Tag{&f.bumpKeep.click, &f.bumpGo.click}
			}
			return f.stops()
		},
		CloseGuard: func() bool {
			if f.bump { // Escape on the speed bump cancels it
				f.bump = false
				return false
			}
			if f.dirty() && !f.saving {
				f.bump = true
				f.h.Invalidate()
				return false
			}
			return true
		},
		Over: f.over,
	})
	f.fetchSections()
	return f
}

// buildFields is BEl1's default field list for this workspace.
func (f *epForm) buildFields() {
	a := f.h.Core()
	team, policy, pronouns := "", "", false
	if a != nil {
		a.S.RLock()
		team = a.S.TeamName
		a.S.RUnlock()
		policy = a.TeamPrefString("username_policy")
		pronouns = teamPrefTrue(a, "display_pronouns")
	}
	add := func(fd *epField) {
		f.fields = append(f.fields, fd)
		f.byName[fd.name] = fd
		fd.ed.SingleLine = true
		v := f.defaults[fd.name]
		fd.ed.SetText(v)
		fd.last = v
		fd.value = v
	}
	add(&epField{name: "real_name", kind: "text", label: nsEditProfile.T("Full name"), placeholder: nsEditProfile.T("Full name"),
		disabledTip: nsEditProfile.T("Locked by an Admin")})
	add(&epField{name: "display_name", kind: "text", label: nsEditProfile.T("Display name"), rules: policy,
		hint: nsEditProfile.T("This could be your first name, or a nickname — however you’d like people to refer to you in Slack.")})
	add(&epField{name: "title", kind: "text", label: nsEditProfile.T("Title"), disabled: true,
		hint: nsEditProfile.T("Let people know what you do at {teamName}.", "teamName", team)})
	if pronouns {
		add(&epField{name: "pronouns", kind: "text", label: nsEditProfile.T("Pronouns"), placeholder: nsEditProfile.T("Ex. they/them/theirs"),
			hint: nsEditProfile.T("Let people know which pronouns you use.")})
	}
	// name_recording (audio_recorder_button) is left out: Clack can't record
	add(&epField{name: "name_pronunciation", kind: "text", label: nsEditProfile.T("Name pronunciation"),
		placeholder: nsEditProfile.T("Zoe (pronounced 'zo-ee')"),
		hint:        nsEditProfile.T("This could be a phonetic pronunciation, or an example of something your name sounds like.")})
	tz := &epField{name: "tz", kind: "options_list", label: nsEditProfile.T("Time zone"),
		hint: nsEditProfile.T("Your current time zone. Used to send summary and notification emails, for times in your activity feeds, and for reminders.")}
	tz.options = append(tz.options, epOption{label: nsEditProfile.T("Please select an option")})
	for _, z := range core.TimezonesFor(i18n.Locale()) {
		tz.options = append(tz.options, epOption{label: z.Label, value: z.ID})
	}
	add(tz)
}

// teamPrefTrue reads a boolean team pref (true, "true" or 1).
func teamPrefTrue(a *core.App, name string) bool {
	raw, ok := a.TeamPref(name)
	if !ok {
		return false
	}
	s := strings.Trim(string(raw), `"`)
	return s == "true" || s == "1"
}

// visible is e0: the fields drawn, in order (the pronunciation only when
// the header section has it).
func (f *epForm) visible() []*epField {
	var out []*epField
	for _, fd := range f.fields {
		if fd.name == "name_pronunciation" && f.header.Pronunciation == nil {
			continue
		}
		out = append(out, fd)
	}
	return out
}

// Fields are the drawn fields' names in order.
func (f *epForm) Fields() []string {
	var out []string
	for _, fd := range f.visible() {
		out = append(out, fd.name)
	}
	return out
}

// fetchSections is useProfileHeaderData (9N9Y) on the viewer's sections.
func (f *epForm) fetchSections() {
	a := f.h.Core()
	if a == nil || f.user == "" {
		f.secsLoading = false
		return
	}
	user := f.user
	run(f.h, func(ctx context.Context) (core.ProfileSections, error) {
		return a.ProfileSections(ctx, user)
	}, func(ps core.ProfileSections, err error) {
		f.secsLoading = false
		if err != nil {
			return
		}
		f.header = core.HeaderOf(ps)
		if t := f.byName["title"]; t != nil {
			t.disabled = f.header.Title == nil || !f.header.Title.EditableInUI
		}
		if p := f.byName["name_pronunciation"]; p != nil && f.header.Pronunciation != nil {
			p.disabled = !f.header.Pronunciation.EditableInUI
			f.pronWas = f.header.Pronunciation.Text
			if !f.pronSet && p.ed.Text() == "" {
				p.ed.SetText(f.pronWas)
				p.last = f.pronWas
			}
			f.pronSet = true
		}
		f.h.Invalidate()
	})
}

// SetField types v into field name (as onChange would).
func (f *epForm) SetField(name, v string) {
	fd := f.byName[name]
	if fd == nil {
		return
	}
	if fd.kind == "options_list" {
		fd.value = v
	} else {
		fd.ed.SetText(v)
	}
	f.changed(fd, v)
}

// FieldError is the field's error text ("" when it's valid).
func (f *epForm) FieldError(name string) string {
	if fd := f.byName[name]; fd != nil {
		return ValidationText(fd.err)
	}
	return ""
}

// changed is e1: the value is kept (title and the pronunciation apart)
// and the field validated.
func (f *epForm) changed(fd *epField, v string) {
	fd.last = v
	switch fd.name {
	case "title", "name_pronunciation":
	default:
		f.changes[fd.name] = v
	}
	fd.err = core.ValidateProfileField(fd.name, v)
	f.h.Invalidate()
}

func (f *epForm) save() core.ProfileFormSave {
	s := core.ProfileFormSave{UserID: f.user, Defaults: f.defaults, Changes: map[string]string{}, Header: f.header,
		PronunciationWas: f.pronWas, TitleWas: f.defaults["title"]}
	for k, v := range f.changes {
		s.Changes[k] = v
	}
	if t := f.byName["title"]; t != nil {
		s.Title = t.ed.Text()
		if s.Title != s.TitleWas {
			s.Changes["title"] = s.Title
		}
	}
	s.Pronunciation = f.pronWas
	if p := f.byName["name_pronunciation"]; p != nil && f.header.Pronunciation != nil {
		s.Pronunciation = p.ed.Text()
	}
	return s
}

func (f *epForm) dirty() bool { return f.save().Dirty() }

// canSave is Save Changes' `disabled: some(errors)`, and not while saving.
func (f *epForm) canSave() bool {
	if f.saving {
		return false
	}
	for _, fd := range f.fields {
		if !fd.err.OK() {
			return false
		}
	}
	return true
}

// Saving reports a save in flight.
func (f *epForm) Saving() bool { return f.saving }

// Save is handleSubmit.
func (f *epForm) Save() {
	if !f.canSave() {
		return
	}
	a := f.h.Core()
	s := f.save()
	if a == nil || !s.Dirty() {
		f.e.close()
		return
	}
	f.saving = true
	refetch := f.e.o.Refetch
	headerChanged := s.Header.Found && (s.Pronunciation != s.PronunciationWas || s.Title != s.TitleWas)
	run(f.h, func(ctx context.Context) (core.ProfileFormResult, error) {
		return a.SaveProfileForm(ctx, s)
	}, func(r core.ProfileFormResult, err error) {
		f.saving = false
		if err != nil {
			f.h.Invalidate()
			return
		}
		if headerChanged && refetch != nil {
			refetch()
		}
		if !r.Closed {
			for name, fe := range r.FieldErrors {
				if fd := f.byName[name]; fd != nil {
					fd.err = fe
				}
			}
			f.h.Invalidate()
			return
		}
		if refetch != nil && !headerChanged {
			refetch()
		}
		f.e.close()
	})
}

func (f *epForm) stops() []event.Tag {
	vis := f.visible()
	var out []event.Tag
	add := func(fd *epField) {
		if fd.kind == "options_list" {
			out = append(out, &fd.sel)
		} else if !fd.disabled {
			out = append(out, &fd.ed)
		}
	}
	for i, fd := range vis {
		if i < 3 {
			add(fd)
		}
	}
	out = append(out, &f.upload)
	if f.hasPhoto() {
		out = append(out, &f.remove)
	}
	for i, fd := range vis {
		if i >= 3 {
			add(fd)
		}
	}
	return out
}

func (f *epForm) hasPhoto() bool {
	a := f.h.Core()
	return a != nil && a.HasCustomPhoto(f.user)
}

func (f *epForm) events(gtx layout.Context) {
	if !f.focused {
		f.focused = true
		f.focusOnMount(gtx)
	}
	for _, fd := range f.fields {
		if fd.kind == "options_list" {
			continue
		}
		for {
			ev, ok := fd.ed.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.ChangeEvent); ok {
				if t := fd.ed.Text(); t != fd.last {
					f.changed(fd, t)
				}
			}
		}
	}
	if f.upload.Clicked(gtx) {
		f.e.pickPhoto()
	}
	if f.remove.Clicked(gtx) && f.hasPhoto() {
		f.e.switchPane("removePane")
	}
}

// focusOnMount is renderField's focusOnMount: the focusedFieldName field,
// else the first field.
func (f *epForm) focusOnMount(gtx layout.Context) {
	vis := f.visible()
	for i, fd := range vis {
		if (f.e.o.Focus != "" && fd.name == f.e.o.Focus) || (f.e.o.Focus == "" && i == 0) {
			if fd.kind == "options_list" {
				gtx.Execute(key.FocusCmd{Tag: &fd.sel})
			} else if !fd.disabled {
				gtx.Execute(key.FocusCmd{Tag: &fd.ed})
				n := len([]rune(fd.ed.Text()))
				fd.ed.SetCaret(n, n)
			}
			return
		}
	}
}

func (f *epForm) layout(gtx layout.Context) layout.Dimensions {
	f.events(gtx)
	th := f.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	vis := f.visible()
	// the pane's boundary (Slick's Nicknames tracks its mount)
	slackc.RenderLazy(gtx, "EditProfileFormPane", func() slackc.Props {
		return slackc.Props{"$key": "edit-profile:" + f.user, "memberId": f.user}
	}, pfNoNative)
	y := 0
	secW := dp(gtx, 192)
	priW := maxInt(1, w-secW-dp(gtx, 32))
	py := 0
	for i, fd := range vis {
		if i >= 3 {
			break
		}
		d := at(gtx, image.Pt(0, py), func(gtx layout.Context) layout.Dimensions {
			return f.drawField(gtx, th, cl, fd, image.Pt(0, py), priW)
		})
		py += d.Size.Y
	}
	sd := at(gtx, image.Pt(w-secW, 0), func(gtx layout.Context) layout.Dimensions {
		return f.photoColumn(gtx, th, cl, secW)
	})
	y = maxInt(py, sd.Size.Y+dp(gtx, 20))
	for i, fd := range vis {
		if i < 3 {
			continue
		}
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return f.drawField(gtx, th, cl, fd, image.Pt(0, y), w)
		})
		y += d.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// photoColumn is .p-edit_profile__column_secondary: "Profile photo", the
// 192px photo (radius 4), Upload Photo (outline, full width) and the
// Remove Photo link, 8px apart.
func (f *epForm) photoColumn(gtx layout.Context, th *theme.Theme, cl *Colors, w int) layout.Dimensions {
	y := 0
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
		return text(gtx, th, 15, theme.Bold, cl.Fg, nsEditProfile.T("Profile photo"))
	})
	y += lh + dp(gtx, 8)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return epPhoto(gtx, f.h, f.user, w)
	})
	y += w + dp(gtx, 8)
	d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return FocusButton(gtx, th, &f.upload, BtnOutline, BtnMedium, nsEditProfile.T("Upload Photo"), "", float32(w)/gtx.Metric.PxPerDp, f.m.ringOn(gtx, &f.upload))
	})
	y += d.Size.Y
	if f.hasPhoto() {
		y += dp(gtx, 8)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return epLinkButton(gtx, th, cl, &f.remove, nsEditProfile.T("Remove Photo"), w, f.m.ringOn(gtx, &f.remove))
		})
		y += d.Size.Y
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// epPhoto is BaseEditProfilePhoto: the member's 192 picture, w square,
// radius 4.
func epPhoto(gtx layout.Context, h Host, user string, w int) layout.Dimensions {
	sz := image.Pt(w, w)
	rad := dp(gtx, 4)
	if a := h.Core(); a != nil {
		if u, ok := a.User(user); ok {
			url := u.Avatar512
			if url == "" {
				url = u.Avatar192
			}
			if url != "" {
				if img, ok := h.Images().Get(url, w); ok {
					return theme.Image(gtx, img, sz, rad)
				}
			}
		}
	}
	rrect(gtx, image.Rectangle{Max: sz}, rad, colorsOf(h.Theme()).Selected)
	return layout.Dimensions{Size: sz}
}

// epLinkButton is a c-link--button: link-coloured 15px text, centred in w.
func epLinkButton(gtx layout.Context, th *theme.Theme, cl *Colors, c *widget.Clickable, label string, w int, ring bool) layout.Dimensions {
	lh := dp(gtx, 22)
	tw := textW(gtx, th, 15, theme.Regular, label)
	x := maxInt(0, (w-tw)/2)
	if ring {
		focusRing(gtx, cl, image.Rect(x-dp(gtx, 2), 0, x+tw+dp(gtx, 2), lh), dp(gtx, 4))
	}
	at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
		return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Regular, cl.Link, label)
			})
		})
	})
	return layout.Dimensions{Size: image.Pt(w, lh)}
}

// drawField is BaseEditProfileField for a text or options_list field, w
// wide: the label (15/700, a lock first when it's locked), 8px, the large
// input (44px, 18px text) or select, the inline error, the hint (13/18)
// and the workspace's rules, 20px under all of it.
func (f *epForm) drawField(gtx layout.Context, th *theme.Theme, cl *Colors, fd *epField, org image.Point, w int) layout.Dimensions {
	y := 0
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, 0), func(gtx layout.Context) layout.Dimensions {
		x := 0
		if fd.disabled {
			at(gtx, image.Pt(0, (lh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "lock", 16, cl.Fg2)
			})
			x = dp(gtx, 20)
		}
		return at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x), lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Bold, cl.Fg, fd.label)
			})
		})
	})
	y += lh + dp(gtx, 8)
	ih := dp(gtx, 44)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(w, ih))
		if fd.kind == "options_list" {
			return f.drawSelect(gtx, th, cl, fd, org.Add(image.Pt(0, y)), w, ih)
		}
		fd.ed.ReadOnly = fd.disabled
		ph := fd.placeholder
		if ph == "" {
			ph = fd.label
		}
		var d layout.Dimensions
		if fd.disabled {
			d = fieldOn(gtx, th, &fd.ed, ph, "", 44, 18, epDisabledBg(th, cl))
		} else {
			d = field(gtx, th, &fd.ed, ph, "", 44, 18)
		}
		if !fd.err.OK() {
			stroke(gtx, image.Rect(0, 0, w, ih), dp(gtx, 8), maxInt(1, dp(gtx, 1)), ccErrColor(th, cl))
		}
		if fd.disabled && fd.disabledTip != "" {
			fd.hov.Layout(gtx, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: image.Pt(w, ih)} })
			Tooltip(gtx, th, &fd.tip, fd.hov.Hovered(), image.Pt(w, ih), Below, fd.disabledTip, "")
		}
		return d
	})
	y += ih
	if !fd.err.OK() {
		y += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			return epInlineError(gtx, th, cl, ValidationText(fd.err), w)
		})
		y += d.Size.Y + dp(gtx, 4)
	}
	if fd.hint != "" {
		y += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, fd.hint, 0)
		})
		y += d.Size.Y
	}
	if fd.rules != "" {
		// .p-edit_profile__rules: 13/18, margin-bottom 20, after the hint's 8
		y += dp(gtx, 8)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			d1 := para(gtx, th, 13, 18, theme.Bold, cl.Fg, nsEditProfile.T("Custom rules for this workspace:"), 0)
			d2 := at(gtx, image.Pt(0, d1.Size.Y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
				return para(gtx, th, 13, 18, theme.Regular, cl.Fg, fd.rules, 0)
			})
			return layout.Dimensions{Size: image.Pt(w, d1.Size.Y+d2.Size.Y)}
		})
		y += d.Size.Y
	}
	y += dp(gtx, 20)
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// epDisabledBg is a read-only input's fill (ctr-sec).
func epDisabledBg(th *theme.Theme, cl *Colors) color.NRGBA {
	if th.Dark {
		return rgb(0x222529)
	}
	return rgb(0xf8f8f8)
}

// epInlineError is the c-inline_alert under an input: the warning icon
// and 13px content-imp text.
func epInlineError(gtx layout.Context, th *theme.Theme, cl *Colors, s string, w int) layout.Dimensions {
	ec := ccErrColor(th, cl)
	at(gtx, image.Pt(0, dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "warning", 14, ec)
	})
	return at(gtx, image.Pt(dp(gtx, 20), 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 20)), 1<<20)}
		return para(gtx, th, 13, 18, theme.Regular, ec, s, 0)
	})
}

// drawSelect is the large BasicSelect: the picked option's label (or the
// placeholder option's), a caret at the right; a click opens the list.
func (f *epForm) drawSelect(gtx layout.Context, th *theme.Theme, cl *Colors, fd *epField, pos image.Point, w, ih int) layout.Dimensions {
	sel := 0
	for i, o := range fd.options {
		if o.value == fd.value {
			sel = i
		}
	}
	if fd.sel.Clicked(gtx) {
		labels := make([]string, len(fd.options))
		for i, o := range fd.options {
			labels[i] = o.label
		}
		org := dtModalOrigin(gtx, f.m)
		r := image.Rectangle{Min: org.Add(pos), Max: org.Add(pos).Add(image.Pt(w, ih))}
		f.h.ShowOverlay(NewSelectList(f.h, r, labels, sel, SelectListOpts{}, func(i int) {
			if i >= 0 && i < len(fd.options) {
				fd.value = fd.options[i].value
				f.changed(fd, fd.value)
			}
		}))
	}
	r := image.Rect(0, 0, w, ih)
	rad := dp(gtx, 8)
	ring := f.m.ringOn(gtx, &fd.sel) || gtx.Focused(&fd.sel)
	if ring {
		focusRing(gtx, cl, r, rad)
	}
	rrect(gtx, r, rad, dtBorder(th))
	rrect(gtx, r.Inset(1), rad-1, cl.Bg)
	label := fd.options[sel].label
	at(gtx, image.Pt(dp(gtx, 12), 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 48)), ih)}
		return vcenter(gtx, ih, func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 18, 24, theme.Regular, cl.Fg, label, 1)
		})
	})
	at(gtx, image.Pt(w-dp(gtx, 12)-dp(gtx, 16), (ih-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "caret-down", 16, cl.Fg2)
	})
	return clickBox(gtx, &fd.sel, nil, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: r.Max}
	})
}

// over draws the title's spinner while the header section loads, and the
// speed bump.
func (f *epForm) over(gtx layout.Context) {
	th := f.h.Theme()
	cl := colorsOf(th)
	if f.secsLoading {
		tw := textW(gtx, th, 22, theme.Black, nsEditProfile.T("Edit your profile"))
		s := dp(gtx, 20)
		at(gtx, image.Pt(dp(gtx, 28)+tw+dp(gtx, 8), (dp(gtx, 70)-s)/2), func(gtx layout.Context) layout.Dimensions {
			epSpinner(gtx, s, cl.Fg3)
			return layout.Dimensions{Size: image.Pt(s, s)}
		})
	}
	if !f.bump {
		f.bumpUp = false
	}
	if f.bump && !f.bumpUp {
		f.bumpUp = true
		f.m.focusTo(gtx, &f.bumpKeep.click, true) // Keep Editing has the focus (no autoFocusContinueButton)
	}
	if f.bump {
		epSpeedBump(gtx, f.h, f.m, &f.veil, f.bumpKeep, f.bumpGo, func() { f.bump = false }, f.e.close)
	}
}

// SpeedBump reports the discard speed bump is up.
func (f *epForm) SpeedBump() bool { return f.bump }

// epSpeedBump is ModalSpeedBump in the footer: the card veiled, the
// c-modal_speed_bump_content sheet at the bottom (base-pry, a 1px otl-ter
// line, padding 20 24) with "**Discard changes?** Closing this window
// will cancel any unsaved changes." 24px over Keep Editing (focused) and
// Close & Discard (danger), 12px apart at the right.
func epSpeedBump(gtx layout.Context, h Host, m *Modal, veil *int, keep, discard *ModalButton, onKeep, onDiscard func()) {
	th := h.Theme()
	cl := colorsOf(th)
	if keep.click.Clicked(gtx) {
		onKeep()
		return
	}
	if discard.click.Clicked(gtx) {
		onDiscard()
		return
	}
	sz := gtx.Constraints.Max
	func() {
		rad := dp(gtx, 8)
		defer clip.RRect{Rect: image.Rectangle{Max: sz}, NE: rad, NW: rad, SE: rad, SW: rad}.Push(gtx.Ops).Pop()
		event.Op(gtx.Ops, veil)
		for {
			if _, ok := gtx.Event(pointer.Filter{Target: veil, Kinds: pointer.Press | pointer.Release | pointer.Scroll,
				ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20}}); !ok {
				break
			}
		}
		fill(gtx, image.Rectangle{Max: sz}, theme.WithAlpha(cl.ModalBg, 179))
	}()
	var runs []cnRun
	for _, p := range nsSpeedBump.RT("<r1>Discard changes?</r1> Closing this window will cancel any unsaved changes.") {
		runs = append(runs, cnRun{s: p.Text, bold: p.Tag != ""})
	}
	px, py := dp(gtx, 24), dp(gtx, 20)
	iw := sz.X - 2*px
	call, td := record(loose(gtx, image.Pt(iw, 1<<20)), func(gtx layout.Context) layout.Dimensions {
		return cnRuns(gtx, th, cl, runs, 15, 22, cl.Fg, iw)
	})
	bh := dp(gtx, 36)
	sheetH := py + td.Size.Y + dp(gtx, 24) + bh + py
	top := sz.Y - sheetH
	sheet := image.Rect(0, top, sz.X, sz.Y)
	rad := dp(gtx, 8)
	func() {
		defer clip.RRect{Rect: sheet, SE: rad, SW: rad}.Push(gtx.Ops).Pop()
		fill(gtx, sheet, BasePryOf(th))
	}()
	fill(gtx, image.Rect(0, top, sz.X, top+maxInt(1, dp(gtx, 1))), theme.OtlTer(th))
	macroAt(gtx, call, image.Pt(px, top+py))
	by := top + py + td.Size.Y + dp(gtx, 24)
	x := sz.X - px
	for _, b := range []*ModalButton{discard, keep} {
		ring := m.ringOn(gtx, &b.click)
		c, d := record(loose(gtx, image.Pt(sz.X, bh)), func(gtx layout.Context) layout.Dimensions {
			return b.draw(gtx, th, b.Kind, ring, grpNone)
		})
		x -= d.Size.X
		macroAt(gtx, c, image.Pt(x, by))
		x -= dp(gtx, 12)
	}
}

// epSpinner is Slack's loading ring (a 3/4 arc turning once a second) in a
// d square.
func epSpinner(gtx layout.Context, d int, c color.NRGBA) {
	t := float64(gtx.Now.UnixMilli()%1000) / 1000
	r := float32(d)/2 - float32(dp(gtx, 1.5))
	ctr := f32.Pt(float32(d)/2, float32(d)/2)
	a0 := float32(t * 2 * math.Pi)
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(ctr.X+r*float32(math.Cos(float64(a0))), ctr.Y+r*float32(math.Sin(float64(a0)))))
	p.ArcTo(ctr, ctr, 1.5*math.Pi)
	st := clip.Stroke{Path: p.End(), Width: float32(dp(gtx, 2))}.Op().Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
	gtx.Execute(op.InvalidateCmd{At: gtx.Now.Add(33 * time.Millisecond)})
}

// ValidationText is getValidationErrorText (h+Rv, ui_validation).
func ValidationText(e core.ProfileFieldError) string {
	switch e.Type {
	case "":
		return ""
	case "error_is_required":
		return nsUIValidation.T("Unfortunately, you can’t leave this blank.")
	case "error_max_length":
		return nsUIValidation.T("You can’t enter more than {maxLength} characters.", "maxLength", e.MaxLength)
	case "error_is_url":
		return nsUIValidation.T("This doesn’t seem like a proper link. Sorry!")
	case "error_no_restricted_values":
		return nsUIValidation.T("That name’s already being used by Slack.")
	case "error_no_restricted_chars":
		return nsUIValidation.T("Mostly, names can’t contain punctuation. (Apostrophes, spaces, and periods are fine.)")
	case "error_no_special_chars":
		return nsUIValidation.T("Names can’t contain most special characters. (Apostrophes, spaces, and periods are okay.)")
	case "error_has_alphanumeric":
		return nsUIValidation.T("Names can’t consist solely of punctuation. Please elaborate!")
	case "error_no_leading_at":
		return nsUIValidation.T("Names can’t start with the @ sign — that’s how your coworkers will mention you in conversation.")
	case "error_no_emoji":
		switch e.Subtype {
		case "poop":
			return nsUIValidation.T("For reasons of party-pooping, names cannot contain emoji. Try again?")
		case "1":
			return nsUIValidation.T("For all manner of complex reasons, names simply cannot contain emoji. Try again?")
		case "2":
			return nsUIValidation.T("Using emoji for names makes some sense, but sadly, not enough. Try again?")
		case "3":
			return nsUIValidation.T("For technical reasons, emoji are not allowed in names. Try again?")
		}
		return nsUIValidation.T("Of course you want a name with an emoji. Sadly, it is not to be. Try letters?")
	}
	return ""
}

// ---- the upload pane ----

type epUpload struct {
	e      *EditProfileModal
	m      *Modal
	file   core.UploadFile
	status string // uploading, success, error
	err    error
	retry  widget.Clickable
}

func newEPUpload(e *EditProfileModal, f core.UploadFile) *epUpload {
	u := &epUpload{e: e, file: f, status: "uploading"}
	cancel := &ModalButton{Label: nsEditProfile.T("Cancel"), Kind: BtnOutline} // e(): closeModal
	retry := &ModalButton{Label: nsEditProfile.T("Try a Different Image"), Kind: BtnPrimary,
		Hidden: func() bool { return u.err == nil }, Do: func() bool { e.pickPhoto(); return false }}
	u.m = NewModal(e.h, ModalOpts{TitleFunc: u.title, Body: u.layout, Buttons: []*ModalButton{cancel, retry},
		OnClose: func() { e.done = true }})
	u.start()
	return u
}

// start is useImageUpload: processUpload, then the crop pane.
func (u *epUpload) start() {
	a := u.e.h.Core()
	if a == nil {
		u.status, u.err = "error", core.ErrPhotoAPI
		return
	}
	f := u.file
	type res struct {
		up  core.PhotoUpload
		img image.Image
	}
	run(u.e.h, func(ctx context.Context) (res, error) {
		up, err := a.PreparePhoto(ctx, f.Name, f.Data)
		if err != nil {
			return res{}, err
		}
		img, _, err := image.Decode(bytes.NewReader(f.Data))
		if err != nil {
			return res{}, core.ErrPhotoAPI
		}
		return res{up, img}, nil
	}, func(r res, err error) {
		if u.e.upl != u {
			return // a different image was picked since
		}
		if err != nil {
			u.status, u.err = "error", err
			u.e.h.Invalidate()
			return
		}
		u.status = "success"
		u.e.crop = newEPCrop(u.e, r.up, r.img)
		u.e.switchPane("cropPane")
	})
}

// Status is uploading, success or error; ErrorText the shown message.
func (u *epUpload) Status() string { return u.status }

func (u *epUpload) title() string {
	if u.err == nil {
		return nsEditProfile.T("Uploading your photo…")
	}
	switch {
	case errors.Is(u.err, core.ErrPhotoTooSmall):
		return nsEditProfile.T("Try a larger image")
	case errors.Is(u.err, core.ErrPhotoTooBig):
		return nsEditProfile.T("Try a smaller image")
	case errors.Is(u.err, core.ErrPhotoType):
		return nsEditProfile.T("Try another photo")
	}
	return nsEditProfile.T("Eek")
}

// message is the error's emoji and text.
func (u *epUpload) message() (emojiName, msg string) {
	switch {
	case errors.Is(u.err, core.ErrPhotoTooSmall):
		return "worried", nsEditProfile.T("That’s a very nice photo, but it’s a bit too small. Try one that’s larger than {minSize} tall by {minSize} wide.", "minSize", "36px")
	case errors.Is(u.err, core.ErrPhotoTooBig):
		return "worried", nsEditProfile.T("That’s a very nice photo, but it’s a bit too big. Try one that’s smaller than {maxSize} tall by {maxSize} wide.", "maxSize", "4,000px")
	case errors.Is(u.err, core.ErrPhotoType):
		return "flushed", nsEditProfile.T("Profile photos must be GIFs, JPGs or PNGs.")
	}
	return "frowning", nsEditProfile.T("Sorry! Something went wrong. Please try again.")
}

// ErrorText is the error's text ("" while uploading).
func (u *epUpload) ErrorText() string {
	if u.err == nil {
		return ""
	}
	_, m := u.message()
	return m
}

// layout is the 460 square (.p-edit_profile__centered_primary--background:
// 13% of the foreground) with the jumbo spinner or the error, and the
// message preview.
func (u *epUpload) layout(gtx layout.Context) layout.Dimensions {
	th := u.e.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	box := dp(gtx, 460)
	x := maxInt(0, (w-box)/2)
	rrect(gtx, image.Rect(x, 0, x+box, box), dp(gtx, 4), theme.WithAlpha(cl.Fg, 0x21))
	if u.err == nil {
		s := dp(gtx, 64)
		at(gtx, image.Pt(x+(box-s)/2, (box-s)/2), func(gtx layout.Context) layout.Dimensions {
			epSpinner(gtx, s, cl.Fg3)
			return layout.Dimensions{Size: image.Pt(s, s)}
		})
	} else {
		name, msg := u.message()
		cw := dp(gtx, 200)
		call, d := record(loose(gtx, image.Pt(cw, box)), func(gtx layout.Context) layout.Dimensions {
			es := dp(gtx, 32)
			at(gtx, image.Pt((cw-es)/2, 0), func(gtx layout.Context) layout.Dimensions {
				return emojiImage(gtx, u.e.h, name, 32)
			})
			y := es + dp(gtx, 8)
			td := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(cw, box)}
				return epCentered(gtx, th, 15, 22, theme.Bold, cl.Fg, msg, cw)
			})
			return layout.Dimensions{Size: image.Pt(cw, y+td.Size.Y)}
		})
		macroAt(gtx, call, image.Pt(x+(box-cw)/2, (box-d.Size.Y)/2))
	}
	pd := at(gtx, image.Pt(0, box), func(gtx layout.Context) layout.Dimensions {
		return epPreview(gtx, u.e.h, nil, w)
	})
	return layout.Dimensions{Size: image.Pt(w, box+pd.Size.Y)}
}

// epCentered is centred wrapping text (text-align center), line by line.
func epCentered(gtx layout.Context, th *theme.Theme, size, lh float32, wt font.Weight, c color.NRGBA, s string, w int) layout.Dimensions {
	lines := epWrap(gtx, th, size, wt, s, w)
	y := 0
	lhp := dp(gtx, lh)
	for _, l := range lines {
		lw := textW(gtx, th, size, wt, l)
		at(gtx, image.Pt((w-lw)/2, y), func(gtx layout.Context) layout.Dimensions {
			return vcenter(gtx, lhp, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, size, wt, c, l)
			})
		})
		y += lhp
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// epWrap breaks s into lines at most w wide (word by word).
func epWrap(gtx layout.Context, th *theme.Theme, size float32, wt font.Weight, s string, w int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		next := word
		if cur != "" {
			next = cur + " " + word
		}
		if cur != "" && textW(gtx, th, size, wt, next) > w {
			lines = append(lines, cur)
			cur = word
			continue
		}
		cur = next
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// epPreview is BaseEditProfileUploadPreview: "Preview" (13px, 70%), 16px,
// then a compact message by you: the 72px picture (the crop when there is
// one), your bold name and the time. 24px above it all.
func epPreview(gtx layout.Context, h Host, photo func(gtx layout.Context, size int) layout.Dimensions, w int) layout.Dimensions {
	th := h.Theme()
	cl := colorsOf(th)
	y := dp(gtx, 24)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(w, dp(gtx, 18))}
		return text(gtx, th, 13, theme.Regular, cl.Fg2, nsEditProfile.T("Preview"))
	})
	y += dp(gtx, 18) + dp(gtx, 16)
	ps := dp(gtx, 72)
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		if photo != nil {
			return photo(gtx, ps)
		}
		self := ""
		if a := h.Core(); a != nil {
			self = a.Self().ID
		}
		return epPhoto(gtx, h, self, ps)
	})
	name := ""
	if a := h.Core(); a != nil {
		name = userName(a.Self())
	}
	at(gtx, image.Pt(ps+dp(gtx, 8), y), func(gtx layout.Context) layout.Dimensions {
		nw := textW(gtx, th, 15, theme.Black, name)
		text(gtx, th, 15, theme.Black, cl.Fg, name)
		return at(gtx, image.Pt(nw+dp(gtx, 8), dp(gtx, 2)), func(gtx layout.Context) layout.Dimensions {
			return text(gtx, th, 12, theme.Regular, cl.Fg2, clock(time.Now()))
		})
	})
	return layout.Dimensions{Size: image.Pt(w, y+ps)}
}

// ---- the crop pane ----

type epCrop struct {
	cropBox
	e      *EditProfileModal
	m      *Modal
	up     core.PhotoUpload
	saving bool
	failed bool
}

// cropBox is BaseImageCrop's square selection on an image fitted into
// 460px (the profile photo's crop pane, and the channel email icon's in
// channel_email.go).
type cropBox struct {
	h       Host
	img     paint.ImageOp // the image at its display size
	dw, dh  float64       // display size (≤ 460)
	x, y, s float64       // the square in display px
	min     float64       // the smallest square (minSize)
	drag    int           // the pointer tag
	mode    string        // move, nw, ne, sw, se
	from    f32.Point
	fx, fy  float64
	fs      float64
}

// newCropBox fits img into 460px with BaseImageCrop's first square
// (defaultCropScale 1, at least minSize, centred).
func newCropBox(h Host, img image.Image, minSize float64) cropBox {
	c := cropBox{h: h, min: minSize}
	b := img.Bounds()
	c.dw, c.dh = core.PhotoDisplaySize(float64(b.Dx()), float64(b.Dy()), core.PhotoCropMaxSize)
	dst := image.NewRGBA(image.Rect(0, 0, max(1, int(math.Round(c.dw))), max(1, int(math.Round(c.dh)))))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	c.img = paint.NewImageOp(dst)
	e, t := math.Min(c.dh, core.PhotoCropMaxSize), math.Min(c.dw, core.PhotoCropMaxSize)
	c.s = math.Max(minSize, math.Min(e, t))
	c.x, c.y = math.Max(0, (t-c.s)/2), math.Max(0, (e-c.s)/2)
	return c
}

func newEPCrop(e *EditProfileModal, up core.PhotoUpload, img image.Image) *epCrop {
	c := &epCrop{e: e, up: up, cropBox: newCropBox(e.h, img, core.PhotoCropMinSize)}
	cancel := &ModalButton{Label: nsEditProfile.T("Cancel"), Kind: BtnOutline, Do: func() bool { e.switchPane("formPane"); return false }}
	save := &ModalButton{Label: nsEditProfile.T("Save"), Kind: BtnPrimary, Enabled: func() bool { return !c.saving }, Do: func() bool { c.Save(); return false }}
	c.m = NewModal(e.h, ModalOpts{Title: nsEditProfile.T("Crop your photo"), HeadH: 65, Body: c.layout,
		Buttons: []*ModalButton{cancel, save}, AutoFocus: save, OnClose: func() { e.done = true }})
	return c
}

// Crop is the square in image pixels as Save sends it.
func (c *epCrop) Crop() core.PhotoCrop {
	return core.CropFromDisplay(c.x, c.y, c.s, c.up.Width, c.up.Height)
}

// SetSquare moves the selection (display px), kept inside the image.
func (c *epCrop) SetSquare(x, y, s float64) { c.x, c.y, c.s = c.clamp(x, y, s) }

func (c *cropBox) clamp(x, y, s float64) (float64, float64, float64) {
	s = math.Max(math.Min(s, math.Min(c.dw, c.dh)), math.Min(c.min, math.Min(c.dw, c.dh)))
	x = math.Min(math.Max(0, x), c.dw-s)
	y = math.Min(math.Max(0, y), c.dh-s)
	return x, y, s
}

// Save is handleUpload: users.setPhoto with the square, then the form.
func (c *epCrop) Save() {
	a := c.e.h.Core()
	if a == nil || c.saving {
		return
	}
	c.saving, c.failed = true, false
	id, crop := c.up.ID, c.Crop()
	run(c.e.h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.SetPhoto(ctx, id, crop)
	}, func(_ struct{}, err error) {
		c.saving = false
		if err != nil {
			c.failed = true
			c.e.h.Invalidate()
			return
		}
		c.e.switchPane("formPane")
	})
}

// Failed reports the error alert is up.
func (c *epCrop) Failed() bool { return c.failed }

func (c *epCrop) layout(gtx layout.Context) layout.Dimensions {
	th := c.e.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := dp(gtx, 5) // .c-sk-modal_content__inner padding-top 5
	if c.failed {
		h := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			hh := cnAlert(gtx, th, cl, true, []cnRun{{s: nsEditProfile.T("Sorry! Something went wrong. Please try again.")}}, w)
			return layout.Dimensions{Size: image.Pt(w, hh)}
		})
		y += h.Size.Y + dp(gtx, 16)
	}
	box := dp(gtx, 460)
	bx := maxInt(0, (w-box)/2)
	rrect(gtx, image.Rect(bx, y, bx+box, y+box), dp(gtx, 4), theme.WithAlpha(cl.Fg, 0x21))
	iw, ih := int(math.Round(c.dw*float64(gtx.Metric.PxPerDp))), int(math.Round(c.dh*float64(gtx.Metric.PxPerDp)))
	ix, iy := bx+(box-iw)/2, y+(box-ih)/2
	at(gtx, image.Pt(ix, iy), func(gtx layout.Context) layout.Dimensions {
		c.drawCropper(gtx, iw, ih)
		return layout.Dimensions{Size: image.Pt(iw, ih)}
	})
	y += box
	pd := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return epPreview(gtx, c.e.h, c.previewPhoto, w)
	})
	return layout.Dimensions{Size: image.Pt(w, y+pd.Size.Y)}
}

// drawCropper is ReactCrop: the image, a 50% black mask outside the
// square, the marching-ants border and the four corner handles (12px,
// #0003 with a #ffffffb3 border; fixed aspect hides the edge ones).
func (c *cropBox) drawCropper(gtx layout.Context, iw, ih int) {
	k := float64(gtx.Metric.PxPerDp)
	c.events(gtx, k)
	theme.Image(gtx, c.img, image.Pt(iw, ih), 0)
	sx, sy, ss := int(math.Round(c.x*k)), int(math.Round(c.y*k)), int(math.Round(c.s*k))
	sel := image.Rect(sx, sy, sx+ss, sy+ss)
	mask := color.NRGBA{A: 0x80}
	fill(gtx, image.Rect(0, 0, iw, sel.Min.Y), mask)
	fill(gtx, image.Rect(0, sel.Max.Y, iw, ih), mask)
	fill(gtx, image.Rect(0, sel.Min.Y, sel.Min.X, sel.Max.Y), mask)
	fill(gtx, image.Rect(sel.Max.X, sel.Min.Y, iw, sel.Max.Y), mask)
	epAnts(gtx, sel)
	hs := dp(gtx, 12)
	for _, p := range []image.Point{sel.Min, {sel.Max.X, sel.Min.Y}, {sel.Min.X, sel.Max.Y}, sel.Max} {
		r := image.Rect(p.X-hs/2, p.Y-hs/2, p.X+hs/2, p.Y+hs/2)
		fill(gtx, r, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xb3})
		fill(gtx, r.Inset(1), color.NRGBA{A: 0x33})
	}
	defer clip.Rect{Max: image.Pt(iw, ih)}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &c.drag)
	pointer.CursorCrosshair.Add(gtx.Ops)
}

// epAnts is the selection's 1px border of 5px white and #444 dashes.
func epAnts(gtx layout.Context, r image.Rectangle) {
	white, dark := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, color.NRGBA{R: 0x44, G: 0x44, B: 0x44, A: 0xff}
	dash := maxInt(1, dp(gtx, 5))
	for x := r.Min.X; x < r.Max.X; x += dash {
		col := white
		if ((x-r.Min.X)/dash)%2 == 1 {
			col = dark
		}
		e := minInt(x+dash, r.Max.X)
		fill(gtx, image.Rect(x, r.Min.Y, e, r.Min.Y+1), col)
		fill(gtx, image.Rect(x, r.Max.Y-1, e, r.Max.Y), col)
	}
	for y := r.Min.Y; y < r.Max.Y; y += dash {
		col := white
		if ((y-r.Min.Y)/dash)%2 == 1 {
			col = dark
		}
		e := minInt(y+dash, r.Max.Y)
		fill(gtx, image.Rect(r.Min.X, y, r.Min.X+1, e), col)
		fill(gtx, image.Rect(r.Max.X-1, y, r.Max.X, e), col)
	}
}

// events moves the square (a drag inside it) or resizes it from the corner
// grabbed, the opposite corner fixed.
func (c *cropBox) events(gtx layout.Context, k float64) {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: &c.drag, Kinds: pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel})
		if !ok {
			break
		}
		pe, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		px, py := float64(pe.Position.X)/k, float64(pe.Position.Y)/k
		switch pe.Kind {
		case pointer.Press:
			c.from, c.fx, c.fy, c.fs = pe.Position, c.x, c.y, c.s
			grab := 12.0
			corner := func(cx, cy float64) bool { return math.Abs(px-cx) <= grab && math.Abs(py-cy) <= grab }
			switch {
			case corner(c.x, c.y):
				c.mode = "nw"
			case corner(c.x+c.s, c.y):
				c.mode = "ne"
			case corner(c.x, c.y+c.s):
				c.mode = "sw"
			case corner(c.x+c.s, c.y+c.s):
				c.mode = "se"
			case px >= c.x && px <= c.x+c.s && py >= c.y && py <= c.y+c.s:
				c.mode = "move"
			default:
				c.mode = ""
			}
		case pointer.Drag:
			dx, dy := float64(pe.Position.X-c.from.X)/k, float64(pe.Position.Y-c.from.Y)/k
			switch c.mode {
			case "move":
				c.x, c.y, c.s = c.clamp(c.fx+dx, c.fy+dy, c.fs)
			case "se":
				s := math.Max(dx, dy) + c.fs
				s = math.Min(s, math.Min(c.dw-c.fx, c.dh-c.fy))
				c.x, c.y, c.s = c.clamp(c.fx, c.fy, s)
			case "nw":
				s := c.fs - math.Min(dx, dy)
				s = math.Min(s, math.Min(c.fx+c.fs, c.fy+c.fs))
				_, _, s = c.clamp(0, 0, s)
				c.x, c.y, c.s = c.fx+c.fs-s, c.fy+c.fs-s, s
			case "ne":
				s := c.fs + math.Max(dx, -dy)
				s = math.Min(s, math.Min(c.dw-c.fx, c.fy+c.fs))
				_, _, s = c.clamp(0, 0, s)
				c.x, c.y, c.s = c.fx, c.fy+c.fs-s, s
			case "sw":
				s := c.fs + math.Max(-dx, dy)
				s = math.Min(s, math.Min(c.fx+c.fs, c.dh-c.fy))
				_, _, s = c.clamp(0, 0, s)
				c.x, c.y, c.s = c.fx+c.fs-s, c.fy, s
			}
			c.h.Invalidate()
		case pointer.Release, pointer.Cancel:
			c.mode = ""
		}
	}
}

// previewPhoto is ImageCropPreview: the square scaled into size, radius 3.
func (c *cropBox) previewPhoto(gtx layout.Context, size int) layout.Dimensions {
	defer clip.UniformRRect(image.Rectangle{Max: image.Pt(size, size)}, dp(gtx, 3)).Push(gtx.Ops).Pop()
	is := c.img.Size()
	if c.s <= 0 || is.X == 0 {
		return layout.Dimensions{Size: image.Pt(size, size)}
	}
	// the display image is c.dw wide in dp terms; its pixels are is.X
	px := float64(is.X) / c.dw
	sc := float32(float64(size) / (c.s * px))
	off := f32.Pt(-float32(c.x*px)*sc, -float32(c.y*px)*sc)
	defer op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sc, sc)).Offset(off)).Push(gtx.Ops).Pop()
	img := c.img
	img.Filter = paint.FilterLinear
	img.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return layout.Dimensions{Size: image.Pt(size, size)}
}

// ---- the remove pane ----

type epRemove struct {
	e        *EditProfileModal
	m        *Modal
	back     widget.Clickable
	removing bool
	failed   bool
}

func newEPRemove(e *EditProfileModal) *epRemove {
	r := &epRemove{e: e}
	cancel := &ModalButton{Label: nsEditProfile.T("Cancel"), Kind: BtnOutline, Do: func() bool { e.switchPane("formPane"); return false }}
	yes := &ModalButton{Label: nsEditProfile.T("Yes, Remove Photo"), Kind: BtnDanger, Enabled: func() bool { return !r.removing },
		Do: func() bool { r.Remove(); return false }}
	r.m = NewModal(e.h, ModalOpts{Header: r.header, Body: r.layout, Buttons: []*ModalButton{cancel, yes}, AutoFocus: yes,
		OnClose: func() { e.done = true }})
	return r
}

// header is the back arrow (BackButton) and the title.
func (r *epRemove) header(gtx layout.Context) layout.Dimensions {
	th := r.e.h.Theme()
	cl := colorsOf(th)
	sz := gtx.Constraints.Max
	if r.back.Clicked(gtx) {
		r.e.switchPane("formPane")
	}
	b := dp(gtx, 36)
	at(gtx, image.Pt(dp(gtx, 20), (sz.Y-b)/2), func(gtx layout.Context) layout.Dimensions {
		return iconButton(gtx, &r.back, "arrow-left", 36, 20, cl.Fg2, cl.BgHover, 8)
	})
	x := dp(gtx, 20) + b + dp(gtx, 8)
	at(gtx, image.Pt(x, (sz.Y-dp(gtx, 30))/2), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, sz.X-x-dp(gtx, 64)), dp(gtx, 30))}
		return para(gtx, th, 22, 30, theme.Black, cl.Fg, nsEditProfile.T("Remove profile photo"), 1)
	})
	return layout.Dimensions{Size: sz}
}

// Remove is the danger button: users.deletePhoto, then the form.
func (r *epRemove) Remove() {
	a := r.e.h.Core()
	if a == nil || r.removing {
		return
	}
	r.removing, r.failed = true, false
	run(r.e.h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.DeletePhoto(ctx)
	}, func(_ struct{}, err error) {
		r.removing = false
		if err != nil {
			r.failed = true
			r.e.h.Invalidate()
			return
		}
		r.e.switchPane("formPane")
	})
}

// layout is .p-edit_profile__centered_content--wide: 320 wide, centred,
// the 192px photo 8px over the question.
func (r *epRemove) layout(gtx layout.Context) layout.Dimensions {
	th := r.e.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	y := 0
	if r.failed {
		hh := cnAlert(gtx, th, cl, true, []cnRun{{s: nsEditProfile.T("Sorry! Something went wrong. Please try again.")}}, w)
		y += hh + dp(gtx, 16)
	}
	cw := dp(gtx, 320)
	cx := maxInt(0, (w-cw)/2)
	ps := dp(gtx, 192)
	self := ""
	if a := r.e.h.Core(); a != nil {
		self = a.Self().ID
	}
	at(gtx, image.Pt(cx+(cw-ps)/2, y), func(gtx layout.Context) layout.Dimensions {
		return epPhoto(gtx, r.e.h, self, ps)
	})
	y += ps + dp(gtx, 8)
	d := at(gtx, image.Pt(cx, y), func(gtx layout.Context) layout.Dimensions {
		return epCentered(gtx, th, 15, 22, theme.Regular, cl.Fg, nsEditProfile.T("Are you sure you want to remove your photo? We’ll replace it with a default Slack avatar."), cw)
	})
	return layout.Dimensions{Size: image.Pt(w, y+d.Size.Y)}
}

// ---- the section editor ----

// EditSectionModal is EditProfileSectionForm/EditProfileSectionModal for
// one of your profile's sections.
type EditSectionModal struct {
	h       Host
	user    string
	sec     core.ProfileSection
	focus   string // focusedElementId (the element's full id)
	refetch func()
	m       *Modal
	els     []*esElem
	saving  bool
	focused bool
	bump    bool
	bumpUp  bool
	keep    *ModalButton
	discard *ModalButton
	veil    int
}

// esElem is one element's input (the converted value) and its editors.
type esElem struct {
	meta      core.ProfileElement
	in        core.SectionElementInput
	disabled  bool
	ed, ed2   widget.Editor // text / long text / display text; the link's URL
	last      string
	last2     string
	err, err2 core.ProfileFieldError
	pick      widget.Clickable // a text select or the date
	start     core.SectionElementInput
	hov       widget.Clickable
	tip       TipState
}

// OpenEditProfileSection opens the section editor for sec (focus: the
// element whose "Add …" link opened it, "" for the section's Edit).
func OpenEditProfileSection(h Host, sec core.ProfileSection, focus string, refetch func()) *EditSectionModal {
	s := NewEditSectionModal(h, sec, focus, refetch)
	h.ShowOverlay(s)
	return s
}

// NewEditSectionModal builds the section editor.
func NewEditSectionModal(h Host, sec core.ProfileSection, focus string, refetch func()) *EditSectionModal {
	s := &EditSectionModal{h: h, sec: sec, focus: focus, refetch: refetch}
	if a := h.Core(); a != nil {
		s.user = a.Self().ID
	}
	meta := map[string]core.ProfileElement{}
	for _, e := range sec.Elements {
		meta[e.ElementID] = e
	}
	for _, in := range core.SectionInput(sec) {
		e := &esElem{meta: meta[in.ElementID], in: in, start: in}
		e.disabled = !e.meta.EditableInUI
		switch in.Kind {
		case "text":
			e.ed.SingleLine = true
			e.ed.SetText(in.Text)
			e.last = in.Text
		case "long_text":
			e.ed.SetText(in.Text)
			e.last = in.Text
		case "link":
			e.ed.SingleLine, e.ed2.SingleLine = true, true
			e.ed.SetText(in.Display)
			e.ed2.SetText(in.URI)
			e.last, e.last2 = in.Display, in.URI
		}
		s.els = append(s.els, e)
	}
	s.keep = &ModalButton{Label: nsSpeedBump.T("Keep Editing"), Kind: BtnOutline}
	s.discard = &ModalButton{Label: nsSpeedBump.T("Close & Discard"), Kind: BtnDanger}
	save := &ModalButton{Label: nsEditProfile.T("Save Changes"), Kind: BtnPrimary, Enabled: s.canSave, Do: func() bool { s.Save(); return false }}
	s.m = NewModal(h, ModalOpts{
		Title: nsAtlasUser.T("Edit {sectionLabel}", "sectionLabel", sec.Label), Body: s.layout, Flush: true,
		Buttons: []*ModalButton{{Label: nsEditProfile.T("Cancel"), Kind: BtnOutline}, save},
		FocusStops: func() []event.Tag {
			if s.bump {
				return []event.Tag{&s.keep.click, &s.discard.click}
			}
			return s.stops()
		},
		CloseGuard: func() bool {
			if s.bump {
				s.bump = false
				return false
			}
			if s.Dirty() && !s.saving { // forceSpeedBumpEnabled: dirty
				s.bump = true
				return false
			}
			return true
		},
		Over: func(gtx layout.Context) {
			if !s.bump {
				s.bumpUp = false
				return
			}
			if !s.bumpUp {
				s.bumpUp = true
				s.m.focusTo(gtx, &s.keep.click, true)
			}
			epSpeedBump(gtx, h, s.m, &s.veil, s.keep, s.discard, func() { s.bump = false }, s.m.Close)
		},
	})
	return s
}

func (s *EditSectionModal) Layout(gtx layout.Context) layout.Dimensions { return s.m.Layout(gtx) }
func (s *EditSectionModal) Dismissed() bool                             { return s.m.Dismissed() }
func (s *EditSectionModal) Dismiss()                                    { s.m.Dismiss() }

// Title is the modal's title.
func (s *EditSectionModal) Title() string { return s.m.o.Title }

// SpeedBump reports the discard speed bump is up.
func (s *EditSectionModal) SpeedBump() bool { return s.bump }

// Editable are the drawn elements' labels in order.
func (s *EditSectionModal) Editable() []string {
	var out []string
	for _, e := range s.els {
		if s.drawn(e) {
			out = append(out, e.meta.Label)
		}
	}
	return out
}

// drawn: the kinds Clack edits (people, tags and locations pass through).
func (s *EditSectionModal) drawn(e *esElem) bool {
	switch e.in.Kind {
	case "text":
		return e.meta.Typename == "ProfileTextElement"
	case "date", "link", "long_text":
		return true
	}
	return false
}

// Set types into the element labelled label: v (the text, long text, a
// link's display text, or a date YYYY-MM-DD) and, for a link, uri.
func (s *EditSectionModal) Set(label, v, uri string) {
	for _, e := range s.els {
		if e.meta.Label != label {
			continue
		}
		switch e.in.Kind {
		case "text", "long_text":
			e.ed.SetText(v)
		case "link":
			e.ed.SetText(v)
			e.ed2.SetText(uri)
		case "date":
			e.in.Date = v
		}
		s.sync(e)
	}
}

// Error is the element's error text ("" when valid).
func (s *EditSectionModal) Error(label string) string {
	for _, e := range s.els {
		if e.meta.Label == label {
			if !e.err.OK() {
				return ValidationText(e.err)
			}
			return ValidationText(e.err2)
		}
	}
	return ""
}

// sync reads e's editors into its input and validates it (the yup schema).
func (s *EditSectionModal) sync(e *esElem) {
	switch e.in.Kind {
	case "text":
		e.in.Text, e.in.Null = e.ed.Text(), false
		e.err = core.ValidateSectionText(e.in.Text)
	case "long_text":
		e.in.Text, e.in.Null = e.ed.Text(), false
		e.err = core.ValidateSectionText(e.in.Text)
	case "link":
		e.in.Display, e.in.URI, e.in.Null = e.ed.Text(), e.ed2.Text(), false
		e.err = core.ValidateSectionLinkText(e.in.Display)
		e.err2 = core.ValidateSectionLinkURL(e.in.URI)
	case "date":
		e.in.Null = false
	}
	e.last, e.last2 = e.ed.Text(), e.ed2.Text()
	s.h.Invalidate()
}

// Dirty is formik's dirty: a value differs from the start.
func (s *EditSectionModal) Dirty() bool {
	for _, e := range s.els {
		a, b := e.in, e.start
		if a.Text != b.Text || a.Date != b.Date || a.URI != b.URI || a.Display != b.Display {
			return true
		}
	}
	return false
}

func (s *EditSectionModal) canSave() bool {
	if s.saving {
		return false
	}
	for _, e := range s.els {
		if !e.err.OK() || !e.err2.OK() {
			return false
		}
	}
	return true
}

// Save is the form's onSubmit: the cleaned elements through
// users.profile.setSections, then the profile refetched and the modal
// closed.
func (s *EditSectionModal) Save() {
	a := s.h.Core()
	if a == nil || !s.canSave() {
		return
	}
	var els []core.SectionElementInput
	for _, e := range s.els {
		els = append(els, e.in)
	}
	els = core.CleanSectionInput(s.sec, els)
	s.saving = true
	user, sec := s.user, core.SectionIDOf(s.sec)
	run(s.h, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, a.SetProfileSection(ctx, user, sec, els)
	}, func(_ struct{}, err error) {
		s.saving = false
		if err != nil {
			s.h.Invalidate()
			return
		}
		if s.refetch != nil {
			s.refetch()
		}
		s.m.Close()
	})
}

func (s *EditSectionModal) stops() []event.Tag {
	var out []event.Tag
	for _, e := range s.els {
		if !s.drawn(e) || e.disabled {
			continue
		}
		switch {
		case e.in.Kind == "date" || len(e.meta.Options) > 0:
			out = append(out, &e.pick)
		case e.in.Kind == "link":
			out = append(out, &e.ed, &e.ed2)
		default:
			out = append(out, &e.ed)
		}
	}
	return out
}

func (s *EditSectionModal) events(gtx layout.Context) {
	if !s.focused {
		s.focused = true
		for _, e := range s.els {
			if !s.drawn(e) || e.disabled || (s.focus != "" && e.meta.ID != s.focus) {
				continue
			}
			if s.focus == "" {
				break // Slack focuses only the element the Add link named
			}
			if e.in.Kind == "date" || len(e.meta.Options) > 0 {
				gtx.Execute(key.FocusCmd{Tag: &e.pick})
			} else {
				gtx.Execute(key.FocusCmd{Tag: &e.ed})
			}
			break
		}
	}
	for _, e := range s.els {
		for _, ed := range []*widget.Editor{&e.ed, &e.ed2} {
			for {
				ev, ok := ed.Update(gtx)
				if !ok {
					break
				}
				if _, ok := ev.(widget.ChangeEvent); ok && (e.ed.Text() != e.last || e.ed2.Text() != e.last2) {
					s.sync(e)
				}
			}
		}
	}
}

// layout is .p-edit_profile_section_form_pane__modal-content: 1px lines
// above and below (foreground_low_solid), 20px top padding, each element
// 20px apart, 28px in from the sides.
func (s *EditSectionModal) layout(gtx layout.Context) layout.Dimensions {
	s.events(gtx)
	th := s.h.Theme()
	cl := colorsOf(th)
	w := gtx.Constraints.Max.X
	px := dp(gtx, 28)
	iw := maxInt(1, w-2*px)
	line := pfSectionBorder(th)
	fill(gtx, image.Rect(0, 0, w, maxInt(1, dp(gtx, 1))), line)
	y := dp(gtx, 20)
	for _, e := range s.els {
		if !s.drawn(e) {
			continue
		}
		d := at(gtx, image.Pt(px, y), func(gtx layout.Context) layout.Dimensions {
			return s.drawElem(gtx, th, cl, e, image.Pt(px, y), iw)
		})
		y += d.Size.Y + dp(gtx, 20)
	}
	fill(gtx, image.Rect(0, y, w, y+maxInt(1, dp(gtx, 1))), line)
	return layout.Dimensions{Size: image.Pt(w, y+maxInt(1, dp(gtx, 1)))}
}

func (s *EditSectionModal) label(gtx layout.Context, th *theme.Theme, cl *Colors, e *esElem, label string, w int) int {
	lh := dp(gtx, 22)
	at(gtx, image.Pt(0, 0), func(gtx layout.Context) layout.Dimensions {
		x := 0
		if e.disabled { // the lock (tooltipLockIconText) and the bold label
			at(gtx, image.Pt(0, (lh-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
				return icon(gtx, "lock-filled", 16, cl.Fg2)
			})
			x = dp(gtx, 20)
		}
		return at(gtx, image.Pt(x, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-x), lh)}
			return vcenter(gtx, lh, func(gtx layout.Context) layout.Dimensions {
				return text(gtx, th, 15, theme.Bold, cl.Fg, label)
			})
		})
	})
	return lh + dp(gtx, 8)
}

// drawElem draws one element's form; org is its place in the body.
func (s *EditSectionModal) drawElem(gtx layout.Context, th *theme.Theme, cl *Colors, e *esElem, org image.Point, w int) layout.Dimensions {
	ih := dp(gtx, 44)
	y := 0
	input := func(ed *widget.Editor, placeholder string, err core.ProfileFieldError, h int) {
		ed.ReadOnly = e.disabled
		at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Pt(w, h))
			bg := cl.Bg
			if e.disabled {
				bg = epDisabledBg(th, cl)
			}
			if ed.SingleLine {
				return fieldOn(gtx, th, ed, placeholder, "", float32(h)/gtx.Metric.PxPerDp, 18, bg)
			}
			return esMultiline(gtx, th, cl, ed, placeholder, w, h, bg)
		})
		if !err.OK() {
			stroke(gtx, image.Rect(0, y, w, y+h), dp(gtx, 8), maxInt(1, dp(gtx, 1)), ccErrColor(th, cl))
		}
		y += h
		if !err.OK() {
			y += dp(gtx, 4)
			d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				return epInlineError(gtx, th, cl, ValidationText(err), w)
			})
			y += d.Size.Y
		}
	}
	hint := func() {
		if e.meta.Hint == "" {
			return
		}
		y += dp(gtx, 4)
		d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
			return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, e.meta.Hint, 0)
		})
		y += d.Size.Y
	}
	switch {
	case e.in.Kind == "link":
		// the label as .p-edit_profile_section_form_pane__link--subtitle
		// (15/700, 12 under), then Display Text and URL
		lh := dp(gtx, 22)
		at(gtx, image.Pt(0, 0), func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
			return text(gtx, th, 15, theme.Bold, cl.Fg, e.meta.Label)
		})
		y += lh + dp(gtx, 12)
		sub := func(label string) {
			at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w, lh)}
				return text(gtx, th, 15, theme.Bold, cl.Fg, label)
			})
			y += lh + dp(gtx, 8)
		}
		sub(nsEditProfile.T("Display Text"))
		input(&e.ed, "", e.err, ih)
		y += dp(gtx, 20)
		sub(nsEditProfile.T("URL"))
		input(&e.ed2, "", e.err2, ih)
		hint()
	case e.in.Kind == "date":
		y += s.label(gtx, th, cl, e, e.meta.Label, w)
		s.dateBox(gtx, th, cl, e, org.Add(image.Pt(0, y)), y, w, ih)
		y += ih
		hint()
	case len(e.meta.Options) > 0:
		y += s.label(gtx, th, cl, e, e.meta.Label, w)
		s.optionBox(gtx, th, cl, e, org.Add(image.Pt(0, y)), y, w, ih)
		y += ih
		hint()
	case e.in.Kind == "long_text":
		y += s.label(gtx, th, cl, e, e.meta.Label, w)
		input(&e.ed, e.meta.Hint, e.err, dp(gtx, 88))
		if e.meta.DefaultVisibility == "EXTERNAL_ONLY" && e.meta.ElementKey == "Role Description" {
			y += dp(gtx, 4)
			d := at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Constraints{Max: image.Pt(w, 1<<20)}
				return para(gtx, th, 13, 18, theme.Regular, cl.Fg2, nsEditProfile.T("What should external people know about your role?"), 0)
			})
			y += d.Size.Y
		} else {
			hint()
		}
	default:
		y += s.label(gtx, th, cl, e, e.meta.Label, w)
		input(&e.ed, "", e.err, ih)
		hint()
	}
	return layout.Dimensions{Size: image.Pt(w, y)}
}

// esMultiline is a long text's taller input: the editor from the top-left
// at 15px, the same border and focus ring as the single-line ones.
func esMultiline(gtx layout.Context, th *theme.Theme, cl *Colors, ed *widget.Editor, placeholder string, w, h int, bg color.NRGBA) layout.Dimensions {
	r := image.Rect(0, 0, w, h)
	rad := dp(gtx, 8)
	if gtx.Focused(ed) {
		ring, one := dp(gtx, 5), maxInt(1, dp(gtx, 1))
		rrect(gtx, r.Inset(-ring), rad+ring, cl.FocusRing)
		rrect(gtx, r.Inset(-one), rad+one, cl.Focus)
		rrect(gtx, r, rad, bg)
	} else {
		rrect(gtx, r, rad, cl.InputBorder)
		rrect(gtx, r.Inset(1), rad-1, bg)
	}
	at(gtx, image.Pt(dp(gtx, 12), dp(gtx, 10)), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Min: image.Pt(w-dp(gtx, 24), 0), Max: image.Pt(w-dp(gtx, 24), h-dp(gtx, 20))}
		if ed.Len() == 0 && placeholder != "" {
			call, _ := record(gtx, func(gtx layout.Context) layout.Dimensions {
				return para(gtx, th, 15, 22, theme.Regular, cl.Placeholder, placeholder, 0)
			})
			call.Add(gtx.Ops)
		}
		return th.Editor(gtx, ed, 15, cl.Fg, theme.WithAlpha(cl.Focus, 0x55))
	})
	return layout.Dimensions{Size: r.Max}
}

// selectBox is a large select's box: label (or the placeholder), a caret.
func esSelectBox(gtx layout.Context, th *theme.Theme, cl *Colors, c *widget.Clickable, label string, placeholder bool, w, h int, ring bool) layout.Dimensions {
	r := image.Rect(0, 0, w, h)
	rad := dp(gtx, 8)
	if ring {
		focusRing(gtx, cl, r, rad)
	}
	rrect(gtx, r, rad, dtBorder(th))
	rrect(gtx, r.Inset(1), rad-1, cl.Bg)
	col := cl.Fg
	if placeholder {
		col = cl.Placeholder
	}
	at(gtx, image.Pt(dp(gtx, 12), 0), func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints = layout.Constraints{Max: image.Pt(maxInt(1, w-dp(gtx, 48)), h)}
		return vcenter(gtx, h, func(gtx layout.Context) layout.Dimensions {
			return para(gtx, th, 18, 24, theme.Regular, col, label, 1)
		})
	})
	at(gtx, image.Pt(w-dp(gtx, 28), (h-dp(gtx, 16))/2), func(gtx layout.Context) layout.Dimensions {
		return icon(gtx, "caret-down", 16, cl.Fg2)
	})
	return clickBox(gtx, c, nil, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: r.Max} })
}

// dateBox is EditDateElementForm's DatePickerSelect: the date ("Select
// date" when none) opening the date picker.
func (s *EditSectionModal) dateBox(gtx layout.Context, th *theme.Theme, cl *Colors, e *esElem, pos image.Point, y, w, h int) {
	day, err := time.Parse("2006-01-02", e.in.Date)
	if e.pick.Clicked(gtx) && !e.disabled {
		org := dtModalOrigin(gtx, s.m).Sub(image.Pt(dp(gtx, 28), 0)) // the body is Flush
		r := image.Rectangle{Min: org.Add(pos), Max: org.Add(pos).Add(image.Pt(w, h))}
		sel := day
		if err != nil {
			sel = time.Now()
		}
		s.h.ShowOverlay(NewDatePicker(s.h, r, sel, time.Time{}, func(d time.Time) {
			e.in.Date = d.Format("2006-01-02")
			s.sync(e)
		}))
	}
	label, ph := nsDatePickerNS.T("Select date"), true
	if err == nil {
		label, ph = CalendarDate(day), false
	}
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return esSelectBox(gtx, th, cl, &e.pick, label, ph, w, h, s.m.ringOn(gtx, &e.pick))
	})
}

// optionBox is a text element with options: a select of them.
func (s *EditSectionModal) optionBox(gtx layout.Context, th *theme.Theme, cl *Colors, e *esElem, pos image.Point, y, w, h int) {
	if e.pick.Clicked(gtx) && !e.disabled {
		org := dtModalOrigin(gtx, s.m).Sub(image.Pt(dp(gtx, 28), 0))
		r := image.Rectangle{Min: org.Add(pos), Max: org.Add(pos).Add(image.Pt(w, h))}
		sel := -1
		for i, o := range e.meta.Options {
			if o == e.in.Text {
				sel = i
			}
		}
		s.h.ShowOverlay(NewSelectList(s.h, r, e.meta.Options, sel, SelectListOpts{}, func(i int) {
			if i >= 0 && i < len(e.meta.Options) {
				e.ed.SetText(e.meta.Options[i])
				s.sync(e)
			}
		}))
	}
	label, ph := e.in.Text, false
	if label == "" {
		label, ph = nsEditProfile.T("Please select an option"), true
	}
	at(gtx, image.Pt(0, y), func(gtx layout.Context) layout.Dimensions {
		return esSelectBox(gtx, th, cl, &e.pick, label, ph, w, h, s.m.ringOn(gtx, &e.pick))
	})
}
